package riotsync

import (
	"net/http"
	"sync"
	"time"
)

// Throttle is an http.RoundTripper wrapper that caps how many requests
// riotsync sends, over a short and a long sliding window. The Riot client's
// own RateLimiter syncs its limits to the key's full budget (from Riot's
// response headers), so it can't reserve any of it for the API server;
// this cap does, because riotsync and the API server share one key.
type Throttle struct {
	Base            http.RoundTripper
	PerSec, Per2Min int

	mu   sync.Mutex
	sent []time.Time // send times within the last 2 minutes, oldest first
}

func (t *Throttle) RoundTrip(r *http.Request) (*http.Response, error) {
	for {
		wait := t.reserve()
		if wait <= 0 {
			break
		}
		select {
		case <-r.Context().Done():
			return nil, r.Context().Err()
		case <-time.After(wait):
		}
	}
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(r)
}

// reserve records a send and returns 0 if allowed now, else how long to wait.
func (t *Throttle) reserve() time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	for len(t.sent) > 0 && now.Sub(t.sent[0]) >= 2*time.Minute {
		t.sent = t.sent[1:]
	}
	if len(t.sent) >= t.Per2Min {
		return t.sent[len(t.sent)-t.Per2Min].Add(2 * time.Minute).Sub(now)
	}
	if len(t.sent) >= t.PerSec {
		if d := t.sent[len(t.sent)-t.PerSec].Add(time.Second).Sub(now); d > 0 {
			return d
		}
	}
	t.sent = append(t.sent, now)
	return 0
}
