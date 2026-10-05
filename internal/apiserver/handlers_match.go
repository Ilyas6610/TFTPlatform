package apiserver

import (
	"encoding/json"
	"net/http"
	"strings"

	"tft-platform/internal/ingest"
	"tft-platform/internal/riotapi"
)

// handleMatchDetail serves GET /api/v1/matches/{matchId}. Postgres is
// checked first; on a cache miss, a single live Riot fetch is always worth
// it (one request) since match_id encodes the platform prefix (e.g.
// "NA1_...") that we derive the region routing from.
func (s *Server) handleMatchDetail(w http.ResponseWriter, r *http.Request) {
	matchID := r.PathValue("matchId")
	ctx := r.Context()

	raw, _, found, err := s.Store.GetMatchRaw(ctx, matchID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	if found {
		writeRawJSONWithSource(w, raw, "cache")
		return
	}

	routing, err := routingFromMatchID(matchID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_match_id", err.Error())
		return
	}

	match, rawLive, err := s.Riot.GetTFTMatch(ctx, routing, matchID)
	if err != nil {
		writeRiotError(w, err)
		return
	}

	if err := ingest.StoreMatch(ctx, s.Store, routing, matchID, match, rawLive); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}

	writeRawJSONWithSource(w, rawLive, "live")
}

// routingFromMatchID derives the regional routing from a match ID's
// platform prefix (e.g. "NA1_1234567890" -> americas), since tft/match-v1 is
// region-routed, not platform-routed.
func routingFromMatchID(matchID string) (riotapi.RoutingRegion, error) {
	prefix, _, ok := strings.Cut(matchID, "_")
	if !ok {
		return "", errInvalidMatchID
	}
	platform := riotapi.PlatformRegion(strings.ToLower(prefix))
	return riotapi.RoutingForPlatform(platform)
}

var errInvalidMatchID = &invalidMatchIDError{}

type invalidMatchIDError struct{}

func (e *invalidMatchIDError) Error() string { return "match id is not in PLATFORM_id form" }

func writeRawJSONWithSource(w http.ResponseWriter, raw []byte, source string) {
	// Wrap the raw Riot payload (or our stored copy of it) with a "source"
	// marker so the frontend can distinguish an instant cached response from
	// a slower live one, same as the player profile endpoint.
	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw, &body); err != nil {
		writeError(w, http.StatusInternalServerError, "decode_error", err.Error())
		return
	}
	sourceJSON, _ := json.Marshal(source)
	body["source"] = sourceJSON

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(body)
}
