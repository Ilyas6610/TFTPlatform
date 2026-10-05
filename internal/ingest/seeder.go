package ingest

import (
	"context"
	"fmt"

	"tft-platform/internal/riotapi"
	"tft-platform/internal/store"
)

// Queue priorities: apex tiers get crawled first for match ingestion since
// they carry the highest information density for meta stats. Organically
// discovered participants (see crawler.go) default to 0.
const (
	PriorityChallenger  int16 = 30
	PriorityGrandmaster int16 = 20
	PriorityMaster      int16 = 10
)

type SeedResult struct {
	RequestsMade  int
	PlayersSeeded int
	// Stopped is set (not an error) if a rate limit was hit partway through
	// the three tier fetches; tiers already seeded are kept.
	Stopped string
}

// SeedLeaderboard pulls challenger/grandmaster/master from tft/league-v1 for
// platform, upserts accounts + league_entries snapshots, and queues each
// PUUID for match crawling at a priority reflecting their tier. This is a
// small, bounded call — at most 3 Riot API requests — safe to re-run daily
// even under a personal key.
func SeedLeaderboard(ctx context.Context, riot *riotapi.Client, st *store.Store, platform riotapi.PlatformRegion) (SeedResult, error) {
	routing, err := riotapi.RoutingForPlatform(platform)
	if err != nil {
		return SeedResult{}, err
	}

	var result SeedResult

	tiers := []struct {
		name     string
		priority int16
		fetch    func(context.Context, riotapi.PlatformRegion) (*riotapi.LeagueList, error)
	}{
		{"CHALLENGER", PriorityChallenger, riot.GetChallenger},
		{"GRANDMASTER", PriorityGrandmaster, riot.GetGrandmaster},
		{"MASTER", PriorityMaster, riot.GetMaster},
	}

	for _, t := range tiers {
		list, err := t.fetch(ctx, platform)
		result.RequestsMade++
		if err != nil {
			if _, ok := err.(*riotapi.ErrRateLimited); ok {
				result.Stopped = "rate_limited"
				return result, nil
			}
			return result, fmt.Errorf("fetch %s: %w", t.name, err)
		}

		for _, entry := range list.Entries {
			if entry.PUUID == "" {
				continue // defensive: skip any entry Riot didn't attach a puuid to
			}
			if err := st.UpsertAccountPUUIDOnly(ctx, entry.PUUID, string(routing)); err != nil {
				return result, fmt.Errorf("upsert account %s: %w", entry.PUUID, err)
			}
			if err := st.UpsertLeagueEntry(ctx, store.LeagueEntry{
				PUUID:          entry.PUUID,
				PlatformRegion: string(platform),
				Tier:           t.name,
				Rank:           entry.Rank,
				LeaguePoints:   entry.LeaguePoints,
				Wins:           entry.Wins,
				Losses:         entry.Losses,
				HotStreak:      entry.HotStreak,
			}); err != nil {
				return result, fmt.Errorf("upsert league entry %s: %w", entry.PUUID, err)
			}
			if err := st.EnqueuePUUID(ctx, entry.PUUID, string(platform), string(routing), t.priority); err != nil {
				return result, fmt.Errorf("enqueue %s: %w", entry.PUUID, err)
			}
			result.PlayersSeeded++
		}
	}

	return result, nil
}
