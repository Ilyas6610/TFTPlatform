package apiserver

import (
	"log"
	"net/http"
	"strconv"
	"time"

	"tft-platform/internal/riotapi"
)

type LeaderboardEntryResponse struct {
	PUUID        string  `json:"puuid"`
	GameName     *string `json:"gameName"`
	TagLine      *string `json:"tagLine"`
	Tier         string  `json:"tier"`
	Rank         *string `json:"rank"`
	LeaguePoints int     `json:"leaguePoints"`
	Wins         int     `json:"wins"`
	Losses       int     `json:"losses"`
	FetchedAt    string  `json:"fetchedAt"`
}

type LeaderboardResponse struct {
	Platform string `json:"platform"`
	// FetchedAt is when this snapshot was pulled from Riot.
	FetchedAt *string `json:"fetchedAt"`
	// Stale is set when a due refresh failed and an older snapshot is being
	// served; StaleReason says why (e.g. "riot_api_key_expired").
	Stale       bool   `json:"stale"`
	StaleReason string `json:"staleReason,omitempty"`
	// Resolving is set while Riot IDs for unnamed entries are being looked
	// up in the background; clients should re-fetch to pick them up.
	Resolving bool                       `json:"resolving"`
	Entries   []LeaderboardEntryResponse `json:"entries"`
}

// handleLeaderboard serves GET /api/v1/leaderboard/{platform}. The stored
// snapshot is refreshed from Riot first if it's older than
// leaderboardStaleAfter (see leaderboard_sync.go); if that refresh fails the
// cached snapshot is served with stale set. Entries without a Riot ID among
// the returned page get resolved in the background.
func (s *Server) handleLeaderboard(w http.ResponseWriter, r *http.Request) {
	platform := riotapi.PlatformRegion(r.PathValue("platform"))
	if _, err := riotapi.RoutingForPlatform(platform); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_region", err.Error())
		return
	}

	limit := 200
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
	}

	ctx := r.Context()
	resp := LeaderboardResponse{Platform: string(platform), Entries: []LeaderboardEntryResponse{}}

	refreshErr := s.refreshIfStale(ctx, platform)
	if refreshErr != nil {
		log.Printf("leaderboard %s: refresh: %v", platform, refreshErr)
		resp.Stale = true
		resp.StaleReason = riotErrorCode(refreshErr)
	}

	entries, err := s.Store.GetLeaderboard(ctx, string(platform), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	if len(entries) == 0 && refreshErr != nil {
		writeRiotError(w, refreshErr)
		return
	}

	fetchedAt, err := s.Store.LeaderboardFetchedAt(ctx, string(platform))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	if fetchedAt != nil {
		f := fetchedAt.Format(time.RFC3339)
		resp.FetchedAt = &f
	}

	unresolved, err := s.Store.UnresolvedLeaderboardPUUIDs(ctx, string(platform), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	resp.Resolving = s.startNameResolution(platform, unresolved)

	for _, e := range entries {
		resp.Entries = append(resp.Entries, LeaderboardEntryResponse{
			PUUID:        e.PUUID,
			GameName:     e.GameName,
			TagLine:      e.TagLine,
			Tier:         e.Tier,
			Rank:         e.Rank,
			LeaguePoints: e.LeaguePoints,
			Wins:         e.Wins,
			Losses:       e.Losses,
			FetchedAt:    e.FetchedAt.Format(time.RFC3339),
		})
	}

	writeJSON(w, http.StatusOK, resp)
}
