package apiserver

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"tft-platform/internal/ingest"
	"tft-platform/internal/lp"
	"tft-platform/internal/riotapi"
)

// handleMatchDetail serves GET /api/v1/matches/{matchId}. Postgres is
// checked first; on a cache miss, a single live Riot fetch is always worth
// it (one request) since match_id encodes the platform prefix (e.g.
// "NA1_...") that we derive the region routing from.
func (s *Server) handleMatchDetail(w http.ResponseWriter, r *http.Request) {
	matchID := r.PathValue("matchId")
	ctx := r.Context()
	if !validMatchID(matchID) {
		writeError(w, http.StatusBadRequest, "invalid_match_id", "match id must look like NA1_1234567890")
		return
	}

	raw, _, found, err := s.Store.GetMatchRaw(ctx, matchID)
	if err != nil {
		writeDBError(w, r, err)
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

	missKey := "match:" + matchID
	if s.live.knownMissing(missKey) {
		writeError(w, http.StatusNotFound, "not_found", "match not found")
		return
	}
	release, ok := s.live.acquire(w, ctx)
	if !ok {
		return
	}
	defer release()

	match, rawLive, err := s.Riot.GetTFTMatch(ctx, routing, matchID)
	if err != nil {
		var notFound *riotapi.ErrNotFound
		if errors.As(err, &notFound) {
			s.live.rememberMissing(missKey)
		}
		writeRiotError(w, err)
		return
	}

	if err := ingest.StoreMatch(ctx, s.Store, routing, matchID, match, rawLive); err != nil {
		writeDBError(w, r, err)
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
		log.Printf("decode match payload: %v", err)
		writeError(w, http.StatusInternalServerError, "decode_error", "stored match could not be read")
		return
	}
	sourceJSON, _ := json.Marshal(source)
	body["source"] = sourceJSON

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(body)
}

// MatchLobbyPlayer is a participant's Ranked standing around the game.
type MatchLobbyPlayer struct {
	PUUID        string `json:"puuid"`
	Tier         string `json:"tier"`
	Rank         string `json:"rank,omitempty"`
	LeaguePoints int    `json:"leaguePoints"`
	Value        int    `json:"value"`   // lp.Value
	Current      bool   `json:"current"` // today's ladder rank, not one near the game
}

// MatchLobbyResponse lists the participants whose rank is known and the
// lobby's average over them.
type MatchLobbyResponse struct {
	Players []MatchLobbyPlayer `json:"players"`
	Average int                `json:"average"` // 0 when none is known
	Total   int                `json:"total"`   // participants stored
}

// handleMatchLobby serves GET /api/v1/matches/{matchId}/lobby: each stored
// participant's Ranked standing around the game (store.LobbyPlayers). It
// never calls Riot; an unstored match has no players.
func (s *Server) handleMatchLobby(w http.ResponseWriter, r *http.Request) {
	matchID := r.PathValue("matchId")
	if !validMatchID(matchID) {
		writeError(w, http.StatusBadRequest, "invalid_match_id", "match id must look like NA1_1234567890")
		return
	}
	players, err := s.Store.LobbyPlayers(r.Context(), []string{matchID})
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	resp := MatchLobbyResponse{Players: []MatchLobbyPlayer{}, Total: len(players)}
	sum := 0
	for _, p := range players {
		if p.Rank == nil {
			continue
		}
		v := lp.Value(*p.Rank)
		sum += v
		resp.Players = append(resp.Players, MatchLobbyPlayer{
			PUUID: p.PUUID, Tier: p.Rank.Tier, Rank: p.Rank.Rank, LeaguePoints: p.Rank.LeaguePoints, Value: v, Current: p.Current,
		})
	}
	if n := len(resp.Players); n > 0 {
		resp.Average = sum / n
	}
	writeJSON(w, http.StatusOK, resp)
}
