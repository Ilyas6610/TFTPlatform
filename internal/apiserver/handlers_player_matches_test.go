package apiserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func matchJSON(id string, gameDatetime int64, puuid string) string {
	return fmt.Sprintf(`{"metadata":{"match_id":%q},"info":{"game_datetime":%d,"game_version":"test","tft_set_number":18,`+
		`"participants":[{"puuid":%q,"placement":3,"level":8,"units":[],"traits":[]}]}}`, id, gameDatetime, puuid)
}

func getMatches(t *testing.T, s *Server, path string) (int, PlayerMatchesResponse) {
	t.Helper()
	rec := httptest.NewRecorder()
	NewRouter(s).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	var resp PlayerMatchesResponse
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
	}
	return rec.Code, resp
}

func newMatchServer(t *testing.T) (*Server, *fakeRiot) {
	s, riot := newTestServer(t)
	riot.matchIDs["me"] = []string{"NA1_2", "NA1_1"}
	riot.matches["NA1_2"] = matchJSON("NA1_2", 2000, "me")
	riot.matches["NA1_1"] = matchJSON("NA1_1", 1000, "me")
	return s, riot
}

const meMatches = "/api/v1/players/me/matches?region=na1"

func TestPlayerMatches_FirstOpenSyncsInBackground(t *testing.T) {
	s, _ := newMatchServer(t)

	code, resp := getMatches(t, s, meMatches)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if !resp.Refreshing || resp.SyncedAt != nil {
		t.Errorf("expected a sync in flight and no prior sync, got %+v", resp)
	}

	waitForJob(t, s, matchSyncKey("me"))
	_, resp = getMatches(t, s, meMatches)
	if resp.Refreshing || resp.SyncedAt == nil || resp.Stale {
		t.Errorf("expected a finished sync, got %+v", resp)
	}
	if len(resp.Matches) != 2 || resp.Matches[0].MatchID != "NA1_2" || resp.Matches[0].Placement != 3 {
		t.Errorf("expected both matches newest first, got %+v", resp.Matches)
	}
}

func TestPlayerMatches_RecentlySyncedSkipsRiot(t *testing.T) {
	s, riot := newMatchServer(t)
	getMatches(t, s, meMatches)
	waitForJob(t, s, matchSyncKey("me"))
	calls := riot.matchCalls.Load()

	_, resp := getMatches(t, s, meMatches)

	if resp.Refreshing || riot.matchCalls.Load() != calls {
		t.Errorf("expected no sync within %s of the last one", matchHistoryStaleAfter)
	}
}

func TestPlayerMatches_OutdatedHistorySyncsNewMatches(t *testing.T) {
	s, riot := newMatchServer(t)
	getMatches(t, s, meMatches)
	waitForJob(t, s, matchSyncKey("me"))

	if _, err := s.Store.Pool.Exec(t.Context(),
		`UPDATE ingest_puuid_queue SET last_crawled_at = now() - interval '1 hour' WHERE puuid = 'me'`); err != nil {
		t.Fatal(err)
	}
	riot.mu.Lock()
	riot.matchIDs["me"] = []string{"NA1_3", "NA1_2", "NA1_1"}
	riot.matches["NA1_3"] = matchJSON("NA1_3", 3000, "me")
	riot.mu.Unlock()
	calls := riot.matchCalls.Load()

	_, resp := getMatches(t, s, meMatches)
	if !resp.Refreshing {
		t.Fatal("expected an outdated history to trigger a sync")
	}
	waitForJob(t, s, matchSyncKey("me"))
	_, resp = getMatches(t, s, meMatches)

	if len(resp.Matches) != 3 || resp.Matches[0].MatchID != "NA1_3" {
		t.Errorf("expected the new match first, got %+v", resp.Matches)
	}
	if got := riot.matchCalls.Load() - calls; got != 2 {
		t.Errorf("expected 2 requests (ids + the one new match), got %d", got)
	}
}

func TestPlayerMatches_WithoutRegionIsCacheOnly(t *testing.T) {
	s, riot := newMatchServer(t)

	code, resp := getMatches(t, s, "/api/v1/players/me/matches")

	if code != http.StatusOK || resp.Refreshing || len(resp.Matches) != 0 {
		t.Errorf("expected an empty cache-only response, got %d %+v", code, resp)
	}
	if riot.matchCalls.Load() != 0 {
		t.Error("expected no Riot requests without a region")
	}
}

func TestPlayerMatches_UnknownRegionIsBadRequest(t *testing.T) {
	s, _ := newMatchServer(t)

	if code, _ := getMatches(t, s, "/api/v1/players/me/matches?region=nope"); code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", code)
	}
}

func TestPlayerMatches_FailedSyncIsReportedStaleAndCoolsDown(t *testing.T) {
	s, riot := newMatchServer(t)
	riot.matchStatus.Store(http.StatusForbidden)

	getMatches(t, s, meMatches)
	waitForJob(t, s, matchSyncKey("me"))
	calls := riot.matchCalls.Load()

	_, resp := getMatches(t, s, meMatches)
	if !resp.Stale || resp.StaleReason != "riot_api_key_expired" {
		t.Errorf("expected stale with riot_api_key_expired, got %+v", resp)
	}
	if resp.Refreshing || riot.matchCalls.Load() != calls {
		t.Error("expected no retry during cooldown")
	}

	// After the cooldown a working key syncs normally and clears the warning.
	riot.matchStatus.Store(0)
	endCooldown(s, matchSyncKey("me"))
	getMatches(t, s, meMatches)
	waitForJob(t, s, matchSyncKey("me"))
	_, resp = getMatches(t, s, meMatches)
	if resp.Stale || len(resp.Matches) != 2 {
		t.Errorf("expected a clean sync after recovery, got %+v", resp)
	}
}

func TestPlayerMatches_ConcurrentViewsShareOneSync(t *testing.T) {
	s, riot := newMatchServer(t)
	riot.matchGate = make(chan struct{})

	var wg sync.WaitGroup
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, resp := getMatches(t, s, meMatches); !resp.Refreshing {
				t.Error("expected every response to report the sync in flight")
			}
		}()
	}
	wg.Wait()
	close(riot.matchGate)
	waitForJob(t, s, matchSyncKey("me"))

	// One sync: 1 ids request + 2 match fetches.
	if got := riot.matchCalls.Load(); got != 3 {
		t.Errorf("expected 3 match requests from a single sync, got %d", got)
	}
}

// A history longer than the newest-page sync: the first page is synced as
// before, and asking for the next page fetches that page of Riot's ids and
// its matches in the background.
func TestPlayerMatches_OlderPageFetchedFromRiot(t *testing.T) {
	s, riot := newTestServer(t)
	var ids []string
	for i := 23; i >= 1; i-- { // newest first, like Riot
		id := fmt.Sprintf("NA1_%d", i)
		ids = append(ids, id)
		riot.matches[id] = matchJSON(id, int64(i)*1000, "me")
	}
	riot.matchIDs["me"] = ids

	getMatches(t, s, meMatches)
	waitForJob(t, s, matchSyncKey("me"))
	_, first := getMatches(t, s, meMatches+"&offset=0&limit=20")
	if len(first.Matches) != 20 || !first.HasMore {
		t.Fatalf("first page: %d matches, hasMore %v; want 20 and more", len(first.Matches), first.HasMore)
	}

	older := meMatches + "&offset=20&limit=20"
	_, resp := getMatches(t, s, older)
	if len(resp.Matches) != 0 || !resp.Refreshing || !resp.HasMore {
		t.Fatalf("older page before fetch: %+v, want empty and refreshing", resp)
	}
	waitForJob(t, s, olderMatchesKey("me", 20))
	_, resp = getMatches(t, s, older)
	if len(resp.Matches) != 3 || resp.Matches[0].MatchID != "NA1_3" || resp.Refreshing || resp.HasMore {
		t.Errorf("older page after fetch: %+v, want NA1_3..NA1_1 and the end of history", resp)
	}

	// Polling the finished short page doesn't re-ask Riot.
	calls := riot.matchCalls.Load()
	getMatches(t, s, older)
	if riot.matchCalls.Load() != calls {
		t.Error("a finished older page was fetched again")
	}
}

func TestPlayerMatches_RejectsBadOffset(t *testing.T) {
	s := &Server{}
	for _, q := range []string{"offset=-1", "offset=x", "offset=501", "offset=1", "offset=19", "offset=21", "offset=520"} {
		if code, _ := getMatches(t, s, "/api/v1/players/me/matches?"+q); code != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400", q, code)
		}
	}
}

func TestPlayerMatches_RefreshSyncsSoonerThanAPlainView(t *testing.T) {
	s, riot := newMatchServer(t)
	getMatches(t, s, meMatches)
	waitForJob(t, s, matchSyncKey("me"))
	calls := riot.matchCalls.Load()

	// Just synced: a refresh is answered, not run.
	_, resp := getMatches(t, s, meMatches+"&refresh=1")
	if resp.Refreshing || !resp.RefreshTooSoon || riot.matchCalls.Load() != calls {
		t.Errorf("a refresh right after a sync should do nothing, got %+v", resp)
	}

	// A minute old: too fresh for a plain view, fine for a refresh.
	if _, err := s.Store.Pool.Exec(t.Context(),
		`UPDATE ingest_puuid_queue SET last_crawled_at = now() - interval '1 minute' WHERE puuid = 'me'`); err != nil {
		t.Fatal(err)
	}
	if _, resp = getMatches(t, s, meMatches); resp.Refreshing {
		t.Fatal("a plain view shouldn't sync a history that is a minute old")
	}
	if _, resp = getMatches(t, s, meMatches+"&refresh=1"); !resp.Refreshing || resp.RefreshTooSoon {
		t.Errorf("a refresh should start a sync, got %+v", resp)
	}
	waitForJob(t, s, matchSyncKey("me"))
}

// Older pages are whole pages at multiples of 20: limit can't multiply the
// Riot jobs a client starts for one player (it used to be part of the work
// each distinct (offset, limit) pair did).
func TestPlayerMatches_OlderPagesIgnoreLimit(t *testing.T) {
	s, riot := newMatchServer(t)
	ids := make([]string, 0, 25)
	for i := 25; i >= 1; i-- {
		id := fmt.Sprintf("NA1_%d", i)
		ids = append(ids, id)
		riot.matches[id] = matchJSON(id, int64(i)*1000, "me")
	}
	riot.matchIDs["me"] = ids
	getMatches(t, s, meMatches)
	waitForJob(t, s, matchSyncKey("me"))

	_, short := getMatches(t, s, meMatches+"&limit=5")
	if len(short.Matches) != 5 {
		t.Errorf("limit=5 on the first page: got %d games", len(short.Matches))
	}
	_, long := getMatches(t, s, meMatches+"&limit=100")
	if len(long.Matches) != matchHistoryPageSize {
		t.Errorf("limit=100 is capped to a page: got %d games", len(long.Matches))
	}
	// Whatever limit an older page is asked with, it's the same job.
	getMatches(t, s, meMatches+"&offset=20&limit=1")
	getMatches(t, s, meMatches+"&offset=20&limit=100")
	getMatches(t, s, meMatches+"&offset=20")
	waitForJob(t, s, olderMatchesKey("me", 20))
	_, older := getMatches(t, s, meMatches+"&offset=20&limit=1")
	if len(older.Matches) != 5 {
		t.Errorf("older page: got %d games, want the page's 5 (limit ignored)", len(older.Matches))
	}
}
