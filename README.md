# TFT Platform

A Teamfight Tactics stats site: a Go backend that ingests ranked match data from the Riot API into Postgres and computes meta statistics, plus a React frontend for player profiles, match details, leaderboards, meta stats and per-set game info with generated patch notes.

## Features

- **Player profiles** — look up any Riot ID; recent match history syncs from Riot in the background when the profile is opened.
- **Match details** — every player's board (champions, star levels, items, traits), with links to each player's profile.
- **Leaderboards** — Challenger / Grandmaster / Master per region, refreshed when the page is opened; player names are resolved in the background.
- **Meta** — every unit's games, average placement, top-4 and pick rate with its most common **exact builds** (the precise three items it carried) and most-used items, plus traits by tier; ranked by default, computed live from match data. Builds and rows link into the Explorer.
- **Explorer** — placement stats for any combination of conditions: units (with minimum star level and items on that unit), items anywhere on the board, trait breakpoints, queue and player level. It also shows what else matching boards ran and the best items on each chosen unit; click any row to add it as a condition. Searches live in the URL, so they can be shared.
- **Set info** — units (stats and abilities), traits (breakpoints), augments (with possible-reward tables), items and Wisps for the current set, with numbers highlighted by what they scale with.
- **Generated patch notes** — every number that changed between patches (plus additions, removals and renames), linked to Riot's official notes.

## Architecture

```
cmd/
  api/           HTTP API server (also syncs set data every 6h)
  ingestcli/     bounded, rate-limit-safe ingestion commands
  aggregator/    recomputes meta stats from ingested matches
  ingestworker/  placeholder for continuous ingestion (not implemented)
internal/
  riotapi/       Riot API client + shared rate limiter
  ingest/        leaderboard seeding, match crawling, name resolution
  aggregate/     meta stats computation
  setdata/       set data extraction, rendering and patch-note diffs
  store/         Postgres queries (pgx)
  apiserver/     HTTP handlers
  db/migrations/ SQL migrations (golang-migrate)
frontend/        React + TypeScript + Vite
deploy/          docker-compose for local Postgres and migrations
scripts/         dev setup helpers
```

Each process creates one Riot API client and rate limiter and shares it, so live requests and ingestion can never jointly exceed Riot's limits. See [CLAUDE.md](CLAUDE.md) for design details.

## Getting started

### Prerequisites

- Go 1.26+
- Node.js 18+
- Docker with Compose (`docker compose` or `docker-compose`)
- A Riot API key from the [Riot Developer Portal](https://developer.riotgames.com/) (personal keys expire every 24 hours)

### 1. Database

```bash
scripts/dev-up.sh
```

Starts Postgres on `localhost:5432` (user/password/database `tft`), applies all migrations, and creates a `tft_test` database for tests.

### 2. Riot API key

```bash
mkdir -p deploy/.secrets
echo "RGAPI-your-key-here" > deploy/.secrets/riot-api-key.txt
```

The key is re-read from this file on every request, so you can replace an expired key without restarting anything. `deploy/.secrets/` is gitignored.

### 3. API server

```bash
DATABASE_URL="postgres://tft:tft@localhost:5432/tft" RIOT_API_KEY_FILE="deploy/.secrets/riot-api-key.txt" go run ./cmd/api
```

Listens on `:8080`. On startup it syncs the newest set's game data (about 100 MB of downloads from CommunityDragon).

### 4. Frontend

```bash
cd frontend
npm install
npm run assets
npm run dev
```

`npm run assets` downloads champion, trait, augment and item icons from Riot's Data Dragon into `public/tft/` (about 100 MB, gitignored); re-run it after a patch. The dev server runs on http://localhost:5173 and proxies `/api` to the API server.

### 5. Data

The site needs ingested matches for meta stats. Build the CLI once:

```bash
go build -o bin/ingestcli ./cmd/ingestcli
```

Then, with `DATABASE_URL` and `RIOT_API_KEY_FILE` set as above:

```bash
bin/ingestcli seed-leaderboard --platform na1
bin/ingestcli crawl-queue
bin/ingestcli aggregate
```

Each `crawl-queue` run is capped at 100 Riot requests to stay within personal-key limits; run it repeatedly to grow the dataset. To backfill patch notes for earlier patches of the current set:

```bash
bin/ingestcli sync-setdata --versions 16.17,16.18,latest
```

## Deployment (Docker)

The production stack is [deploy/docker-compose.prod.yml](deploy/docker-compose.prod.yml): Postgres, a one-shot migration job, the API server, the frontend (nginx serving the built site and proxying `/api`), and an optional ingestion worker. Only the frontend is published; put a TLS-terminating proxy or load balancer in front of it for HTTPS.

| Image | Built from | Contents |
|---|---|---|
| `tft-platform-backend` | [Dockerfile](Dockerfile) | `api` (default command), `ingestcli`, `aggregator`, `ingest-loop` (~40 MB) |
| `tft-platform-frontend` | [frontend/Dockerfile](frontend/Dockerfile) | Built site + game icons on nginx (~150 MB; `FETCH_ASSETS=0` skips the icons) |
| `tft-platform-migrate` | [deploy/migrate/Dockerfile](deploy/migrate/Dockerfile) | golang-migrate with the migrations baked in |

On the server, from a checkout of this repo:

```bash
cp deploy/.env.example deploy/.env
```

Edit `deploy/.env`: set `POSTGRES_PASSWORD`, and set `APP_UID` / `APP_GID` to the output of `id -u` / `id -g`. The backend containers run as that user so they can read the Riot key while it stays owner-only.

```bash
mkdir -p deploy/.secrets
echo "RGAPI-your-key-here" > deploy/.secrets/riot-api-key.txt
chmod 600 deploy/.secrets/riot-api-key.txt
docker compose -f deploy/docker-compose.prod.yml --env-file deploy/.env up -d --build
```

The site is then on port 80 (`HTTP_PORT` changes it). Migrations run automatically before the API starts, and the API syncs set data on startup. Replace an expired Riot key by rewriting the key file; no restart needed.

Optional, once:

```bash
# Backfill earlier patches of the current set, so patch notes have history.
docker compose -f deploy/docker-compose.prod.yml --env-file deploy/.env run --rm api ingestcli sync-setdata --versions 16.17,16.18,latest
```

To keep match data and meta stats fresh, start the ingestion worker. It re-seeds the ladders every 6 hours and crawls a bounded batch every 5 minutes; tune it with the `INGEST_*` settings in `.env`:

```bash
docker compose -f deploy/docker-compose.prod.yml --env-file deploy/.env --profile ingest up -d
```

To update after pulling new code, run the same `up -d --build` command.

## Configuration

| Variable | Default | Purpose |
|---|---|---|
| `DATABASE_URL` | — (required) | Postgres connection string |
| `RIOT_API_KEY_FILE` | — | File holding the Riot API key (preferred) |
| `RIOT_API_KEY` | — | Key as a value, if no file is used (needs a restart to rotate) |
| `RIOT_APP_RATE_LIMIT_PER_SEC` | `20` | App rate limit, per second |
| `RIOT_APP_RATE_LIMIT_PER_2MIN` | `100` | App rate limit, per 2 minutes |
| `HTTP_ADDR` | `:8080` | API server listen address |
| `SETDATA_SYNC_INTERVAL` | `6h` | How often the API server re-syncs set data (`0` disables) |

The rate-limit defaults match a personal key; raise them for a production key.

## API

| Endpoint | Description |
|---|---|
| `GET /api/v1/players/{region}/{name}/{tag}` | Player profile |
| `GET /api/v1/players/{puuid}/matches?region=` | Recent matches; with `region`, syncs from Riot in the background |
| `GET /api/v1/matches/{matchId}` | Match detail |
| `GET /api/v1/leaderboard/{platform}` | Apex ladder, refreshed when stale |
| `GET /api/v1/meta/builds?set=&queue=&level=` | Per-unit stats with exact 3-item builds (seen 3+ times) and most-used items |
| `GET /api/v1/meta/{units,traits,augments}?set=` | Precomputed aggregate tables (from `aggregate`; no longer used by the UI) |
| `GET /api/v1/explore?set=&unit=&item=&trait=&queue=&level=` | Explorer stats; `unit=ID[*minStar][:item,item]`, `trait=ID[*min[-max]]` (unit count, e.g. `*4-5` for an exact tier), `level=8-` (repeat `unit`/`item`/`trait`/`queue`) |
| `GET /api/v1/explore/options?set=` | Queues, units, items, traits and levels present in a set's matches |
| `GET /api/v1/sets/{set}/data[?version=]` | Set units, traits, augments, items, Wisps |
| `GET /api/v1/sets/{set}/patches` | Generated patch notes |
| `GET /healthz` | Health check |

## Tests

```bash
go test ./...
```

Tests that need Postgres are skipped unless `TEST_DATABASE_URL` is set; each such test gets its own throwaway schema:

```bash
TEST_DATABASE_URL="postgres://tft:tft@localhost:5432/tft_test?sslmode=disable" go test -race ./...
```

## Data sources

- **Riot Games API** — players, leaderboards and matches.
- **[Data Dragon](https://developer.riotgames.com/docs/lol#data-dragon)** — icons.
- **[CommunityDragon](https://www.communitydragon.org/)** — set data extracted from the game files (units, traits, augments, items, set pools).
- **[tactics.tools](https://tactics.tools/)** — ability and item text for the current patch. Set 18's new client doesn't expose these numbers in the exported game files. Credited on every description that uses it.
- **[Little Buddy Bot](https://www.littlebuddybot.com/)** — augment reward tables, hand-transcribed (facts only) and credited per augment.

## Legal

TFT Stats was created under Riot Games' "Legal Jibber Jabber" policy using assets owned by Riot Games. Riot Games does not endorse or sponsor this project.
