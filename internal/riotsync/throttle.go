package riotsync

import (
	"net/http"
	"sync"

	"tft-platform/internal/riotapi"
)

// Throttle is an http.RoundTripper wrapper that caps how many requests
// riotsync sends, over a short and a long sliding window: riotsync's share
// of the key (a riotapi.Budget, sized from RIOT_BUDGET_SPLIT). The Riot
// client's own RateLimiter syncs its limits to the key's full budget (from
// Riot's response headers), so it can't reserve any of it for the API
// server; this cap does, because riotsync and the API server share one key.
type Throttle struct {
	Base            http.RoundTripper
	PerSec, Per2Min int

	once   sync.Once
	budget *riotapi.Budget
}

func (t *Throttle) RoundTrip(r *http.Request) (*http.Response, error) {
	t.once.Do(func() { t.budget = riotapi.NewBudget(t.PerSec, t.Per2Min) })
	if err := t.budget.Acquire(r.Context(), 0); err != nil {
		return nil, err
	}
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(r)
}
