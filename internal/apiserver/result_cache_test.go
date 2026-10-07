package apiserver

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestResultCache_ReusesUntilExpiry(t *testing.T) {
	now := time.Unix(0, 0)
	c := &resultCache{now: func() time.Time { return now }}
	calls := 0
	compute := func(context.Context) (any, error) { calls++; return calls, nil }

	for i := 0; i < 3; i++ {
		if v, _ := c.get(context.Background(), "k", compute); v != 1 {
			t.Fatalf("call %d: got %v, want the cached 1", i, v)
		}
	}
	now = now.Add(resultCacheTTL)
	if v, _ := c.get(context.Background(), "k", compute); v != 2 {
		t.Errorf("after TTL: got %v, want a recomputed 2", v)
	}
	if v, _ := c.get(context.Background(), "other", compute); v != 3 {
		t.Errorf("other key: got %v, want its own computation", v)
	}
}

func TestResultCache_ErrorsAreNotCached(t *testing.T) {
	c := &resultCache{}
	fail := true
	compute := func(context.Context) (any, error) {
		if fail {
			return nil, errors.New("db down")
		}
		return "ok", nil
	}
	if _, err := c.get(context.Background(), "k", compute); err == nil {
		t.Fatal("expected the error")
	}
	fail = false
	if v, err := c.get(context.Background(), "k", compute); err != nil || v != "ok" {
		t.Errorf("got %v, %v; want a fresh computation after a failure", v, err)
	}
}

func TestResultCache_ConcurrentMissesComputeOnce(t *testing.T) {
	c := &resultCache{}
	var calls atomic.Int32
	release := make(chan struct{})
	compute := func(context.Context) (any, error) {
		calls.Add(1)
		<-release
		return "v", nil
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if v, err := c.get(context.Background(), "k", compute); v != "v" || err != nil {
				t.Errorf("got %v, %v", v, err)
			}
		}()
	}
	time.Sleep(20 * time.Millisecond) // let every caller reach the cache
	close(release)
	wg.Wait()
	if n := calls.Load(); n != 1 {
		t.Errorf("computed %d times, want 1", n)
	}
}

func TestResultCache_Bounded(t *testing.T) {
	c := &resultCache{}
	for i := 0; i < resultCacheMaxEntries*2; i++ {
		c.get(context.Background(), fmt.Sprint(i), func(context.Context) (any, error) { return i, nil })
	}
	if n := len(c.entries); n > resultCacheMaxEntries {
		t.Errorf("%d entries, want at most %d", n, resultCacheMaxEntries)
	}
}

func TestResultCache_LimitsConcurrentComputes(t *testing.T) {
	c := &resultCache{}
	var inFlight, peak atomic.Int32
	release := make(chan struct{})
	compute := func(context.Context) (any, error) {
		n := inFlight.Add(1)
		for p := peak.Load(); n > p && !peak.CompareAndSwap(p, n); p = peak.Load() {
		}
		<-release
		inFlight.Add(-1)
		return "v", nil
	}
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.get(context.Background(), fmt.Sprint(i), compute)
		}()
	}
	time.Sleep(50 * time.Millisecond)
	if n := inFlight.Load(); n != resultCacheMaxComputes {
		t.Errorf("%d computing at once, want %d", n, resultCacheMaxComputes)
	}
	close(release)
	wg.Wait()
	if p := peak.Load(); p > resultCacheMaxComputes {
		t.Errorf("peak %d concurrent computes, want at most %d", p, resultCacheMaxComputes)
	}
}

// A caller that gives up while waiting for a slot gets its context error,
// and the key isn't left stuck: the next caller computes it.
func TestResultCache_WaitingCallerCanLeave(t *testing.T) {
	c := &resultCache{}
	release := make(chan struct{})
	for i := 0; i < resultCacheMaxComputes; i++ {
		go c.get(context.Background(), fmt.Sprint("busy", i), func(context.Context) (any, error) { <-release; return nil, nil })
	}
	time.Sleep(20 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := c.get(ctx, "k", func(context.Context) (any, error) { return "v", nil }); err == nil {
		t.Fatal("expected the caller's context error while all slots are busy")
	}
	close(release)
	if v, err := c.get(context.Background(), "k", func(context.Context) (any, error) { return "v", nil }); v != "v" || err != nil {
		t.Errorf("got %v, %v; want a fresh computation", v, err)
	}
}

// When the caller that created an entry leaves before computing, callers
// that joined it still get a real result, not its cancellation.
func TestResultCache_WaitersDontInheritCancellation(t *testing.T) {
	c := &resultCache{}
	release := make(chan struct{})
	for i := 0; i < resultCacheMaxComputes; i++ {
		go c.get(context.Background(), fmt.Sprint("busy", i), func(context.Context) (any, error) { <-release; return nil, nil })
	}
	time.Sleep(20 * time.Millisecond)

	leaderCtx, cancel := context.WithCancel(context.Background())
	compute := func(context.Context) (any, error) { return "v", nil }
	leaderDone := make(chan error, 1)
	go func() { _, err := c.get(leaderCtx, "k", compute); leaderDone <- err }()
	time.Sleep(20 * time.Millisecond)

	waiterDone := make(chan any, 1)
	go func() {
		v, err := c.get(context.Background(), "k", compute)
		if err != nil {
			t.Errorf("waiter got %v", err)
		}
		waiterDone <- v
	}()
	time.Sleep(20 * time.Millisecond)

	cancel()
	if err := <-leaderDone; err == nil {
		t.Error("leader: expected its own context error")
	}
	close(release)
	select {
	case v := <-waiterDone:
		if v != "v" {
			t.Errorf("waiter got %v, want the computed value", v)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("waiter never answered")
	}
}

// A panicking computation becomes an error, frees its slot and doesn't
// leave the key stuck.
func TestResultCache_PanicReleasesSlot(t *testing.T) {
	c := &resultCache{}
	for i := 0; i < resultCacheMaxComputes+1; i++ {
		if _, err := c.get(context.Background(), "k", func(context.Context) (any, error) { panic("boom") }); err == nil {
			t.Fatal("expected an error from a panicking compute")
		}
	}
	if n := len(c.slots); n != 0 {
		t.Errorf("%d slots still taken after panics", n)
	}
	if v, err := c.get(context.Background(), "k", func(context.Context) (any, error) { return "v", nil }); v != "v" || err != nil {
		t.Errorf("got %v, %v; want a fresh computation", v, err)
	}
}

// A computation that fails after running a long time (the statement timeout)
// isn't retried by every visitor's miss: it's answered with the same error
// for a minute, so it can't keep a compute slot and the database busy.
func TestResultCache_SlowFailureIsRememberedBriefly(t *testing.T) {
	now := time.Unix(0, 0)
	c := &resultCache{now: func() time.Time { return now }}
	boom := errors.New("canceling statement due to statement timeout")
	calls := 0
	failSlowly := true
	compute := func(context.Context) (any, error) {
		calls++
		if failSlowly {
			now = now.Add(resultCacheSlowFailure + time.Second) // the query ran that long
			return nil, boom
		}
		return "ok", nil
	}
	get := func() (any, error) { return c.get(context.Background(), "k", compute) }

	if _, err := get(); !errors.Is(err, boom) {
		t.Fatalf("first call: %v", err)
	}
	for i := 0; i < 5; i++ {
		if _, err := get(); !errors.Is(err, boom) {
			t.Fatalf("repeat %d: %v, want the remembered error", i, err)
		}
	}
	if calls != 1 {
		t.Fatalf("computed %d times within the memory window, want 1", calls)
	}

	now = now.Add(resultCacheFailureTTL / 2)
	if get(); calls != 1 {
		t.Errorf("recomputed after half the window (%d calls)", calls)
	}

	// Once the window is over the computation is tried again, and a success
	// replaces the failure.
	now = now.Add(resultCacheFailureTTL)
	failSlowly = false
	if v, err := get(); err != nil || v != "ok" || calls != 2 {
		t.Errorf("after the window: %v, %v, %d calls; want a fresh success", v, err, calls)
	}
}

func TestResultCache_QuickFailureIsRetriedAtOnce(t *testing.T) {
	now := time.Unix(0, 0)
	c := &resultCache{now: func() time.Time { return now }}
	calls := 0
	compute := func(context.Context) (any, error) {
		calls++
		now = now.Add(100 * time.Millisecond)
		return nil, errors.New("connection reset")
	}
	for i := 0; i < 3; i++ {
		c.get(context.Background(), "k", compute)
	}
	if calls != 3 {
		t.Errorf("a quick failure was remembered: %d calls, want 3", calls)
	}
}
