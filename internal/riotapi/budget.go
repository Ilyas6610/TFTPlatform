package riotapi

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Budget is a fixed share of the key for one kind of traffic (background
// sync, request-triggered lookups, history backfills): at most PerSec
// requests in any second and Per2Min in any two minutes. Unlike RateLimiter
// it is never reconciled with Riot's response headers, which report the
// whole key's usage; a share has to stay a share. It keeps an exact sliding
// log of send times, so it can't let twice the limit through across a
// window boundary.
//
// A Budget limits only its own traffic; the process's RateLimiter still
// guards the key as a whole (Client.WithBudget consults both).
type Budget struct {
	PerSec, Per2Min int

	mu   sync.Mutex
	sent []time.Time // send times within the last 2 minutes, oldest first
}

// NewBudget returns a budget allowing perSec requests per second and
// per2Min per two minutes (each at least 1).
func NewBudget(perSec, per2Min int) *Budget {
	return &Budget{PerSec: max(perSec, 1), Per2Min: max(per2Min, 1)}
}

// Reserve records a send and returns 0 if one is allowed now; otherwise it
// records nothing and returns how long until one is.
func (b *Budget) Reserve() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	for len(b.sent) > 0 && now.Sub(b.sent[0]) >= 2*time.Minute {
		b.sent = b.sent[1:]
	}
	if len(b.sent) >= b.Per2Min {
		return b.sent[len(b.sent)-b.Per2Min].Add(2 * time.Minute).Sub(now)
	}
	if len(b.sent) >= b.PerSec {
		if d := b.sent[len(b.sent)-b.PerSec].Add(time.Second).Sub(now); d > 0 {
			return d
		}
	}
	b.sent = append(b.sent, now)
	return 0
}

// Acquire waits, respecting ctx, until a send is allowed and records it.
// With maxWait > 0, a wait longer than that isn't attempted: it returns
// *ErrBudgetExhausted saying when to retry, so an interactive caller can
// answer at once instead of holding the request.
func (b *Budget) Acquire(ctx context.Context, maxWait time.Duration) error {
	for {
		wait := b.Reserve()
		if wait <= 0 {
			return nil
		}
		if maxWait > 0 && wait > maxWait {
			return &ErrBudgetExhausted{RetryAfter: wait}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

// ErrBudgetExhausted means a Budget's share is used up for longer than the
// caller was willing to wait.
type ErrBudgetExhausted struct {
	RetryAfter time.Duration
}

func (e *ErrBudgetExhausted) Error() string {
	return fmt.Sprintf("riot api budget exhausted; retry in %s", e.RetryAfter.Round(time.Second))
}
