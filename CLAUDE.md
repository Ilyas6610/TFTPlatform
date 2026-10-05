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
npm run dev      # Vite dev server on :5173, proxies /api to localhost:8080 (API_URL=http://localhost:18080 npm run dev to use another backend)
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
- **riotsync** — background service (`internal/riotsync`) that pulls Riot data into Postgres on a schedule: leaderboard seeding, bounded `CrawlQueue` batches, Riot ID resolution and meta-stat aggregation, run one task at a time under its own single `RateLimiter` plus a `Throttle` transport capping its traffic at `RIOTSYNC_RATE_LIMIT_PER_SEC`/`_PER_2MIN` (default 10/s, 50 per 2 min) because it shares the key with the API server and the client's limiter syncs to the key's full budget. Runs are recorded in `ingest_runs`; a Postgres advisory lock allows one instance per database; it pauses after an expired key or rate-limit stop. Tuned with `RIOTSYNC_*` env vars (`RIOTSYNC_PLATFORMS`, `RIOTSYNC_SEED_INTERVAL`, `RIOTSYNC_CRAWL_INTERVAL`, `RIOTSYNC_NAMES_INTERVAL`, `RIOTSYNC_AGGREGATE_INTERVAL`, `RIOTSYNC_CRAWL_*`, `RIOTSYNC_NAMES_BATCH`, `RIOTSYNC_KEY_RETRY`). Pauses for Riot's `Retry-After` after a 429. Invalid `RIOTSYNC_*` values fail startup. Replaces the old ingestworker placeholder and the `ingest` compose service (use `--remove-orphans` when upgrading).

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

### Build advisor (Explorer)
`GET /api/v1/explore/suggest?set=&queue=&level=&have_unit=…&have_item=…` (`handlers_suggest.go`, pure logic in `internal/suggest`) answers "what can I build with what I hold right now". Inputs are the player's units and inventory (one `have_item` per copy; completed items or components). It takes the exact 3-item builds per unit (`store.MetaBuilds`, wider than the Meta page's) and comps (`comps.Build`, shared cache with `/meta/comps`) from real boards, then: `craft` decides which build items the inventory covers, from a whole item or two components (recipes = `Composition` of the newest set-data snapshot, cached; without a snapshot only whole items match), never spending a piece twice; `plan` greedily gives each unit its best-fitting build without double-spending items; `units` are per-unit alternatives against the whole inventory; `comps` are comps ranked by owned units on their board, with item fits. Units alone list their best builds (nothing makeable yet = aim-for builds, all missing); items alone (no `have_unit`) return `candidates`, the units whose builds the inventory fits best. Emblems are recipes too (Spatula/Frying Pan + component); artifacts only count when held whole. Frontend: `components/BuildAdvisor.tsx` in the Explorer; its lists are the `hu`/`hi` URL params.

### Data freshness (request-triggered Riot fetches)
- **Leaderboard** — `GET /api/v1/leaderboard/{platform}` re-seeds the platform from Riot (3 requests, `ingest.SeedLeaderboard`) when its snapshot is older than 2 minutes; a complete seed prunes players who left master+. Missing Riot IDs on the returned page are resolved in the background via account-v1 by-PUUID (`ingest.ResolveNames`). Refresh serialization is in `internal/apiserver/leaderboard_sync.go`.
- **Match history** — `GET /api/v1/players/{puuid}/matches?region=<platform>` starts a background sync (`ingest.SyncPlayerMatches`: ids + any unsaved matches from the last 20) when the player's history wasn't synced in the last 2 minutes. Sync time is `ingest_puuid_queue.last_crawled_at`, so viewed players also join the crawl queue. Without `region` the endpoint is cache-only.
- Background runs go through `backgroundJobs` (`internal/apiserver/background.go`): one run per key at a time, a 1-minute cooldown after incomplete/failed runs, and the last error surfaced as `stale`/`staleReason`. Responses carry `resolving`/`refreshing` flags; the frontend polls every 3s while set.

### Set data and generated patch notes
`internal/setdata` downloads CommunityDragon's TFT export (`raw.communitydragon.org/<channel>/cdragon/tft/en_us.json`, ~25 MB; channel is `latest` or a patch archive like `16.18`), extracts the set's standard-mode entry (`TFTSet<N>`), renders description templates (`@Var@`, `@Var*100@`, `%i:scaleAP%`, `<row>` breakpoints) to plain text, and stores the result in `set_data_snapshots`, one row per content version. Patch notes are not scraped: `GET /api/v1/sets/{set}/patches` diffs each snapshot against the previous one (`setdata.Diff`). `GET /api/v1/sets/{set}/data[?version=]` serves a snapshot.
- Updating: the API server syncs `latest` at startup and every `SETDATA_SYNC_INTERVAL`; `ingestcli sync-setdata [--set N] --versions 16.17,16.18,latest` syncs or backfills manually. A sync skips versions already stored and builds whose data is identical to the previous snapshot.
- Variables CommunityDragon couldn't name are keyed by FNV-1a of the lowercased name (`{b027c2f9}`); `namedValues` maps them back using names found in the description.
- Set 18 moved champion spell math and augment reward logic into the new client; neither is in CommunityDragon's export or `map22.bin` (67 of 69 Set 18 spells have only placeholder `DataValue`/`OtherValue`, on live and PBE). Ability text comes from tactics.tools instead (`overrides.go`): `ap.tft.tools/static/s<N>/en.js` holds fully rendered tooltips keyed `<apiName>_desc`; it's fetched with each set data sync into `set_text_overrides`, tagged with the newest snapshot version, and applied at serve time only to that version and only where our text has unknowns, with `descSource` attribution. Remaining unresolved variables render as `[[Label]]` (`unknownValue`, e.g. `@MagicDamageCalc1@` -> `[[Magic Damage]]`), which the frontend styles as an unknown-value chip. Unit stats and trait/augment/item values are exact.
- Live in-game values have no static text: League-client counters (`@TFTUnitProperty...@`) and the new client's runtime placeholders (`{ItemTags.Deathblade.DeadlierBladeStacks}`, `{Augment.Variant.MagicRoll.Reward}`; dotted paths only, so CommunityDragon's `{0f90e7a4}` hashes never match). `stripTrackers` drops parentheticals around them and lines where one is a label's value, and otherwise removes just the placeholder.
- Augment reward tables (`internal/setdata/rewards/set<N>.json`, embedded) are hand-transcribed from Little Buddy Bot because Set 18's reward logic isn't in any export. They cite a per-augment `sourceUrl` and `updated` date, key on explicit `apiNames` (the set's own `DA_*` augments), and are attached when serving (`AttachRewards`), never stored in snapshots. Only facts are transcribed (never their images); icons that couldn't be identified are marked with a footnote instead of guessed. The loader validates the file at startup (row/column counts, duplicates, required source fields).
- The site footer carries Riot's required "Legal Jibber Jabber" notice; keep it on every page.
- Set pools come from Riot's set definition, not name heuristics (`pools.go`): `map22.bin.json` (~70 MB, streamed) has `TFTSetData` `TFTSet<N>` -> `itemLists`; the set's own lists are `Set18_Items` (156 items), `Set18_Items_Augments` (250) and `TFTSet18_Items_Charms` (343 Wisps), while shared lists (`Common_Items`, which holds debug items, legacy copies and other modes' items) are ignored. A sync fails if pools can't be read, so all snapshots stay extracted the same way. Set 18's own items have no description in the export; they're kept and get text from tactics.tools overrides. Without pools (older sets) the heuristics below apply.
- Augments (no-pools fallback): when a set has `DA_*` augments, only those are kept (`dedupeAugments`). The ~340 legacy `TFT*_Augment_*` entries in Set 18's list aren't live — every 18.2 official augment change landed on the `DA_` id while legacy copies stayed frozen, and Cursed Crown's removal (18.3) dropped only its `DA_` id. Patch notes pair entities by id, then by unique name (catching id swaps; only fields both sides define are compared), and report renames.
- Generated patch notes miss balance changes not reflected in the exported files (most of 18.3's), so each patch links Riot's official notes (`TFTPatch`, `OfficialNotesURL`; add a set's first game patch to `firstGamePatch` when it launches).
- Riot's set augment list also carries legacy copies (`TFT_Augment_MagicRoll` "A Magic Roll" next to `DA_MagicRoll` "Magic Roll"); `dedupeAugments` drops a legacy augment when a set-specific one has the same name (case-insensitive, ignoring a leading article). Same-named set-specific augments are distinct variants and are kept.
- Set 18's remaining `DA_*` items are Wisps (the set mechanic) and live in `SetData.Wisps` / patch-note category `wisp`, not `Items`.
- Snapshots hold extracted data, not the raw export, so changing the extraction (new fields, filters) makes old and new snapshots differ in shape and produces spurious diffs. After such a change, delete the set's rows from `set_data_snapshots` and backfill again with `sync-setdata`.

### Stats explorer
`GET /api/v1/explore` (`handlers_explore.go`, `store/explore.go`) filters player boards (`match_participants` joined to `matches`) by AND-ed conditions: units (`unit=ID[*minStar][:I1,I2]`; star and items must hold on the *same* unit copy), items anywhere, traits by `num_units` (`trait=ID[*min[-max]]`; an exact tier is `min..nextBreakpoint-1`, e.g. Juggernaut tier 2 = `*4-5`), queues and level range. It returns summary + placement histogram, a baseline (same set/queues/levels without board conditions), breakdowns of units/items/active trait tiers on matching boards (each board counted once), and per unit condition the items held and best star level on matching copies (each board counted once, so counts line up with Boards everywhere). The UI groups trait rows by trait with one row per tier; tier N maps to the set data's Nth breakpoint (`tier_current` counts breakpoints reached; repeated breakpoints like Rival's `1, 1, 2` are clamped). All values are bound parameters; ids must match `^[A-Za-z0-9_]{1,80}$`, max 6 conditions per kind. JSONB columns go through `arrayOr` so a malformed row can't fail a query; unit conditions add a `units @> ...` containment test served by the GIN index from migration 0009. `/explore/options` lists what occurs in a set's data so the UI only offers matchable conditions. The page (`ExplorerPage.tsx`) keeps the search in its URL in the API's own format and waits for options (which pick the default ranked queue) before searching. Augments and Set 18 damage aren't filterable: Riot's match data has no augments and reports 0 damage.

### Meta page
`GET /api/v1/meta/builds` (`store/meta.go`) computes, live over the same scope as the explorer (set/queues/levels; board conditions are rejected), each unit's stats and pick rate, its most common exact builds (the unit copy's three items as a sorted multiset, duplicates kept; one count per board; only builds seen `metaMinBuildGames`+ times) and its most-held items. The page (`MetaStatsPage.tsx`) shows unit cards (cost filter, search, sort by games or by average with a 10-game minimum) and a Traits tab reusing the explorer's tier-grouped table (`components/stats.tsx`, shared with `ExplorerPage.tsx`). Builds link to the explorer as `unit=ID:I1,I2,I3`; explorer unit-item conditions count duplicates (two Archangel's must both be present) so those numbers match. The old precomputed `meta_*_stats` tables/endpoints remain (the `aggregate` job still fills them) but the UI no longer uses them.

### Comps
`GET /api/v1/meta/comps` loads final boards (player level 8+, set/queue scope; `store.FinalBoards`) and groups them with `internal/comps` (pure Go, no DB): distinct exact unit sets are taken most-played first, and each joins the comp whose *anchor* board it overlaps most (Jaccard >= 0.6) or anchors a new one, so every comp is built around a board people actually ran. Per comp: stats and play rate, the anchor board with each unit's most common star level and most common real item set (3+ copies and 5%+ of copies; tanks with scattered items show none), stats for boards that ran exactly the anchor, variants (other exact boards in the comp, as add/remove swaps — "level up: +X" when only adding) and flex units (off-anchor units on 15%+ of the comp's boards). Comps under 5 boards are dropped. `/meta/builds` and `/meta/comps` scan every board in scope, so results are cached per (kind, set, queues, levels) for 5 minutes (`metaCache` in `internal/apiserver/meta_cache.go`: concurrent misses share one computation, errors aren't cached, at most 64 entries; queue params are capped at 6). Database failures in these and the explorer handlers go through `writeDBError`, which logs the error and returns a generic message. The Meta page's default tab shows them; names come from traits the anchor invests in (tier 2+ or 3+ units), else its itemized carries.

The explorer hides results until a condition is set (otherwise it repeats Meta) and leaves condition rows (chosen units, required items, chosen traits) out of its breakdowns.

### Key invariant: one rate limiter per process
Exactly one `riotapi.Client`/`RateLimiter` is built at startup and injected (e.g. into `apiserver.Server`). Handlers and other code must never construct their own client, so live fetches and ingestion can't jointly exceed Riot's rate limits.

### Deploy
`deploy/docker-compose.yml` is local dev (Postgres + migrations). `deploy/docker-compose.prod.yml` is the production stack: images from `Dockerfile` (backend: `api`/`ingestcli`/`aggregator`/`riotsync`; `scripts/ingest-loop.sh` is the older shell-loop alternative to riotsync), `frontend/Dockerfile` (nginx; `nginx.conf.template` proxies `/api` to `API_UPSTREAM`; icons fetched at build unless `FETCH_ASSETS=0`) and `deploy/migrate/Dockerfile` (migrations baked in). The Riot key is a Compose secret mounted with host permissions, so backend containers run as `APP_UID:APP_GID` (the key file's owner) and the key stays `chmod 600`. Dockerfiles avoid BuildKit-only syntax (no buildx on the dev machine).
