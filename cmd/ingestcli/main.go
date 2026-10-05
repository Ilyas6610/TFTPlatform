// ingestcli is the bounded, manually-triggered (or CronJob-scheduled) entrypoint
// for ingestion. Every subcommand makes a small, known number of Riot API calls
// so it stays safe under a personal API key's rate limits. See internal/ingest.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"tft-platform/internal/aggregate"
	"tft-platform/internal/config"
	"tft-platform/internal/ingest"
	"tft-platform/internal/riotapi"
	"tft-platform/internal/setdata"
	"tft-platform/internal/store"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	ctx := context.Background()

	switch os.Args[1] {
	case "whoami":
		runWhoami(ctx, cfg, os.Args[2:])
	case "seed-leaderboard":
		runSeedLeaderboard(ctx, cfg, os.Args[2:])
	case "crawl-matches":
		runCrawlMatches(ctx, cfg, os.Args[2:])
	case "crawl-queue":
		runCrawlQueue(ctx, cfg, os.Args[2:])
	case "aggregate":
		runAggregate(ctx, cfg)
	case "sync-setdata":
		runSyncSetData(ctx, cfg, os.Args[2:])
	default:
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `ingestcli <command>

Commands:
  whoami            Resolve a Riot ID to PUUID + TFT summoner info (smoke test)
  seed-leaderboard  Seed PUUIDs from challenger/grandmaster/master
  crawl-matches     Crawl a bounded batch of matches for one explicit PUUID/Riot ID
  crawl-queue       Crawl a bounded batch of matches for PUUIDs queued by seed-leaderboard
  aggregate         Recompute meta-stats summary tables (no Riot API calls)
  sync-setdata      Store set data (units/traits/augments/items) from CommunityDragon for patch notes

whoami usage:
  ingestcli whoami --platform na1 --riotid "GameName#TAG"

seed-leaderboard usage:
  ingestcli seed-leaderboard --platform na1

crawl-matches usage:
  ingestcli crawl-matches --platform na1 --riotid "GameName#TAG" [--max-ids 20] [--max-requests 50]
  ingestcli crawl-matches --platform na1 --puuid <puuid> [--max-ids 20] [--max-requests 50]

crawl-queue usage:
  ingestcli crawl-queue [--max-puuids 10] [--max-ids-per-puuid 20] [--max-requests 100]`)
}

func newRiotClient(cfg config.Config) *riotapi.Client {
	var keySource riotapi.KeySource
	if cfg.RiotAPIKeyFile != "" {
		keySource = riotapi.FileKeySource{Path: cfg.RiotAPIKeyFile}
	} else {
		keySource = riotapi.EnvKeySource{Key: cfg.RiotAPIKey}
	}
	limiter := riotapi.NewRateLimiter(cfg.RiotAppRateLimitPerSec, cfg.RiotAppRateLimitPer2Min)
	return riotapi.NewClient(keySource, limiter)
}

func runWhoami(ctx context.Context, cfg config.Config, args []string) {
	fs := flag.NewFlagSet("whoami", flag.ExitOnError)
	platform := fs.String("platform", "na1", "platform region, e.g. na1, euw1, kr")
	riotID := fs.String("riotid", "", `Riot ID in "GameName#TAG" form`)
	fs.Parse(args)

	if *riotID == "" {
		fmt.Fprintln(os.Stderr, `whoami: --riotid "GameName#TAG" is required`)
		os.Exit(1)
	}
	gameName, tagLine, ok := strings.Cut(*riotID, "#")
	if !ok || gameName == "" || tagLine == "" {
		fmt.Fprintln(os.Stderr, `whoami: --riotid must be in "GameName#TAG" form`)
		os.Exit(1)
	}

	plat := riotapi.PlatformRegion(*platform)
	routing, err := riotapi.RoutingForPlatform(plat)
	if err != nil {
		log.Fatalf("whoami: %v", err)
	}

	client := newRiotClient(cfg)

	account, err := client.GetAccountByRiotID(ctx, routing, gameName, tagLine)
	if err != nil {
		log.Fatalf("whoami: resolve riot id: %v", err)
	}
	fmt.Printf("account: puuid=%s gameName=%s tagLine=%s\n", account.PUUID, account.GameName, account.TagLine)

	summoner, err := client.GetTFTSummonerByPUUID(ctx, plat, account.PUUID)
	if err != nil {
		log.Fatalf("whoami: fetch tft summoner: %v", err)
	}
	fmt.Printf("tft summoner: level=%d profileIconId=%d\n", summoner.SummonerLevel, summoner.ProfileIconID)
}

func runCrawlMatches(ctx context.Context, cfg config.Config, args []string) {
	fs := flag.NewFlagSet("crawl-matches", flag.ExitOnError)
	platform := fs.String("platform", "na1", "platform region, e.g. na1, euw1, kr")
	puuidFlag := fs.String("puuid", "", "PUUID to crawl matches for")
	riotID := fs.String("riotid", "", `Riot ID in "GameName#TAG" form, resolved to a PUUID if --puuid is not given`)
	maxIDs := fs.Int("max-ids", 20, "how many recent match ids to fetch")
	maxRequests := fs.Int("max-requests", 50, "hard cap on Riot API requests this run makes (personal-key safety budget)")
	fs.Parse(args)

	plat := riotapi.PlatformRegion(*platform)
	routing, err := riotapi.RoutingForPlatform(plat)
	if err != nil {
		log.Fatalf("crawl-matches: %v", err)
	}

	client := newRiotClient(cfg)

	targetPUUID := *puuidFlag
	if targetPUUID == "" {
		if *riotID == "" {
			fmt.Fprintln(os.Stderr, "crawl-matches: either --puuid or --riotid is required")
			os.Exit(1)
		}
		gameName, tagLine, ok := strings.Cut(*riotID, "#")
		if !ok || gameName == "" || tagLine == "" {
			fmt.Fprintln(os.Stderr, `crawl-matches: --riotid must be in "GameName#TAG" form`)
			os.Exit(1)
		}
		account, err := client.GetAccountByRiotID(ctx, routing, gameName, tagLine)
		if err != nil {
			log.Fatalf("crawl-matches: resolve riot id: %v", err)
		}
		targetPUUID = account.PUUID
	}

	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("crawl-matches: %v", err)
	}
	defer st.Close()

	if err := st.UpsertAccountPUUIDOnly(ctx, targetPUUID, string(routing)); err != nil {
		log.Fatalf("crawl-matches: register puuid: %v", err)
	}

	runID, err := st.StartIngestRun(ctx, "crawl_matches")
	if err != nil {
		log.Fatalf("crawl-matches: start run: %v", err)
	}

	result, crawlErr := ingest.CrawlPUUID(ctx, client, st, routing, targetPUUID, *maxIDs, *maxRequests)

	status := "completed"
	errDetail := ""
	switch {
	case crawlErr != nil:
		var keyExpired *riotapi.ErrKeyExpired
		if errors.As(crawlErr, &keyExpired) {
			status = "failed_key_expired"
		} else {
			status = "failed_error"
		}
		errDetail = crawlErr.Error()
	case result.Stopped != "":
		status = result.Stopped + "_stopped"
	}

	if err := st.FinishIngestRun(ctx, runID, status, result.RequestsMade, result.MatchesIngested, errDetail); err != nil {
		log.Printf("crawl-matches: record run finish: %v", err)
	}

	fmt.Printf("crawl-matches: requests=%d matches_ingested=%d status=%s\n", result.RequestsMade, result.MatchesIngested, status)

	if crawlErr != nil {
		log.Fatalf("crawl-matches: %v", crawlErr)
	}
}

func runSeedLeaderboard(ctx context.Context, cfg config.Config, args []string) {
	fs := flag.NewFlagSet("seed-leaderboard", flag.ExitOnError)
	platform := fs.String("platform", "na1", "platform region, e.g. na1, euw1, kr")
	fs.Parse(args)

	plat := riotapi.PlatformRegion(*platform)
	client := newRiotClient(cfg)

	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("seed-leaderboard: %v", err)
	}
	defer st.Close()

	runID, err := st.StartIngestRun(ctx, "seed_leaderboard")
	if err != nil {
		log.Fatalf("seed-leaderboard: start run: %v", err)
	}

	result, seedErr := ingest.SeedLeaderboard(ctx, client, st, plat)

	status := "completed"
	errDetail := ""
	switch {
	case seedErr != nil:
		var keyExpired *riotapi.ErrKeyExpired
		if errors.As(seedErr, &keyExpired) {
			status = "failed_key_expired"
		} else {
			status = "failed_error"
		}
		errDetail = seedErr.Error()
	case result.Stopped != "":
		status = result.Stopped + "_stopped"
	}

	if err := st.FinishIngestRun(ctx, runID, status, result.RequestsMade, result.PlayersSeeded, errDetail); err != nil {
		log.Printf("seed-leaderboard: record run finish: %v", err)
	}

	fmt.Printf("seed-leaderboard: requests=%d players_seeded=%d status=%s\n", result.RequestsMade, result.PlayersSeeded, status)

	if seedErr != nil {
		log.Fatalf("seed-leaderboard: %v", seedErr)
	}
}

func runCrawlQueue(ctx context.Context, cfg config.Config, args []string) {
	fs := flag.NewFlagSet("crawl-queue", flag.ExitOnError)
	maxPUUIDs := fs.Int("max-puuids", 10, "how many queued PUUIDs to crawl this run")
	maxIDsPerPUUID := fs.Int("max-ids-per-puuid", 20, "how many recent match ids to fetch per PUUID")
	maxRequests := fs.Int("max-requests", 100, "hard cap on total Riot API requests this run makes across all PUUIDs (personal-key safety budget)")
	fs.Parse(args)

	client := newRiotClient(cfg)

	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("crawl-queue: %v", err)
	}
	defer st.Close()

	runID, err := st.StartIngestRun(ctx, "crawl_queue")
	if err != nil {
		log.Fatalf("crawl-queue: start run: %v", err)
	}

	result, crawlErr := ingest.CrawlQueue(ctx, client, st, *maxPUUIDs, *maxIDsPerPUUID, *maxRequests)

	status := "completed"
	errDetail := ""
	switch {
	case crawlErr != nil:
		var keyExpired *riotapi.ErrKeyExpired
		if errors.As(crawlErr, &keyExpired) {
			status = "failed_key_expired"
		} else {
			status = "failed_error"
		}
		errDetail = crawlErr.Error()
	case result.Stopped != "":
		status = result.Stopped + "_stopped"
	}

	if err := st.FinishIngestRun(ctx, runID, status, result.RequestsMade, result.MatchesIngested, errDetail); err != nil {
		log.Printf("crawl-queue: record run finish: %v", err)
	}

	fmt.Printf("crawl-queue: puuids_crawled=%d requests=%d matches_ingested=%d status=%s\n",
		result.PUUIDsCrawled, result.RequestsMade, result.MatchesIngested, status)

	if crawlErr != nil {
		log.Fatalf("crawl-queue: %v", crawlErr)
	}
}

func runAggregate(ctx context.Context, cfg config.Config) {
	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("aggregate: %v", err)
	}
	defer st.Close()

	results, err := aggregate.RecomputeAll(ctx, st)
	if err != nil {
		log.Fatalf("aggregate: %v", err)
	}
	for _, r := range results {
		fmt.Printf("aggregate: set=%d units=%d traits=%d augments=%d\n", r.TFTSetNumber, r.UnitsWritten, r.TraitsWritten, r.AugmentsWritten)
	}
}

// runSyncSetData stores set data snapshots from CommunityDragon. Passing
// several archived patches (--versions 16.17,16.18,latest) backfills history
// so patch notes exist for patches before the first sync.
func runSyncSetData(ctx context.Context, cfg config.Config, args []string) {
	fs := flag.NewFlagSet("sync-setdata", flag.ExitOnError)
	set := fs.Int("set", 0, "TFT set number (0 = newest set in the export)")
	versions := fs.String("versions", "latest", "comma-separated CommunityDragon channels: \"latest\" or patch archives like 16.18")
	fs.Parse(args)

	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("sync-setdata: %v", err)
	}
	defer st.Close()

	src := setdata.DefaultSource()
	lastSet := *set
	for _, channel := range strings.Split(*versions, ",") {
		channel = strings.TrimSpace(channel)
		result, err := setdata.Sync(ctx, src, st, *set, channel)
		if err != nil {
			log.Fatalf("sync-setdata %s: %v", channel, err)
		}
		status := "stored"
		if !result.Stored {
			status = "unchanged"
		}
		fmt.Printf("sync-setdata: channel=%s set=%d version=%s %s\n", channel, result.SetNumber, result.Version, status)
		lastSet = result.SetNumber
	}

	// Rendered ability text for values Riot's export leaves out; best
	// effort, attached to the newest snapshot.
	ov := setdata.TacticsToolsSource()
	if n, err := setdata.SyncTextOverrides(ctx, ov, st, lastSet); err != nil {
		log.Printf("sync-setdata: %s text overrides: %v", ov.Name, err)
	} else {
		fmt.Printf("sync-setdata: %s text overrides for set %d: %d descriptions\n", ov.Name, lastSet, n)
	}
}
