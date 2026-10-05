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
	// the three tier fetches; tiers already fetched are still written.
	Stopped string
}

// SeedLeaderboard pulls challenger/grandmaster/master from tft/league-v1 for
// platform, writes them as platform's leaderboard snapshot (accounts +
// league_entries), and queues each PUUID for match crawling at a priority
// reflecting their tier. This is a small, bounded call — at most 3 Riot API
// requests — safe to re-run often even under a personal key. When all three
// tiers are fetched, players no longer on the apex ladder are dropped from
// the snapshot.
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

	var entries []store.SnapshotEntry
	for _, t := range tiers {
		list, err := t.fetch(ctx, platform)
		result.RequestsMade++
		if err != nil {
			if _, ok := err.(*riotapi.ErrRateLimited); ok {
				result.Stopped = "rate_limited"
				break
			}
			return result, fmt.Errorf("fetch %s: %w", t.name, err)
		}

		for _, entry := range list.Entries {
			if entry.PUUID == "" {
				continue // defensive: skip any entry Riot didn't attach a puuid to
			}
			entries = append(entries, store.SnapshotEntry{
				LeagueEntry: store.LeagueEntry{
					PUUID:          entry.PUUID,
					PlatformRegion: string(platform),
					Tier:           t.name,
					Rank:           entry.Rank,
					LeaguePoints:   entry.LeaguePoints,
					Wins:           entry.Wins,
					Losses:         entry.Losses,
					HotStreak:      entry.HotStreak,
				},
				RoutingRegion: string(routing),
				Priority:      t.priority,
			})
		}
	}

	if err := st.WriteLeagueSnapshot(ctx, string(platform), entries, result.Stopped == ""); err != nil {
		return result, fmt.Errorf("write leaderboard snapshot: %w", err)
	}
	result.PlayersSeeded = len(entries)
	return result, nil
}
