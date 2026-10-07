// Package config loads process configuration from environment variables.
// All cmd/* binaries share this loader so the personal-key-vs-production-key
// distinction (rate limit ceilings, key source) lives in one place.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
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

	// DBStatementTimeout and DBMaxConns tune the API server's Postgres pool
	// (other binaries keep pgx's defaults): a statement running longer than
	// the timeout is cancelled so a slow query can't hold a connection for
	// good, and the pool is capped so analytics can't open more than the
	// database can serve.
	DBStatementTimeout time.Duration
	DBMaxConns         int

	// StatsCacheTTL is how long the API server reuses stats computed from
	// match data (meta, explorer) before querying Postgres again.
	StatsCacheTTL time.Duration

	// RedisURL (redis:// or rediss://) makes the API server keep those stats
	// in a cache shared by all its replicas; empty keeps each replica's
	// results in its own memory.
	RedisURL string
}

// Load reads the configuration. A malformed value (RIOT_APP_RATE_LIMIT_PER_2MIN=1OO)
// is an error, not a silent fallback to the default: a typo in a rate limit
// shouldn't go unnoticed. An empty value counts as unset.
func Load() (Config, error) {
	var e envReader
	cfg := Config{
		DatabaseURL:             e.str("DATABASE_URL", ""),
		RiotAPIKeyFile:          e.str("RIOT_API_KEY_FILE", ""),
		RiotAPIKey:              e.str("RIOT_API_KEY", ""),
		RiotAppRateLimitPerSec:  e.int("RIOT_APP_RATE_LIMIT_PER_SEC", 20),
		RiotAppRateLimitPer2Min: e.int("RIOT_APP_RATE_LIMIT_PER_2MIN", 100),
		HTTPAddr:                e.str("HTTP_ADDR", ":8080"),
		SetDataSyncInterval:     e.duration("SETDATA_SYNC_INTERVAL", 6*time.Hour),
		StatsCacheTTL:           e.duration("STATS_CACHE_TTL", 10*time.Minute),
		DBStatementTimeout:      e.duration("DB_STATEMENT_TIMEOUT", 20*time.Second),
		DBMaxConns:              e.int("DB_MAX_CONNS", 10),
		RedisURL:                e.str("REDIS_URL", ""),
	}
	if len(e.errs) > 0 {
		return Config{}, fmt.Errorf("invalid configuration: %s", strings.Join(e.errs, "; "))
	}

	switch {
	case cfg.RiotAppRateLimitPerSec <= 0:
		return Config{}, fmt.Errorf("RIOT_APP_RATE_LIMIT_PER_SEC must be positive")
	case cfg.RiotAppRateLimitPer2Min <= 0:
		return Config{}, fmt.Errorf("RIOT_APP_RATE_LIMIT_PER_2MIN must be positive")
	case cfg.SetDataSyncInterval < 0:
		return Config{}, fmt.Errorf("SETDATA_SYNC_INTERVAL must not be negative (0 disables the sync)")
	case cfg.StatsCacheTTL <= 0:
		return Config{}, fmt.Errorf("STATS_CACHE_TTL must be a positive duration")
	case cfg.DBStatementTimeout < 0:
		return Config{}, fmt.Errorf("DB_STATEMENT_TIMEOUT must not be negative")
	case cfg.DBMaxConns <= 0:
		return Config{}, fmt.Errorf("DB_MAX_CONNS must be positive")
	case cfg.DatabaseURL == "":
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	case cfg.RiotAPIKeyFile == "" && cfg.RiotAPIKey == "":
		return Config{}, fmt.Errorf("one of RIOT_API_KEY_FILE or RIOT_API_KEY is required")
	}
	return cfg, nil
}

// envReader reads typed environment variables, collecting every malformed
// value so startup reports them all at once.
type envReader struct{ errs []string }

func (e *envReader) str(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}

func (e *envReader) int(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		e.errs = append(e.errs, fmt.Sprintf("%s=%q is not an integer", key, v))
		return def
	}
	return n
}

func (e *envReader) duration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		e.errs = append(e.errs, fmt.Sprintf("%s=%q is not a duration like 30s or 10m", key, v))
		return def
	}
	return d
}
