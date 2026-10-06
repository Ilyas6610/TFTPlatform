package apiserver

import (
	"context"
	"net/http"
	"slices"
	"sort"
	"time"

	"tft-platform/internal/comps"
	"tft-platform/internal/lp"
	"tft-platform/internal/setdata"
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
	// Patches splits the games by patch, newest first (queue scope applies,
	// level scope doesn't).
	Patches []PatchStats `json:"patches"`
}

// PatchStats is a player's results on one patch. LP is the known LP change
// over LPGames ranked games (rank snapshots don't cover every game).
type PatchStats struct {
	Patch string `json:"patch"` // "" when unknown
	store.PlacementStats
	LP      int `json:"lp"`
	LPGames int `json:"lpGames"`
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

	patches, err := s.playerPatches(ctx, puuid, f)
	if err != nil {
		writeDBError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, PlayerStatsResponse{
		Sets: sets, Summary: ex.Summary, Baseline: ex.Baseline, Queues: queues,
		Units: ex.Units, Items: ex.Items, Traits: ex.Traits, Comps: playerComps, Patches: patches,
	})
}

// playerPatches groups the player's games in f's set and queues by patch,
// with the LP change rank snapshots account for.
func (s *Server) playerPatches(ctx context.Context, puuid string, f store.ExploreFilter) ([]PatchStats, error) {
	games, err := s.Store.PlayerGamesSince(ctx, puuid, time.Time{})
	if err != nil {
		return nil, err
	}
	cal, err := s.Store.PatchCalendar(ctx)
	if err != nil {
		return nil, err
	}
	history, err := s.Store.RankHistory(ctx, puuid)
	if err != nil {
		return nil, err
	}
	changes := lp.Attribute(history, games)

	type acc struct {
		stats      PatchStats
		last       time.Time
		sum, top4s float64
		wins       int
	}
	byPatch := map[string]*acc{}
	for _, g := range games {
		if g.SetNumber != f.Set || (len(f.Queues) > 0 && !slices.Contains(f.Queues, g.QueueID)) {
			continue
		}
		patch := setdata.PatchOfGame(cal, g.SetNumber, g.GameDatetime, g.GameVersion)
		a := byPatch[patch]
		if a == nil {
			a = &acc{stats: PatchStats{Patch: patch}}
			byPatch[patch] = a
		}
		a.stats.Boards++
		a.sum += float64(g.Placement)
		if g.Placement <= 4 {
			a.top4s++
		}
		if g.Placement == 1 {
			a.wins++
		}
		if g.GameDatetime.After(a.last) {
			a.last = g.GameDatetime
		}
		if c, ok := changes[g.MatchID]; ok {
			a.stats.LP += c.Delta
			a.stats.LPGames += c.Games
		}
	}
	out := make([]PatchStats, 0, len(byPatch))
	order := map[string]time.Time{}
	for patch, a := range byPatch {
		n := float64(a.stats.Boards)
		a.stats.AvgPlacement, a.stats.Top4Rate, a.stats.WinRate = a.sum/n, a.top4s/n, float64(a.wins)/n
		out = append(out, a.stats)
		order[patch] = a.last
	}
	sort.Slice(out, func(i, j int) bool { return order[out[i].Patch].After(order[out[j].Patch]) })
	return out, nil
}

// RankPoint is one rank snapshot placed on the linear LP scale (lp.Value),
// for charting LP over time.
type RankPoint struct {
	store.RankSnapshot
	Value int `json:"value"`
}

// handlePlayerRankHistory serves GET /api/v1/players/{puuid}/ranks: every
// recorded rank change per ranked queue, oldest first. Snapshots exist from
// the player's first profile sync (or apex ladder refresh) on.
func (s *Server) handlePlayerRankHistory(w http.ResponseWriter, r *http.Request) {
	puuid := r.PathValue("puuid")
	if !validPUUID(puuid) {
		writeError(w, http.StatusBadRequest, "invalid_puuid", "puuid must be 1-100 letters, digits, '-' or '_'")
		return
	}
	history, err := s.Store.RankHistory(r.Context(), puuid)
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	out := map[string][]RankPoint{}
	for _, h := range history {
		out[h.QueueType] = append(out[h.QueueType], RankPoint{RankSnapshot: h, Value: lp.Value(h)})
	}
	writeJSON(w, http.StatusOK, out)
}
