package riotsync

import (
	"context"
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
