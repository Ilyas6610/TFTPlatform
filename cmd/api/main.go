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
	"tft-platform/internal/rediscache"
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

	st, err := store.NewWithOptions(ctx, cfg.DatabaseURL, store.Options{
		StatementTimeout: cfg.DBStatementTimeout,
		MaxConns:         int32(cfg.DBMaxConns),
	})
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
	// Views of that client, each on its class's share of the key
	// (RIOT_BUDGET_SPLIT): request-triggered work and live lookups share the
	// on-demand budget (live lookups fail fast rather than wait long), and
	// whole-set loads have their own.
	onDemand := riotapi.NewBudget(cfg.Share(cfg.Budget.OnDemand))
	backfill := riotapi.NewBudget(cfg.Share(cfg.Budget.Backfill))
	log.Printf("riot budget: on-demand %d/s %d/2min, backfill %d/s %d/2min (split %+v)",
		onDemand.PerSec, onDemand.Per2Min, backfill.PerSec, backfill.Per2Min, cfg.Budget)

	server := &apiserver.Server{
		Riot:          riotClient.WithBudget(onDemand, 0),
		RiotLive:      riotClient.WithBudget(onDemand, apiserver.LiveBudgetWait),
		RiotBackfill:  riotClient.WithBudget(backfill, 0),
		Store:         st,
		StatsCacheTTL: cfg.StatsCacheTTL,
	}
	if cfg.RedisURL != "" {
		shared, err := rediscache.New(cfg.RedisURL)
		if err != nil {
			log.Fatalf("%v", err)
		}
		defer shared.Close()
		// Redis being down is not fatal: stats are then computed per
		// replica, and the client reconnects by itself.
		if err := shared.Ping(ctx); err != nil {
			log.Printf("shared cache: redis not reachable yet: %v", err)
		}
		server.Shared = shared
		log.Printf("stats cache: shared via redis, ttl %s", cfg.StatsCacheTTL)
	}
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
			if _, err := setdata.SyncPatchCalendar(ctx, st, result.SetNumber, setdata.FetchNotesDate); err != nil {
				log.Printf("patch calendar sync: %v", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}
