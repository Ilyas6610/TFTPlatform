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
Running a binary requires env config (see `internal/config/config.go`):
```bash
DATABASE_URL="postgres://tft:tft@localhost:5432/tft" RIOT_API_KEY_FILE="deploy/.secrets/riot-api-key.txt" go run ./cmd/api
```
Other env vars: `RIOT_API_KEY` (fallback when no key file), `RIOT_APP_RATE_LIMIT_PER_SEC` (default 20), `RIOT_APP_RATE_LIMIT_PER_2MIN` (default 100), `HTTP_ADDR` (default `:8080`).

Personal Riot API keys expire every 24h — refresh `deploy/.secrets/riot-api-key.txt` (gitignored) before ingesting.

### Frontend (`frontend/`)
```bash
npm install
npm run dev      # Vite dev server on :5173, proxies /api to localhost:8080
npm run build    # tsc -b && vite build
```

## Architecture

### Binaries (`cmd/`)
- **api** — HTTP server (`internal/apiserver`). Routes under `/api/v1/` for player profiles, player match history, match detail, leaderboard, and meta units/traits/augments; plus `/healthz`.
- **ingestcli** — bounded ingestion runs, safe under personal-key rate limits. Subcommands: `whoami`, `seed-leaderboard`, `crawl-matches`, `crawl-queue`, `aggregate`.
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
- **settags** — empty, not yet implemented.

### Key invariant: one rate limiter per process
Exactly one `riotapi.Client`/`RateLimiter` is built at startup and injected (e.g. into `apiserver.Server`). Handlers and other code must never construct their own client, so live fetches and ingestion can't jointly exceed Riot's rate limits.

### Deploy
`deploy/docker-compose.yml` for local Postgres + migrations. `deploy/k8s/` (base + `overlays/local`) is scaffolded but currently empty.
