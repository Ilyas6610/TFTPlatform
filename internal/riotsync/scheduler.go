package riotsync

import (
	"context"
	"errors"
	"log"
	"time"

	"tft-platform/internal/riotapi"
)

// Outcome is what one task run reports back to the scheduler.
type Outcome struct {
	Requests int
	Items    int
	// Stopped is set (not an error) when the run ended early on a rate limit
	// or request budget; Err is a real failure.
	Stopped string
	Err     error
}

// Task is one recurring job. Tasks run one at a time, so they never compete
// with each other for the shared rate limiter.
type Task struct {
	Name     string
	Interval time.Duration
	Run      func(ctx context.Context) Outcome
}

// Recorder persists a run's lifecycle (ingest_runs). Optional.
type Recorder interface {
	Start(ctx context.Context, name string) (int64, error)
	Finish(ctx context.Context, id int64, status string, o Outcome) error
}

type Scheduler struct {
	Tasks    []Task
	Recorder Recorder
	// KeyRetry pauses every task after Riot rejects the key.
	KeyRetry time.Duration
	// RateLimitPause pauses every task after a rate-limited stop; if
	// Limits is set, the Retry-After Riot sent is used when it's longer.
	RateLimitPause time.Duration
	Limits         *RateLimitWatcher

	now func() time.Time // tests only
}

// RunOnce runs every task once, in order, ignoring intervals and pauses.
func (s *Scheduler) RunOnce(ctx context.Context) {
	for _, t := range s.Tasks {
		if ctx.Err() != nil {
			return
		}
		s.runTask(ctx, t)
	}
}

// Run executes tasks as they come due until ctx is cancelled. Every task
// runs once at startup.
func (s *Scheduler) Run(ctx context.Context) {
	if s.now == nil {
		s.now = time.Now
	}
	next := make([]time.Time, len(s.Tasks)) // zero time: due immediately
	var pausedUntil time.Time

	for ctx.Err() == nil {
		now := s.now()
		if now.Before(pausedUntil) {
			sleep(ctx, pausedUntil.Sub(now))
			continue
		}
		ranAny := false
		for i, t := range s.Tasks {
			if ctx.Err() != nil {
				return
			}
			if s.now().Before(next[i]) {
				continue
			}
			ranAny = true
			o := s.runTask(ctx, t)
			next[i] = s.now().Add(t.Interval)

			if d := s.pauseFor(o); d > 0 {
				pausedUntil = s.now().Add(d)
				log.Printf("riotsync: pausing %s", d)
				break
			}
		}
		if !ranAny {
			sleep(ctx, time.Until(earliest(next)))
		}
	}
}

func (s *Scheduler) pauseFor(o Outcome) time.Duration {
	var keyExpired *riotapi.ErrKeyExpired
	var rateLimited *riotapi.ErrRateLimited
	switch {
	case errors.As(o.Err, &keyExpired):
		return s.KeyRetry
	case o.Stopped == "rate_limited" || errors.As(o.Err, &rateLimited):
		d := s.RateLimitPause
		if s.Limits != nil {
			// +1s margin: Retry-After is whole seconds.
			if ra := s.Limits.TakeRetryDelay(); ra > 0 && ra+time.Second > d {
				d = ra + time.Second
			}
		}
		return d
	}
	return 0
}

func (s *Scheduler) runTask(ctx context.Context, t Task) Outcome {
	var id int64
	recorded := false
	if s.Recorder != nil {
		var err error
		if id, err = s.Recorder.Start(ctx, t.Name); err != nil {
			log.Printf("riotsync: %s: record start: %v", t.Name, err)
		} else {
			recorded = true
		}
	}

	o := t.Run(ctx)

	status := "completed"
	var keyExpired *riotapi.ErrKeyExpired
	switch {
	case ctx.Err() != nil:
		// Shutdown cut the run short; whatever it reports (usually a
		// "context canceled" error) is not a real failure.
		status = "interrupted"
		o.Err = nil
	case errors.As(o.Err, &keyExpired):
		status = "failed_key_expired"
	case o.Err != nil:
		status = "failed_error"
	case o.Stopped != "":
		status = o.Stopped + "_stopped"
	}
	log.Printf("riotsync: %s: status=%s requests=%d items=%d err=%v", t.Name, status, o.Requests, o.Items, o.Err)

	if recorded {
		// The run's ctx may be cancelled by shutdown; still close the row.
		fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := s.Recorder.Finish(fctx, id, status, o); err != nil {
			log.Printf("riotsync: %s: record finish: %v", t.Name, err)
		}
	}
	return o
}

func earliest(ts []time.Time) time.Time {
	e := ts[0]
	for _, t := range ts[1:] {
		if t.Before(e) {
			e = t
		}
	}
	return e
}

func sleep(ctx context.Context, d time.Duration) {
	if d <= 0 {
		return
	}
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}
