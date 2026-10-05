package store_test

import (
	"context"
	"slices"
	"testing"

	"tft-platform/internal/store"
	"tft-platform/internal/store/storetest"
)

func entry(puuid, tier string, lp int, priority int16) store.SnapshotEntry {
	return store.SnapshotEntry{
		LeagueEntry:   store.LeagueEntry{PUUID: puuid, Tier: tier, LeaguePoints: lp},
		RoutingRegion: "americas",
		Priority:      priority,
	}
}

func leaderboardPUUIDs(t *testing.T, st *store.Store, platform string) []string {
	t.Helper()
	entries, err := st.GetLeaderboard(context.Background(), platform, 100)
	if err != nil {
		t.Fatalf("GetLeaderboard: %v", err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.PUUID)
	}
	return out
}

func TestWriteLeagueSnapshot_CompletePrunesPlayersNoLongerListed(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()

	if err := st.WriteLeagueSnapshot(ctx, "na1", []store.SnapshotEntry{
		entry("a", "CHALLENGER", 1000, 30),
		entry("b", "MASTER", 100, 10),
	}, true); err != nil {
		t.Fatal(err)
	}
	if err := st.WriteLeagueSnapshot(ctx, "euw1", []store.SnapshotEntry{entry("e", "MASTER", 50, 10)}, true); err != nil {
		t.Fatal(err)
	}

	// b dropped out of master; c is new.
	if err := st.WriteLeagueSnapshot(ctx, "na1", []store.SnapshotEntry{
		entry("a", "CHALLENGER", 1100, 30),
		entry("c", "GRANDMASTER", 600, 20),
	}, true); err != nil {
		t.Fatal(err)
	}

	if got, want := leaderboardPUUIDs(t, st, "na1"), []string{"a", "c"}; !slices.Equal(got, want) {
		t.Errorf("na1 leaderboard: expected %v, got %v", want, got)
	}
	if got := leaderboardPUUIDs(t, st, "euw1"); !slices.Equal(got, []string{"e"}) {
		t.Errorf("pruning na1 must not touch euw1, got %v", got)
	}
}

func TestWriteLeagueSnapshot_PartialDoesNotPrune(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()

	if err := st.WriteLeagueSnapshot(ctx, "na1", []store.SnapshotEntry{
		entry("a", "CHALLENGER", 1000, 30),
		entry("b", "MASTER", 100, 10),
	}, true); err != nil {
		t.Fatal(err)
	}
	if err := st.WriteLeagueSnapshot(ctx, "na1", []store.SnapshotEntry{entry("a", "CHALLENGER", 1200, 30)}, false); err != nil {
		t.Fatal(err)
	}

	if got, want := leaderboardPUUIDs(t, st, "na1"), []string{"a", "b"}; !slices.Equal(got, want) {
		t.Errorf("expected partial snapshot to keep %v, got %v", want, got)
	}
}

func TestWriteLeagueSnapshot_QueuePriorityOnlyIncreases(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()

	if err := st.WriteLeagueSnapshot(ctx, "na1", []store.SnapshotEntry{entry("a", "CHALLENGER", 1000, 30)}, true); err != nil {
		t.Fatal(err)
	}
	if err := st.WriteLeagueSnapshot(ctx, "na1", []store.SnapshotEntry{entry("a", "MASTER", 10, 10)}, true); err != nil {
		t.Fatal(err)
	}

	batch, err := st.NextQueueBatch(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(batch) != 1 || batch[0].Priority != 30 {
		t.Errorf("expected a single queue entry at priority 30, got %+v", batch)
	}
}

func TestWriteLeagueSnapshot_KeepsResolvedNames(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()

	if err := st.WriteLeagueSnapshot(ctx, "na1", []store.SnapshotEntry{entry("a", "CHALLENGER", 1000, 30)}, true); err != nil {
		t.Fatal(err)
	}
	if err := st.SetAccountRiotID(ctx, "a", "Alice", "NA1"); err != nil {
		t.Fatal(err)
	}
	if err := st.WriteLeagueSnapshot(ctx, "na1", []store.SnapshotEntry{entry("a", "CHALLENGER", 1100, 30)}, true); err != nil {
		t.Fatal(err)
	}

	entries, err := st.GetLeaderboard(ctx, "na1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if entries[0].GameName == nil || *entries[0].GameName != "Alice" {
		t.Errorf("expected re-seeding to keep resolved name, got %v", entries[0].GameName)
	}
}

func TestUnresolvedLeaderboardPUUIDs_OnlyUnnamedWithinLimit(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()

	if err := st.WriteLeagueSnapshot(ctx, "na1", []store.SnapshotEntry{
		entry("gm", "GRANDMASTER", 900, 20),
		entry("chal-low", "CHALLENGER", 1000, 30),
		entry("chal-high", "CHALLENGER", 1500, 30),
		entry("master", "MASTER", 2000, 10), // highest LP but lowest tier
	}, true); err != nil {
		t.Fatal(err)
	}
	if err := st.SetAccountRiotID(ctx, "chal-low", "Named", "NA1"); err != nil {
		t.Fatal(err)
	}

	got, err := st.UnresolvedLeaderboardPUUIDs(ctx, "na1", 3)
	if err != nil {
		t.Fatal(err)
	}
	// Top 3 by tier then LP is chal-high, chal-low, gm; chal-low is named.
	if want := []string{"chal-high", "gm"}; !slices.Equal(got, want) {
		t.Errorf("expected %v, got %v", want, got)
	}
}
