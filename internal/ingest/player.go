package ingest

import (
	"context"
	"fmt"

	"tft-platform/internal/riotapi"
	"tft-platform/internal/store"
)

// SyncPlayerMatches brings one player's recent match history up to date: it
// crawls their last maxIDs matches (see CrawlPUUID), and on a complete pass
// records the sync time (store.MatchHistorySyncedAt). The player is added to
// the crawl queue if they weren't already, so viewed profiles also get
// picked up by the ingestion pipeline.
func SyncPlayerMatches(ctx context.Context, riot *riotapi.Client, st *store.Store, platform riotapi.PlatformRegion, puuid string, maxIDs int) (CrawlResult, error) {
	routing, err := riotapi.RoutingForPlatform(platform)
	if err != nil {
		return CrawlResult{}, err
	}
	if err := st.UpsertAccountPUUIDOnly(ctx, puuid, string(routing)); err != nil {
		return CrawlResult{}, fmt.Errorf("upsert account: %w", err)
	}
	if err := st.EnqueuePUUID(ctx, puuid, string(platform), string(routing), 0); err != nil {
		return CrawlResult{}, fmt.Errorf("enqueue: %w", err)
	}

	// One request for the id list plus one per match: enough to fetch every
	// new match in the window.
	result, err := CrawlPUUID(ctx, riot, st, routing, puuid, maxIDs, maxIDs+1)
	if err != nil || result.Stopped != "" {
		return result, err
	}
	if err := st.MarkCrawled(ctx, puuid, result.MostRecentMatchID); err != nil {
		return result, fmt.Errorf("mark crawled: %w", err)
	}
	return result, nil
}
