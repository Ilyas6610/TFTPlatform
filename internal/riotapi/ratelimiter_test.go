package riotapi

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestRateLimiter_BlocksAtAppLimit(t *testing.T) {
	rl := NewRateLimiter(2, 100) // 2 req/sec, generous 2min budget
	ctx := context.Background()

	if err := rl.Acquire(ctx, "test.method"); err != nil {
		t.Fatalf("acquire 1: %v", err)
	}
	if err := rl.Acquire(ctx, "test.method"); err != nil {
		t.Fatalf("acquire 2: %v", err)
	}

	// Third acquire within the same 1s window must wait rather than proceed
	// immediately.
	start := time.Now()
	shortCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if err := rl.Acquire(shortCtx, "test.method"); err == nil {
		t.Fatalf("expected acquire 3 to block past the short deadline, but it returned immediately after %s", time.Since(start))
	}
}

func TestRateLimiter_ReconcileTightensFromHeaders(t *testing.T) {
	rl := NewRateLimiter(20, 100)

	header := http.Header{}
	header.Set("X-App-Rate-Limit", "20:1,100:120")
	// Server reports 19 of 20 already used in the 1s window (e.g. another
	// process shares this key) — local tracking (0 so far) must tighten to
	// match rather than trusting its own undercount.
	header.Set("X-App-Rate-Limit-Count", "19:1,50:120")
	rl.ReportResponse("test.method", header)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// One token remains in the 1s window (19 used of 20); this should
	// succeed...
	if err := rl.Acquire(ctx, "test.method"); err != nil {
		t.Fatalf("expected one remaining token, got error: %v", err)
	}
	// ...but the next one should now block (20/20 used) rather than proceed.
	shortCtx, cancel2 := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel2()
	if err := rl.Acquire(shortCtx, "test.method"); err == nil {
		t.Fatal("expected acquire to block after reconciled count reached the limit")
	}
}

func TestRateLimiter_ReportRateLimitedBlocksUntilRetryAfter(t *testing.T) {
	rl := NewRateLimiter(20, 100)

	header := http.Header{}
	header.Set("Retry-After", "1")
	header.Set("X-Rate-Limit-Type", "application")
	retryAfter := rl.ReportRateLimited("test.method", header)

	if retryAfter != time.Second {
		t.Fatalf("expected retryAfter=1s, got %s", retryAfter)
	}

	shortCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := rl.Acquire(shortCtx, "test.method"); err == nil {
		t.Fatal("expected acquire to be blocked immediately after a reported 429")
	}
}

func TestRateLimiter_MethodLimitIndependentOfApp(t *testing.T) {
	rl := NewRateLimiter(100, 1000) // generous app budget

	header := http.Header{}
	header.Set("X-Method-Rate-Limit", "1:60")
	header.Set("X-Method-Rate-Limit-Count", "0:60")
	rl.ReportResponse("scarce.method", header)

	ctx := context.Background()
	if err := rl.Acquire(ctx, "scarce.method"); err != nil {
		t.Fatalf("first acquire on method budget should succeed: %v", err)
	}

	shortCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if err := rl.Acquire(shortCtx, "scarce.method"); err == nil {
		t.Fatal("expected method-specific limit to block a second call within its window")
	}

	// A different, unrelated method key should be unaffected.
	if err := rl.Acquire(context.Background(), "other.method"); err != nil {
		t.Fatalf("unrelated method should not be blocked: %v", err)
	}
}
