package apiserver

import (
	"context"
	"log"
	"net/http"
	"sync"
	"time"

	"tft-platform/internal/ingest"
	"tft-platform/internal/riotapi"
)

const (
	// backfillMaxGames caps one whole-set load (like the history's paging).
	backfillMaxGames = 500
	// backfillTimeout bounds a run; on a personal key 500 games take ~10 min.
	backfillTimeout = 30 * time.Minute
	// backfillRecent is how long a finished load is reported (and not rerun).
	backfillRecent = 10 * time.Minute
	// backfillKeep bounds remembered statuses.
	backfillKeep = 200
)

// BackfillStatus is a player's whole-set history load.
type BackfillStatus struct {
	State string `json:"state"` // idle, running, done, failed
	ingest.BackfillProgress
	Since      *time.Time `json:"since,omitempty"` // the set's start, when known
	Error      string     `json:"error,omitempty"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

// backfills runs whole-set loads one at a time server-wide: each spends
// hundreds of Riot requests from the key the live pages share, so two at
// once would starve everything else. The zero value is ready.
type backfills struct {
	mu      sync.Mutex
	running string // puuid of the load in flight, if any
	status  map[string]*BackfillStatus
}

func (b *backfills) get(puuid string) BackfillStatus {
	b.mu.Lock()
	defer b.mu.Unlock()
	if st := b.status[puuid]; st != nil {
		return *st
	}
	return BackfillStatus{State: "idle"}
}

// start begins a load for puuid unless one is running or just finished for
// it (then its status is returned). busy is set when another player's load
// is in flight.
func (b *backfills) start(puuid string, run func(ctx context.Context, report func(ingest.BackfillProgress)) (*time.Time, error)) (st BackfillStatus, busy bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.status == nil {
		b.status = map[string]*BackfillStatus{}
	}
	if cur := b.status[puuid]; cur != nil && (cur.State == "running" ||
		(cur.State == "done" && cur.FinishedAt != nil && time.Since(*cur.FinishedAt) < backfillRecent)) {
		return *cur, false
	}
	if b.running != "" {
		return BackfillStatus{State: "idle"}, true
	}
	b.pruneLocked()
	cur := &BackfillStatus{State: "running"}
	b.status[puuid] = cur
	b.running = puuid

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), backfillTimeout)
		defer cancel()
		since, err := run(ctx, func(p ingest.BackfillProgress) {
			b.mu.Lock()
			cur.BackfillProgress = p
			b.mu.Unlock()
		})
		b.mu.Lock()
		defer b.mu.Unlock()
		now := time.Now()
		cur.FinishedAt, cur.Since, cur.State = &now, since, "done"
		if err != nil {
			log.Printf("backfill %s: %v", puuid, err)
			cur.State, cur.Error = "failed", riotErrorCode(err)
		}
		b.running = ""
	}()
	return *cur, false
}

// pruneLocked forgets finished loads once there are too many.
func (b *backfills) pruneLocked() {
	if len(b.status) < backfillKeep {
		return
	}
	for k, st := range b.status {
		if st.State != "running" {
			delete(b.status, k)
		}
	}
}

// handlePlayerBackfill serves GET (status) and POST (start) on
// /api/v1/players/{puuid}/backfill[?region=<platform>]: loading every game
// of the player's current set from Riot, from the set's start (the patch
// calendar's first patch) up to backfillMaxGames, in the background.
func (s *Server) handlePlayerBackfill(w http.ResponseWriter, r *http.Request) {
	puuid := r.PathValue("puuid")
	if !validPUUID(puuid) {
		writeError(w, http.StatusBadRequest, "invalid_puuid", "puuid must be 1-100 letters, digits, '-' or '_'")
		return
	}
	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, s.backfill.get(puuid))
		return
	}
	platform := riotapi.PlatformRegion(r.URL.Query().Get("region"))
	if _, err := riotapi.RoutingForPlatform(platform); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_region", "region must be a platform like na1")
		return
	}
	st, busy := s.backfill.start(puuid, func(ctx context.Context, report func(ingest.BackfillProgress)) (*time.Time, error) {
		since, err := s.currentSetStart(ctx)
		if err != nil {
			return nil, err
		}
		if !since.IsZero() {
			// The calendar's start is approximate (notes + a day); a day of
			// margin keeps launch-day games. Older ones are other sets,
			// which the profile's set filter leaves out anyway.
			since = since.Add(-24 * time.Hour)
		}
		_, err = ingest.BackfillSet(ctx, s.Riot, s.Store, platform, puuid, since, backfillMaxGames, report)
		if since.IsZero() {
			return nil, err
		}
		return &since, err
	})
	if busy {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusConflict, "busy", "another player's history is loading; try again in a few minutes")
		return
	}
	writeJSON(w, http.StatusAccepted, st)
}

// currentSetStart is when the newest set in the patch calendar began, or
// zero if the calendar is empty (then the load isn't limited by date).
func (s *Server) currentSetStart(ctx context.Context) (time.Time, error) {
	cal, err := s.Store.PatchCalendar(ctx)
	if err != nil {
		return time.Time{}, err
	}
	set := 0
	for _, p := range cal {
		set = max(set, p.SetNumber)
	}
	var start time.Time
	for _, p := range cal {
		if p.SetNumber == set && (start.IsZero() || p.StartsAt.Before(start)) {
			start = p.StartsAt
		}
	}
	return start, nil
}
