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
	// Games and Top4Rate come from Riot's league entry: in TFT its wins are
	// top 4 finishes and losses the rest, so this covers every ranked game of
	// the season. Zero games = null.
	Games    int      `json:"games"`
	Top4Rate *float64 `json:"top4Rate"`
	// Stored covers only the ranked games we have stored (1st place rate and
	// average placement aren't in Riot's league data); nil when there are
	// none. Compare Stored.Games with Games to judge how representative it is.
	Stored *StoredStatsResponse `json:"stored"`
}

type StoredStatsResponse struct {
	Games        int     `json:"games"`
	WinRate      float64 `json:"winRate"`
	Top4Rate     float64 `json:"top4Rate"`
	AvgPlacement float64 `json:"avgPlacement"`
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
		writeDBError(w, r, err)
		return
	}
	if len(entries) == 0 && refreshErr != nil {
		writeRiotError(w, refreshErr)
		return
	}

	fetchedAt, err := s.Store.LeaderboardFetchedAt(ctx, string(platform))
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	if fetchedAt != nil {
		f := fetchedAt.Format(time.RFC3339)
		resp.FetchedAt = &f
	}

	unresolved, err := s.Store.UnresolvedLeaderboardPUUIDs(ctx, string(platform), limit)
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	resp.Resolving = s.startNameResolution(platform, unresolved)

	puuids := make([]string, len(entries))
	for i, e := range entries {
		puuids[i] = e.PUUID
	}
	stored, err := s.Store.RankedBoardStats(ctx, puuids)
	if err != nil {
		writeDBError(w, r, err)
		return
	}

	for _, e := range entries {
		var top4Rate *float64
		if g := e.Wins + e.Losses; g > 0 {
			v := float64(e.Wins) / float64(g)
			top4Rate = &v
		}
		var st *StoredStatsResponse
		if x, ok := stored[e.PUUID]; ok && x.Games > 0 {
			n := float64(x.Games)
			st = &StoredStatsResponse{Games: x.Games, WinRate: float64(x.Wins) / n, Top4Rate: float64(x.Top4) / n, AvgPlacement: x.AvgPlacement}
		}
		resp.Entries = append(resp.Entries, LeaderboardEntryResponse{
			Games:        e.Wins + e.Losses,
			Top4Rate:     top4Rate,
			Stored:       st,
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
