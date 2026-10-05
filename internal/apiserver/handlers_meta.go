package apiserver

import (
	"net/http"
	"strconv"
)

type UnitStatResponse struct {
	CharacterID  string  `json:"characterId"`
	GamesPlayed  int     `json:"gamesPlayed"`
	AvgPlacement float64 `json:"avgPlacement"`
	Top4Rate     float64 `json:"top4Rate"`
	WinRate      float64 `json:"winRate"`
	PickRate     float64 `json:"pickRate"`
}

type TraitStatResponse struct {
	TraitName    string  `json:"traitName"`
	TraitTier    int     `json:"traitTier"`
	GamesPlayed  int     `json:"gamesPlayed"`
	AvgPlacement float64 `json:"avgPlacement"`
	Top4Rate     float64 `json:"top4Rate"`
	WinRate      float64 `json:"winRate"`
}

type AugmentStatResponse struct {
	AugmentID    string  `json:"augmentId"`
	GamesPlayed  int     `json:"gamesPlayed"`
	AvgPlacement float64 `json:"avgPlacement"`
	Top4Rate     float64 `json:"top4Rate"`
	WinRate      float64 `json:"winRate"`
}

// meta-stats endpoints are Postgres-only, never computed live — see
// internal/aggregate. They require a ?set=<tftSetNumber> query parameter
// since stats are always scoped to one TFT set.

func (s *Server) handleMetaUnits(w http.ResponseWriter, r *http.Request) {
	setNumber, ok := parseSetQuery(w, r)
	if !ok {
		return
	}
	stats, err := s.Store.GetUnitStats(r.Context(), setNumber)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	out := make([]UnitStatResponse, len(stats))
	for i, u := range stats {
		out[i] = UnitStatResponse{
			CharacterID:  u.CharacterID,
			GamesPlayed:  u.GamesPlayed,
			AvgPlacement: u.AvgPlacement,
			Top4Rate:     u.Top4Rate,
			WinRate:      u.WinRate,
			PickRate:     u.PickRate,
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleMetaTraits(w http.ResponseWriter, r *http.Request) {
	setNumber, ok := parseSetQuery(w, r)
	if !ok {
		return
	}
	stats, err := s.Store.GetTraitStats(r.Context(), setNumber)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	out := make([]TraitStatResponse, len(stats))
	for i, t := range stats {
		out[i] = TraitStatResponse{
			TraitName:    t.TraitName,
			TraitTier:    t.TraitTier,
			GamesPlayed:  t.GamesPlayed,
			AvgPlacement: t.AvgPlacement,
			Top4Rate:     t.Top4Rate,
			WinRate:      t.WinRate,
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleMetaAugments(w http.ResponseWriter, r *http.Request) {
	setNumber, ok := parseSetQuery(w, r)
	if !ok {
		return
	}
	stats, err := s.Store.GetAugmentStats(r.Context(), setNumber)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	out := make([]AugmentStatResponse, len(stats))
	for i, a := range stats {
		out[i] = AugmentStatResponse{
			AugmentID:    a.AugmentID,
			GamesPlayed:  a.GamesPlayed,
			AvgPlacement: a.AvgPlacement,
			Top4Rate:     a.Top4Rate,
			WinRate:      a.WinRate,
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func parseSetQuery(w http.ResponseWriter, r *http.Request) (int, bool) {
	v := r.URL.Query().Get("set")
	if v == "" {
		writeError(w, http.StatusBadRequest, "missing_set", "query parameter 'set' (tft set number) is required")
		return 0, false
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_set", "query parameter 'set' must be an integer")
		return 0, false
	}
	return n, true
}
