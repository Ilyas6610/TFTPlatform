package apiserver

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"time"

	"tft-platform/internal/ingest"
	"tft-platform/internal/riotapi"
	"tft-platform/internal/store"
)

const (
	// matchHistoryStaleAfter is how long after a sync a profile view
	// triggers another one (1 request, plus 1 per new match).
	matchHistoryStaleAfter = 2 * time.Minute
	// matchHistorySyncCount is how many recent matches a sync covers — one
	// profile page's worth.
	matchHistorySyncCount = 20
	matchSyncTimeout      = 2 * time.Minute
	matchSyncCooldown     = time.Minute
	// matchHistoryMaxOffset bounds paging back through a history (Riot keeps
	// about a thousand recent match ids; a profile rarely needs more).
	matchHistoryMaxOffset = 500
)

type PlayerMatchesResponse struct {
	// Matches is the requested page, newest first, with each final board.
	Matches []store.PlayerMatch `json:"matches"`
	// HasMore is set when an older page may exist: this page is full, or
	// its missing games are still being fetched from Riot.
	HasMore bool `json:"hasMore"`
	// SyncedAt is when this player's history was last synced from Riot, or
	// null if never.
	SyncedAt *string `json:"syncedAt"`
	// Refreshing is set while a sync is running in the background; clients
	// should re-fetch to pick up new matches.
	Refreshing bool `json:"refreshing"`
	// Stale is set when the last sync attempt failed; StaleReason says why.
	Stale       bool   `json:"stale"`
	StaleReason string `json:"staleReason,omitempty"`
}

func matchSyncKey(puuid string) string {
	return "matches:" + puuid
}

func olderMatchesKey(puuid string, offset int) string {
	return "matches-older:" + puuid + ":" + strconv.Itoa(offset)
}

// handlePlayerMatches serves GET /api/v1/players/{puuid}/matches
// [?offset=0&limit=20&region=<platform>]. Ingested matches are always served
// from Postgres, newest first, offset games skipped. With region, the first
// page also kicks off a background sync of the newest games when the
// history wasn't synced within matchHistoryStaleAfter, and an older page
// that isn't fully stored kicks off a background fetch of that page from
// Riot (see backgroundJobs); clients re-fetch while refreshing. Without
// region the response is cache-only.
func (s *Server) handlePlayerMatches(w http.ResponseWriter, r *http.Request) {
	puuid := r.PathValue("puuid")
	ctx := r.Context()
	// Checked before the store or the background job table see it, so junk
	// is never queued for crawling or kept in memory.
	if !validPUUID(puuid) {
		writeError(w, http.StatusBadRequest, "invalid_puuid", "puuid must be 1-100 letters, digits, '-' or '_'")
		return
	}

	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	offset := 0
	if v := r.URL.Query().Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || n > matchHistoryMaxOffset {
			writeError(w, http.StatusBadRequest, "invalid_offset", "offset must be 0 to "+strconv.Itoa(matchHistoryMaxOffset))
			return
		}
		offset = n
	}

	matches, err := s.Store.PlayerMatches(ctx, puuid, offset, limit)
	if err != nil {
		writeDBError(w, r, err)
		return
	}

	resp := PlayerMatchesResponse{Matches: matches}
	key := matchSyncKey(puuid)
	if offset > 0 {
		key = olderMatchesKey(puuid, offset)
	}

	syncedAt, err := s.Store.MatchHistorySyncedAt(ctx, puuid)
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	if region := r.URL.Query().Get("region"); region != "" {
		platform := riotapi.PlatformRegion(region)
		if _, err := riotapi.RoutingForPlatform(platform); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_region", err.Error())
			return
		}
		switch {
		case offset > 0 && len(matches) < limit:
			// An older page not fully stored: fetch that page of Riot's ids.
			// A finished run reports incomplete, which holds off a rerun for
			// matchSyncCooldown, so a page Riot has nothing more for isn't
			// re-requested by every poll.
			resp.Refreshing = s.jobs.start(key, matchSyncTimeout, matchSyncCooldown, func(ctx context.Context) (bool, error) {
				_, err := ingest.SyncOlderMatches(ctx, s.Riot, s.Store, platform, puuid, offset, limit)
				return false, err
			})
		case offset == 0 && (syncedAt == nil || time.Since(*syncedAt) >= matchHistoryStaleAfter):
			resp.Refreshing = s.jobs.start(key, matchSyncTimeout, matchSyncCooldown, func(ctx context.Context) (bool, error) {
				result, err := ingest.SyncPlayerMatches(ctx, s.Riot, s.Store, platform, puuid, matchHistorySyncCount)
				if err == nil && result.Stopped != "" {
					log.Printf("match sync %s: stopped early: %s", puuid, result.Stopped)
				}
				return result.Stopped == "", err
			})
		}
	}
	if !resp.Refreshing {
		resp.Refreshing = s.jobs.isRunning(key)
	}
	if err := s.jobs.lastError(key); err != nil && !resp.Refreshing {
		resp.Stale = true
		resp.StaleReason = riotErrorCode(err)
	}
	if syncedAt != nil {
		t := syncedAt.Format(time.RFC3339)
		resp.SyncedAt = &t
	}

	resp.HasMore = len(matches) == limit || resp.Refreshing
	writeJSON(w, http.StatusOK, resp)
}
