package ingest

import (
	"context"
	"fmt"

	"tft-platform/internal/riotapi"
	"tft-platform/internal/store"
)

// CrawlQueueResult aggregates per-PUUID CrawlResults across one bounded run
// over the priority queue.
type CrawlQueueResult struct {
	PUUIDsCrawled   int
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
// same players.
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
			return result, fmt.Errorf("crawl %s: %w", q.PUUID, err)
		}
		if err := st.MarkCrawled(ctx, q.PUUID, r.MostRecentMatchID); err != nil {
			return result, fmt.Errorf("mark crawled %s: %w", q.PUUID, err)
		}
		if r.Stopped == "rate_limited" {
			result.Stopped = "rate_limited"
			break
		}
	}

	return result, nil
}
