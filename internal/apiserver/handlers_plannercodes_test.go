package apiserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"tft-platform/internal/setdata"
)

func TestPlannerCodes(t *testing.T) {
	var fetches atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetches.Add(1)
		w.Write([]byte(`{"TFTSet18": [{"character_id": "DA_18_Ahri", "team_planner_code": 1001}], "TFTSet17": [{"character_id": "TFT17_Akali", "team_planner_code": 13}]}`))
	}))
	defer up.Close()
	s := &Server{Source: &setdata.Source{BaseURL: up.URL, HTTP: up.Client()}}
	get := func(set string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		NewRouter(s).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/sets/"+set+"/planner-codes", nil))
		return rec
	}

	rec := get("18")
	var res PlannerCodesResponse
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &res) != nil || res.Set != 18 || res.Codes["DA_18_Ahri"] != 1001 || len(res.Codes) != 1 {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
	if rec.Header().Get("Cache-Control") == "" {
		t.Error("want a Cache-Control header")
	}
	if get("17").Code != http.StatusOK || get("19").Code != http.StatusNotFound {
		t.Error("17 should be served and 19 (not in the file) should be a 404")
	}
	if n := fetches.Load(); n != 1 {
		t.Errorf("source fetched %d times; one download serves every set", n)
	}
	for _, bad := range []string{"0", "x", "101", "-1"} {
		if c := get(bad).Code; c != http.StatusBadRequest {
			t.Errorf("set %q: got %d, want 400", bad, c)
		}
	}
}

func TestPlannerCodes_UpstreamFailureIsGeneric(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "secret internals", 500) }))
	defer up.Close()
	s := &Server{Source: &setdata.Source{BaseURL: up.URL, HTTP: up.Client()}}
	rec := httptest.NewRecorder()
	NewRouter(s).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/sets/18/planner-codes", nil))
	if rec.Code != http.StatusBadGateway || rec.Body.String() == "" {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
	if b := rec.Body.String(); len(b) > 0 && (strings.Contains(b, "secret") || strings.Contains(b, up.URL)) {
		t.Errorf("response leaks upstream detail: %s", b)
	}
}

func TestPlannerCodes_FailureIsRememberedBriefly(t *testing.T) {
	old := plannerFailCooldown
	plannerFailCooldown = 150 * time.Millisecond
	defer func() { plannerFailCooldown = old }()

	var fetches atomic.Int32
	var fail atomic.Bool
	fail.Store(true)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetches.Add(1)
		if fail.Load() {
			http.Error(w, "down", http.StatusInternalServerError)
			return
		}
		w.Write([]byte(`{"TFTSet18": [{"character_id": "DA_18_Ahri", "team_planner_code": 1001}]}`))
	}))
	defer up.Close()
	s := &Server{Source: &setdata.Source{BaseURL: up.URL, HTTP: up.Client()}}
	get := func() int {
		rec := httptest.NewRecorder()
		NewRouter(s).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/sets/18/planner-codes", nil))
		return rec.Code
	}

	if get() != http.StatusBadGateway || get() != http.StatusBadGateway || get() != http.StatusBadGateway {
		t.Fatal("a failing source should answer 502")
	}
	if n := fetches.Load(); n != 1 {
		t.Errorf("source fetched %d times; the failure should be remembered, not retried per request", n)
	}
	// Once the cooldown passes and the source is back, it recovers.
	fail.Store(false)
	time.Sleep(200 * time.Millisecond)
	if c := get(); c != http.StatusOK {
		t.Errorf("after the cooldown got %d, want 200", c)
	}
}
