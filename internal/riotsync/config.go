// Package riotsync is the background service that pulls data from the Riot
// API and writes it to Postgres: leaderboard seeding, match crawling for the
// queued players, Riot ID resolution and meta-stat aggregation. It reuses
// internal/ingest and internal/aggregate and runs them on a schedule, so the
// HTTP API never has to talk to Riot to keep the database fresh.
package riotsync

import (
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

	CrawlPUUIDs      int // queued players per crawl batch
	CrawlIDsPerPUUID int // recent match ids per player
	CrawlRequests    int // Riot request cap per crawl batch
	NamesBatch       int // names resolved per run

	// KeyRetry is how long everything pauses after Riot rejects the key
	// (expired personal keys); the key file is re-read on the next try.
	KeyRetry time.Duration
}

func LoadConfig() (Config, error) {
	cfg := Config{
		SeedInterval:      envDuration("RIOTSYNC_SEED_INTERVAL", 6*time.Hour),
		CrawlInterval:     envDuration("RIOTSYNC_CRAWL_INTERVAL", 5*time.Minute),
		NamesInterval:     envDuration("RIOTSYNC_NAMES_INTERVAL", 10*time.Minute),
		AggregateInterval: envDuration("RIOTSYNC_AGGREGATE_INTERVAL", 10*time.Minute),
		CrawlPUUIDs:       envInt("RIOTSYNC_CRAWL_PUUIDS", 10),
		CrawlIDsPerPUUID:  envInt("RIOTSYNC_CRAWL_IDS_PER_PUUID", 20),
		CrawlRequests:     envInt("RIOTSYNC_CRAWL_REQUESTS", 100),
		NamesBatch:        envInt("RIOTSYNC_NAMES_BATCH", 50),
		KeyRetry:          envDuration("RIOTSYNC_KEY_RETRY", 5*time.Minute),
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
		"RIOTSYNC_CRAWL_PUUIDS": cfg.CrawlPUUIDs, "RIOTSYNC_CRAWL_IDS_PER_PUUID": cfg.CrawlIDsPerPUUID,
		"RIOTSYNC_CRAWL_REQUESTS": cfg.CrawlRequests, "RIOTSYNC_NAMES_BATCH": cfg.NamesBatch,
	} {
		if v <= 0 {
			return Config{}, fmt.Errorf("%s must be positive", name)
		}
	}
	return cfg, nil
}

func envString(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if n, err := strconv.Atoi(os.Getenv(key)); err == nil {
		return n
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	if d, err := time.ParseDuration(os.Getenv(key)); err == nil {
		return d
	}
	return def
}
