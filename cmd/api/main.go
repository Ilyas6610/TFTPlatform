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

	server := &apiserver.Server{Riot: riotClient, Store: st}
	handler := apiserver.NewRouter(server)

	httpServer := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: handler,
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
