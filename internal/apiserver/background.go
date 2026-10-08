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
	// starts is when each client (IP) started its recent runs, for
	// jobQuotaPerClient; pruned past maxQuotaClients.
	starts map[string][]time.Time
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
	// jobQuotaPerClient caps how many new runs one client (IP) may start per
	// jobQuotaWindow. Each run can make dozens of Riot requests, so a
	// per-request rate limit (nginx) doesn't bound what one client costs;
	// joining a run that's already going is free.
	jobQuotaPerClient = 10
	jobQuotaWindow    = 2 * time.Minute
	maxQuotaClients   = 10000
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
	pending, _ := b.startFor("", key, timeout, cooldown, fn)
	return pending
}

// startFor is start on behalf of client (an IP; "" for the server's own
// work): a new run also counts against client's quota. Past it the job
// isn't started (not pending, so the client stops polling) and overQuota
// says how long until the client may start another, so the response can
// say why nothing happened.
func (b *backgroundJobs) startFor(client, key string, timeout, cooldown time.Duration, fn jobFunc) (pending bool, overQuota time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.running == nil {
		b.running = make(map[string]bool)
		b.failed = make(map[string]jobFailure)
		b.starts = make(map[string][]time.Time)
	}
	if b.running[key] {
		return true, 0
	}
	if time.Now().Before(b.failed[key].retryAt) {
		return false, 0
	}
	if len(b.running) >= maxRunningJobs {
		return true, 0 // busy: pending, retried on the client's next poll
	}
	if client != "" {
		if wait := b.takeQuotaLocked(client); wait > 0 {
			log.Printf("background %s: %s is over its job quota", key, client)
			return false, wait
		}
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
	return true, 0
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

// takeQuotaLocked records a new run for client if it's under
// jobQuotaPerClient in the last jobQuotaWindow and returns 0; otherwise it
// records nothing and returns how long until the oldest run leaves the
// window.
func (b *backgroundJobs) takeQuotaLocked(client string) time.Duration {
	now := time.Now()
	recent := b.starts[client]
	for len(recent) > 0 && now.Sub(recent[0]) >= jobQuotaWindow {
		recent = recent[1:]
	}
	if len(recent) >= jobQuotaPerClient {
		b.starts[client] = recent
		return recent[len(recent)-jobQuotaPerClient].Add(jobQuotaWindow).Sub(now)
	}
	if len(b.starts) >= maxQuotaClients {
		// Forget clients with nothing in the window; the table stays
		// bounded however many addresses send requests.
		for c, ts := range b.starts {
			if len(ts) == 0 || now.Sub(ts[len(ts)-1]) >= jobQuotaWindow {
				delete(b.starts, c)
			}
		}
	}
	b.starts[client] = append(recent, now)
	return 0
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
