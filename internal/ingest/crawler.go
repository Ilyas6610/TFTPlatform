// Package ingest implements bounded, resumable ingestion of TFT match data.
// Every entrypoint here is designed to run as a small, budget-capped batch
// (see internal/riotapi.RateLimiter) so it stays safe under a personal Riot
// API key, and to stop cleanly and resumably rather than crash when it hits
// a rate limit.
package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"tft-platform/internal/riotapi"
	"tft-platform/internal/store"
)

// CrawlResult summarizes one bounded crawl invocation.
type CrawlResult struct {
	RequestsMade    int
	MatchesIngested int
	// Stopped is empty on a clean full pass, or one of "rate_limited" /
	// "request_budget_exhausted" when the crawl stopped early — both are
	// expected, non-error outcomes: the next run resumes correctly because
	// dedupe is based on what's already in the matches table.
	Stopped string
	// MostRecentMatchID is Riot's most recent match ID for this PUUID at the
	// time of the ids fetch (empty if the fetch itself was rate-limited
	// before returning), used to update ingest_puuid_queue.last_match_id_seen.
	MostRecentMatchID string
	// IDsFetched is set once Riot returned the match id list, which also
	// confirms the PUUID exists; IDs is how many it returned (fewer than
	// asked means the history ends there).
	IDsFetched bool
	IDs        int
}

// CrawlPUUID fetches up to maxIDs recent match IDs for puuid, dedupes
// against already-ingested matches, and fetches+stores each new match (one
// DB transaction per match, so a crash or rate-limit stop mid-crawl never
// leaves a match half-written). It stops cleanly — not as an error — on
// ErrRateLimited or once maxRequests is spent; ErrKeyExpired propagates as a
// real error since that needs operator attention (key rotation).
func CrawlPUUID(ctx context.Context, riot *riotapi.Client, st *store.Store, routing riotapi.RoutingRegion, puuid string, maxIDs, maxRequests int) (CrawlResult, error) {
	return crawlPage(ctx, riot, st, routing, puuid, 0, maxIDs, maxRequests)
}

// crawlPage is CrawlPUUID for the page of ids after the start most recent.
func crawlPage(ctx context.Context, riot *riotapi.Client, st *store.Store, routing riotapi.RoutingRegion, puuid string, start, maxIDs, maxRequests int) (CrawlResult, error) {
	var result CrawlResult

	ids, err := riot.GetTFTMatchIDsPage(ctx, routing, puuid, start, maxIDs)
	result.RequestsMade++
	if err != nil {
		if _, ok := err.(*riotapi.ErrRateLimited); ok {
			result.Stopped = "rate_limited"
			return result, nil
		}
		return result, fmt.Errorf("fetch match ids for %s: %w", puuid, err)
	}
	result.IDsFetched = true
	result.IDs = len(ids)
	if len(ids) > 0 {
		result.MostRecentMatchID = ids[0]
	}

	existing, err := st.ExistingMatchIDs(ctx, ids)
	if err != nil {
		return result, fmt.Errorf("check existing matches: %w", err)
	}

	for _, matchID := range ids {
		if existing[matchID] {
			continue
		}
		if result.RequestsMade >= maxRequests {
			result.Stopped = "request_budget_exhausted"
			return result, nil
		}
		// Re-check right before spending a Riot request: another process
		// (or an earlier player's match list in this run) may have stored
		// it since the batch lookup above.
		if now, err := st.ExistingMatchIDs(ctx, []string{matchID}); err != nil {
			return result, fmt.Errorf("check existing match %s: %w", matchID, err)
		} else if now[matchID] {
			continue
		}

		match, raw, err := riot.GetTFTMatch(ctx, routing, matchID)
		result.RequestsMade++
		if err != nil {
			if _, ok := err.(*riotapi.ErrRateLimited); ok {
				result.Stopped = "rate_limited"
				return result, nil
			}
			if _, ok := err.(*riotapi.ErrNotFound); ok {
				continue // match unavailable server-side; skip, don't fail the whole run
			}
			return result, fmt.Errorf("fetch match %s: %w", matchID, err)
		}

		inserted, err := storeMatch(ctx, st, routing, matchID, match, raw)
		if err != nil {
			return result, fmt.Errorf("store match %s: %w", matchID, err)
		}
		if inserted { // false: another process stored it while we fetched
			result.MatchesIngested++
		}
	}

	return result, nil
}

// StoreMatch persists a fully-fetched match and its participants. Exported
// so both the crawler above and the API server's match-detail live-fallback
// path (internal/apiserver/handlers_match.go) share the same write logic —
// including organic PUUID discovery — instead of duplicating it.
func StoreMatch(ctx context.Context, st *store.Store, routing riotapi.RoutingRegion, matchID string, match *riotapi.TFTMatch, raw []byte) error {
	_, err := storeMatch(ctx, st, routing, matchID, match, raw)
	return err
}

// storeMatch is StoreMatch that also reports whether the match was new.
func storeMatch(ctx context.Context, st *store.Store, routing riotapi.RoutingRegion, matchID string, match *riotapi.TFTMatch, raw []byte) (bool, error) {
	participants := make(map[string]store.MatchParticipant, len(match.Info.Participants))

	for _, p := range match.Info.Participants {
		// Register every participant's PUUID, not just the one we were
		// crawling for — this is how coverage grows organically beyond the
		// explicitly tracked/seeded players over time.
		if err := st.UpsertAccountPUUIDOnly(ctx, p.PUUID, string(routing)); err != nil {
			return false, fmt.Errorf("upsert participant account %s: %w", p.PUUID, err)
		}

		rawParticipant, err := json.Marshal(p)
		if err != nil {
			return false, fmt.Errorf("marshal participant %s: %w", p.PUUID, err)
		}

		participants[p.PUUID] = store.MatchParticipant{
			PUUID:                p.PUUID,
			Placement:            p.Placement,
			Level:                p.Level,
			LastRound:            p.LastRound,
			PlayersEliminated:    p.PlayersEliminated,
			TotalDamageToPlayers: p.TotalDamageToPlayers,
			GoldLeft:             p.GoldLeft,
			TimeEliminated:       p.TimeEliminated,
			Units:                p.Units,
			Traits:               p.Traits,
			Augments:             p.Augments,
			Companion:            p.Companion,
			RawParticipant:       rawParticipant,
		}
	}

	m := store.Match{
		MatchID:       matchID,
		RoutingRegion: string(routing),
		GameDatetime:  time.UnixMilli(match.Info.GameDatetime),
		GameLength:    match.Info.GameLength,
		GameVersion:   match.Info.GameVersion,
		TFTSetNumber:  match.Info.TFTSetNumber,
		QueueID:       match.Info.QueueID,
		TFTGameType:   match.Info.TFTGameType,
		RawPayload:    raw,
	}

	return st.InsertMatchIfNew(ctx, m, participants)
}
