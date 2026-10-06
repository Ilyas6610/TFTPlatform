package apiserver

import (
	"net/http"

	"tft-platform/internal/comps"
	"tft-platform/internal/store"
)

const (
	// playerStatsRows bounds each breakdown (units, items) on the profile.
	playerStatsRows = 15
	// playerMaxComps bounds the player's own comps.
	playerMaxComps = 6
)

// PlayerStatsResponse is a player's results in one set (and optional queue
// and level scope), from their stored games.
type PlayerStatsResponse struct {
	// Sets the player has stored games in, newest first (for a set picker).
	Sets []int `json:"sets"`
	// Summary and its placement histogram are the player's; Baseline is
	// everyone's in the same scope, for comparison.
	Summary  store.ExploreSummary        `json:"summary"`
	Baseline store.PlacementStats        `json:"baseline"`
	Queues   []store.PlayerQueueStats    `json:"queues"`
	Units    []store.ExploreBreakdownRow `json:"units"`
	Items    []store.ExploreBreakdownRow `json:"items"`
	Traits   []store.ExploreBreakdownRow `json:"traits"` // active trait tiers
	// Comps groups the player's level 8+ boards like the Meta page does,
	// keeping comps they ran at least twice.
	Comps []comps.Comp `json:"comps"`
}

// handlePlayerStats serves GET /api/v1/players/{puuid}/stats?set=18
// [&queue=1100][&level=8-]: the player's placement stats, histogram, queue
// split, most-played units, items and traits, and their comps, over their
// stored games. Computed live; it never calls Riot (the match history
// endpoint fetches games).
func (s *Server) handlePlayerStats(w http.ResponseWriter, r *http.Request) {
	puuid := r.PathValue("puuid")
	if !validPUUID(puuid) {
		writeError(w, http.StatusBadRequest, "invalid_puuid", "puuid must be 1-100 letters, digits, '-' or '_'")
		return
	}
	q := r.URL.Query()
	for _, k := range []string{"unit", "item", "trait"} {
		if q.Has(k) {
			writeError(w, http.StatusBadRequest, "invalid_filter", k+" isn't supported here")
			return
		}
	}
	f, err := parseExploreFilter(q)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_filter", err.Error())
		return
	}
	f.PUUID = puuid
	ctx := r.Context()

	sets, err := s.Store.PlayerSets(ctx, puuid)
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	ex, err := s.Store.Explore(ctx, f, playerStatsRows)
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	queues, err := s.Store.PlayerQueues(ctx, puuid, f.Set)
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	boards, err := s.Store.FinalBoards(ctx, f, compMinLevel)
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	// A single player's history is small, so a comp needs only two games,
	// and one more game makes a variant.
	playerComps := comps.Build(boards, comps.Options{MinBoards: 2, MinVariantBoards: 1, MaxVariants: 3})
	if len(playerComps) > playerMaxComps {
		playerComps = playerComps[:playerMaxComps]
	}
	if playerComps == nil {
		playerComps = []comps.Comp{}
	}

	writeJSON(w, http.StatusOK, PlayerStatsResponse{
		Sets: sets, Summary: ex.Summary, Baseline: ex.Baseline, Queues: queues,
		Units: ex.Units, Items: ex.Items, Traits: ex.Traits, Comps: playerComps,
	})
}
