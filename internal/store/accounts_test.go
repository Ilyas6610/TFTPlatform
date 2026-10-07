package store_test

import (
	"context"
	"testing"

	"tft-platform/internal/store"
	"tft-platform/internal/store/storetest"
)

func TestSetAccountRiotID_SetsNameAndFetchedAt(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()

	if err := st.UpsertAccountPUUIDOnly(ctx, "a", "americas"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetAccountRiotID(ctx, "a", "Alice", "NA1"); err != nil {
		t.Fatal(err)
	}

	got, err := st.GetAccountByRiotID(ctx, "Alice", "NA1")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.PUUID != "a" || got.LastFetchedAt == nil {
		t.Errorf("expected account a with last_fetched_at set, got %+v", got)
	}
}

func TestSetAccountRiotID_TakesIDFromStaleHolder(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()

	// "old" held the Riot ID, then gave it up and "new" claimed it.
	if err := st.UpsertAccount(ctx, store.Account{PUUID: "old", GameName: "Alice", TagLine: "NA1", RoutingRegion: "americas"}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertAccountPUUIDOnly(ctx, "new", "americas"); err != nil {
		t.Fatal(err)
	}

	if err := st.SetAccountRiotID(ctx, "new", "Alice", "NA1"); err != nil {
		t.Fatalf("expected the ID to move instead of violating the unique index: %v", err)
	}

	got, err := st.GetAccountByRiotID(ctx, "Alice", "NA1")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.PUUID != "new" {
		t.Errorf("expected Alice#NA1 to belong to new, got %+v", got)
	}
	var oldName *string
	if err := st.Pool.QueryRow(ctx, `SELECT game_name FROM accounts WHERE puuid = 'old'`).Scan(&oldName); err != nil {
		t.Fatal(err)
	}
	if oldName != nil {
		t.Errorf("expected old's name to be cleared, got %q", *oldName)
	}
}

// A profile lookup that finds a Riot ID now belonging to another PUUID must
// store it (it used to hit idx_accounts_riot_id and fail with a 500).
func TestUpsertAccount_TakesIDFromStaleHolder(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()

	if err := st.UpsertAccount(ctx, store.Account{PUUID: "old", GameName: "Alice", TagLine: "NA1", RoutingRegion: "americas"}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertAccount(ctx, store.Account{PUUID: "new", GameName: "Alice", TagLine: "NA1", RoutingRegion: "americas"}); err != nil {
		t.Fatalf("expected the Riot ID to move to the new holder: %v", err)
	}

	got, err := st.GetAccountByRiotID(ctx, "Alice", "NA1")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.PUUID != "new" {
		t.Errorf("expected Alice#NA1 to belong to new, got %+v", got)
	}
	var oldName *string
	if err := st.Pool.QueryRow(ctx, `SELECT game_name FROM accounts WHERE puuid = 'old'`).Scan(&oldName); err != nil {
		t.Fatal(err)
	}
	if oldName != nil {
		t.Errorf("expected old's name to be cleared, got %q", *oldName)
	}

	// Refreshing an account that keeps its ID still works.
	if err := st.UpsertAccount(ctx, store.Account{PUUID: "new", GameName: "Alice", TagLine: "NA1", RoutingRegion: "americas"}); err != nil {
		t.Fatalf("refresh: %v", err)
	}
}

// Lookups ignore case, so a stale "alice#na1" must go when "Alice#NA1" is
// claimed by another PUUID (it used to survive: only the exact spelling was
// cleared, and GetAccountByRiotID could then see two rows).
func TestUpsertAccount_ClearsStaleHolderCaseInsensitively(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()
	if err := st.UpsertAccount(ctx, store.Account{PUUID: "old", GameName: "alice", TagLine: "na1", RoutingRegion: "americas"}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertAccount(ctx, store.Account{PUUID: "new", GameName: "Alice", TagLine: "NA1", RoutingRegion: "americas"}); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := st.Pool.QueryRow(ctx, `SELECT count(*) FROM accounts WHERE lower(game_name) = 'alice' AND lower(tag_line) = 'na1'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("%d accounts hold alice#na1, want 1", n)
	}
	got, err := st.GetAccountByRiotID(ctx, "alice", "na1")
	if err != nil || got == nil || got.PUUID != "new" {
		t.Errorf("lookup = %+v, %v; want the new holder", got, err)
	}

	// SetAccountRiotID (the name resolver) clears the same way.
	if err := st.UpsertAccountPUUIDOnly(ctx, "third", "americas"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetAccountRiotID(ctx, "third", "ALICE", "Na1"); err != nil {
		t.Fatal(err)
	}
	if err := st.Pool.QueryRow(ctx, `SELECT count(*) FROM accounts WHERE lower(game_name) = 'alice' AND lower(tag_line) = 'na1'`).Scan(&n); err != nil || n != 1 {
		t.Errorf("after SetAccountRiotID: %d holders (%v), want 1", n, err)
	}
}
