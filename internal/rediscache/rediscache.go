// Package rediscache is a small Redis-backed key/value cache that API server
// replicas share, so a stat computed by one is served by all of them. It
// holds opaque bytes with a TTL; callers decide what to store.
package rediscache

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// opTimeout bounds each Redis round trip: the cache is an optimisation, so a
// slow Redis must degrade to a cache miss rather than slow requests down.
const opTimeout = time.Second

// keyPrefix namespaces keys; bump the version when a cached value's shape
// changes so old entries from a previous deploy are never decoded.
const keyPrefix = "tft:v1:"

type Cache struct {
	rdb *redis.Client
}

// New parses a redis:// or rediss:// URL. It doesn't connect: the client
// reconnects on its own, and Ping reports whether Redis is reachable now.
func New(url string) (*Cache, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("parse REDIS_URL: %w", err)
	}
	opts.DialTimeout = opTimeout
	opts.ReadTimeout = opTimeout
	opts.WriteTimeout = opTimeout
	return &Cache{rdb: redis.NewClient(opts)}, nil
}

func (c *Cache) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	return c.rdb.Ping(ctx).Err()
}

func (c *Cache) Close() error { return c.rdb.Close() }

// Get returns the stored bytes, or ok=false when the key is absent.
func (c *Cache) Get(ctx context.Context, key string) ([]byte, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	b, err := c.rdb.Get(ctx, keyPrefix+key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return b, true, nil
}

func (c *Cache) Set(ctx context.Context, key string, val []byte, ttl time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	return c.rdb.Set(ctx, keyPrefix+key, val, ttl).Err()
}

// Lock tries to claim key for ttl; it reports whether this caller got it.
// It's advisory: it only keeps replicas from computing the same stat at
// once, and expires by itself if the holder dies.
func (c *Cache) Lock(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	return c.rdb.SetNX(ctx, keyPrefix+"lock:"+key, 1, ttl).Result()
}

func (c *Cache) Unlock(ctx context.Context, key string) error {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	return c.rdb.Del(ctx, keyPrefix+"lock:"+key).Err()
}
