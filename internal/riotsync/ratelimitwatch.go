package riotsync

import (
	"net/http"
	"strconv"
	"sync"
	"time"
)

// RateLimitWatcher is an http.RoundTripper wrapper that notes Riot's 429
// responses. The ingest packages report a rate-limit stop as just
// "rate_limited", dropping Retry-After, so the scheduler reads it here to
// pause for exactly as long as Riot asks instead of a fixed guess.
type RateLimitWatcher struct {
	Base http.RoundTripper

	mu    sync.Mutex
	until time.Time
}

func (w *RateLimitWatcher) RoundTrip(r *http.Request) (*http.Response, error) {
	base := w.Base
	if base == nil {
		base = http.DefaultTransport
	}
	resp, err := base.RoundTrip(r)
	if err == nil && resp.StatusCode == http.StatusTooManyRequests {
		retry := time.Second
		if secs, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && secs > 0 {
			retry = time.Duration(secs) * time.Second
		}
		w.mu.Lock()
		if t := time.Now().Add(retry); t.After(w.until) {
			w.until = t
		}
		w.mu.Unlock()
	}
	return resp, err
}

// TakeRetryDelay returns how long Riot last asked us to wait and clears it;
// zero if there was no 429 since the last call or the wait already passed.
func (w *RateLimitWatcher) TakeRetryDelay() time.Duration {
	w.mu.Lock()
	defer w.mu.Unlock()
	d := time.Until(w.until)
	w.until = time.Time{}
	if d < 0 {
		return 0
	}
	return d
}
