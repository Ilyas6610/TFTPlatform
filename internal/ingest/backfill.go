package ingest

import (
	"context"
	"errors"
	"fmt"
	"time"

	"tft-platform/internal/riotapi"
	"tft-platform/internal/store"
)

// riotIDPageMax is the most match ids Riot returns per request.
const riotIDPageMax = 200

// BackfillProgress reports a whole-set history load.
type BackfillProgress struct {
	Found   int `json:"found"`   // the player's games this set, per Riot (capped)
	Missing int `json:"missing"` // of those, not stored when the load started
	Fetched int `json:"fetched"` // missing games fetched so far
}

// BackfillSet loads a player's whole history since since (the set's start):
// every match id Riot lists from then on, up to maxGames, then each one not
// stored yet. report is called after the id listing and after every game.
// Requests are spaced at least pace apart, so a long load leaves the shared
// key's budget for other callers instead of queueing them behind it. A rate
// limit doesn't end the run: it waits out Riot's Retry-After (the shared
// limiter normally prevents that) and carries on.
func BackfillSet(ctx context.Context, riot *riotapi.Client, st *store.Store, platform riotapi.PlatformRegion, puuid string,
	since time.Time, maxGames int, pace time.Duration, report func(BackfillProgress)) (BackfillProgress, error) {
	var p BackfillProgress
	routing, err := riotapi.RoutingForPlatform(platform)
	if err != nil {
		return p, err
	}
	var last time.Time
	paced := func(fn func() error) error {
		if wait := time.Until(last.Add(pace)); !last.IsZero() && wait > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
			}
		}
		last = time.Now()
		return withRateLimitRetry(ctx, fn)
	}

	var ids []string
	for start := 0; len(ids) < maxGames; start += riotIDPageMax {
		count := min(riotIDPageMax, maxGames-len(ids))
		var page []string
		err := paced(func() error {
			var err error
			page, err = riot.GetTFTMatchIDsSince(ctx, routing, puuid, start, count, since)
			return err
		})
		if err != nil {
			return p, fmt.Errorf("list match ids: %w", err)
		}
		ids = append(ids, page...)
		if len(page) < count {
			break // the history ends here
		}
	}
	existing, err := st.ExistingMatchIDs(ctx, ids)
	if err != nil {
		return p, fmt.Errorf("check existing matches: %w", err)
	}
	var missing []string
	for _, id := range ids {
		if !existing[id] {
			missing = append(missing, id)
		}
	}
	p.Found, p.Missing = len(ids), len(missing)
	report(p)

	for _, id := range missing {
		var match *riotapi.TFTMatch
		var raw []byte
		err := paced(func() error {
			var err error
			match, raw, err = riot.GetTFTMatch(ctx, routing, id)
			return err
		})
		var notFound *riotapi.ErrNotFound
		switch {
		case errors.As(err, &notFound):
			// Gone server-side: count it as handled.
		case err != nil:
			return p, fmt.Errorf("fetch match %s: %w", id, err)
		default:
			if _, err := storeMatch(ctx, st, routing, id, match, raw); err != nil {
				return p, fmt.Errorf("store match %s: %w", id, err)
			}
		}
		p.Fetched++
		report(p)
	}
	return p, nil
}

// withRateLimitRetry runs fn, waiting out Riot's Retry-After and retrying
// (up to 5 times) when it's rate limited.
func withRateLimitRetry(ctx context.Context, fn func() error) error {
	for attempt := 0; ; attempt++ {
		err := fn()
		var limited *riotapi.ErrRateLimited
		if !errors.As(err, &limited) || attempt >= 5 {
			return err
		}
		wait := max(limited.RetryAfter, time.Second)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}
