package apiserver

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"math"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"

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
// table names and connection details stay out of responses. A query the
// statement timeout cancelled (the database is busy, not broken) is a 503
// with Retry-After instead.
func writeDBError(w http.ResponseWriter, r *http.Request, err error) {
	log.Printf("%s %s: %v", r.Method, r.URL.Path, err)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "57014" { // query_canceled
		w.Header().Set("Retry-After", "10")
		writeError(w, http.StatusServiceUnavailable, "db_busy", "the database is busy; try again shortly")
		return
	}
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
	var budget *riotapi.ErrBudgetExhausted
	switch {
	case errors.As(err, &notFound):
		writeError(w, http.StatusNotFound, "not_found", notFound.Error())
	case errors.As(err, &rateLimited):
		w.Header().Set("Retry-After", strconv.Itoa(int(rateLimited.RetryAfter.Seconds())))
		writeError(w, http.StatusServiceUnavailable, "riot_api_rate_limited", rateLimited.Error())
	case errors.As(err, &keyExpired):
		writeError(w, http.StatusBadGateway, "riot_api_key_expired",
			"live data temporarily unavailable: riot api key needs rotation")
	case errors.As(err, &budget):
		// This server's share of the key for lookups is used up: the
		// shared budget, not anything about this request.
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(budget.RetryAfter.Seconds()))))
		writeError(w, http.StatusServiceUnavailable, "riot_api_busy", "live lookups are busy; try again shortly")
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
	case errors.As(err, new(*riotapi.ErrBudgetExhausted)):
		return "riot_api_busy"
	case errors.Is(err, context.DeadlineExceeded):
		return "riot_api_timeout"
	default:
		return "riot_api_error"
	}
}

// clientIP identifies who sent r, for per-client quotas. Behind nginx every
// connection comes from the proxy, which sets X-Forwarded-For to the peer
// address only (frontend/nginx.conf.template), so that header is used when
// the connection itself comes from a private or loopback address; a direct
// connection from a public address is identified by that address, so a
// client reaching the API directly can't pick its own identity. The proxy
// must overwrite the header (nginx: proxy_set_header X-Forwarded-For
// $remote_addr), not append to a client-supplied one.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err == nil && (peer.IsLoopback() || peer.IsPrivate()) {
		if fwd := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); fwd != "" {
			// nginx sets one address; take the last in case of a list.
			if i := strings.LastIndexByte(fwd, ','); i >= 0 {
				fwd = strings.TrimSpace(fwd[i+1:])
			}
			if a, err := netip.ParseAddr(fwd); err == nil {
				return a.String()
			}
		}
	}
	return host
}
