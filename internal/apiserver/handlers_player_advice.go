package apiserver

import (
	"context"
	"net/http"

	"tft-platform/internal/advice"
	"tft-platform/internal/store"
)

// playerAdviceBuilds: the player's builds per unit considered (their most
// played one is compared).
const playerAdviceBuilds = 3

// handlePlayerAdvice serves GET /api/v1/players/{puuid}/advice?set=18
// [&queue=1100][&level=8-]: units the player does worse or better with than
// their usual and builds everyone does better with than theirs, from their
// stored games against everyone's in the same scope (internal/advice).
// Never calls Riot.
func (s *Server) handlePlayerAdvice(w http.ResponseWriter, r *http.Request) {
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
	scope, err := parseExploreFilter(q)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_filter", err.Error())
		return
	}
	ctx := r.Context()
	mine := scope
	mine.PUUID = puuid

	playerStats, err := s.Store.PlacementSummary(ctx, mine)
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	if playerStats.Boards == 0 {
		writeJSON(w, http.StatusOK, advice.Build(&store.MetaResult{}, &store.MetaResult{}, 0, 0, advice.Defaults))
		return
	}
	everyoneStats, err := s.scopeBaseline(ctx, scope)
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	player, err := s.Store.MetaBuilds(ctx, mine, advice.Defaults.MinBuildGames, playerAdviceBuilds, 0)
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	// Everyone's builds, wide enough to include the player's: the same
	// cached result the build advisor uses.
	v, err := s.meta.get(ctx, metaCacheKey("builds-wide", scope), func(ctx context.Context) (any, error) {
		return s.Store.MetaBuilds(ctx, scope, metaMinBuildGames, suggestBuildsPerUnit, metaItemsPerUnit)
	})
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, advice.Build(player, v.(*store.MetaResult), playerStats.AvgPlacement, everyoneStats.AvgPlacement, advice.Defaults))
}
