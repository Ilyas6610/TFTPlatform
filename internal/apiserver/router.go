package apiserver

import (
	"net/http"

	"tft-platform/internal/riotapi"
	"tft-platform/internal/store"
)

// Server holds dependencies shared by every handler. Exactly one *riotapi.Client
// (and its RateLimiter) is constructed at process startup and injected here —
// handlers must never construct their own, so live-fetch traffic and the
// ingestion pipeline can never jointly exceed Riot's rate limits.
type Server struct {
	Riot  *riotapi.Client
	Store *store.Store

	leaderboard leaderboardSync
	jobs        backgroundJobs
	meta        resultCache
	sets        resultCache
	live        liveFetches
}

func NewRouter(s *Server) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/players/{region}/{name}/{tag}", s.handlePlayerProfile)
	mux.HandleFunc("GET /api/v1/players/{puuid}/matches", s.handlePlayerMatches)
	mux.HandleFunc("GET /api/v1/matches/{matchId}", s.handleMatchDetail)
	mux.HandleFunc("GET /api/v1/leaderboard/{platform}", s.handleLeaderboard)
	mux.HandleFunc("GET /api/v1/meta/units", s.handleMetaUnits)
	mux.HandleFunc("GET /api/v1/meta/traits", s.handleMetaTraits)
	mux.HandleFunc("GET /api/v1/meta/augments", s.handleMetaAugments)
	mux.HandleFunc("GET /api/v1/meta/builds", s.handleMetaBuilds)
	mux.HandleFunc("GET /api/v1/meta/comps", s.handleMetaComps)
	mux.HandleFunc("GET /api/v1/explore", s.handleExplore)
	mux.HandleFunc("GET /api/v1/explore/options", s.handleExploreOptions)
	mux.HandleFunc("GET /api/v1/sets/{set}/data", s.handleSetData)
	mux.HandleFunc("GET /api/v1/sets/{set}/patches", s.handleSetPatches)
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	return mux
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
