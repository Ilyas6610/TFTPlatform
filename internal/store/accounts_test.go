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
