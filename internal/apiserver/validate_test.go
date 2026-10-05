package apiserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"tft-platform/internal/riotapi/riotapitest"
	"tft-platform/internal/store/storetest"
)

// Malformed ids are rejected before the store, the job table or Riot see
// them: the server has no store here, so touching it would panic.
func TestMalformedIDsRejectedEarly(t *testing.T) {
	var riotCalls atomic.Int32
	s := &Server{Riot: riotapitest.NewClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		riotCalls.Add(1)
	}))}
	for _, path := range []string{
		"/api/v1/matches/NA1_..%2F..%2Fx",
		"/api/v1/matches/NA1_1%3Fa=b",
		"/api/v1/matches/" + strings.Repeat("9", 40),
		"/api/v1/players/..%2F..%2Fx/matches?region=na1",
		"/api/v1/players/a%3Fb/matches?region=na1",
		"/api/v1/players/" + strings.Repeat("a", 101) + "/matches?region=na1",
		"/api/v1/players/na1/" + strings.Repeat("n", 17) + "/NA1",
		"/api/v1/players/na1/name/TOOLONG",
		"/api/v1/players/na1/a%2Fb/NA1",
		"/api/v1/players/na1/name/%20",
	} {
		rec := httptest.NewRecorder()
		NewRouter(s).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400", path, rec.Code)
		}
	}
	if len(s.jobs.running) != 0 {
		t.Errorf("background jobs started for malformed ids")
	}
	if n := riotCalls.Load(); n != 0 {
		t.Errorf("%d Riot requests made, want none", n)
	}
}

func TestValidRiotID(t *testing.T) {
	for _, ok := range [][2]string{{"Alice", "NA1"}, {"하이브리드", "KR1"}, {"Space Name", "EUW"}} {
		if !validRiotID(ok[0], ok[1]) {
			t.Errorf("%v rejected", ok)
		}
	}
}

// A Riot 404 is remembered: asking again doesn't spend another request.
func TestMatchNotFoundIsRemembered(t *testing.T) {
	var riotCalls atomic.Int32
	s := &Server{Store: storetest.New(t), Riot: riotapitest.NewClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		riotCalls.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))}
	for i := 0; i < 3; i++ {
		rec := httptest.NewRecorder()
		NewRouter(s).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/matches/NA1_999", nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("request %d: got %d, want 404", i, rec.Code)
		}
	}
	if n := riotCalls.Load(); n != 1 {
		t.Errorf("%d Riot requests, want 1", n)
	}
}

func TestLiveFetches_CapsConcurrency(t *testing.T) {
	var l liveFetches
	var releases []func()
	for i := 0; i < maxLiveFetches; i++ {
		release, ok := l.acquire(httptest.NewRecorder(), context.Background())
		if !ok {
			t.Fatalf("slot %d refused", i)
		}
		releases = append(releases, release)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	rec := httptest.NewRecorder()
	if _, ok := l.acquire(rec, ctx); ok || rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") == "" {
		t.Errorf("over the cap: ok=%v code=%d, want a 503 with Retry-After", ok, rec.Code)
	}
	releases[0]()
	if _, ok := l.acquire(httptest.NewRecorder(), context.Background()); !ok {
		t.Error("slot not reusable after release")
	}
}
