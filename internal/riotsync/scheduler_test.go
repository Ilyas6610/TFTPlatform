package riotsync

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"tft-platform/internal/riotapi"
)

func TestSchedulerRunsEachTaskAtStartupAndRepeats(t *testing.T) {
	var a, b atomic.Int32
	ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
	defer cancel()
	(&Scheduler{Tasks: []Task{
		{Name: "a", Interval: 100 * time.Millisecond, Run: func(context.Context) Outcome { a.Add(1); return Outcome{} }},
		{Name: "b", Interval: time.Hour, Run: func(context.Context) Outcome { b.Add(1); return Outcome{} }},
	}}).Run(ctx)
	if a.Load() < 3 || b.Load() != 1 {
		t.Fatalf("a=%d b=%d, want a>=3 b=1", a.Load(), b.Load())
	}
}

func TestSchedulerPausesOnExpiredKey(t *testing.T) {
	var runs atomic.Int32
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	(&Scheduler{
		KeyRetry: time.Hour,
		Tasks: []Task{
			{Name: "x", Interval: time.Millisecond, Run: func(context.Context) Outcome {
				runs.Add(1)
				return Outcome{Err: &riotapi.ErrKeyExpired{}}
			}},
			{Name: "y", Interval: time.Millisecond, Run: func(context.Context) Outcome { runs.Add(1); return Outcome{} }},
		},
	}).Run(ctx)
	if runs.Load() != 1 {
		t.Fatalf("runs=%d, want 1 (paused after key failure, later task skipped)", runs.Load())
	}
}

func TestLoadConfigRejectsUnknownPlatform(t *testing.T) {
	t.Setenv("RIOTSYNC_PLATFORMS", "na1,mars1")
	if _, err := LoadConfig(8, 40); err == nil {
		t.Fatal("want error for unknown platform")
	}
}

func TestRateLimitWatcherCapturesRetryAfter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	w := &RateLimitWatcher{}
	resp, err := (&http.Client{Transport: w}).Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	s := &Scheduler{RateLimitPause: time.Second, Limits: w}
	d := s.pauseFor(Outcome{Stopped: "rate_limited"})
	if d < 7*time.Second || d > 9*time.Second {
		t.Fatalf("pause = %s, want ~8s (Retry-After 7s + margin)", d)
	}
	// Consumed: the next rate-limited stop without a fresh 429 uses the floor.
	if d := s.pauseFor(Outcome{Stopped: "rate_limited"}); d != time.Second {
		t.Fatalf("second pause = %s, want floor 1s", d)
	}
}

func TestPauseForRateLimitedError(t *testing.T) {
	s := &Scheduler{RateLimitPause: time.Second}
	if d := s.pauseFor(Outcome{Err: &riotapi.ErrRateLimited{}}); d != time.Second {
		t.Fatalf("pause = %s, want 1s", d)
	}
	if d := s.pauseFor(Outcome{}); d != 0 {
		t.Fatalf("pause = %s, want 0", d)
	}
}

func TestPauseForKeyExpiredStop(t *testing.T) {
	// ResolveNames reports a dead key as Stopped, not as an error.
	s := &Scheduler{KeyRetry: time.Hour}
	if d := s.pauseFor(Outcome{Stopped: "key_expired"}); d != time.Hour {
		t.Fatalf("pause = %s, want KeyRetry", d)
	}
}

func TestLoadConfigRejectsUnparsableValues(t *testing.T) {
	t.Setenv("RIOTSYNC_CRAWL_INTERVAL", "5min")
	t.Setenv("RIOTSYNC_CRAWL_REQUESTS", "lots")
	_, err := LoadConfig(8, 40)
	if err == nil || !strings.Contains(err.Error(), "RIOTSYNC_CRAWL_INTERVAL") || !strings.Contains(err.Error(), "RIOTSYNC_CRAWL_REQUESTS") {
		t.Fatalf("err = %v, want both bad values named", err)
	}
}

func TestLoadConfigDefaultsToItsShare(t *testing.T) {
	// The default split gives sync 40% of a personal key: 8/s, 40 per 2 min.
	cfg, err := LoadConfig(8, 40)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RateLimitPerSec != 8 || cfg.RateLimitPer2Min != 40 {
		t.Fatalf("defaults %d/s %d/2min, want the share 8/40", cfg.RateLimitPerSec, cfg.RateLimitPer2Min)
	}
	if cfg.CrawlRequests > cfg.RateLimitPer2Min {
		t.Fatalf("one crawl batch (%d) exceeds the 2-minute cap (%d)", cfg.CrawlRequests, cfg.RateLimitPer2Min)
	}
}

func TestThrottleCapsRequestsPerSecond(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { n.Add(1) }))
	defer srv.Close()
	c := &http.Client{Transport: &Throttle{PerSec: 3, Per2Min: 1000}}

	start := time.Now()
	for i := 0; i < 4; i++ {
		resp, err := c.Get(srv.URL)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	if el := time.Since(start); el < 900*time.Millisecond {
		t.Fatalf("4 requests at 3/s took %s; the 4th should wait ~1s", el)
	}
}

func TestThrottleWaitHonorsContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	c := &http.Client{Transport: &Throttle{PerSec: 1000, Per2Min: 1}}
	resp, err := c.Get(srv.URL) // uses the whole 2-minute budget
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if _, err := c.Do(req); err == nil {
		t.Fatal("want the throttled request to fail when its context ends")
	}
}

func TestLoadConfigRejectsMoreThanItsShare(t *testing.T) {
	t.Setenv("RIOTSYNC_RATE_LIMIT_PER_2MIN", "50")
	if _, err := LoadConfig(8, 40); err == nil || !strings.Contains(err.Error(), "RIOT_BUDGET_SPLIT") {
		t.Fatalf("err = %v, want the share exceeded", err)
	}
	t.Setenv("RIOTSYNC_RATE_LIMIT_PER_2MIN", "30") // below the share is fine
	if cfg, err := LoadConfig(8, 40); err != nil || cfg.RateLimitPer2Min != 30 {
		t.Fatalf("%+v %v", cfg, err)
	}
}
