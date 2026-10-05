package apiserver

import (
	"context"
	"sync"
	"time"
)

const (
	// resultCacheTTL: cached results (meta builds and comps, set data and
	// patch notes) are expensive to compute and change slowly — new
	// matches arrive in crawl batches minutes apart, set data every few
	// hours — so they're reused for a while instead of recomputed per
	// request.
	resultCacheTTL = 5 * time.Minute
	// resultCacheMaxEntries bounds memory: keys come from request
	// parameters (set, queue combinations, levels, versions), so callers
	// could otherwise create entries without limit.
	resultCacheMaxEntries = 64
	// resultCacheMaxComputes caps cache misses computed at once per cache.
	// Each is a heavy query, and callers choose the key, so without a cap
	// a client cycling through scopes could run many in parallel.
	resultCacheMaxComputes = 2
)

// resultCache memoizes expensive results by key for resultCacheTTL.
// Concurrent misses for one key share a single computation. Errors are not
// cached. The zero value is ready to use.
type resultCache struct {
	mu      sync.Mutex
	entries map[string]*resultCacheEntry
	slots   chan struct{}    // computations in flight
	now     func() time.Time // tests only
}

type resultCacheEntry struct {
	ready chan struct{} // closed once val/err are set
	val   any
	err   error
	at    time.Time
}

func (c *resultCache) get(ctx context.Context, key string, compute func(context.Context) (any, error)) (any, error) {
	now := time.Now
	if c.now != nil {
		now = c.now
	}

	c.mu.Lock()
	if c.entries == nil {
		c.entries = map[string]*resultCacheEntry{}
		c.slots = make(chan struct{}, resultCacheMaxComputes)
	}
	e := c.entries[key]
	if e != nil {
		select {
		case <-e.ready:
			if e.err != nil || now().Sub(e.at) >= resultCacheTTL {
				e = nil // failed or expired: recompute
			}
		default: // in flight: wait for it below
		}
	}
	if e == nil {
		c.evictLocked(now())
		e = &resultCacheEntry{ready: make(chan struct{})}
		c.entries[key] = e
		c.mu.Unlock()

		// Wait for a computation slot (giving up if the caller leaves),
		// then compute detached from the request: a caller hanging up
		// mustn't fail a computation that other waiters share.
		select {
		case c.slots <- struct{}{}:
			e.val, e.err = compute(context.WithoutCancel(ctx))
			<-c.slots
		case <-ctx.Done():
			e.err = ctx.Err()
		}
		e.at = now()
		if e.err != nil {
			c.mu.Lock()
			if c.entries[key] == e {
				delete(c.entries, key)
			}
			c.mu.Unlock()
		}
		close(e.ready)
		return e.val, e.err
	}
	c.mu.Unlock()

	select {
	case <-e.ready:
		return e.val, e.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// evictLocked makes room for one more entry: it drops expired entries, and
// if still full, the oldest finished one.
func (c *resultCache) evictLocked(now time.Time) {
	if len(c.entries) < resultCacheMaxEntries {
		return
	}
	var oldestKey string
	var oldest time.Time
	for k, e := range c.entries {
		select {
		case <-e.ready:
		default:
			continue // in flight
		}
		if now.Sub(e.at) >= resultCacheTTL {
			delete(c.entries, k)
			continue
		}
		if oldestKey == "" || e.at.Before(oldest) {
			oldestKey, oldest = k, e.at
		}
	}
	if len(c.entries) >= resultCacheMaxEntries && oldestKey != "" {
		delete(c.entries, oldestKey)
	}
}
