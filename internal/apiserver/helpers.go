package apiserver

import (
	"context"
	"encoding/json"
	"errors"
	"log"
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

// writeDBError logs a database failure and answers with a generic 500, so
// table names and connection details stay out of responses.
func writeDBError(w http.ResponseWriter, r *http.Request, err error) {
	log.Printf("%s %s: %v", r.Method, r.URL.Path, err)
	writeError(w, http.StatusInternalServerError, "db_error", "database error")
}

// writeRiotError maps the typed errors from internal/riotapi onto HTTP
// responses that let the frontend distinguish "not found" from "Riot is
// temporarily unavailable" from "our key is dead" — none of which should
// look like a generic 500.
func writeRiotError(w http.ResponseWriter, err error) {
	var notFound *riotapi.ErrNotFound
	var rateLimited *riotapi.ErrRateLimited
	var keyExpired *riotapi.ErrKeyExpired
	switch {
	case errors.As(err, &notFound):
		writeError(w, http.StatusNotFound, "not_found", notFound.Error())
	case errors.As(err, &rateLimited):
		w.Header().Set("Retry-After", strconv.Itoa(int(rateLimited.RetryAfter.Seconds())))
		writeError(w, http.StatusServiceUnavailable, "riot_api_rate_limited", rateLimited.Error())
	case errors.As(err, &keyExpired):
		writeError(w, http.StatusBadGateway, "riot_api_key_expired",
			"live data temporarily unavailable: riot api key needs rotation")
	default:
		// The detail can carry Riot's response body; it goes to the log only.
		log.Printf("riot api error: %v", err)
		writeError(w, http.StatusBadGateway, "riot_api_error", "riot api request failed")
	}
}

// riotErrorCode returns the error code writeRiotError would use for err, for
// responses that report a Riot failure without failing outright.
func riotErrorCode(err error) string {
	var rateLimited *riotapi.ErrRateLimited
	var keyExpired *riotapi.ErrKeyExpired
	switch {
	case errors.As(err, &rateLimited):
		return "riot_api_rate_limited"
	case errors.As(err, &keyExpired):
		return "riot_api_key_expired"
	case errors.Is(err, context.DeadlineExceeded):
		return "riot_api_timeout"
	default:
		return "riot_api_error"
	}
}
