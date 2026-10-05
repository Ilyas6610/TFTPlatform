package ingest

import (
	"context"
	"errors"
	"fmt"

	"tft-platform/internal/riotapi"
	"tft-platform/internal/store"
)

type ResolveResult struct {
	RequestsMade int
	Resolved     int
	// Stopped is set (not an error) if the run ended early on a rate limit
	// or dead key; names already resolved are kept.
	Stopped string
}

// ResolveNames backfills Riot IDs for PUUIDs that were discovered without
// one, via account-v1 (one request per PUUID). PUUIDs Riot no longer knows
// are skipped.
func ResolveNames(ctx context.Context, riot *riotapi.Client, st *store.Store, platform riotapi.PlatformRegion, puuids []string) (ResolveResult, error) {
	routing, err := riotapi.AccountRoutingForPlatform(platform)
	if err != nil {
		return ResolveResult{}, err
	}

	var result ResolveResult
	for _, puuid := range puuids {
		account, err := riot.GetAccountByPUUID(ctx, routing, puuid)
		result.RequestsMade++
		var notFound *riotapi.ErrNotFound
		var rateLimited *riotapi.ErrRateLimited
		var keyExpired *riotapi.ErrKeyExpired
		switch {
		case errors.As(err, &notFound):
			continue
		case errors.As(err, &rateLimited):
			result.Stopped = "rate_limited"
			return result, nil
		case errors.As(err, &keyExpired):
			result.Stopped = "key_expired"
			return result, nil
		case err != nil:
			return result, fmt.Errorf("resolve %s: %w", puuid, err)
		}
		if account.GameName == "" {
			continue
		}
		if err := st.SetAccountRiotID(ctx, puuid, account.GameName, account.TagLine); err != nil {
			return result, fmt.Errorf("store riot id for %s: %w", puuid, err)
		}
		result.Resolved++
	}
	return result, nil
}
