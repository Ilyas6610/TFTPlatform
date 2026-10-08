package apiserver

import (
	"cmp"
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"tft-platform/internal/ingest"
	"tft-platform/internal/riotapi"
)

const (
	// backfillMaxGames caps one whole-set load (like the history's paging).
	backfillMaxGames = 500
	// backfillTimeout bounds a run. A load's requests draw from the
	// backfill share of the key (Server.RiotBackfill, RIOT_BUDGET_SPLIT),
	// which on a personal key's default split is 20 per 2 minutes: 500
	// games take ~50 minutes, so this leaves room for 429 pauses.
	backfillTimeout = 2 * time.Hour
	// backfillRecent is how long a finished load is reported and not rerun
	// for its player; a failed one isn't retried for as long either.
	backfillRecent = 10 * time.Minute
	// backfillGap is the server-wide pause after any load ends before the
	// next may start, so loads for different players can't be chained to
	// keep the key busy.
	backfillGap = 10 * time.Minute
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

// backfills runs whole-set loads one at a time server-wide, with a gap
// between them: each spends hundreds of Riot requests from the key the
// live pages share. The zero value is ready; gap defaults to backfillGap.
// pace, extra spacing between a load's requests, is 0 by default: the
// backfill share of the key (Server.RiotBackfill) already paces it. Tests
// shorten gap and set pace.
type backfills struct {
	mu         sync.Mutex
	running    string // puuid of the load in flight, if any
	lastFinish time.Time
	status     map[string]*BackfillStatus
	pace, gap  time.Duration
}

// backfillRefusal says why a load can't start now.
type backfillRefusal struct {
	code, message string
	retryAfter    time.Duration
}

func (b *backfills) get(puuid string) BackfillStatus {
	b.mu.Lock()
	defer b.mu.Unlock()
	if st := b.status[puuid]; st != nil {
		return *st
	}
	return BackfillStatus{State: "idle"}
}

// minutes renders a wait for messages, rounded up.
func minutes(d time.Duration) string {
	n := int((d + time.Minute - 1) / time.Minute)
	if n <= 1 {
		return "a minute"
	}
	return fmt.Sprintf("%d minutes", n)
}

// start begins a load for puuid. A running or just finished load for it is
// reported instead (no refusal). It's refused while another player's load
// runs, within the gap after any load, and for a while after this
// player's last load failed.
func (b *backfills) start(puuid string, run func(ctx context.Context, pace time.Duration, report func(ingest.BackfillProgress)) (*time.Time, error)) (BackfillStatus, *backfillRefusal) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.status == nil {
		b.status = map[string]*BackfillStatus{}
	}
	pace, gap := b.pace, cmp.Or(b.gap, backfillGap)
	if cur := b.status[puuid]; cur != nil {
		if cur.State == "running" {
			return *cur, nil
		}
		if cur.FinishedAt != nil {
			if left := backfillRecent - time.Since(*cur.FinishedAt); left > 0 {
				if cur.State == "done" {
					return *cur, nil
				}
				return *cur, &backfillRefusal{"cooldown", "this player's load just failed; try again in " + minutes(left), left}
			}
		}
	}
	if b.running != "" {
		return BackfillStatus{State: "idle"}, &backfillRefusal{"busy", "another player's history is loading; try again in a few minutes", time.Minute}
	}
	if left := gap - time.Since(b.lastFinish); !b.lastFinish.IsZero() && left > 0 {
		return BackfillStatus{State: "idle"}, &backfillRefusal{"busy", "another player's history just loaded; try again in " + minutes(left), left}
	}
	b.pruneLocked()
	cur := &BackfillStatus{State: "running"}
	b.status[puuid] = cur
	b.running = puuid

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), backfillTimeout)
		defer cancel()
		since, err := run(ctx, pace, func(p ingest.BackfillProgress) {
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
		b.running, b.lastFinish = "", now
	}()
	return *cur, nil
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
// calendar's first patch) up to backfillMaxGames, in the background. Only
// stored players can be loaded; refusals are 409 with Retry-After.
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
	// Only players we already know (a history page synced them): a load
	// can't be pointed at arbitrary PUUIDs.
	sets, err := s.Store.PlayerSets(r.Context(), puuid)
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	if len(sets) == 0 {
		writeError(w, http.StatusNotFound, "unknown_player", "no stored games for this player; open their profile first")
		return
	}
	st, refused := s.backfill.start(puuid, func(ctx context.Context, pace time.Duration, report func(ingest.BackfillProgress)) (*time.Time, error) {
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
		_, err = ingest.BackfillSet(ctx, s.backfillRiot(), s.Store, platform, puuid, since, backfillMaxGames, pace, report)
		if since.IsZero() {
			return nil, err
		}
		return &since, err
	})
	if refused != nil {
		w.Header().Set("Retry-After", strconv.Itoa(int(refused.retryAfter.Seconds()+0.5)))
		writeError(w, http.StatusConflict, refused.code, refused.message)
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
