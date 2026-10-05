package store_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"tft-platform/internal/store"
	"tft-platform/internal/store/storetest"
)

func TestInsertMatchIfNew_ConcurrentWritersStoreOnce(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()
	for _, p := range []string{"p1", "p2"} {
		if err := st.UpsertAccountPUUIDOnly(ctx, p, "americas"); err != nil {
			t.Fatal(err)
		}
	}
	m := store.Match{MatchID: "NA1_1", RoutingRegion: "americas", GameDatetime: time.Now(), GameVersion: "x",
		TFTSetNumber: 18, QueueID: 1100, TFTGameType: "standard", RawPayload: []byte(`{}`)}
	parts := map[string]store.MatchParticipant{}
	for _, p := range []string{"p1", "p2"} {
		parts[p] = store.MatchParticipant{PUUID: p, Placement: 1, Units: []byte(`[]`), Traits: []byte(`[]`), RawParticipant: []byte(`{}`)}
	}

	var wg sync.WaitGroup
	var inserted atomic.Int32
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := st.InsertMatchIfNew(ctx, m, parts)
			if err != nil {
				t.Error(err)
			}
			if ok {
				inserted.Add(1)
			}
		}()
	}
	wg.Wait()
	if inserted.Load() != 1 {
		t.Fatalf("%d writers reported inserting the same match, want exactly 1", inserted.Load())
	}
	var matches, participants int
	st.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM matches), (SELECT count(*) FROM match_participants)`).Scan(&matches, &participants)
	if matches != 1 || participants != 2 {
		t.Fatalf("matches=%d participants=%d, want 1 and 2", matches, participants)
	}
}
