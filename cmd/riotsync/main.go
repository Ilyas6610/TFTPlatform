// riotsync is the background service that keeps Postgres filled from the
// Riot API: it seeds leaderboards, crawls queued players' matches, resolves
// Riot IDs and recomputes meta stats on a schedule. It is the only process
// that needs to ingest continuously, so the API server can stay read-mostly.
// See internal/riotsync for the settings (RIOTSYNC_* env vars).
package main

import (
	"context"
	"log"
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

	var keySource riotapi.KeySource
	if cfg.RiotAPIKeyFile != "" {
		keySource = riotapi.FileKeySource{Path: cfg.RiotAPIKeyFile}
	} else {
		keySource = riotapi.EnvKeySource{Key: cfg.RiotAPIKey}
	}
	limiter := riotapi.NewRateLimiter(cfg.RiotAppRateLimitPerSec, cfg.RiotAppRateLimitPer2Min)
	riot := riotapi.NewClient(keySource, limiter)

	sched := &riotsync.Scheduler{
		Tasks:          riotsync.BuildTasks(syncCfg, riot, st),
		Recorder:       riotsync.StoreRecorder{Store: st},
		KeyRetry:       syncCfg.KeyRetry,
		RateLimitPause: 2 * time.Minute, // one app rate-limit window
	}
	log.Printf("riotsync starting: platforms=%v", syncCfg.Platforms)
	sched.Run(ctx)
	log.Print("riotsync stopped")
}
