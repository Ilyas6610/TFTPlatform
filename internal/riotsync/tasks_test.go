package riotsync_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"tft-platform/internal/riotapi"
	"tft-platform/internal/riotapi/riotapitest"
	"tft-platform/internal/riotsync"
	"tft-platform/internal/store/storetest"
)

func TestAcquireSingleton_SecondInstanceIsRefusedUntilRelease(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()

	ok, release, err := riotsync.AcquireSingleton(ctx, st)
	if err != nil || !ok {
		t.Fatalf("first acquire: ok=%v err=%v", ok, err)
	}
	if ok2, _, err := riotsync.AcquireSingleton(ctx, st); err != nil || ok2 {
		t.Fatalf("second acquire while held: ok=%v err=%v, want refused", ok2, err)
	}
	release()
	ok3, release3, err := riotsync.AcquireSingleton(ctx, st)
	if err != nil || !ok3 {
		t.Fatalf("acquire after release: ok=%v err=%v", ok3, err)
	}
	release3()
}

// A seed task run through the scheduler hits the (fake) league endpoints and
// leaves a finished ingest_runs row.
func TestSchedulerRecordsRuns(t *testing.T) {
	st := storetest.New(t)
	var paths []string
	riot := riotapitest.NewClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if strings.HasPrefix(r.URL.Path, "/tft/league/v1/") {
			json.NewEncoder(w).Encode(riotapi.LeagueList{Entries: []riotapi.LeagueEntry{}})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))

	cfg := riotsync.Config{
		Platforms:     []riotapi.PlatformRegion{riotapi.PlatformNA1},
		SeedInterval:  time.Hour,
		CrawlInterval: time.Hour, NamesInterval: time.Hour, AggregateInterval: time.Hour,
		CrawlPUUIDs: 1, CrawlIDsPerPUUID: 1, CrawlRequests: 1, NamesBatch: 1,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	(&riotsync.Scheduler{
		Tasks:    riotsync.BuildTasks(cfg, riot, st),
		Recorder: riotsync.StoreRecorder{Store: st},
	}).RunOnce(ctx)

	if len(paths) != 3 {
		t.Fatalf("league requests = %v, want one per apex tier", paths)
	}
	var runs, unfinished int
	st.Pool.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE finished_at IS NULL) FROM ingest_runs`).Scan(&runs, &unfinished)
	if runs != 4 || unfinished != 0 {
		t.Fatalf("ingest_runs: %d rows, %d unfinished; want 4 (seed, crawl, names, aggregate), 0", runs, unfinished)
	}
}

func TestCloseStale_OnlyTouchesUnfinishedRiotsyncRuns(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()
	rec := riotsync.StoreRecorder{Store: st}

	stale, _ := rec.Start(ctx, "crawl_queue") // riotsync run that never finished
	done, _ := rec.Start(ctx, "aggregate")    // riotsync run that finished
	rec.Finish(ctx, done, "completed", riotsync.Outcome{})
	foreign, _ := st.StartIngestRun(ctx, "crawl_queue") // ingestcli run, still running

	n, err := rec.CloseStale(ctx)
	if err != nil || n != 1 {
		t.Fatalf("CloseStale = %d, %v; want 1", n, err)
	}
	status := func(id int64) (s string) {
		st.Pool.QueryRow(ctx, `SELECT status FROM ingest_runs WHERE id=$1`, id).Scan(&s)
		return
	}
	if got := status(stale); got != "failed_interrupted" {
		t.Errorf("stale run status = %q", got)
	}
	if got := status(done); got != "completed" {
		t.Errorf("finished run status = %q", got)
	}
	if got := status(foreign); got != "running" {
		t.Errorf("ingestcli run status = %q, want untouched", got)
	}
}

// Cancelling mid-run (shutdown) still closes the run row, as "interrupted".
func TestShutdownMidRunClosesRow(t *testing.T) {
	st := storetest.New(t)
	ctx, cancel := context.WithCancel(context.Background())
	(&riotsync.Scheduler{
		Recorder: riotsync.StoreRecorder{Store: st},
		Tasks: []riotsync.Task{{Name: "slow", Interval: time.Hour, Run: func(ctx context.Context) riotsync.Outcome {
			cancel() // SIGTERM arrives during the run
			<-ctx.Done()
			return riotsync.Outcome{Err: ctx.Err()}
		}}},
	}).RunOnce(ctx)

	var status string
	var finished bool
	st.Pool.QueryRow(context.Background(), `SELECT status, finished_at IS NOT NULL FROM ingest_runs`).Scan(&status, &finished)
	if status != "interrupted" || !finished {
		t.Fatalf("status=%q finished=%v, want interrupted/true", status, finished)
	}
}
