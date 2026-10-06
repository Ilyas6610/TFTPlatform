package apiserver

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"tft-platform/internal/store/storetest"
)

// fakeShared is an in-memory SharedCache, standing in for Redis.
type fakeShared struct {
	mu     sync.Mutex
	vals   map[string][]byte
	locks  map[string]bool
	ttls   map[string]time.Duration
	failed error // returned by every call when set
}

func newFakeShared() *fakeShared {
	return &fakeShared{vals: map[string][]byte{}, locks: map[string]bool{}, ttls: map[string]time.Duration{}}
}

func (f *fakeShared) Get(_ context.Context, k string) ([]byte, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failed != nil {
		return nil, false, f.failed
	}
	v, ok := f.vals[k]
	return v, ok, nil
}

func (f *fakeShared) Set(_ context.Context, k string, v []byte, ttl time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failed != nil {
		return f.failed
	}
	f.vals[k], f.ttls[k] = v, ttl
	return nil
}

func (f *fakeShared) Lock(_ context.Context, k string, _ time.Duration) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failed != nil {
		return false, f.failed
	}
	if f.locks[k] {
		return false, nil
	}
	f.locks[k] = true
	return true, nil
}

func (f *fakeShared) Unlock(_ context.Context, k string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.locks, k)
	return nil
}

func body(t *testing.T, h http.Handler, path string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("%s: %d %s", path, rec.Code, rec.Body)
	}
	return rec.Body.String()
}

// Two replicas (separate Server values, one database) share results: the
// second serves exactly what the first computed, even though the data has
// changed since, for every kind of cached stat.
func TestSharedCache_ReplicasServeTheSameResult(t *testing.T) {
	st := storetest.New(t)
	shared := newFakeShared()
	a := &Server{Store: st, Shared: shared}
	b := &Server{Store: st, Shared: shared}
	comp := []string{"A", "B", "C", "D", "E", "F", "G", "H"}
	seedBoards(t, st, "NA1_1", [][]string{comp, comp, comp, comp, comp, comp})

	paths := []string{
		"/api/v1/explore/options?set=18",
		"/api/v1/meta/builds?set=18&queue=1100",
		"/api/v1/meta/comps?set=18&queue=1100",
	}
	first := map[string]string{}
	for _, p := range paths {
		first[p] = body(t, NewRouter(a), p)
	}
	seedBoards(t, st, "NA1_2", [][]string{comp, comp, comp, comp, comp, comp})
	for _, p := range paths {
		if got := body(t, NewRouter(b), p); got != first[p] {
			t.Errorf("%s: replica B served a different result:\n a: %s\n b: %s", p, first[p], got)
		}
	}
	// Searches stay in this replica's memory: they're one-offs that would
	// crowd out the entries worth sharing.
	body(t, NewRouter(a), "/api/v1/explore?set=18&queue=1100&unit=A")
	for k := range shared.vals {
		if strings.HasPrefix(k, "explore|") {
			t.Errorf("explorer search %q was written to the shared cache", k)
		}
	}
	for k, ttl := range shared.ttls {
		if ttl != DefaultStatsCacheTTL {
			t.Errorf("%s stored for %v, want %v", k, ttl, DefaultStatsCacheTTL)
		}
	}
	if a.meta.lifetime() != localSharedTTL {
		t.Errorf("local lifetime %v, want %v with a shared cache", a.meta.lifetime(), localSharedTTL)
	}
}

func TestSharedCache_FailureFallsBackToComputing(t *testing.T) {
	st := storetest.New(t)
	shared := newFakeShared()
	shared.failed = errors.New("redis down")
	s := &Server{Store: st, Shared: shared}
	seedBoards(t, st, "NA1_1", [][]string{{"A"}})
	if b := body(t, NewRouter(s), "/api/v1/explore/options?set=18"); !strings.Contains(b, `"A"`) {
		t.Fatalf("options %s should list unit A despite the shared cache failing", b)
	}
}

func TestSharedCache_WaitsForTheReplicaComputing(t *testing.T) {
	shared := newFakeShared()
	shared.locks["k"] = true // another replica holds the claim
	go func() {
		time.Sleep(400 * time.Millisecond)
		shared.Set(context.Background(), "k", []byte(`7`), time.Minute)
	}()
	computed := false
	v, err := sharedFetch(context.Background(), shared, new(sharedBreaker), "k", time.Minute, func(context.Context) (int, error) {
		computed = true
		return 1, nil
	})
	if err != nil || v != 7 || computed {
		t.Errorf("got %v %v computed=%v, want the other replica's 7 without computing", v, err, computed)
	}
}

func TestSharedCache_UndecodableEntryIsRecomputed(t *testing.T) {
	shared := newFakeShared()
	shared.vals["k"] = []byte(`"not an int"`)
	v, err := sharedFetch(context.Background(), shared, new(sharedBreaker), "k", time.Minute, func(context.Context) (int, error) { return 5, nil })
	if err != nil || v != 5 {
		t.Fatalf("got %v %v", v, err)
	}
	if string(shared.vals["k"]) != "5" {
		t.Errorf("entry not replaced: %s", shared.vals["k"])
	}
}

func TestSharedCache_BreakerSkipsAFailingCache(t *testing.T) {
	st := storetest.New(t)
	shared := newFakeShared()
	shared.failed = errors.New("redis down")
	s := &Server{Store: st, Shared: shared}
	seedBoards(t, st, "NA1_1", [][]string{{"A"}})
	h := NewRouter(s)
	body(t, h, "/api/v1/explore/options?set=18")
	if !s.sharedDown.open() {
		t.Fatal("a failing shared cache should be skipped for a while")
	}
	// While open, nothing touches the shared cache.
	shared.failed = nil
	body(t, h, "/api/v1/explore/options?set=19")
	if len(shared.vals) != 0 {
		t.Errorf("shared cache used while skipped: %v", shared.vals)
	}
}
