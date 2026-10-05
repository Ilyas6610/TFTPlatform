// ingestworker is the phase-2 continuous ingestion loop, promoted from the
// bounded ingestcli runs once a production Riot API key is available. It
// wraps the same internal/ingest package in a scheduling loop instead of a
// single bounded batch.
package main

import (
	"log"

	"tft-platform/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	log.Printf("ingestworker starting (db=%s)", cfg.DatabaseURL)
	log.Fatal("ingestworker not yet implemented — use ingestcli for bounded runs until phase 2 (see M3/M4 in the project plan)")
}
