# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

`tft-platform` is a Teamfight Tactics stats site: a Go backend that ingests match data from the Riot API into Postgres, computes meta statistics, and serves a JSON API consumed by a React/Vite frontend.

## Commands

### Local environment
```bash
scripts/dev-up.sh                 # start Postgres (docker-compose) and apply all migrations
scripts/migrate.sh <name>         # create a new NNNN_<name>.up.sql / .down.sql pair in internal/db/migrations
```
Postgres runs at `postgres://tft:tft@localhost:5432/tft`. Migrations are applied by the `migrate/migrate` container in `deploy/docker-compose.yml`, not by Go code.

### Go backend
```bash
go build ./...
go vet ./...
gofmt -l -w .
go test ./...
go test ./internal/riotapi -run TestName   # single test
```
Tests that need Postgres (`store`, `ingest`, `apiserver`) skip unless `TEST_DATABASE_URL` is set; `scripts/dev-up.sh` creates the `tft_test` database:
```bash
TEST_DATABASE_URL="postgres://tft:tft@localhost:5432/tft_test?sslmode=disable" go test -race ./...
```
Each such test gets a fresh schema with all migrations applied (`internal/store/storetest`), so they're parallel-safe. Fake Riot responses go through `internal/riotapi/riotapitest`, which points a real `riotapi.Client` at an `httptest` server.
Running a binary requires env config (see `internal/config/config.go`):
```bash
DATABASE_URL="postgres://tft:tft@localhost:5432/tft" RIOT_API_KEY_FILE="deploy/.secrets/riot-api-key.txt" go run ./cmd/api
```
Other env vars: `RIOT_API_KEY` (fallback when no key file), `RIOT_APP_RATE_LIMIT_PER_SEC` (default 20), `RIOT_APP_RATE_LIMIT_PER_2MIN` (default 100), `HTTP_ADDR` (default `:8080`), `SETDATA_SYNC_INTERVAL` (default `6h`, `0` disables the API server's set data sync).

Personal Riot API keys expire every 24h — refresh `deploy/.secrets/riot-api-key.txt` (gitignored) before ingesting.

### Frontend (`frontend/`)
```bash
npm install
npm run dev      # Vite dev server on :5173, proxies /api to localhost:8080
npm run build    # tsc -b && vite build
npm run assets   # download TFT icons + manifest from Data Dragon into public/tft/ (gitignored, ~100 MB; re-run after a patch)
```
Game icons are looked up through `src/assets/tft.tsx` (`GameIcon`, `GameLabel`, `lookup`), keyed by Riot API id (`TFT17_Jinx`, `TFT_Item_InfinityEdge`; Set 18 switched to a `DA_` prefix, e.g. `DA_Amumu18`, `DA_18_Elderwood`). Without downloaded assets the UI falls back to text names. Profile icons load from the Data Dragon CDN rather than being downloaded.

The live TFT set number is `CURRENT_TFT_SET` in `frontend/src/config.ts` — the only place to bump when a new set launches. The backend is set-agnostic (aggregation runs for every `tft_set_number` present in `matches`).

## Architecture

### Binaries (`cmd/`)
- **api** — HTTP server (`internal/apiserver`). Routes under `/api/v1/` for player profiles, player match history, match detail, leaderboard, and meta units/traits/augments; plus `/healthz`.
- **ingestcli** — bounded ingestion runs, safe under personal-key rate limits. Subcommands: `whoami`, `seed-leaderboard`, `crawl-matches`, `crawl-queue`, `aggregate`, `sync-setdata`.
- **aggregator** — recomputes the `meta_*_stats` tables from `match_participants`. Makes no Riot calls; can run frequently (e.g. as a CronJob).
- **ingestworker** — placeholder for phase-2 continuous ingestion; currently exits with "not yet implemented".

### Packages (`internal/`)
- **config** — env-var loader shared by every binary.
- **riotapi** — Riot API client, region routing, and `RateLimiter`. `KeySource` abstracts the key: `FileKeySource` re-reads the file on every use so keys can be rotated without restarting; `EnvKeySource` is fixed at startup.
- **ingest** — `seeder` (leaderboard PUUIDs → queue), `queue`, and `crawler` (fetch matches for queued PUUIDs).
- **aggregate** — `RecomputeAll` builds the meta stats tables.
- **store** — hand-written pgx queries (no sqlc/ORM, intentionally, while the schema evolves).
- **apiserver** — handlers, DTOs, router.
- **db/migrations** — sequential golang-migrate SQL files (`0001`…).
- **setdata** — static set data (units, traits, augments, items) from CommunityDragon, stored as per-version snapshots; see below.
- **settags** — empty, not yet implemented.

### Data freshness (request-triggered Riot fetches)
- **Leaderboard** — `GET /api/v1/leaderboard/{platform}` re-seeds the platform from Riot (3 requests, `ingest.SeedLeaderboard`) when its snapshot is older than 2 minutes; a complete seed prunes players who left master+. Missing Riot IDs on the returned page are resolved in the background via account-v1 by-PUUID (`ingest.ResolveNames`). Refresh serialization is in `internal/apiserver/leaderboard_sync.go`.
- **Match history** — `GET /api/v1/players/{puuid}/matches?region=<platform>` starts a background sync (`ingest.SyncPlayerMatches`: ids + any unsaved matches from the last 20) when the player's history wasn't synced in the last 2 minutes. Sync time is `ingest_puuid_queue.last_crawled_at`, so viewed players also join the crawl queue. Without `region` the endpoint is cache-only.
- Background runs go through `backgroundJobs` (`internal/apiserver/background.go`): one run per key at a time, a 1-minute cooldown after incomplete/failed runs, and the last error surfaced as `stale`/`staleReason`. Responses carry `resolving`/`refreshing` flags; the frontend polls every 3s while set.

### Set data and generated patch notes
`internal/setdata` downloads CommunityDragon's TFT export (`raw.communitydragon.org/<channel>/cdragon/tft/en_us.json`, ~25 MB; channel is `latest` or a patch archive like `16.18`), extracts the set's standard-mode entry (`TFTSet<N>`), renders description templates (`@Var@`, `@Var*100@`, `%i:scaleAP%`, `<row>` breakpoints) to plain text, and stores the result in `set_data_snapshots`, one row per content version. Patch notes are not scraped: `GET /api/v1/sets/{set}/patches` diffs each snapshot against the previous one (`setdata.Diff`). `GET /api/v1/sets/{set}/data[?version=]` serves a snapshot.
- Updating: the API server syncs `latest` at startup and every `SETDATA_SYNC_INTERVAL`; `ingestcli sync-setdata [--set N] --versions 16.17,16.18,latest` syncs or backfills manually. A sync skips versions already stored and builds whose data is identical to the previous snapshot.
- Variables CommunityDragon couldn't name are keyed by FNV-1a of the lowercased name (`{b027c2f9}`); `namedValues` maps them back using names found in the description.
- Set 18 ability values computed at runtime (`@MagicDamageCalc1@`) aren't in any export, so they render as `?`; unit stats, trait/augment/item values are exact.
- Snapshots hold extracted data, not the raw export, so changing the extraction (new fields, filters) makes old and new snapshots differ in shape and produces spurious diffs. After such a change, delete the set's rows from `set_data_snapshots` and backfill again with `sync-setdata`.

### Key invariant: one rate limiter per process
Exactly one `riotapi.Client`/`RateLimiter` is built at startup and injected (e.g. into `apiserver.Server`). Handlers and other code must never construct their own client, so live fetches and ingestion can't jointly exceed Riot's rate limits.

### Deploy
`deploy/docker-compose.yml` for local Postgres + migrations. `deploy/k8s/` (base + `overlays/local`) is scaffolded but currently empty.
