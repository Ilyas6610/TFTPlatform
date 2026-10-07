// Package config loads process configuration from environment variables.
// All cmd/* binaries share this loader so the personal-key-vs-production-key
// distinction (rate limit ceilings, key source) lives in one place.
package config

import (
	"cmp"
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

	// Budget splits the key between the kinds of traffic that share it
	// (RIOT_BUDGET_SPLIT), so their caps can't add up to more than the key.
	Budget BudgetSplit
}

// BudgetSplit is how the Riot key is shared, in percent of its limits:
//   - Sync: riotsync's background crawling, seeding and name resolution;
//   - OnDemand: Riot calls a page request triggers on the API server
//     (profile and match lookups, history and rank syncs, ladder refresh);
//   - Backfill: "Load whole set" history loads;
//   - Reserve: left unused, for other processes and Riot's own slack.
//
// The shares must add up to at most 100.
type BudgetSplit struct {
	Sync, OnDemand, Backfill, Reserve int
}

// DefaultBudgetSplit is RIOT_BUDGET_SPLIT's default.
const DefaultBudgetSplit = "sync:40,ondemand:30,backfill:20,reserve:10"

// Share is pct percent of the key's limits, at least 1 request each.
func (c Config) Share(pct int) (perSec, per2Min int) {
	return max(1, c.RiotAppRateLimitPerSec*pct/100), max(1, c.RiotAppRateLimitPer2Min*pct/100)
}

// parseBudgetSplit reads "sync:40,ondemand:30,backfill:20,reserve:10".
// Every class must be named exactly once.
func parseBudgetSplit(v string) (BudgetSplit, error) {
	var b BudgetSplit
	fields := map[string]*int{"sync": &b.Sync, "ondemand": &b.OnDemand, "backfill": &b.Backfill, "reserve": &b.Reserve}
	seen := map[string]bool{}
	for _, part := range strings.Split(v, ",") {
		name, num, ok := strings.Cut(strings.TrimSpace(part), ":")
		name = strings.ToLower(strings.TrimSpace(name))
		dst, known := fields[name]
		if !ok || !known {
			return b, fmt.Errorf("RIOT_BUDGET_SPLIT=%q: want class:percent pairs for sync, ondemand, backfill and reserve", v)
		}
		n, err := strconv.Atoi(strings.TrimSpace(num))
		if err != nil || n < 0 || seen[name] {
			return b, fmt.Errorf("RIOT_BUDGET_SPLIT=%q: %s needs one non-negative whole percentage", v, name)
		}
		*dst, seen[name] = n, true
	}
	if len(seen) != len(fields) {
		return b, fmt.Errorf("RIOT_BUDGET_SPLIT=%q: name all of sync, ondemand, backfill and reserve", v)
	}
	if sum := b.Sync + b.OnDemand + b.Backfill + b.Reserve; sum > 100 {
		return b, fmt.Errorf("RIOT_BUDGET_SPLIT=%q adds up to %d%%; the shares can't exceed the key (100)", v, sum)
	}
	if b.Sync == 0 || b.OnDemand == 0 || b.Backfill == 0 {
		return b, fmt.Errorf("RIOT_BUDGET_SPLIT=%q: sync, ondemand and backfill each need a share above 0", v)
	}
	return b, nil
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
	if b, err := parseBudgetSplit(cmp.Or(e.str("RIOT_BUDGET_SPLIT", ""), DefaultBudgetSplit)); err != nil {
		e.errs = append(e.errs, err.Error())
	} else {
		cfg.Budget = b
	}

	// Range problems are reported together with the malformed values.
	problem := func(bad bool, msg string) {
		if bad {
			e.errs = append(e.errs, msg)
		}
	}
	problem(cfg.RiotAppRateLimitPerSec <= 0, "RIOT_APP_RATE_LIMIT_PER_SEC must be positive")
	problem(cfg.RiotAppRateLimitPer2Min <= 0, "RIOT_APP_RATE_LIMIT_PER_2MIN must be positive")
	problem(cfg.SetDataSyncInterval < 0, "SETDATA_SYNC_INTERVAL must not be negative (0 disables the sync)")
	problem(cfg.StatsCacheTTL <= 0, "STATS_CACHE_TTL must be a positive duration")
	problem(cfg.DBStatementTimeout < 0, "DB_STATEMENT_TIMEOUT must not be negative")
	problem(cfg.DBMaxConns <= 0, "DB_MAX_CONNS must be positive")
	problem(cfg.DatabaseURL == "", "DATABASE_URL is required")
	problem(cfg.RiotAPIKeyFile == "" && cfg.RiotAPIKey == "", "one of RIOT_API_KEY_FILE or RIOT_API_KEY is required")
	if len(e.errs) > 0 {
		return Config{}, fmt.Errorf("invalid configuration: %s", strings.Join(e.errs, "; "))
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
