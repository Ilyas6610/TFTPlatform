package apiserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"tft-platform/internal/riotapi"
)

func TestClientIP(t *testing.T) {
	for _, c := range []struct {
		remote, xff, want string
	}{
		{"203.0.113.7:5000", "", "203.0.113.7"},
		// A direct connection from a public address can't pick its identity.
		{"203.0.113.7:5000", "198.51.100.1", "203.0.113.7"},
		// Behind nginx (a private or loopback peer), its header names the client.
		{"172.18.0.5:40000", "198.51.100.1", "198.51.100.1"},
		{"127.0.0.1:40000", "198.51.100.1", "198.51.100.1"},
		{"10.0.0.2:40000", "1.1.1.1, 198.51.100.1", "198.51.100.1"},
		{"127.0.0.1:40000", "not-an-ip", "127.0.0.1"},
		{"[::1]:40000", "2001:db8::1", "2001:db8::1"},
	} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = c.remote
		if c.xff != "" {
			r.Header.Set("X-Forwarded-For", c.xff)
		}
		if got := clientIP(r); got != c.want {
			t.Errorf("clientIP(%s, %q) = %q, want %q", c.remote, c.xff, got, c.want)
		}
	}
}

func TestBackgroundJobs_PerClientQuota(t *testing.T) {
	var b backgroundJobs
	block := make(chan struct{})
	defer close(block)
	job := func(ctx context.Context) (bool, error) {
		<-block
		return true, nil
	}
	for i := 0; i < jobQuotaPerClient; i++ {
		if ok, _ := b.startFor("1.2.3.4", fmt.Sprint("k", i), time.Minute, time.Minute, job); !ok {
			t.Fatalf("job %d refused within the quota", i)
		}
	}
	ok, wait := b.startFor("1.2.3.4", "one-too-many", time.Minute, time.Minute, job)
	if ok || wait < jobQuotaWindow-time.Second || wait > jobQuotaWindow {
		t.Errorf("past the quota: started=%v, wait %v; want refused with ~%v to wait", ok, wait, jobQuotaWindow)
	}
	// Joining a run already in flight is free, even past the quota.
	if ok, wait := b.startFor("1.2.3.4", "k0", time.Minute, time.Minute, job); !ok || wait != 0 {
		t.Error("joining a running job was refused")
	}
	// Other clients and the server's own work aren't affected.
	if ok, _ := b.startFor("5.6.7.8", "other", time.Minute, time.Minute, job); !ok {
		t.Error("another client was refused")
	}
	if !b.start("server-work", time.Minute, time.Minute, job) {
		t.Error("server work was refused")
	}
}

// A live lookup whose on-demand share is used up answers 503 with
// Retry-After instead of waiting; the request never reaches Riot.
func TestLiveLookup_BudgetExhausted(t *testing.T) {
	s, riot := newTestServer(t)
	riot.matches["NA1_7"] = matchJSON("NA1_7", 1000, "me")
	riot.matches["NA1_8"] = matchJSON("NA1_8", 2000, "me")
	s.RiotLive = s.Riot.WithBudget(riotapi.NewBudget(5, 1), time.Second)

	get := func(id string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		NewRouter(s).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/matches/"+id, nil))
		return rec
	}
	if rec := get("NA1_7"); rec.Code != http.StatusOK {
		t.Fatalf("first lookup: %d %s", rec.Code, rec.Body)
	}
	calls := riot.matchCalls.Load()
	rec := get("NA1_8")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("second lookup: %d %s, want 503", rec.Code, rec.Body)
	}
	if after, _ := strconv.Atoi(rec.Header().Get("Retry-After")); after < 60 {
		t.Errorf("Retry-After = %q, want the 2-minute window", rec.Header().Get("Retry-After"))
	}
	if riot.matchCalls.Load() != calls {
		t.Error("the refused lookup reached Riot")
	}
	// A stored match never needs the budget.
	if rec := get("NA1_7"); rec.Code != http.StatusOK {
		t.Errorf("stored match: %d", rec.Code)
	}
}

// Over the quota, the history page says why it isn't refreshing.
func TestPlayerMatches_QuotaIsReported(t *testing.T) {
	s, _ := newMatchServer(t)
	block := make(chan struct{})
	defer close(block)
	// Use up the test client's quota with jobs that stay running.
	for i := 0; i < jobQuotaPerClient; i++ {
		s.jobs.startFor("192.0.2.1", fmt.Sprint("filler", i), time.Minute, time.Minute, func(ctx context.Context) (bool, error) {
			<-block
			return true, nil
		})
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, meMatches, nil) // RemoteAddr 192.0.2.1
	NewRouter(s).ServeHTTP(rec, req)
	var resp PlayerMatchesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("%d %v %s", rec.Code, err, rec.Body)
	}
	if resp.Refreshing || !resp.Stale || resp.StaleReason != "client_quota" || rec.Header().Get("Retry-After") == "" {
		t.Errorf("over quota: %+v, Retry-After %q; want a stale client_quota answer", resp, rec.Header().Get("Retry-After"))
	}
}
