package ingest_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"tft-platform/internal/ingest"
	"tft-platform/internal/riotapi"
	"tft-platform/internal/riotapi/riotapitest"
	"tft-platform/internal/store"
	"tft-platform/internal/store/storetest"
)

// enqueue registers puuid (the queue references accounts) and queues it.
func enqueue(t *testing.T, st *store.Store, puuid string, priority int16) {
	t.Helper()
	ctx := context.Background()
	if err := st.UpsertAccountPUUIDOnly(ctx, puuid, "americas"); err != nil {
		t.Fatal(err)
	}
	if err := st.EnqueuePUUID(ctx, puuid, "na1", "americas", priority); err != nil {
		t.Fatal(err)
	}
}

func queueState(t *testing.T, st *store.Store, puuid string) (crawled bool, failures int, nextAttempt *time.Time) {
	t.Helper()
	var at *time.Time
	if err := st.Pool.QueryRow(context.Background(),
		`SELECT last_crawled_at, failures, next_attempt_at FROM ingest_puuid_queue WHERE puuid = $1`, puuid).
		Scan(&at, &failures, &nextAttempt); err != nil {
		t.Fatal(err)
	}
	return at != nil, failures, nextAttempt
}

func TestCrawlQueue_FailingPlayerDoesNotBlockTheQueue(t *testing.T) {
	st := storetest.New(t)
	riot := newFakeRiot()
	client := riotapitest.NewClient(t, riot)
	ctx := context.Background()

	// "bad" is first in line (higher priority) but Riot rejects its id list.
	enqueue(t, st, "bad", 30)
	enqueue(t, st, "good", 10)
	riot.status["bad/ids"] = 400
	riot.matchIDs["good"] = []string{"NA1_1"}
	riot.matches["NA1_1"] = matchJSON("NA1_1", 1000, "good", "other")

	res, err := ingest.CrawlQueue(ctx, client, st, 10, 20, 50)
	if err != nil {
		t.Fatalf("a failing player must not fail the run: %v", err)
	}
	if res.PUUIDsFailed != 1 || res.MatchesIngested != 1 {
		t.Errorf("want 1 failure and the healthy player's match stored, got %+v", res)
	}
	if crawled, failures, next := queueState(t, st, "good"); !crawled || failures != 0 || next != nil {
		t.Errorf("good: crawled=%v failures=%d next=%v", crawled, failures, next)
	}
	crawled, failures, next := queueState(t, st, "bad")
	if crawled || failures != 1 || next == nil || !next.After(time.Now()) {
		t.Errorf("bad should be backing off and not marked crawled: crawled=%v failures=%d next=%v", crawled, failures, next)
	}

	// While backing off the failing player isn't offered again...
	batch, err := st.NextQueueBatch(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range batch {
		if q.PUUID == "bad" {
			t.Error("a player in backoff was offered again")
		}
	}
	// ...and is once the backoff is over; a later success clears it.
	if _, err := st.Pool.Exec(ctx, `UPDATE ingest_puuid_queue SET next_attempt_at = now() - interval '1 second' WHERE puuid = 'bad'`); err != nil {
		t.Fatal(err)
	}
	delete(riot.status, "bad/ids")
	if _, err := ingest.CrawlQueue(ctx, client, st, 10, 20, 50); err != nil {
		t.Fatal(err)
	}
	if crawled, failures, next := queueState(t, st, "bad"); !crawled || failures != 0 || next != nil {
		t.Errorf("a successful retry should clear the backoff: crawled=%v failures=%d next=%v", crawled, failures, next)
	}
}

func TestCrawlQueue_BackoffGrowsAndIsCapped(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()
	enqueue(t, st, "p", 0)
	var last time.Duration
	for i := 1; i <= 3; i++ {
		if err := st.MarkCrawlFailed(ctx, "p", "boom"); err != nil {
			t.Fatal(err)
		}
		var secs float64
		if err := st.Pool.QueryRow(ctx, `SELECT extract(epoch FROM next_attempt_at - now()) FROM ingest_puuid_queue WHERE puuid = 'p'`).Scan(&secs); err != nil {
			t.Fatal(err)
		}
		d := time.Duration(secs * float64(time.Second))
		if d <= last {
			t.Errorf("failure %d: backoff %v should be longer than %v", i, d, last)
		}
		last = d
	}
	for i := 0; i < 40; i++ {
		if err := st.MarkCrawlFailed(ctx, "p", "boom"); err != nil {
			t.Fatal(err)
		}
	}
	var secs float64
	if err := st.Pool.QueryRow(ctx, `SELECT extract(epoch FROM next_attempt_at - now()) FROM ingest_puuid_queue WHERE puuid = 'p'`).Scan(&secs); err != nil {
		t.Fatal(err)
	}
	if secs > 24*3600+5 {
		t.Errorf("backoff %v exceeds a day", time.Duration(secs*float64(time.Second)))
	}
}

func TestCrawlQueue_RateLimitedIDListIsNotMarkedCrawled(t *testing.T) {
	st := storetest.New(t)
	riot := newFakeRiot()
	client := riotapitest.NewClient(t, riot)
	ctx := context.Background()
	enqueue(t, st, "p", 10)
	riot.status["p/ids"] = 429

	res, err := ingest.CrawlQueue(ctx, client, st, 10, 20, 50)
	if err != nil {
		t.Fatal(err)
	}
	if res.Stopped != "rate_limited" {
		t.Fatalf("want a rate-limit stop, got %+v", res)
	}
	if crawled, failures, _ := queueState(t, st, "p"); crawled || failures != 0 {
		t.Errorf("a rate-limited id fetch crawled nothing: crawled=%v failures=%d", crawled, failures)
	}
	synced, err := st.MatchHistorySyncedAt(ctx, "p")
	if err != nil || synced != nil {
		t.Errorf("the player must not look synced: %v %v", synced, err)
	}
}

func TestCrawlQueue_ExpiredKeyEndsTheRun(t *testing.T) {
	st := storetest.New(t)
	riot := newFakeRiot()
	client := riotapitest.NewClient(t, riot)
	ctx := context.Background()
	enqueue(t, st, "p", 10)
	riot.status["p/ids"] = 403

	if _, err := ingest.CrawlQueue(ctx, client, st, 10, 20, 50); err == nil {
		t.Fatal("an expired key should end the run with an error")
	}
	if _, failures, _ := queueState(t, st, "p"); failures != 0 {
		t.Errorf("a key problem isn't the player's fault: failures=%d", failures)
	}
}

// Riot being down isn't any one player's fault: the run ends and nobody is
// backed off, so the queue is whole again as soon as Riot is.
func TestCrawlQueue_RiotOutageEndsTheRunWithoutBackingOffPlayers(t *testing.T) {
	st := storetest.New(t)
	riot := newFakeRiot()
	client := riotapitest.NewClient(t, riot)
	ctx := context.Background()
	enqueue(t, st, "first", 30)
	enqueue(t, st, "second", 20)
	riot.status["first/ids"] = 503
	riot.status["second/ids"] = 503

	_, err := ingest.CrawlQueue(ctx, client, st, 10, 20, 50)
	var unavailable *riotapi.ErrUnavailable
	if !errors.As(err, &unavailable) {
		t.Fatalf("an outage should end the run with ErrUnavailable, got %v", err)
	}
	for _, p := range []string{"first", "second"} {
		if crawled, failures, next := queueState(t, st, p); crawled || failures != 0 || next != nil {
			t.Errorf("%s: crawled=%v failures=%d next=%v; an outage must leave players untouched", p, crawled, failures, next)
		}
	}
	// Only the first player was tried before the run ended.
	for _, path := range riot.requestPaths() {
		if strings.Contains(path, "second") {
			t.Errorf("the run went on to the next player: %s", path)
		}
	}
}
