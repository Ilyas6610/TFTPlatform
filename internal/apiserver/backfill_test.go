package apiserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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
	if len(page.Matches) != 30 {
		t.Errorf("%d games stored, want 30", len(page.Matches))
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
