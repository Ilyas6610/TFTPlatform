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
