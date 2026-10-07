package ingest

import (
	"context"
	"errors"
	"fmt"

	"tft-platform/internal/riotapi"
	"tft-platform/internal/store"
)

// CrawlQueueResult aggregates per-PUUID CrawlResults across one bounded run
// over the priority queue.
type CrawlQueueResult struct {
	PUUIDsCrawled   int
	PUUIDsFailed    int // crawls that errored; each backs off (store.MarkCrawlFailed)
	RequestsMade    int
	MatchesIngested int
	Stopped         string
}

// CrawlQueue pulls up to maxPUUIDs from the priority queue (highest
// priority, then longest-since-crawled first — see
// internal/store.NextQueueBatch) and crawls each in turn, capping total
// Riot API requests across the whole run at maxRequests. Each PUUID's match
// writes are already transactional (see StoreMatch), so stopping between —
// or even mid — PUUIDs never leaves partial data; the queue's
// last_crawled_at/last_match_id_seen bookkeeping ensures the next run
// resumes rotating through the rest of the queue rather than re-crawling the
// same players. A player whose crawl fails is backed off from (see
// store.MarkCrawlFailed) and the run carries on with the next one; only an
// expired key or a cancelled context ends it with an error. A rate limit
// stops the run without marking the player it hit.
func CrawlQueue(ctx context.Context, riot *riotapi.Client, st *store.Store, maxPUUIDs, maxIDsPerPUUID, maxRequests int) (CrawlQueueResult, error) {
	var result CrawlQueueResult

	batch, err := st.NextQueueBatch(ctx, maxPUUIDs)
	if err != nil {
		return result, fmt.Errorf("load queue batch: %w", err)
	}

	for _, q := range batch {
		if result.RequestsMade >= maxRequests {
			result.Stopped = "request_budget_exhausted"
			break
		}
		remaining := maxRequests - result.RequestsMade

		r, err := CrawlPUUID(ctx, riot, st, riotapi.RoutingRegion(q.RoutingRegion), q.PUUID, maxIDsPerPUUID, remaining)
		result.RequestsMade += r.RequestsMade
		result.MatchesIngested += r.MatchesIngested
		result.PUUIDsCrawled++

		if err != nil {
			// A key that needs rotating or a cancelled run ends the batch;
			// anything else is this player's problem: back off from them and
			// carry on, so one bad PUUID can't block the whole queue.
			var keyExpired *riotapi.ErrKeyExpired
			if errors.As(err, &keyExpired) || ctx.Err() != nil {
				return result, fmt.Errorf("crawl %s: %w", q.PUUID, err)
			}
			if merr := st.MarkCrawlFailed(ctx, q.PUUID, err.Error()); merr != nil {
				return result, fmt.Errorf("crawl %s: %w (recording the failure: %v)", q.PUUID, err, merr)
			}
			result.PUUIDsFailed++
			continue
		}
		// A rate limit before Riot returned the id list crawled nothing: the
		// player keeps their turn instead of being marked as synced.
		if r.IDsFetched {
			if err := st.MarkCrawled(ctx, q.PUUID, r.MostRecentMatchID); err != nil {
				return result, fmt.Errorf("mark crawled %s: %w", q.PUUID, err)
			}
		}
		if r.Stopped == "rate_limited" {
			result.Stopped = "rate_limited"
			break
		}
	}

	return result, nil
}
