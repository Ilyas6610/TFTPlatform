package apiserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"tft-platform/internal/riotapi"
	"tft-platform/internal/riotapi/riotapitest"
	"tft-platform/internal/store"
	"tft-platform/internal/store/storetest"
)

// fakeRiot serves a fixed apex ladder and account-v1 by-puuid lookups.
// accountGate, when set, holds each account lookup until it's closed, so
// tests can observe a resolution run in flight.
type fakeRiot struct {
	ladderStatus atomic.Int32 // non-zero forces this status on league requests
	ladderCalls  atomic.Int32
	accountCalls atomic.Int32
	mu           sync.Mutex
	challengers  []riotapi.LeagueEntry
	accounts     map[string]riotapi.Account
	accountGate  chan struct{}
}

func (f *fakeRiot) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if tier, ok := strings.CutPrefix(r.URL.Path, "/tft/league/v1/"); ok {
		f.ladderCalls.Add(1)
		if code := f.ladderStatus.Load(); code != 0 {
			w.WriteHeader(int(code))
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		var entries []riotapi.LeagueEntry
		if tier == "challenger" {
			entries = f.challengers
		}
		json.NewEncoder(w).Encode(riotapi.LeagueList{Tier: strings.ToUpper(tier), Entries: entries})
		return
	}
	if puuid, ok := strings.CutPrefix(r.URL.Path, "/riot/account/v1/accounts/by-puuid/"); ok {
		f.accountCalls.Add(1)
		if f.accountGate != nil {
			<-f.accountGate
		}
		f.mu.Lock()
		a, ok := f.accounts[puuid]
		f.mu.Unlock()
		if ok {
			json.NewEncoder(w).Encode(a)
			return
		}
	}
	w.WriteHeader(http.StatusNotFound)
}

func newTestServer(t *testing.T) (*Server, *fakeRiot) {
	t.Helper()
	riot := &fakeRiot{
		challengers: []riotapi.LeagueEntry{{PUUID: "p1", LeaguePoints: 1500}, {PUUID: "p2", LeaguePoints: 1400}},
		accounts: map[string]riotapi.Account{
			"p1": {PUUID: "p1", GameName: "Alice", TagLine: "NA1"},
			"p2": {PUUID: "p2", GameName: "Bob", TagLine: "NA1"},
		},
	}
	return &Server{Riot: riotapitest.NewClient(t, riot), Store: storetest.New(t)}, riot
}

func getLeaderboard(t *testing.T, s *Server, path string) (int, LeaderboardResponse) {
	t.Helper()
	rec := httptest.NewRecorder()
	NewRouter(s).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	var resp LeaderboardResponse
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
	}
	return rec.Code, resp
}

// waitForResolution blocks until no name-resolution run is in flight.
func waitForResolution(t *testing.T, s *Server, platform string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s.leaderboard.mu.Lock()
		running := s.leaderboard.resolving[platform]
		s.leaderboard.mu.Unlock()
		if !running {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("name resolution did not finish")
}

func ageSnapshot(t *testing.T, st *store.Store, platform string, by time.Duration) {
	t.Helper()
	if _, err := st.Pool.Exec(context.Background(),
		`UPDATE league_entries SET fetched_at = fetched_at - $2::interval WHERE platform_region = $1`,
		platform, by.String()); err != nil {
		t.Fatal(err)
	}
}

func TestLeaderboard_FirstOpenFetchesLadderAndResolvesNames(t *testing.T) {
	s, riot := newTestServer(t)

	code, resp := getLeaderboard(t, s, "/api/v1/leaderboard/na1")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if riot.ladderCalls.Load() != 3 {
		t.Errorf("expected 3 ladder requests, got %d", riot.ladderCalls.Load())
	}
	if len(resp.Entries) != 2 || resp.Entries[0].PUUID != "p1" || resp.FetchedAt == nil || resp.Stale {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if !resp.Resolving {
		t.Error("expected resolving=true while names are unknown")
	}

	waitForResolution(t, s, "na1")
	_, resp = getLeaderboard(t, s, "/api/v1/leaderboard/na1")
	if resp.Resolving {
		t.Error("expected resolving=false once all names are known")
	}
	if g := resp.Entries[0].GameName; g == nil || *g != "Alice" {
		t.Errorf("expected Alice, got %v", g)
	}
	if g := resp.Entries[1].GameName; g == nil || *g != "Bob" {
		t.Errorf("expected Bob, got %v", g)
	}
}

func TestLeaderboard_FreshSnapshotSkipsRiot(t *testing.T) {
	s, riot := newTestServer(t)
	getLeaderboard(t, s, "/api/v1/leaderboard/na1")
	waitForResolution(t, s, "na1")
	ladder, accounts := riot.ladderCalls.Load(), riot.accountCalls.Load()

	getLeaderboard(t, s, "/api/v1/leaderboard/na1")

	if riot.ladderCalls.Load() != ladder || riot.accountCalls.Load() != accounts {
		t.Errorf("expected no Riot requests for a fresh, fully named snapshot")
	}
}

func TestLeaderboard_StaleSnapshotIsRefreshed(t *testing.T) {
	s, riot := newTestServer(t)
	getLeaderboard(t, s, "/api/v1/leaderboard/na1")
	waitForResolution(t, s, "na1")

	ageSnapshot(t, s.Store, "na1", leaderboardStaleAfter+time.Minute)
	riot.mu.Lock()
	riot.challengers = []riotapi.LeagueEntry{{PUUID: "p2", LeaguePoints: 1600}} // p1 dropped out
	riot.mu.Unlock()

	_, resp := getLeaderboard(t, s, "/api/v1/leaderboard/na1")

	if riot.ladderCalls.Load() != 6 {
		t.Errorf("expected a second 3-request refresh, got %d ladder requests total", riot.ladderCalls.Load())
	}
	if len(resp.Entries) != 1 || resp.Entries[0].PUUID != "p2" || resp.Entries[0].LeaguePoints != 1600 {
		t.Errorf("expected refreshed ladder with only p2, got %+v", resp.Entries)
	}
}

func TestLeaderboard_FailedRefreshServesCachedSnapshotAsStale(t *testing.T) {
	s, riot := newTestServer(t)
	getLeaderboard(t, s, "/api/v1/leaderboard/na1")
	waitForResolution(t, s, "na1")
	ageSnapshot(t, s.Store, "na1", leaderboardStaleAfter+time.Minute)

	riot.ladderStatus.Store(http.StatusForbidden)
	code, resp := getLeaderboard(t, s, "/api/v1/leaderboard/na1")

	if code != http.StatusOK {
		t.Fatalf("expected cached snapshot with 200, got %d", code)
	}
	if !resp.Stale || resp.StaleReason != "riot_api_key_expired" {
		t.Errorf("expected stale with riot_api_key_expired, got stale=%v reason=%q", resp.Stale, resp.StaleReason)
	}
	if len(resp.Entries) != 2 {
		t.Errorf("expected cached entries, got %d", len(resp.Entries))
	}
}

func TestLeaderboard_FailedRefreshWithNoCacheIsAnError(t *testing.T) {
	s, riot := newTestServer(t)
	riot.ladderStatus.Store(http.StatusForbidden)

	code, _ := getLeaderboard(t, s, "/api/v1/leaderboard/na1")

	if code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", code)
	}
}

func TestLeaderboard_UnknownRegionIsBadRequest(t *testing.T) {
	s, riot := newTestServer(t)

	code, _ := getLeaderboard(t, s, "/api/v1/leaderboard/nope")

	if code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", code)
	}
	if riot.ladderCalls.Load() != 0 {
		t.Error("expected no Riot requests for an unknown region")
	}
}

func TestLeaderboard_ConcurrentRequestsShareOneRefreshAndOneResolution(t *testing.T) {
	s, riot := newTestServer(t)
	riot.accountGate = make(chan struct{})

	var wg sync.WaitGroup
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, resp := getLeaderboard(t, s, "/api/v1/leaderboard/na1")
			if !resp.Resolving {
				t.Error("expected every response to report resolving while the run is in flight")
			}
		}()
	}
	wg.Wait()
	close(riot.accountGate)
	waitForResolution(t, s, "na1")

	if riot.ladderCalls.Load() != 3 {
		t.Errorf("expected one refresh (3 requests) across 5 requests, got %d", riot.ladderCalls.Load())
	}
	if riot.accountCalls.Load() != 2 {
		t.Errorf("expected one resolution run (2 lookups), got %d", riot.accountCalls.Load())
	}
}

func TestLeaderboard_UnresolvableNamesCoolDownBeforeRetry(t *testing.T) {
	s, riot := newTestServer(t)
	riot.mu.Lock()
	delete(riot.accounts, "p2") // Riot doesn't know p2
	riot.mu.Unlock()

	getLeaderboard(t, s, "/api/v1/leaderboard/na1")
	waitForResolution(t, s, "na1")
	calls := riot.accountCalls.Load()

	_, resp := getLeaderboard(t, s, "/api/v1/leaderboard/na1")

	if resp.Resolving {
		t.Error("expected no new run during cooldown")
	}
	if riot.accountCalls.Load() != calls {
		t.Errorf("expected no lookups during cooldown, got %d more", riot.accountCalls.Load()-calls)
	}

	// Once the cooldown has passed, the leftover is retried.
	s.leaderboard.mu.Lock()
	s.leaderboard.retryAt["na1"] = time.Now().Add(-time.Second)
	s.leaderboard.mu.Unlock()
	_, resp = getLeaderboard(t, s, "/api/v1/leaderboard/na1")
	waitForResolution(t, s, "na1")
	if !resp.Resolving || riot.accountCalls.Load() != calls+1 {
		t.Errorf("expected a retry of p2 after cooldown (resolving=%v, lookups=%d)", resp.Resolving, riot.accountCalls.Load()-calls)
	}
}
