package apiserver

import (
	"net/http"
	"strconv"
	"time"
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

// handleLeaderboard serves GET /api/v1/leaderboard/{platform}. This is
// pipeline-fed only — no live Riot fallback, since a leaderboard is
// inherently a background-ingestion feature (see internal/ingest/seeder.go).
// Each entry's fetchedAt lets the frontend show "as of" staleness rather
// than presenting a snapshot as if it were live.
func (s *Server) handleLeaderboard(w http.ResponseWriter, r *http.Request) {
	platform := r.PathValue("platform")

	limit := 200
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
	}

	entries, err := s.Store.GetLeaderboard(r.Context(), platform, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}

	out := make([]LeaderboardEntryResponse, len(entries))
	for i, e := range entries {
		out[i] = LeaderboardEntryResponse{
			PUUID:        e.PUUID,
			GameName:     e.GameName,
			TagLine:      e.TagLine,
			Tier:         e.Tier,
			Rank:         e.Rank,
			LeaguePoints: e.LeaguePoints,
			Wins:         e.Wins,
			Losses:       e.Losses,
			FetchedAt:    e.FetchedAt.Format(time.RFC3339),
		}
	}

	writeJSON(w, http.StatusOK, out)
}
