package config

import (
	"strings"
	"testing"
	"time"
)

func setEnv(t *testing.T, kv map[string]string) {
	t.Helper()
	for _, k := range []string{
		"DATABASE_URL", "RIOT_API_KEY", "RIOT_API_KEY_FILE", "RIOT_APP_RATE_LIMIT_PER_SEC", "RIOT_APP_RATE_LIMIT_PER_2MIN",
		"HTTP_ADDR", "SETDATA_SYNC_INTERVAL", "STATS_CACHE_TTL", "DB_STATEMENT_TIMEOUT", "DB_MAX_CONNS", "REDIS_URL",
		"RIOT_BUDGET_SPLIT",
	} {
		t.Setenv(k, "")
	}
	for k, v := range kv {
		t.Setenv(k, v)
	}
}

func valid() map[string]string {
	return map[string]string{"DATABASE_URL": "postgres://x", "RIOT_API_KEY": "RGAPI-x"}
}

func TestLoad_Defaults(t *testing.T) {
	setEnv(t, valid())
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RiotAppRateLimitPer2Min != 100 || cfg.SetDataSyncInterval != 6*time.Hour || cfg.StatsCacheTTL != 10*time.Minute ||
		cfg.DBStatementTimeout != 20*time.Second || cfg.DBMaxConns != 10 {
		t.Errorf("unexpected defaults: %+v", cfg)
	}
}

func TestLoad_ValuesAreRead(t *testing.T) {
	env := valid()
	env["RIOT_APP_RATE_LIMIT_PER_2MIN"] = "500"
	env["SETDATA_SYNC_INTERVAL"] = "0"
	env["DB_STATEMENT_TIMEOUT"] = "5s"
	setEnv(t, env)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RiotAppRateLimitPer2Min != 500 || cfg.SetDataSyncInterval != 0 || cfg.DBStatementTimeout != 5*time.Second {
		t.Errorf("unexpected values: %+v", cfg)
	}
}

// A typo in a rate limit used to fall back to the default silently.
func TestLoad_MalformedValuesFailStartup(t *testing.T) {
	cases := map[string]string{
		"RIOT_APP_RATE_LIMIT_PER_2MIN": "1OO",
		"RIOT_APP_RATE_LIMIT_PER_SEC":  "fast",
		"SETDATA_SYNC_INTERVAL":        "6 hours",
		"STATS_CACHE_TTL":              "10",
		"DB_STATEMENT_TIMEOUT":         "8sec",
		"DB_MAX_CONNS":                 "ten",
	}
	for key, bad := range cases {
		env := valid()
		env[key] = bad
		setEnv(t, env)
		_, err := Load()
		if err == nil || !strings.Contains(err.Error(), key) {
			t.Errorf("%s=%q: want a startup error naming the variable, got %v", key, bad, err)
		}
	}

	// All the problems are reported together.
	env := valid()
	env["RIOT_APP_RATE_LIMIT_PER_2MIN"] = "1OO"
	env["DB_MAX_CONNS"] = "ten"
	setEnv(t, env)
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "RIOT_APP_RATE_LIMIT_PER_2MIN") || !strings.Contains(err.Error(), "DB_MAX_CONNS") {
		t.Errorf("want both variables named, got %v", err)
	}
}

func TestLoad_RejectsValuesThatWouldBreakTheService(t *testing.T) {
	cases := map[string]string{
		"RIOT_APP_RATE_LIMIT_PER_SEC":  "0",
		"RIOT_APP_RATE_LIMIT_PER_2MIN": "-5",
		"SETDATA_SYNC_INTERVAL":        "-1h",
		"STATS_CACHE_TTL":              "0s",
		"DB_MAX_CONNS":                 "0",
	}
	for key, bad := range cases {
		env := valid()
		env[key] = bad
		setEnv(t, env)
		if _, err := Load(); err == nil {
			t.Errorf("%s=%s should be rejected", key, bad)
		}
	}
}

func TestLoad_RequiredSettings(t *testing.T) {
	setEnv(t, map[string]string{"RIOT_API_KEY": "RGAPI-x"})
	if _, err := Load(); err == nil {
		t.Error("DATABASE_URL is required")
	}
	setEnv(t, map[string]string{"DATABASE_URL": "postgres://x"})
	if _, err := Load(); err == nil {
		t.Error("a Riot key is required")
	}
}

// Range problems are reported with the malformed values, not one at a time.
func TestLoad_ReportsEveryProblemTogether(t *testing.T) {
	env := valid()
	env["RIOT_APP_RATE_LIMIT_PER_2MIN"] = "1OO" // malformed
	env["DB_MAX_CONNS"] = "0"                   // out of range
	env["STATS_CACHE_TTL"] = "0s"               // out of range
	setEnv(t, env)
	_, err := Load()
	if err == nil {
		t.Fatal("want an error")
	}
	for _, name := range []string{"RIOT_APP_RATE_LIMIT_PER_2MIN", "DB_MAX_CONNS", "STATS_CACHE_TTL"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("%s missing from %q", name, err)
		}
	}
}

func TestLoad_BudgetSplit(t *testing.T) {
	setEnv(t, valid())
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Budget != (BudgetSplit{Sync: 40, OnDemand: 30, Backfill: 20, Reserve: 10}) {
		t.Errorf("default split = %+v", cfg.Budget)
	}
	// Shares of a personal key (20/s, 100 per 2 min).
	if s, m := cfg.Share(cfg.Budget.Sync); s != 8 || m != 40 {
		t.Errorf("sync share = %d/%d, want 8/40", s, m)
	}
	if s, m := cfg.Share(cfg.Budget.OnDemand); s != 6 || m != 30 {
		t.Errorf("on-demand share = %d/%d, want 6/30", s, m)
	}
	if s, m := cfg.Share(1); s != 1 || m != 1 {
		t.Errorf("a tiny share = %d/%d, want at least 1/1", s, m)
	}

	env := valid()
	env["RIOT_BUDGET_SPLIT"] = " Reserve:0, backfill:10 ,ondemand:50,sync:40"
	setEnv(t, env)
	if cfg, err := Load(); err != nil || cfg.Budget != (BudgetSplit{Sync: 40, OnDemand: 50, Backfill: 10}) {
		t.Errorf("custom split: %+v %v", cfg.Budget, err)
	}

	for _, bad := range []string{
		"sync:50,ondemand:30,backfill:20,reserve:10", // 110%
		"sync:40,ondemand:30,backfill:20",            // reserve missing
		"sync:40,ondemand:30,backfill:20,reserve:10,crawl:5",
		"sync:40,ondemand:30,backfill:20,reserve:x",
		"sync:40,ondemand:30,backfill:20,reserve:-5",
		"sync:40,sync:30,backfill:20,reserve:10",
		"sync:0,ondemand:70,backfill:20,reserve:10", // a class with nothing
		"40,30,20,10",
	} {
		env := valid()
		env["RIOT_BUDGET_SPLIT"] = bad
		setEnv(t, env)
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "RIOT_BUDGET_SPLIT") {
			t.Errorf("%q: err = %v, want RIOT_BUDGET_SPLIT rejected", bad, err)
		}
	}
}
