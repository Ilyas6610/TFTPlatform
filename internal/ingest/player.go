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
// picked up by the ingestion pipeline — but only once Riot has accepted the
// PUUID, so made-up ids from requests are never stored or queued.
func SyncPlayerMatches(ctx context.Context, riot *riotapi.Client, st *store.Store, platform riotapi.PlatformRegion, puuid string, maxIDs int) (CrawlResult, error) {
	routing, err := riotapi.RoutingForPlatform(platform)
	if err != nil {
		return CrawlResult{}, err
	}

	// One request for the id list plus one per match: enough to fetch every
	// new match in the window.
	result, crawlErr := CrawlPUUID(ctx, riot, st, routing, puuid, maxIDs, maxIDs+1)
	if !result.IDsFetched {
		return result, crawlErr
	}
	if err := st.UpsertAccountPUUIDOnly(ctx, puuid, string(routing)); err != nil {
		return result, fmt.Errorf("upsert account: %w", err)
	}
	if err := st.EnqueuePUUID(ctx, puuid, string(platform), string(routing), 0); err != nil {
		return result, fmt.Errorf("enqueue: %w", err)
	}
	if crawlErr != nil || result.Stopped != "" {
		return result, crawlErr
	}
	if err := st.MarkCrawled(ctx, puuid, result.MostRecentMatchID); err != nil {
		return result, fmt.Errorf("mark crawled: %w", err)
	}
	return result, nil
}

// SyncOlderMatches fetches one older page of a player's history: the count
// match ids after the start most recent, and any of those matches not yet
// stored (one request for the ids plus one per new match). It doesn't touch
// the sync time, which tracks the newest page. The player must already be
// stored (a newest-page sync confirmed the PUUID).
func SyncOlderMatches(ctx context.Context, riot *riotapi.Client, st *store.Store, platform riotapi.PlatformRegion, puuid string, start, count int) (CrawlResult, error) {
	routing, err := riotapi.RoutingForPlatform(platform)
	if err != nil {
		return CrawlResult{}, err
	}
	return crawlPage(ctx, riot, st, routing, puuid, start, count, count+1)
}

// SyncPlayerRank records a player's current rank in each ranked queue (one
// request) as rank snapshots, unchanged standings excepted. Taken right
// after a match history sync, consecutive snapshots bracket the games in
// between, which is how per-game LP is worked out (internal/lp). It returns
// how many snapshots were added.
func SyncPlayerRank(ctx context.Context, riot *riotapi.Client, st *store.Store, platform riotapi.PlatformRegion, puuid string) (int, error) {
	entries, err := riot.GetRankedEntries(ctx, platform, puuid)
	if err != nil {
		return 0, fmt.Errorf("fetch rank: %w", err)
	}
	added := 0
	for _, e := range entries {
		ok, err := st.AddRankSnapshot(ctx, puuid, store.RankSnapshot{
			QueueType: e.QueueType, Tier: e.Tier, Rank: e.Rank,
			LeaguePoints: e.LeaguePoints, Wins: e.Wins, Losses: e.Losses,
		})
		if err != nil {
			return added, fmt.Errorf("store rank: %w", err)
		}
		if ok {
			added++
		}
	}
	return added, nil
}
