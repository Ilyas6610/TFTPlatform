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
//
// The three clients are views of that one client (riotapi.Client.WithBudget),
// each drawing from its class's share of the key (RIOT_BUDGET_SPLIT):
//   - Riot: background work a request triggers (history and rank syncs,
//     older pages, ladder refreshes, name resolution); waits for its share
//     (the rest of the on-demand share).
//   - RiotLive: lookups a request waits on (profile, match detail), on a
//     reserved third of the on-demand share so background work can't crowd
//     them out; when it's used up it fails at once with
//     riotapi.ErrBudgetExhausted, answered as 503 with Retry-After.
//   - RiotBackfill: "Load whole set" loads, on their own share.
//
// RiotLive and RiotBackfill default to Riot when nil (tests).
type Server struct {
	Riot         *riotapi.Client
	RiotLive     *riotapi.Client
	RiotBackfill *riotapi.Client
	Store        *store.Store
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
	stats       resultCache   // explorer options, scope baselines
	explore     resultCache   // explorer searches: arbitrary keys, so apart from the cheap shared entries and kept in memory
	sets        resultCache   // set data, patch notes, planner codes
	live        liveFetches
	backfill    backfills
	plannerFail failureMemo
}

func (s *Server) statsTTLValue() time.Duration { return s.statsTTL }

// exploreMaxComputes is how many different explorer searches one replica
// runs at once on a cache miss: more than the shared default of 2 because
// every distinct search misses, fewer than unbounded to protect Postgres.
const exploreMaxComputes = 4

// DefaultStatsCacheTTL is the default for Server.StatsCacheTTL.
const DefaultStatsCacheTTL = 10 * time.Minute

// LiveBudgetWait is how long a live lookup (RiotLive) may wait for the
// on-demand share before answering 503: a short wait smooths a burst, a
// long one would just hold the request.
const LiveBudgetWait = 2 * time.Second

func (s *Server) liveRiot() *riotapi.Client {
	if s.RiotLive != nil {
		return s.RiotLive
	}
	return s.Riot
}

func (s *Server) backfillRiot() *riotapi.Client {
	if s.RiotBackfill != nil {
		return s.RiotBackfill
	}
	return s.Riot
}

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
		// Searches never go to the shared cache (each distinct search is a
		// one-off that would crowd out the entries worth sharing), so they
		// keep the full lifetime locally.
		s.explore.ttl, s.explore.maxComputes = ttl, exploreMaxComputes
	})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/players/{region}/{name}/{tag}", s.handlePlayerProfile)
	mux.HandleFunc("GET /api/v1/players/{puuid}/matches", s.handlePlayerMatches)
	mux.HandleFunc("GET /api/v1/players/{puuid}/stats", s.handlePlayerStats)
	mux.HandleFunc("GET /api/v1/players/{puuid}/ranks", s.handlePlayerRankHistory)
	// GET /api/v1/players/{puuid}/advice (handlePlayerAdvice) stays
	// unregistered while the profile's advice panel is disabled: it scans
	// everyone's boards per scope, too much to expose unused.
	mux.HandleFunc("GET /api/v1/players/{puuid}/backfill", s.handlePlayerBackfill)
	mux.HandleFunc("POST /api/v1/players/{puuid}/backfill", s.handlePlayerBackfill)
	mux.HandleFunc("GET /api/v1/matches/{matchId}", s.handleMatchDetail)
	mux.HandleFunc("GET /api/v1/matches/{matchId}/lobby", s.handleMatchLobby)
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
