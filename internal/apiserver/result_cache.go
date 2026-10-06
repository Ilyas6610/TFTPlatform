package apiserver

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

const (
	// resultCacheTTL: the default lifetime of cached results (set data and
	// patch notes) — expensive to compute and slow to change, so they're
	// reused for a while instead of recomputed per request. Stats computed
	// from match data use Server.StatsCacheTTL instead.
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
	ttl     time.Duration    // how long a result is reused; 0 = resultCacheTTL
}

func (c *resultCache) lifetime() time.Duration {
	if c.ttl > 0 {
		return c.ttl
	}
	return resultCacheTTL
}

type resultCacheEntry struct {
	ready chan struct{} // closed once val/err are set
	val   any
	err   error
	at    time.Time
	// abandoned is set when the caller that created the entry left before
	// a computation slot freed up: nothing was computed, so waiters retry
	// rather than inherit that caller's cancellation.
	abandoned bool
}

func (c *resultCache) get(ctx context.Context, key string, compute func(context.Context) (any, error)) (any, error) {
	now := time.Now
	if c.now != nil {
		now = c.now
	}

	for {
		c.mu.Lock()
		if c.entries == nil {
			c.entries = map[string]*resultCacheEntry{}
			c.slots = make(chan struct{}, resultCacheMaxComputes)
		}
		e := c.entries[key]
		if e != nil {
			select {
			case <-e.ready:
				if e.err != nil || now().Sub(e.at) >= c.lifetime() {
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
			return c.compute(ctx, key, e, compute, now)
		}
		c.mu.Unlock()

		select {
		case <-e.ready:
			if e.abandoned {
				continue // its creator left before computing: try again
			}
			return e.val, e.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// compute fills in e, the entry this caller created for key, and closes
// e.ready whatever happens, so waiters are never left blocked.
func (c *resultCache) compute(ctx context.Context, key string, e *resultCacheEntry, compute func(context.Context) (any, error), now func() time.Time) (any, error) {
	defer close(e.ready)
	drop := func() {
		c.mu.Lock()
		if c.entries[key] == e {
			delete(c.entries, key)
		}
		c.mu.Unlock()
	}

	// Wait for a computation slot, giving up if the caller leaves.
	select {
	case c.slots <- struct{}{}:
	case <-ctx.Done():
		e.abandoned = true
		drop()
		return nil, ctx.Err()
	}

	// Detached from the request: a caller hanging up mustn't fail a
	// computation that other waiters share. A panic is turned into an error
	// so the slot is released and waiters are answered.
	func() {
		defer func() {
			<-c.slots
			if r := recover(); r != nil {
				log.Printf("result cache %s: compute panicked: %v", key, r)
				e.val, e.err = nil, fmt.Errorf("compute %s panicked: %v", key, r)
			}
		}()
		e.val, e.err = compute(context.WithoutCancel(ctx))
	}()
	e.at = now()
	if e.err != nil {
		drop()
	}
	return e.val, e.err
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
		if now.Sub(e.at) >= c.lifetime() {
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
