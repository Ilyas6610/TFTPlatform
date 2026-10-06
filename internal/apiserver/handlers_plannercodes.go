package apiserver

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"tft-platform/internal/setdata"
)

const (
	// plannerCodesTimeout bounds the download. The file is ~65 KB, so the 3
	// minutes DefaultSource allows for the 25 MB set export would only hold a
	// request until the server's WriteTimeout cuts it off.
	plannerCodesTimeout = 15 * time.Second
)

// plannerFailCooldown is how long a failed download is remembered, so a
// source that is down isn't retried by every visitor. A var for tests.
var plannerFailCooldown = 30 * time.Second

// failureMemo remembers that a download just failed. The zero value is ready.
type failureMemo struct {
	mu    sync.Mutex
	until time.Time
}

func (f *failureMemo) active() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return time.Now().Before(f.until)
}

func (f *failureMemo) record() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.until = time.Now().Add(plannerFailCooldown)
}

// PlannerCodesResponse is GET /api/v1/sets/{set}/planner-codes.
type PlannerCodesResponse struct {
	Set int `json:"set"`
	// Codes maps a champion's apiName to the number the in-game Team Planner
	// encodes it as. Only shop champions appear; the frontend builds the
	// pasteable code from these (see frontend/src/planner/teamCode.ts).
	Codes map[string]int `json:"codes"`
}

// handlePlannerCodes serves the Team Planner champion codes for one set. The
// source file covers every set, so it is fetched once and cached
// (resultCacheTTL) whichever set is asked for; a set the file doesn't list is
// a 404.
func (s *Server) handlePlannerCodes(w http.ResponseWriter, r *http.Request) {
	set, err := strconv.Atoi(r.PathValue("set"))
	if err != nil || set <= 0 || set > maxSetNumber {
		writeError(w, http.StatusBadRequest, "invalid_set", "set must be a number from 1 to "+strconv.Itoa(maxSetNumber))
		return
	}
	if s.plannerFail.active() {
		writeError(w, http.StatusBadGateway, "upstream_error", "couldn't load the data from its source")
		return
	}
	v, err := s.sets.get(r.Context(), "plannercodes", func(ctx context.Context) (any, error) {
		src := setdata.DefaultSource()
		src.HTTP = &http.Client{Timeout: plannerCodesTimeout}
		if s.Source != nil {
			src = *s.Source
		}
		ctx, cancel := context.WithTimeout(ctx, plannerCodesTimeout)
		defer cancel()
		return setdata.FetchPlannerCodes(ctx, src)
	})
	if err != nil {
		// A visitor who left while waiting says nothing about the source.
		if r.Context().Err() == nil {
			s.plannerFail.record()
		}
		writeUpstreamError(w, r, err)
		return
	}
	codes, ok := v.(setdata.PlannerCodes)[set]
	if !ok {
		writeError(w, http.StatusNotFound, "no_planner_codes", "no Team Planner codes are published for this set")
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeJSON(w, http.StatusOK, PlannerCodesResponse{Set: set, Codes: codes})
}

// writeUpstreamError reports a failed third-party download without its
// detail (logged), as writeDBError does for the database.
func writeUpstreamError(w http.ResponseWriter, r *http.Request, err error) {
	log.Printf("%s %s: %v", r.Method, r.URL.Path, err)
	writeError(w, http.StatusBadGateway, "upstream_error", "couldn't load the data from its source")
}
