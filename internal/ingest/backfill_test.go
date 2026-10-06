package ingest_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"tft-platform/internal/ingest"
	"tft-platform/internal/riotapi"
	"tft-platform/internal/riotapi/riotapitest"
	"tft-platform/internal/store/storetest"
)

func TestBackfillSet(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()
	setStart := time.Unix(100_000, 0)

	// 250 games since the set started (ids newest first, like Riot), plus
	// older ones from before it. NA1_<n> was played at n*1000s.
	var ids []string
	for n := 300; n >= 1; n-- {
		ids = append(ids, fmt.Sprintf("NA1_%d", n))
	}
	var limited atomic.Bool
	var matchCalls atomic.Int32
	riot := riotapitest.NewClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Has("count") { // id listing
			start, _ := strconv.Atoi(q.Get("start"))
			count, _ := strconv.Atoi(q.Get("count"))
			since, _ := strconv.ParseInt(q.Get("startTime"), 10, 64)
			var in []string
			for _, id := range ids {
				var n int64
				fmt.Sscanf(id, "NA1_%d", &n)
				if n*1000 >= since {
					in = append(in, id)
				}
			}
			in = in[min(start, len(in)):]
			in = in[:min(count, len(in))]
			fmt.Fprintf(w, "%s", mustJSON(in))
			return
		}
		// One 429 along the way: the run waits and carries on.
		if matchCalls.Add(1) == 10 && limited.CompareAndSwap(false, true) {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		id := r.URL.Path[len("/tft/match/v1/matches/"):]
		var n int64
		fmt.Sscanf(id, "NA1_%d", &n)
		w.Write([]byte(matchJSON(id, n*1000*1000, "me")))
	}))

	// Two of the set's games are already stored.
	if _, err := ingest.SyncOlderMatches(ctx, riot, st, riotapi.PlatformNA1, "me", 0, 2); err != nil {
		t.Fatal(err)
	}
	calls := matchCalls.Load()

	var reports int
	p, err := ingest.BackfillSet(ctx, riot, st, riotapi.PlatformNA1, "me", setStart, 500, func(ingest.BackfillProgress) { reports++ })
	if err != nil {
		t.Fatal(err)
	}
	// Games 100..300 were played since the set started.
	if p.Found != 201 || p.Missing != 199 || p.Fetched != 199 {
		t.Errorf("progress = %+v, want 201 found, 199 missing, 199 fetched", p)
	}
	if reports != 200 { // after the listing, then once per game
		t.Errorf("%d reports, want 200", reports)
	}
	if got := matchCalls.Load() - calls; got != 200 { // 199 games + the retried 429
		t.Errorf("%d match requests, want 200", got)
	}
	var stored int
	st.Pool.QueryRow(ctx, `SELECT count(*) FROM match_participants WHERE puuid = 'me'`).Scan(&stored)
	if stored != 201 {
		t.Errorf("%d games stored, want 201", stored)
	}

	// A cap stops the listing early.
	p, err = ingest.BackfillSet(ctx, riot, st, riotapi.PlatformNA1, "me", time.Time{}, 50, func(ingest.BackfillProgress) {})
	if err != nil || p.Found != 50 || p.Missing != 0 {
		t.Errorf("capped run = %+v, %v; want 50 found, none missing", p, err)
	}
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}
