// Package riotsync is the background service that pulls data from the Riot
// API and writes it to Postgres: leaderboard seeding, match crawling for the
// queued players, Riot ID resolution and meta-stat aggregation. It reuses
// internal/ingest and internal/aggregate and runs them on a schedule, so the
// HTTP API never has to talk to Riot to keep the database fresh.
package riotsync

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"tft-platform/internal/riotapi"
)

// Config is the service's own tuning, read from RIOTSYNC_* env vars. The
// database URL and Riot key come from internal/config as for every binary.
type Config struct {
	Platforms []riotapi.PlatformRegion

	SeedInterval      time.Duration // leaderboard re-seed, per platform
	CrawlInterval     time.Duration // one bounded crawl-queue batch
	NamesInterval     time.Duration // Riot ID backfill, per platform
	AggregateInterval time.Duration // meta stats recompute (no Riot calls)

	// RateLimitPerSec / RateLimitPer2Min cap riotsync's own Riot traffic,
	// below the key's limits (20/s, 100/2min on a personal key) so the API
	// server's live fetches, which share the key, always have headroom.
	RateLimitPerSec  int
	RateLimitPer2Min int

	CrawlPUUIDs      int // queued players per crawl batch
	CrawlIDsPerPUUID int // recent match ids per player
	CrawlRequests    int // Riot request cap per crawl batch
	NamesBatch       int // names resolved per run

	// KeyRetry is how long everything pauses after Riot rejects the key
	// (expired personal keys); the key file is re-read on the next try.
	KeyRetry time.Duration
}

func LoadConfig() (Config, error) {
	var e envReader
	cfg := Config{
		SeedInterval:      e.duration("RIOTSYNC_SEED_INTERVAL", 6*time.Hour),
		CrawlInterval:     e.duration("RIOTSYNC_CRAWL_INTERVAL", 5*time.Minute),
		NamesInterval:     e.duration("RIOTSYNC_NAMES_INTERVAL", 10*time.Minute),
		AggregateInterval: e.duration("RIOTSYNC_AGGREGATE_INTERVAL", time.Hour),
		RateLimitPerSec:   e.int("RIOTSYNC_RATE_LIMIT_PER_SEC", 10),
		RateLimitPer2Min:  e.int("RIOTSYNC_RATE_LIMIT_PER_2MIN", 50),
		CrawlPUUIDs:       e.int("RIOTSYNC_CRAWL_PUUIDS", 10),
		CrawlIDsPerPUUID:  e.int("RIOTSYNC_CRAWL_IDS_PER_PUUID", 20),
		CrawlRequests:     e.int("RIOTSYNC_CRAWL_REQUESTS", 40),
		NamesBatch:        e.int("RIOTSYNC_NAMES_BATCH", 50),
		KeyRetry:          e.duration("RIOTSYNC_KEY_RETRY", 5*time.Minute),
	}
	if err := e.err(); err != nil {
		return Config{}, err
	}
	for _, p := range strings.Fields(strings.ReplaceAll(envString("RIOTSYNC_PLATFORMS", "na1"), ",", " ")) {
		platform := riotapi.PlatformRegion(strings.ToLower(p))
		if _, err := riotapi.RoutingForPlatform(platform); err != nil {
			return Config{}, fmt.Errorf("RIOTSYNC_PLATFORMS: %w", err)
		}
		cfg.Platforms = append(cfg.Platforms, platform)
	}
	if len(cfg.Platforms) == 0 {
		return Config{}, fmt.Errorf("RIOTSYNC_PLATFORMS is empty")
	}
	for name, v := range map[string]time.Duration{
		"RIOTSYNC_SEED_INTERVAL": cfg.SeedInterval, "RIOTSYNC_CRAWL_INTERVAL": cfg.CrawlInterval,
		"RIOTSYNC_NAMES_INTERVAL": cfg.NamesInterval, "RIOTSYNC_AGGREGATE_INTERVAL": cfg.AggregateInterval,
		"RIOTSYNC_KEY_RETRY": cfg.KeyRetry,
	} {
		if v <= 0 {
			return Config{}, fmt.Errorf("%s must be positive", name)
		}
	}
	for name, v := range map[string]int{
		"RIOTSYNC_RATE_LIMIT_PER_SEC": cfg.RateLimitPerSec, "RIOTSYNC_RATE_LIMIT_PER_2MIN": cfg.RateLimitPer2Min,
		"RIOTSYNC_CRAWL_PUUIDS": cfg.CrawlPUUIDs, "RIOTSYNC_CRAWL_IDS_PER_PUUID": cfg.CrawlIDsPerPUUID,
		"RIOTSYNC_CRAWL_REQUESTS": cfg.CrawlRequests, "RIOTSYNC_NAMES_BATCH": cfg.NamesBatch,
	} {
		if v <= 0 {
			return Config{}, fmt.Errorf("%s must be positive", name)
		}
	}
	return cfg, nil
}

// envReader reads typed env vars, remembering unparsable values so a typo
// like RIOTSYNC_CRAWL_INTERVAL=5min fails startup instead of being ignored.
type envReader struct{ errs []error }

func (e *envReader) int(key string, def int) int {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		e.errs = append(e.errs, fmt.Errorf("%s=%q: not an integer", key, v))
		return def
	}
	return n
}

func (e *envReader) duration(key string, def time.Duration) time.Duration {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		e.errs = append(e.errs, fmt.Errorf("%s=%q: not a duration like 30s, 5m or 6h", key, v))
		return def
	}
	return d
}

func (e *envReader) err() error { return errors.Join(e.errs...) }

func envString(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}
