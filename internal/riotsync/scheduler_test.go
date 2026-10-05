package riotsync

import (
	"context"
	"net/http"
	"net/http/httptest"
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
	if _, err := LoadConfig(); err == nil {
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
