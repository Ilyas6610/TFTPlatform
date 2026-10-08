package apiserver

import (
	"context"
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
		if !b.startFor("1.2.3.4", fmt.Sprint("k", i), time.Minute, time.Minute, job) {
			t.Fatalf("job %d refused within the quota", i)
		}
	}
	if b.startFor("1.2.3.4", "one-too-many", time.Minute, time.Minute, job) {
		t.Error("a job past the quota started")
	}
	// Joining a run already in flight is free, even past the quota.
	if !b.startFor("1.2.3.4", "k0", time.Minute, time.Minute, job) {
		t.Error("joining a running job was refused")
	}
	// Other clients and the server's own work aren't affected.
	if !b.startFor("5.6.7.8", "other", time.Minute, time.Minute, job) {
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
