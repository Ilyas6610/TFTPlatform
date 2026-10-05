// aggregator recomputes the meta_*_stats summary tables from
// match_participants. It makes no Riot API calls and is not
// rate-limit-constrained, so it can run as often as desired independent of
// the ingestion pipeline (e.g. as a frequent Kubernetes CronJob).
package main

import (
	"context"
	"fmt"
	"log"

	"tft-platform/internal/aggregate"
	"tft-platform/internal/config"
	"tft-platform/internal/store"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	ctx := context.Background()
	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer st.Close()

	results, err := aggregate.RecomputeAll(ctx, st)
	if err != nil {
		log.Fatalf("aggregator: %v", err)
	}
	for _, r := range results {
		fmt.Printf("set=%d units=%d traits=%d augments=%d\n", r.TFTSetNumber, r.UnitsWritten, r.TraitsWritten, r.AugmentsWritten)
	}
}
