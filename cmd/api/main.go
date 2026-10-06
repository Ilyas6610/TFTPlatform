package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"tft-platform/internal/apiserver"
	"tft-platform/internal/config"
	"tft-platform/internal/riotapi"
	"tft-platform/internal/setdata"
	"tft-platform/internal/store"
)

func main() {
	cfg, err := config.Load()
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

	var keySource riotapi.KeySource
	if cfg.RiotAPIKeyFile != "" {
		keySource = riotapi.FileKeySource{Path: cfg.RiotAPIKeyFile}
	} else {
		keySource = riotapi.EnvKeySource{Key: cfg.RiotAPIKey}
	}
	// One Client/RateLimiter for the whole process — every handler shares
	// it, so live-fetch traffic can't jointly exceed Riot's limits with
	// whatever ingestion is also running under the same key.
	limiter := riotapi.NewRateLimiter(cfg.RiotAppRateLimitPerSec, cfg.RiotAppRateLimitPer2Min)
	riotClient := riotapi.NewClient(keySource, limiter)

	server := &apiserver.Server{Riot: riotClient, Store: st, StatsCacheTTL: cfg.StatsCacheTTL}
	handler := apiserver.NewRouter(server)

	// Timeouts keep slow or idle clients from holding connections open.
	// WriteTimeout must outlast the slowest handler (a leaderboard refresh
	// waits up to 10s on Riot) and stay above nginx's 30s proxy timeout.
	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      45 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}

	if cfg.SetDataSyncInterval > 0 {
		go syncSetDataPeriodically(ctx, st, cfg.SetDataSyncInterval)
	}

	go func() {
		log.Printf("api server listening on %s", cfg.HTTPAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("api server: %v", err)
		}
	}()

	<-ctx.Done()
	log.Print("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown error: %v", err)
	}
}

// syncSetDataPeriodically keeps the newest set's game data current: it syncs
// from CommunityDragon at startup and then every interval, so a new patch's
// numbers (and its generated patch notes) appear without a manual run.
func syncSetDataPeriodically(ctx context.Context, st *store.Store, interval time.Duration) {
	src := setdata.DefaultSource()
	for {
		result, err := setdata.Sync(ctx, src, st, 0, "latest")
		switch {
		case err != nil:
			log.Printf("set data sync: %v", err)
		case result.Stored:
			log.Printf("set data sync: stored set %d %s", result.SetNumber, result.Version)
		}
		if err == nil {
			if _, err := setdata.SyncTextOverrides(ctx, setdata.TacticsToolsSource(), st, result.SetNumber); err != nil {
				log.Printf("set text overrides sync: %v", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}
