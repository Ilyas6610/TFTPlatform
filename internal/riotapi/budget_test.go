package riotapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestBudget_PerSecondAndTwoMinuteCaps(t *testing.T) {
	b := NewBudget(2, 3)
	if b.Reserve() != 0 || b.Reserve() != 0 {
		t.Fatal("the first two sends should go at once")
	}
	if w := b.Reserve(); w <= 0 || w > time.Second {
		t.Fatalf("third send within a second: wait %v, want under a second", w)
	}
	// Pretend the first two were sent over a second ago: the per-second
	// window frees up, the 2-minute one (3) still has room for one.
	b.mu.Lock()
	for i := range b.sent {
		b.sent[i] = b.sent[i].Add(-1100 * time.Millisecond)
	}
	b.mu.Unlock()
	if b.Reserve() != 0 {
		t.Fatal("a send should fit after the per-second window passed")
	}
	if w := b.Reserve(); w < 100*time.Second {
		t.Fatalf("fourth send: wait %v, want the 2-minute window", w)
	}
	if NewBudget(0, 0).Per2Min != 1 {
		t.Error("a budget allows at least one request")
	}
}

func TestBudget_AcquireFailsFastPastMaxWait(t *testing.T) {
	b := NewBudget(10, 1)
	if err := b.Acquire(context.Background(), time.Second); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	err := b.Acquire(context.Background(), time.Second)
	var ex *ErrBudgetExhausted
	if !errors.As(err, &ex) || ex.RetryAfter < 100*time.Second {
		t.Fatalf("err = %v, want ErrBudgetExhausted with ~2 min to wait", err)
	}
	if time.Since(start) > 100*time.Millisecond {
		t.Error("an exhausted budget should answer at once, not wait")
	}
	// Without maxWait it waits, until the context ends.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := b.Acquire(ctx, 0); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the context's deadline", err)
	}
}

// A WithBudget view spends its own share; the client it came from, and
// views on other budgets, don't.
func TestClient_WithBudget(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	base := testClient()
	live := base.WithBudget(NewBudget(10, 2), time.Second)
	other := base.WithBudget(NewBudget(10, 1), time.Second)
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		if err := live.do(ctx, "m", srv.URL, nil); err != nil {
			t.Fatal(err)
		}
	}
	var ex *ErrBudgetExhausted
	if err := live.do(ctx, "m", srv.URL, nil); !errors.As(err, &ex) {
		t.Fatalf("third request on a 2-request budget: %v, want ErrBudgetExhausted", err)
	}
	if calls.Load() != 2 {
		t.Errorf("%d requests reached Riot, want 2 (the refused one never left)", calls.Load())
	}
	if err := other.do(ctx, "m", srv.URL, nil); err != nil {
		t.Errorf("another budget's view: %v", err)
	}
	if err := base.do(ctx, "m", srv.URL, nil); err != nil {
		t.Errorf("the unbudgeted client: %v", err)
	}
}
