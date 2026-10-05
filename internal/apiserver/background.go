package apiserver

import (
	"context"
	"log"
	"sync"
	"time"
)

// backgroundJobs runs request-triggered Riot work (name resolution, match
// history sync) off the request path. Jobs are keyed so concurrent requests
// for the same thing share one run, and a run that ends incomplete or with
// an error starts a cooldown so polling clients can't turn it into a retry
// loop. The zero value is ready to use.
type backgroundJobs struct {
	mu      sync.Mutex
	running map[string]bool
	retryAt map[string]time.Time
	lastErr map[string]error
}

// jobFunc does one run of a job. complete reports whether everything it set
// out to do got done; false (e.g. stopped on a rate limit) triggers the
// cooldown just like an error does.
type jobFunc func(ctx context.Context) (complete bool, err error)

// start runs fn in the background under key unless a run is already in
// flight or key is cooling down. It reports whether a run is in flight after
// the call.
func (b *backgroundJobs) start(key string, timeout, cooldown time.Duration, fn jobFunc) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.running == nil {
		b.running = make(map[string]bool)
		b.retryAt = make(map[string]time.Time)
		b.lastErr = make(map[string]error)
	}
	if b.running[key] {
		return true
	}
	if time.Now().Before(b.retryAt[key]) {
		return false
	}
	b.running[key] = true

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		complete, err := fn(ctx)
		if err != nil {
			log.Printf("background %s: %v", key, err)
		}

		b.mu.Lock()
		defer b.mu.Unlock()
		b.running[key] = false
		b.lastErr[key] = err
		if err != nil || !complete {
			b.retryAt[key] = time.Now().Add(cooldown)
		} else {
			delete(b.retryAt, key)
		}
	}()
	return true
}

// isRunning reports whether a run for key is in flight.
func (b *backgroundJobs) isRunning(key string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.running[key]
}

// lastError returns the error from key's most recent finished run, if any.
func (b *backgroundJobs) lastError(key string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.lastErr[key]
}
