package store_test

import (
	"context"
	"testing"
	"time"

	"tft-platform/internal/store"
	"tft-platform/internal/store/storetest"
)

func TestLobbyPlayers(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()
	game := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	parts := map[string]store.MatchParticipant{}
	for i, p := range []string{"near", "after", "far", "ladder", "nobody"} {
		if err := st.UpsertAccountPUUIDOnly(ctx, p, "americas"); err != nil {
			t.Fatal(err)
		}
		parts[p] = store.MatchParticipant{PUUID: p, Placement: i + 1, Units: []byte(`[]`), Traits: []byte(`[]`), RawParticipant: []byte(`{}`)}
	}
	if err := st.InsertMatchWithParticipants(ctx, store.Match{
		MatchID: "NA1_1", RoutingRegion: "americas", GameDatetime: game, GameVersion: "x",
		TFTSetNumber: 18, QueueID: 1100, TFTGameType: "standard", RawPayload: []byte(`{}`),
	}, parts); err != nil {
		t.Fatal(err)
	}
	snap := func(puuid, queue, tier string, lp int, at time.Time) {
		t.Helper()
		if _, err := st.Pool.Exec(ctx, `INSERT INTO rank_snapshots (puuid, queue_type, tier, rank, league_points, wins, losses, fetched_at)
			VALUES ($1, $2, $3, 'I', $4, 10, 10, $5)`, puuid, queue, tier, lp, at); err != nil {
			t.Fatal(err)
		}
	}
	// "near": snapshots 3 days before and 1 day after; the later is closer.
	snap("near", "RANKED_TFT", "MASTER", 100, game.Add(-72*time.Hour))
	snap("near", "RANKED_TFT", "MASTER", 150, game.Add(24*time.Hour))
	snap("near", "RANKED_TFT_DOUBLE_UP", "CHALLENGER", 999, game) // other queue: ignored
	// "after": only a snapshot after the game, within the window.
	snap("after", "RANKED_TFT", "DIAMOND", 50, game.Add(10*24*time.Hour))
	// "far": a snapshot outside the window and no ladder entry: unknown.
	snap("far", "RANKED_TFT", "MASTER", 500, game.Add(-30*24*time.Hour))
	// "ladder": a snapshot outside the window, but on today's ladder.
	snap("ladder", "RANKED_TFT", "MASTER", 1, game.Add(-30*24*time.Hour))
	if _, err := st.Pool.Exec(ctx, `INSERT INTO league_entries (puuid, platform_region, tier, rank, league_points, wins, losses)
		VALUES ('ladder', 'na1', 'GRANDMASTER', 'I', 700, 50, 40)`); err != nil {
		t.Fatal(err)
	}

	got, err := st.LobbyPlayers(ctx, []string{"NA1_1", "NA1_missing"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Fatalf("got %d players, want 5", len(got))
	}
	by := map[string]store.LobbyPlayer{}
	for _, p := range got {
		by[p.PUUID] = p
	}
	if r := by["near"].Rank; r == nil || r.LeaguePoints != 150 || by["near"].Current {
		t.Errorf("near = %+v %+v, want the closer snapshot (150 LP)", by["near"], r)
	}
	if r := by["after"].Rank; r == nil || r.Tier != "DIAMOND" || r.Rank != "I" {
		t.Errorf("after = %+v", r)
	}
	if by["far"].Rank != nil || by["nobody"].Rank != nil {
		t.Errorf("far/nobody should be unknown: %+v %+v", by["far"].Rank, by["nobody"].Rank)
	}
	if r := by["ladder"].Rank; r == nil || r.Tier != "GRANDMASTER" || r.LeaguePoints != 700 || !by["ladder"].Current {
		t.Errorf("ladder = %+v %+v, want today's ladder entry, marked current", by["ladder"], r)
	}
	if by["near"].QueueID != 1100 || by["after"].Placement != 2 {
		t.Errorf("row fields: %+v %+v", by["near"], by["after"])
	}
	if empty, err := st.LobbyPlayers(ctx, nil); err != nil || len(empty) != 0 {
		t.Errorf("no ids: %v %v", empty, err)
	}
}
