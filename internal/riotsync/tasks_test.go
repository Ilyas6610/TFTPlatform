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
