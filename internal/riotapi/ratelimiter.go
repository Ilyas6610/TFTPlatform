package riotapi

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RateLimiter enforces Riot's application-wide and per-method rate limits.
// It is constructed once per process and shared by every call path that
// hits Riot — the ingestion pipeline and the API server's live-fetch
// fallback alike — so the two can never jointly exceed the budget of a
// single (personal) API key.
//
// Riot enforces limits via sliding windows and reports them on every
// response via X-App-Rate-Limit(-Count) and X-Method-Rate-Limit(-Count)
// headers. Those headers are authoritative: after every call, local bucket
// state is reconciled against them so the limiter tightens itself if actual
// usage (e.g. from another process sharing the same key) runs ahead of what
// this process tracked locally.
type RateLimiter struct {
	mu      sync.Mutex
	app     []*window
	methods map[string][]*window
}

type window struct {
	limit        int
	period       time.Duration
	count        int
	windowStart  time.Time
	blockedUntil time.Time
}

func newWindow(limit int, period time.Duration) *window {
	return &window{limit: limit, period: period, windowStart: time.Now()}
}

// NewRateLimiter seeds the application-wide limits (defaults match a
// personal key: ~20 req/sec, ~100 req/2min — see internal/config). Per-method
// limits are not published as stable constants, so they start unconstrained
// and are learned from response headers the first time each method is
// called, then enforced from then on.
func NewRateLimiter(appPerSec, appPer2Min int) *RateLimiter {
	return &RateLimiter{
		app: []*window{
			newWindow(appPerSec, time.Second),
			newWindow(appPer2Min, 2*time.Minute),
		},
		methods: make(map[string][]*window),
	}
}

// Acquire blocks, respecting ctx cancellation, until a request may be sent
// under both the application-wide and methodKey-specific budgets.
func (r *RateLimiter) Acquire(ctx context.Context, methodKey string) error {
	for {
		wait := r.tryAcquire(methodKey)
		if wait <= 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

func (r *RateLimiter) tryAcquire(methodKey string) time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	windows := make([]*window, 0, len(r.app)+len(r.methods[methodKey]))
	windows = append(windows, r.app...)
	windows = append(windows, r.methods[methodKey]...)

	var maxWait time.Duration
	for _, w := range windows {
		if now.Before(w.blockedUntil) {
			if d := w.blockedUntil.Sub(now); d > maxWait {
				maxWait = d
			}
			continue
		}
		if now.Sub(w.windowStart) >= w.period {
			w.windowStart = now
			w.count = 0
		}
		if w.count >= w.limit {
			if d := w.period - now.Sub(w.windowStart); d > maxWait {
				maxWait = d
			}
		}
	}
	if maxWait > 0 {
		return maxWait
	}

	for _, w := range windows {
		w.count++
	}
	return 0
}

// ReportResponse reconciles local bucket state against a successful
// response's rate-limit headers. Call this after every 2xx response.
func (r *RateLimiter) ReportResponse(methodKey string, header http.Header) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.app = reconcile(r.app, header.Get("X-App-Rate-Limit"), header.Get("X-App-Rate-Limit-Count"))

	methodWindows := r.methods[methodKey]
	methodWindows = reconcile(methodWindows, header.Get("X-Method-Rate-Limit"), header.Get("X-Method-Rate-Limit-Count"))
	r.methods[methodKey] = methodWindows
}

func reconcile(windows []*window, limitHeader, countHeader string) []*window {
	if limitHeader == "" || countHeader == "" {
		return windows
	}
	limits := parseRateHeader(limitHeader)
	counts := parseRateHeader(countHeader)

	byPeriod := make(map[int]*window, len(windows))
	for _, w := range windows {
		byPeriod[int(w.period.Seconds())] = w
	}

	now := time.Now()
	for periodSec, limit := range limits {
		w, ok := byPeriod[periodSec]
		if !ok {
			w = newWindow(limit, time.Duration(periodSec)*time.Second)
			w.windowStart = now
			windows = append(windows, w)
			byPeriod[periodSec] = w
		}
		w.limit = limit
		if c, ok := counts[periodSec]; ok && c > w.count {
			// Riot's authoritative count is ahead of local tracking (e.g.
			// another process shares this key) — tighten to match.
			w.count = c
		}
	}
	return windows
}

// parseRateHeader parses Riot's "count:seconds,count:seconds" header format
// into a map of window period (seconds) -> count/limit.
func parseRateHeader(s string) map[int]int {
	out := make(map[int]int)
	for _, part := range strings.Split(s, ",") {
		kv := strings.Split(strings.TrimSpace(part), ":")
		if len(kv) != 2 {
			continue
		}
		n, err1 := strconv.Atoi(kv[0])
		period, err2 := strconv.Atoi(kv[1])
		if err1 != nil || err2 != nil {
			continue
		}
		out[period] = n
	}
	return out
}

// ReportRateLimited marks the appropriate budget (application or method, per
// X-Rate-Limit-Type) as blocked until Retry-After elapses, following a 429
// response. Returns the parsed retry-after duration for the caller to
// surface in an error.
func (r *RateLimiter) ReportRateLimited(methodKey string, header http.Header) time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()

	retryAfter := time.Second
	if v := header.Get("Retry-After"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil {
			retryAfter = time.Duration(secs) * time.Second
		}
	}
	until := time.Now().Add(retryAfter)

	if header.Get("X-Rate-Limit-Type") == "method" {
		for _, w := range r.methods[methodKey] {
			w.blockedUntil = until
		}
	} else {
		// "application", "service", or unspecified: block conservatively at
		// the app level since that's the shared, higher-blast-radius budget.
		for _, w := range r.app {
			w.blockedUntil = until
		}
	}
	return retryAfter
}
