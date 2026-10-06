package apiserver

import (
	"net/http"
	"sync"
	"time"

	"tft-platform/internal/riotapi"
	"tft-platform/internal/setdata"
	"tft-platform/internal/store"
)

// Server holds dependencies shared by every handler. Exactly one *riotapi.Client
// (and its RateLimiter) is constructed at process startup and injected here —
// handlers must never construct their own, so live-fetch traffic and the
// ingestion pipeline can never jointly exceed Riot's rate limits.
type Server struct {
	Riot  *riotapi.Client
	Store *store.Store
	// Source is where public game data is downloaded from (CommunityDragon);
	// nil means the real one. Tests point it at a fake.
	Source *setdata.Source

	leaderboard leaderboardSync
	jobs        backgroundJobs
	// StatsCacheTTL is how long stats computed from match data (meta,
	// explorer, set options) are reused before the database is queried
	// again; 0 means DefaultStatsCacheTTL. New matches arrive in crawl
	// batches, so a slightly old figure costs little and saves heavy scans.
	StatsCacheTTL time.Duration
	// Shared, when set, holds those stats for every API replica (Redis); nil
	// keeps each replica's results in its own memory. See shared_cache.go.
	Shared SharedCache

	cacheInit   sync.Once
	sharedDown  sharedBreaker
	statsTTL    time.Duration // StatsCacheTTL after defaulting
	meta        resultCache   // meta builds and comps, advisor inputs
	stats       resultCache   // explorer, explorer options, precomputed meta tables
	sets        resultCache   // set data, patch notes, planner codes
	live        liveFetches
	plannerFail failureMemo
}

func (s *Server) statsTTLValue() time.Duration { return s.statsTTL }

// DefaultStatsCacheTTL is the default for Server.StatsCacheTTL.
const DefaultStatsCacheTTL = 10 * time.Minute

func NewRouter(s *Server) http.Handler {
	// Tests build a router per request, so set the lifetimes only once.
	s.cacheInit.Do(func() {
		ttl := s.StatsCacheTTL
		if ttl <= 0 {
			ttl = DefaultStatsCacheTTL
		}
		s.statsTTL = ttl
		local := ttl
		if s.Shared != nil && local > localSharedTTL {
			local = localSharedTTL // the shared entry's TTL governs freshness
		}
		s.meta.ttl, s.stats.ttl = local, local
	})
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
	mux.HandleFunc("GET /api/v1/explore/suggest", s.handleExploreSuggest)
	mux.HandleFunc("GET /api/v1/explore/options", s.handleExploreOptions)
	mux.HandleFunc("GET /api/v1/sets/{set}/data", s.handleSetData)
	mux.HandleFunc("GET /api/v1/sets/{set}/patches", s.handleSetPatches)
	mux.HandleFunc("GET /api/v1/sets/{set}/planner-codes", s.handlePlannerCodes)
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	return mux
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
