package apiserver

import (
	"context"
	"sync"
	"time"
)

const (
	// metaCacheTTL: meta builds and comps scan every board in scope, so
	// results are reused for a while instead of recomputed per request.
	// New matches arrive in crawl batches minutes apart anyway.
	metaCacheTTL = 5 * time.Minute
	// metaCacheMaxEntries bounds memory: keys come from request parameters
	// (set, queue combinations, levels), so callers could otherwise create
	// entries without limit.
	metaCacheMaxEntries = 64
)

// metaCache memoizes expensive meta results by key for metaCacheTTL.
// Concurrent misses for one key share a single computation. Errors are not
// cached. The zero value is ready to use.
type metaCache struct {
	mu      sync.Mutex
	entries map[string]*metaCacheEntry
	now     func() time.Time // tests only
}

type metaCacheEntry struct {
	ready chan struct{} // closed once val/err are set
	val   any
	err   error
	at    time.Time
}

func (c *metaCache) get(ctx context.Context, key string, compute func(context.Context) (any, error)) (any, error) {
	now := time.Now
	if c.now != nil {
		now = c.now
	}

	c.mu.Lock()
	if c.entries == nil {
		c.entries = map[string]*metaCacheEntry{}
	}
	e := c.entries[key]
	if e != nil {
		select {
		case <-e.ready:
			if e.err != nil || now().Sub(e.at) >= metaCacheTTL {
				e = nil // failed or expired: recompute
			}
		default: // in flight: wait for it below
		}
	}
	if e == nil {
		c.evictLocked(now())
		e = &metaCacheEntry{ready: make(chan struct{})}
		c.entries[key] = e
		c.mu.Unlock()

		// Detached from the request: a caller hanging up mustn't fail the
		// computation that other waiters share.
		e.val, e.err = compute(context.WithoutCancel(ctx))
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
func (c *metaCache) evictLocked(now time.Time) {
	if len(c.entries) < metaCacheMaxEntries {
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
		if now.Sub(e.at) >= metaCacheTTL {
			delete(c.entries, k)
			continue
		}
		if oldestKey == "" || e.at.Before(oldest) {
			oldestKey, oldest = k, e.at
		}
	}
	if len(c.entries) >= metaCacheMaxEntries && oldestKey != "" {
		delete(c.entries, oldestKey)
	}
}
