package apiserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"tft-platform/internal/ingest"
	"tft-platform/internal/store"
)

func backfillCall(t *testing.T, s *Server, method, puuid string) (int, BackfillStatus) {
	t.Helper()
	rec := httptest.NewRecorder()
	NewRouter(s).ServeHTTP(rec, httptest.NewRequest(method, "/api/v1/players/"+puuid+"/backfill?region=na1", nil))
	var st BackfillStatus
	json.Unmarshal(rec.Body.Bytes(), &st)
	return rec.Code, st
}

func TestPlayerBackfill(t *testing.T) {
	s, riot := newTestServer(t)
	for i := 1; i <= 30; i++ {
		id := fmt.Sprintf("NA1_%d", i)
		riot.matchIDs["me"] = append([]string{id}, riot.matchIDs["me"]...)
		riot.matches[id] = matchJSON(id, int64(i)*1000, "me")
	}
	s.backfill.pace, s.backfill.gap = time.Nanosecond, time.Hour
	// Only stored players can be loaded: unknown ones get 404.
	if code, _ := backfillCall(t, s, http.MethodPost, "me"); code != http.StatusNotFound {
		t.Fatalf("unknown player: got %d, want 404", code)
	}
	for _, p := range []string{"me", "other"} {
		if err := s.Store.UpsertAccountPUUIDOnly(context.Background(), p, "americas"); err != nil {
			t.Fatal(err)
		}
		if err := s.Store.InsertMatchWithParticipants(context.Background(), store.Match{
			MatchID: "NA1_seed_" + p, RoutingRegion: "americas", GameDatetime: time.Unix(1, 0),
			GameVersion: "x", TFTSetNumber: 18, QueueID: 1100, TFTGameType: "standard", RawPayload: []byte(`{}`),
		}, map[string]store.MatchParticipant{p: {PUUID: p, Placement: 1, Units: []byte(`[]`), Traits: []byte(`[]`), RawParticipant: []byte(`{}`)}}); err != nil {
			t.Fatal(err)
		}
	}
	gate := make(chan struct{})
	riot.matchGate = gate

	if code, st := backfillCall(t, s, http.MethodGet, "me"); code != http.StatusOK || st.State != "idle" {
		t.Fatalf("before: %d %+v, want idle", code, st)
	}
	if code, st := backfillCall(t, s, http.MethodPost, "me"); code != http.StatusAccepted || st.State != "running" {
		t.Fatalf("start: %d %+v, want 202 running", code, st)
	}
	// One load at a time server-wide.
	if code, _ := backfillCall(t, s, http.MethodPost, "other"); code != http.StatusConflict {
		t.Errorf("second player's load: got %d, want 409 busy", code)
	}
	// Starting again for the same player just reports the running load.
	if code, st := backfillCall(t, s, http.MethodPost, "me"); code != http.StatusAccepted || st.State != "running" {
		t.Errorf("restart: %d %+v", code, st)
	}
	close(gate)

	var st BackfillStatus
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if _, st = backfillCall(t, s, http.MethodGet, "me"); st.State != "running" {
			break
		}
	}
	if st.State != "done" || st.Found != 30 || st.Missing != 30 || st.Fetched != 30 {
		t.Fatalf("finished: %+v, want done with 30 found and fetched", st)
	}
	_, page := getMatches(t, s, "/api/v1/players/me/matches?limit=100")
	if len(page.Matches) != 31 {
		t.Errorf("%d games stored, want 30 + the seed", len(page.Matches))
	}
	// Another player's load waits out the server-wide gap.
	if code, _ := backfillCall(t, s, http.MethodPost, "other"); code != http.StatusConflict {
		t.Errorf("load right after another finished: got %d, want 409", code)
	}
	// Just finished: not rerun.
	calls := riot.matchCalls.Load()
	if _, st := backfillCall(t, s, http.MethodPost, "me"); st.State != "done" || riot.matchCalls.Load() != calls {
		t.Errorf("rerun right after finishing: %+v", st)
	}
	if code, _ := backfillCall(t, s, http.MethodPost, "bad%20id"); code != http.StatusBadRequest {
		t.Errorf("bad puuid: got %d", code)
	}
}

func TestBackfills_FailureCooldownAndGap(t *testing.T) {
	b := &backfills{pace: time.Nanosecond, gap: time.Nanosecond}
	wait := func() {
		t.Helper()
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
			b.mu.Lock()
			idle := b.running == ""
			b.mu.Unlock()
			if idle {
				return
			}
		}
		t.Fatal("load didn't finish")
	}
	fail := func(context.Context, time.Duration, func(ingest.BackfillProgress)) (*time.Time, error) {
		return nil, fmt.Errorf("riot down")
	}
	if _, refused := b.start("x", fail); refused != nil {
		t.Fatalf("first load refused: %+v", refused)
	}
	wait()
	// A failed load isn't retried right away...
	st, refused := b.start("x", fail)
	if refused == nil || refused.code != "cooldown" || st.State != "failed" {
		t.Errorf("retry after failure: %+v %+v, want cooldown", st, refused)
	}
	// ...but another player's can start once the (short) gap has passed.
	time.Sleep(time.Millisecond)
	if _, refused := b.start("y", fail); refused != nil {
		t.Errorf("other player refused: %+v", refused)
	}
	wait()
}
