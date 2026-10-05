package apiserver

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// Requests for many distinct keys must not leave one entry each behind.
func TestBackgroundJobs_Bounded(t *testing.T) {
	var b backgroundJobs
	release := make(chan struct{})
	started := 0
	for i := 0; i < 100; i++ {
		if b.start(fmt.Sprint("run", i), time.Minute, time.Minute, func(context.Context) (bool, error) {
			<-release
			return true, nil
		}) {
			started++
		}
	}
	if started != maxRunningJobs {
		t.Errorf("started %d runs, want the cap of %d", started, maxRunningJobs)
	}
	close(release)
	waitIdle(t, &b)
	if len(b.running) != 0 || len(b.failed) != 0 {
		t.Errorf("after successful runs: %d running, %d failed entries; want none", len(b.running), len(b.failed))
	}

	// Failures are kept for their cooldown, then pruned once over the cap.
	for i := 0; i < maxFailedJobs*3; i++ {
		b.start(fmt.Sprint("fail", i), time.Minute, time.Nanosecond, func(context.Context) (bool, error) {
			return false, errors.New("boom")
		})
		if i%maxRunningJobs == 0 {
			waitIdle(t, &b)
		}
	}
	waitIdle(t, &b)
	if n := len(b.failed); n > maxFailedJobs+maxRunningJobs {
		t.Errorf("%d failed entries kept, want about %d at most", n, maxFailedJobs)
	}
}

func waitIdle(t *testing.T, b *backgroundJobs) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
		b.mu.Lock()
		n := len(b.running)
		b.mu.Unlock()
		if n == 0 {
			return
		}
	}
	t.Fatal("background runs did not finish")
}
