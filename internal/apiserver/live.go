package apiserver

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const (
	// maxLiveFetches caps request-path Riot fetches (match detail, profile
	// lookups) in flight at once, so a flood of uncached requests can't
	// queue up on the rate limiter and starve everything else.
	maxLiveFetches = 4
	// liveFetchWait is how long a request waits for a free slot before
	// getting a 503.
	liveFetchWait = 2 * time.Second

	// notFoundTTL is how long a Riot 404 (unknown match or Riot ID) is
	// remembered, so repeating the request doesn't spend quota again.
	notFoundTTL        = 10 * time.Minute
	maxNotFoundEntries = 10000
)

// liveFetches gates request-path Riot fetches. The zero value is ready.
type liveFetches struct {
	once  sync.Once
	slots chan struct{}

	mu       sync.Mutex
	notFound map[string]time.Time // key -> expiry
}

// acquire takes a fetch slot, waiting up to liveFetchWait. On false it has
// already written a 503; otherwise call the returned release when done.
func (l *liveFetches) acquire(w http.ResponseWriter, ctx context.Context) (release func(), ok bool) {
	l.once.Do(func() { l.slots = make(chan struct{}, maxLiveFetches) })
	timer := time.NewTimer(liveFetchWait)
	defer timer.Stop()
	select {
	case l.slots <- struct{}{}:
		return func() { <-l.slots }, true
	case <-timer.C:
	case <-ctx.Done():
	}
	w.Header().Set("Retry-After", strconv.Itoa(int(liveFetchWait.Seconds())))
	writeError(w, http.StatusServiceUnavailable, "busy", "too many live lookups right now, try again shortly")
	return nil, false
}

// knownMissing reports whether Riot recently answered 404 for key.
func (l *liveFetches) knownMissing(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	exp, ok := l.notFound[key]
	if ok && time.Now().After(exp) {
		delete(l.notFound, key)
		return false
	}
	return ok
}

// rememberMissing records a Riot 404 for key.
func (l *liveFetches) rememberMissing(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.notFound == nil {
		l.notFound = map[string]time.Time{}
	}
	now := time.Now()
	if len(l.notFound) >= maxNotFoundEntries {
		for k, exp := range l.notFound {
			if now.After(exp) {
				delete(l.notFound, k)
			}
		}
		if len(l.notFound) >= maxNotFoundEntries {
			return // full of live entries; skip rather than grow
		}
	}
	l.notFound[key] = now.Add(notFoundTTL)
}
