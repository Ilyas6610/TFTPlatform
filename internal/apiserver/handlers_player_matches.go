package apiserver

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"time"

	"tft-platform/internal/ingest"
	"tft-platform/internal/riotapi"
)

type PlayerMatchSummaryResponse struct {
	MatchID      string `json:"matchId"`
	GameDatetime string `json:"gameDatetime"`
	TFTSetNumber int    `json:"tftSetNumber"`
	Placement    int    `json:"placement"`
	Level        int    `json:"level"`
}

const (
	// matchHistoryStaleAfter is how long after a sync a profile view
	// triggers another one (1 request, plus 1 per new match).
	matchHistoryStaleAfter = 2 * time.Minute
	// matchHistorySyncCount is how many recent matches a sync covers — one
	// profile page's worth.
	matchHistorySyncCount = 20
	matchSyncTimeout      = 2 * time.Minute
	matchSyncCooldown     = time.Minute
)

type PlayerMatchesResponse struct {
	Matches []PlayerMatchSummaryResponse `json:"matches"`
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

// handlePlayerMatches serves GET /api/v1/players/{puuid}/matches. Ingested
// matches are always served from Postgres. With ?region=<platform>, a
// history not synced within matchHistoryStaleAfter also kicks off a
// background sync from Riot (see backgroundJobs); without it the response
// is cache-only.
func (s *Server) handlePlayerMatches(w http.ResponseWriter, r *http.Request) {
	puuid := r.PathValue("puuid")
	ctx := r.Context()

	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}

	var resp PlayerMatchesResponse
	key := matchSyncKey(puuid)

	syncedAt, err := s.Store.MatchHistorySyncedAt(ctx, puuid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	if region := r.URL.Query().Get("region"); region != "" {
		platform := riotapi.PlatformRegion(region)
		if _, err := riotapi.RoutingForPlatform(platform); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_region", err.Error())
			return
		}
		if syncedAt == nil || time.Since(*syncedAt) >= matchHistoryStaleAfter {
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

	matches, err := s.Store.GetRecentMatchesForPUUID(ctx, puuid, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	resp.Matches = make([]PlayerMatchSummaryResponse, len(matches))
	for i, m := range matches {
		resp.Matches[i] = PlayerMatchSummaryResponse{
			MatchID:      m.MatchID,
			GameDatetime: m.GameDatetime.Format(time.RFC3339),
			TFTSetNumber: m.TFTSetNumber,
			Placement:    m.Placement,
			Level:        m.Level,
		}
	}
	writeJSON(w, http.StatusOK, resp)
}
