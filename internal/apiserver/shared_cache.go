package apiserver

import (
	"context"
	"encoding/json"
	"log"
	"sync/atomic"
	"time"
)

// SharedCache is an optional cache that every API replica reads and writes
// (Redis, see internal/rediscache), so a stat computed by one replica is
// served by all of them. It sits behind each replica's in-process
// resultCache, which keeps its single-flight and compute limits. Nothing
// depends on it: an error or an absent entry just means "compute it here".
type SharedCache interface {
	Get(ctx context.Context, key string) ([]byte, bool, error)
	Set(ctx context.Context, key string, val []byte, ttl time.Duration) error
	// Lock claims key for ttl so replicas don't all compute the same stat at
	// once; it's advisory and expires by itself.
	Lock(ctx context.Context, key string, ttl time.Duration) (bool, error)
	Unlock(ctx context.Context, key string) error
}

const (
	// sharedLockTTL bounds how long a replica's claim on a stat lasts if it
	// dies mid-computation.
	sharedLockTTL = 60 * time.Second
	// sharedWait is how long a replica waits for the one holding the claim
	// before computing the stat itself.
	sharedWait         = 10 * time.Second
	sharedPollInterval = 250 * time.Millisecond
	// localSharedTTL is how long a replica keeps a result in memory when a
	// shared cache is in use: short, so the shared entry's TTL is what sets
	// how often the database is queried and replicas don't lag it by a full
	// extra TTL.
	localSharedTTL = time.Minute
)

// sharedBreaker stops using the shared cache for sharedCooldown after it
// fails, so an unreachable Redis costs one slow request instead of several
// timeouts on every cache miss.
type sharedBreaker struct{ until atomic.Int64 }

const sharedCooldown = 30 * time.Second

func (b *sharedBreaker) open() bool { return time.Now().UnixNano() < b.until.Load() }
func (b *sharedBreaker) trip()      { b.until.Store(time.Now().Add(sharedCooldown).UnixNano()) }

// sharedCacheLog rate-limits cache error logs (one per 30s) so a Redis outage
// doesn't flood the log.
var sharedCacheLog atomic.Int64

func logSharedErr(op string, err error) {
	now := time.Now().Unix()
	if last := sharedCacheLog.Load(); now-last >= 30 && sharedCacheLog.CompareAndSwap(last, now) {
		log.Printf("shared cache %s: %v (computing locally)", op, err)
	}
}

// cached returns key's value from c (this replica's memory) or, on a miss,
// from the shared cache, or computes it. The computed value is written to the
// shared cache for ttl. T must survive a JSON round trip.
//
// Don't call cached on a cache from inside a computation that holds a slot of
// the same cache: with every slot held by outer computations each waiting for
// an inner one, they'd deadlock. Today no computation nests within its own
// cache (Store.Explore computes its baseline itself).
func cached[T any](ctx context.Context, s *Server, c *resultCache, key string, compute func(context.Context) (T, error)) (T, error) {
	v, err := c.get(ctx, key, func(ctx context.Context) (any, error) {
		if s.Shared == nil || s.sharedDown.open() {
			return compute(ctx)
		}
		return sharedFetch(ctx, s.Shared, &s.sharedDown, key, s.statsTTLValue(), compute)
	})
	if err != nil {
		var zero T
		return zero, err
	}
	return v.(T), nil
}

func sharedFetch[T any](ctx context.Context, sc SharedCache, brk *sharedBreaker, key string, ttl time.Duration, compute func(context.Context) (T, error)) (T, error) {
	read := func() (T, bool) {
		var out T
		b, ok, err := sc.Get(ctx, key)
		if err != nil {
			logSharedErr("get", err)
			brk.trip()
			return out, false
		}
		if !ok {
			return out, false
		}
		if err := json.Unmarshal(b, &out); err != nil {
			logSharedErr("decode", err) // e.g. written by another version
			var zero T
			return zero, false
		}
		return out, true
	}
	if out, ok := read(); ok {
		return out, nil
	}
	if brk.open() { // the read just failed: don't wait on lock and write too
		return compute(ctx)
	}

	locked, err := sc.Lock(ctx, key, sharedLockTTL)
	if err != nil {
		logSharedErr("lock", err)
		brk.trip()
	}
	if err == nil && !locked {
		// Another replica is computing it: wait for its result.
		deadline := time.Now().Add(sharedWait)
		for time.Now().Before(deadline) {
			select {
			case <-ctx.Done():
				var zero T
				return zero, ctx.Err()
			case <-time.After(sharedPollInterval):
			}
			if out, ok := read(); ok {
				return out, nil
			}
		}
	}
	if locked {
		defer func() {
			if err := sc.Unlock(context.WithoutCancel(ctx), key); err != nil {
				logSharedErr("unlock", err)
			}
		}()
	}

	out, err := compute(ctx)
	if err != nil {
		var zero T
		return zero, err
	}
	if b, err := json.Marshal(out); err == nil {
		if err := sc.Set(ctx, key, b, ttl); err != nil {
			logSharedErr("set", err)
			brk.trip()
		}
	}
	return out, nil
}
