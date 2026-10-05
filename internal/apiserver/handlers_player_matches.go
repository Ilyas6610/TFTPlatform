package apiserver

import (
	"net/http"
	"strconv"
	"time"
)

type PlayerMatchSummaryResponse struct {
	MatchID      string `json:"matchId"`
	GameDatetime string `json:"gameDatetime"`
	TFTSetNumber int    `json:"tftSetNumber"`
	Placement    int    `json:"placement"`
	Level        int    `json:"level"`
}

// handlePlayerMatches serves GET /api/v1/players/{puuid}/matches.
// Postgres-only: an empty slice (not an error) means nothing has been
// ingested yet for this player, which the frontend renders as
// "no matches yet" rather than triggering a slow live crawl.
func (s *Server) handlePlayerMatches(w http.ResponseWriter, r *http.Request) {
	puuid := r.PathValue("puuid")

	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}

	matches, err := s.Store.GetRecentMatchesForPUUID(r.Context(), puuid, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}

	out := make([]PlayerMatchSummaryResponse, len(matches))
	for i, m := range matches {
		out[i] = PlayerMatchSummaryResponse{
			MatchID:      m.MatchID,
			GameDatetime: m.GameDatetime.Format(time.RFC3339),
			TFTSetNumber: m.TFTSetNumber,
			Placement:    m.Placement,
			Level:        m.Level,
		}
	}
	writeJSON(w, http.StatusOK, out)
}
