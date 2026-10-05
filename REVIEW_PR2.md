# Review of PR #2: Meta tab (builds, comps, explorer cleanup)

Verdict: **approve with suggestions**. Nothing blocking.

Checked: read all backend code (`internal/comps`, `internal/store/meta.go`, `explore.go` changes, new handlers) and skimmed the frontend page and links for XSS and unsafe links; `go vet` clean; `go test -race ./...` passes against Postgres; `tsc -b` passes. The UI was not clicked through.

## What looks good
- No SQL injection: every filter value is a bound parameter; only internal constants are joined into queries.
- The duplicate-item condition in `whereClause` (counting two copies of one item) is correct and applies once per distinct item.
- `/meta/builds` and `/meta/comps` reject unsupported filters with a 400 instead of ignoring them silently.
- No raw-HTML rendering in the frontend.

## Issues

### 1. Per-request cost grows with data (medium)
- `GET /api/v1/meta/comps` (`FinalBoards` + `comps.Build`) loads every level 8+ board into memory, parses the JSON in Go, and clusters on every request. Clustering is O(distinct boards x clusters).
- `GET /api/v1/meta/builds` (`MetaBuilds`) runs four JSONB-scanning queries per request.
- Neither is cached and both are public and unauthenticated, so they are cheap for a caller to hammer. At 3,204 participants it is instant, but it will get slow as the database grows.
- **Suggestion:** cache the result for a few minutes keyed by (set, queue), or precompute it in the aggregate / `riotsync` step.

### 2. Explorer links are not URL-encoded (low)
- `exploreLink` ([MetaStatsPage.tsx:104](frontend/src/pages/MetaStatsPage.tsx)) and its callers put unit, item and trait IDs straight into the query string. Safe for today's alphanumeric IDs, but fragile.
- **Suggestion:** build the query with `URLSearchParams` or `encodeURIComponent`.

### 3. No handler test for `/meta/comps` (low)
- `internal/comps` has unit tests and `/meta/builds` has a handler test, but the comps route itself is untested.
- **Suggestion:** add a small test covering filter rejection (`unit`, `item`, `trait`, `level`) and the response shape.

### 4. Raw database errors returned to clients (low)
- The new handlers return `err.Error()` on failure, as the existing ones do. This is the same issue as M2 in `SECURITY_REVIEW_PLAN.md`, so it can be fixed together with that.

### 5. Minor
- The `commonBuild` doc comment says "3+ copies" while the code hard-codes 3; use a named constant.
- `append(args, minBuildGames)` in `MetaBuilds` can share a backing array with `args`. Harmless here, but a copy would be safer.
