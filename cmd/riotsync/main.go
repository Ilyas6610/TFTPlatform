// riotsync is the background service that keeps Postgres filled from the
// Riot API: it seeds leaderboards, crawls queued players' matches, resolves
// Riot IDs and recomputes meta stats on a schedule. It is the only process
// that needs to ingest continuously, so the API server can stay read-mostly.
// See internal/riotsync for the settings (RIOTSYNC_* env vars).
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"tft-platform/internal/config"
	"tft-platform/internal/riotapi"
	"tft-platform/internal/riotsync"
	"tft-platform/internal/store"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	syncCfg, err := riotsync.LoadConfig()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer st.Close()

	ok, release, err := riotsync.AcquireSingleton(ctx, st)
	if err != nil {
		log.Fatalf("lock: %v", err)
	}
	if !ok {
		log.Fatal("another riotsync instance is already running against this database")
	}
	defer release()

	// Holding the lock means no other riotsync is running, so any of its runs
	// still marked "running" were left by an instance that died.
	recorder := riotsync.StoreRecorder{Store: st}
	if n, err := recorder.CloseStale(ctx); err != nil {
		log.Printf("close stale runs: %v", err)
	} else if n > 0 {
		log.Printf("marked %d unfinished run(s) from a previous instance as interrupted", n)
	}

	var keySource riotapi.KeySource
	if cfg.RiotAPIKeyFile != "" {
		keySource = riotapi.FileKeySource{Path: cfg.RiotAPIKeyFile}
	} else {
		keySource = riotapi.EnvKeySource{Key: cfg.RiotAPIKey}
	}
	limiter := riotapi.NewRateLimiter(cfg.RiotAppRateLimitPerSec, cfg.RiotAppRateLimitPer2Min)
	// Transport chain: Throttle keeps riotsync under its own share of the
	// key (leaving headroom for the API server), and the watcher lets the
	// scheduler honor Riot's Retry-After after a 429. No Client.Timeout: it
	// would also cut off a throttle wait, so the transport times out the
	// response instead.
	watcher := &riotsync.RateLimitWatcher{Base: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		MaxIdleConns:          4,
		IdleConnTimeout:       90 * time.Second,
	}}
	throttle := &riotsync.Throttle{Base: watcher, PerSec: syncCfg.RateLimitPerSec, Per2Min: syncCfg.RateLimitPer2Min}
	riot := riotapi.NewClient(keySource, limiter, riotapi.WithHTTPClient(&http.Client{Transport: throttle}))

	sched := &riotsync.Scheduler{
		Tasks:          riotsync.BuildTasks(syncCfg, riot, st),
		Recorder:       recorder,
		KeyRetry:       syncCfg.KeyRetry,
		RateLimitPause: 10 * time.Second, // floor; Retry-After is used when longer
		Limits:         watcher,
	}
	log.Printf("riotsync starting: platforms=%v", syncCfg.Platforms)
	sched.Run(ctx)
	log.Print("riotsync stopped")
}
