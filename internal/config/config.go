// Package config loads process configuration from environment variables.
// All cmd/* binaries share this loader so the personal-key-vs-production-key
// distinction (rate limit ceilings, key source) lives in one place.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	DatabaseURL string

	// RiotAPIKeyFile, when set, takes precedence: the key is read from this
	// file on every use (see internal/riotapi.FileKeySource), so rotating a
	// personal key (which expires every 24h) or swapping to a production key
	// never requires a rebuild/redeploy. Falls back to RiotAPIKey (env var)
	// if unset, which does require a restart to rotate.
	RiotAPIKeyFile string
	RiotAPIKey     string

	// Rate limit ceilings. Defaults match Riot's personal-key limits; bump
	// via env when promoted to a production key (see deploy/k8s ConfigMap).
	RiotAppRateLimitPerSec  int
	RiotAppRateLimitPer2Min int

	HTTPAddr string

	// SetDataSyncInterval is how often the API server re-syncs the newest
	// set's game data from CommunityDragon (see internal/setdata); 0
	// disables it.
	SetDataSyncInterval time.Duration

	// StatsCacheTTL is how long the API server reuses stats computed from
	// match data (meta, explorer) before querying Postgres again.
	StatsCacheTTL time.Duration

	// RedisURL (redis:// or rediss://) makes the API server keep those stats
	// in a cache shared by all its replicas; empty keeps each replica's
	// results in its own memory.
	RedisURL string
}

func Load() (Config, error) {
	cfg := Config{
		DatabaseURL:             getEnv("DATABASE_URL", ""),
		RiotAPIKeyFile:          getEnv("RIOT_API_KEY_FILE", ""),
		RiotAPIKey:              getEnv("RIOT_API_KEY", ""),
		RiotAppRateLimitPerSec:  getEnvInt("RIOT_APP_RATE_LIMIT_PER_SEC", 20),
		RiotAppRateLimitPer2Min: getEnvInt("RIOT_APP_RATE_LIMIT_PER_2MIN", 100),
		HTTPAddr:                getEnv("HTTP_ADDR", ":8080"),
		SetDataSyncInterval:     getEnvDuration("SETDATA_SYNC_INTERVAL", 6*time.Hour),
		StatsCacheTTL:           getEnvDuration("STATS_CACHE_TTL", 10*time.Minute),
		RedisURL:                getEnv("REDIS_URL", ""),
	}

	if cfg.StatsCacheTTL <= 0 {
		return Config{}, fmt.Errorf("STATS_CACHE_TTL must be a positive duration")
	}
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}
	if cfg.RiotAPIKeyFile == "" && cfg.RiotAPIKey == "" {
		return Config{}, fmt.Errorf("one of RIOT_API_KEY_FILE or RIOT_API_KEY is required")
	}

	return cfg, nil
}

func getEnv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}

func getEnvInt(key string, def int) int {
	v, ok := os.LookupEnv(key)
	if !ok {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func getEnvDuration(key string, def time.Duration) time.Duration {
	v, ok := os.LookupEnv(key)
	if !ok {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}
