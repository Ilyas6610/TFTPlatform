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
	// failed holds keys whose last run failed or ended incomplete, until
	// they succeed; entries whose cooldown is over are dropped once there
	// are more than maxFailedJobs, so the table stays bounded however many
	// distinct keys requests bring.
	failed map[string]jobFailure
}

type jobFailure struct {
	retryAt time.Time
	err     error // nil for an incomplete run
}

const (
	maxFailedJobs = 1000
	// maxRunningJobs caps runs in flight. Each waits on the shared Riot
	// rate limiter, so requests for many distinct keys would otherwise pile
	// up goroutines; past the cap, start declines but still reports the job
	// as pending, so the client keeps polling and its next poll tries again.
	maxRunningJobs = 32
)

// jobFunc does one run of a job. complete reports whether everything it set
// out to do got done; false (e.g. stopped on a rate limit) triggers the
// cooldown just like an error does.
type jobFunc func(ctx context.Context) (complete bool, err error)

// start runs fn in the background under key unless a run is already in
// flight or key is cooling down. It reports whether the job is pending
// after the call: running, or declined only because too many runs are in
// flight (so the caller tells the client to poll again).
func (b *backgroundJobs) start(key string, timeout, cooldown time.Duration, fn jobFunc) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.running == nil {
		b.running = make(map[string]bool)
		b.failed = make(map[string]jobFailure)
	}
	if b.running[key] {
		return true
	}
	if time.Now().Before(b.failed[key].retryAt) {
		return false
	}
	if len(b.running) >= maxRunningJobs {
		return true // busy: pending, retried on the client's next poll
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
		delete(b.running, key)
		if err != nil || !complete {
			b.failed[key] = jobFailure{retryAt: time.Now().Add(cooldown), err: err}
			b.pruneLocked()
		} else {
			delete(b.failed, key)
		}
	}()
	return true
}

// pruneLocked drops failures whose cooldown is over once the table is
// larger than maxFailedJobs. Those keys may run again anyway; they only
// lose their stale marker.
func (b *backgroundJobs) pruneLocked() {
	if len(b.failed) <= maxFailedJobs {
		return
	}
	now := time.Now()
	for k, f := range b.failed {
		if !now.Before(f.retryAt) {
			delete(b.failed, k)
		}
	}
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
	return b.failed[key].err
}
