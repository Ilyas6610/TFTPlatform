package apiserver

import (
	"encoding/json"
	"net/http"
	"strconv"

	"tft-platform/internal/riotapi"
)

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorResponse{Error: code, Message: message})
}

// writeRiotError maps the typed errors from internal/riotapi onto HTTP
// responses that let the frontend distinguish "not found" from "Riot is
// temporarily unavailable" from "our key is dead" — none of which should
// look like a generic 500.
func writeRiotError(w http.ResponseWriter, err error) {
	switch e := err.(type) {
	case *riotapi.ErrNotFound:
		writeError(w, http.StatusNotFound, "not_found", e.Error())
	case *riotapi.ErrRateLimited:
		w.Header().Set("Retry-After", strconv.Itoa(int(e.RetryAfter.Seconds())))
		writeError(w, http.StatusServiceUnavailable, "riot_api_rate_limited", e.Error())
	case *riotapi.ErrKeyExpired:
		writeError(w, http.StatusBadGateway, "riot_api_key_expired",
			"live data temporarily unavailable: riot api key needs rotation")
	default:
		writeError(w, http.StatusBadGateway, "riot_api_error", err.Error())
	}
}
