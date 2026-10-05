package ingest_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"tft-platform/internal/ingest"
	"tft-platform/internal/riotapi"
	"tft-platform/internal/riotapi/riotapitest"
	"tft-platform/internal/store"
	"tft-platform/internal/store/storetest"
)

// matchJSON is a minimal tft/match-v1 payload; participants place in order.
func matchJSON(id string, gameDatetime int64, puuids ...string) string {
	var parts []string
	for i, p := range puuids {
		parts = append(parts, fmt.Sprintf(`{"puuid":%q,"placement":%d,"level":8,"units":[],"traits":[]}`, p, i+1))
	}
	return fmt.Sprintf(`{"metadata":{"match_id":%q},"info":{"game_datetime":%d,"game_version":"test","tft_set_number":18,"participants":[%s]}}`,
		id, gameDatetime, strings.Join(parts, ","))
}

func recentMatchIDs(t *testing.T, st *store.Store, puuid string) []string {
	t.Helper()
	matches, err := st.GetRecentMatchesForPUUID(context.Background(), puuid, 100)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, m := range matches {
		ids = append(ids, m.MatchID)
	}
	return ids
}

func TestSyncPlayerMatches_IngestsNewMatchesAndRecordsSync(t *testing.T) {
	st := storetest.New(t)
	riot := newFakeRiot()
	client := riotapitest.NewClient(t, riot)
	ctx := context.Background()

	riot.matchIDs["me"] = []string{"NA1_2", "NA1_1"}
	riot.matches["NA1_2"] = matchJSON("NA1_2", 2000, "other", "me")
	riot.matches["NA1_1"] = matchJSON("NA1_1", 1000, "me", "other")

	result, err := ingest.SyncPlayerMatches(ctx, client, st, riotapi.PlatformNA1, "me", 20)
	if err != nil {
		t.Fatal(err)
	}
	if result.MatchesIngested != 2 || result.Stopped != "" {
		t.Errorf("unexpected result: %+v", result)
	}
	if got := recentMatchIDs(t, st, "me"); !slices.Equal(got, []string{"NA1_2", "NA1_1"}) {
		t.Errorf("expected both matches newest first, got %v", got)
	}
	if syncedAt, err := st.MatchHistorySyncedAt(ctx, "me"); err != nil || syncedAt == nil {
		t.Errorf("expected sync time recorded, got %v (err %v)", syncedAt, err)
	}

	// A new game: the next sync fetches only that one.
	riot.matchIDs["me"] = []string{"NA1_3", "NA1_2", "NA1_1"}
	riot.matches["NA1_3"] = matchJSON("NA1_3", 3000, "me")
	before := riot.requestCount()
	result, err = ingest.SyncPlayerMatches(ctx, client, st, riotapi.PlatformNA1, "me", 20)
	if err != nil {
		t.Fatal(err)
	}
	if result.MatchesIngested != 1 || riot.requestCount()-before != 2 {
		t.Errorf("expected 1 new match in 2 requests, got %+v in %d", result, riot.requestCount()-before)
	}
}

func TestSyncPlayerMatches_StoppedEarlyDoesNotRecordSync(t *testing.T) {
	st := storetest.New(t)
	riot := newFakeRiot()
	ctx := context.Background()

	riot.matchIDs["me"] = []string{"NA1_2", "NA1_1"}
	riot.matches["NA1_2"] = matchJSON("NA1_2", 2000, "me")
	riot.status["/NA1_1"] = http.StatusTooManyRequests

	result, err := ingest.SyncPlayerMatches(ctx, riotapitest.NewClient(t, riot), st, riotapi.PlatformNA1, "me", 20)
	if err != nil {
		t.Fatal(err)
	}
	if result.Stopped != "rate_limited" || result.MatchesIngested != 1 {
		t.Errorf("unexpected result: %+v", result)
	}
	if syncedAt, _ := st.MatchHistorySyncedAt(ctx, "me"); syncedAt != nil {
		t.Errorf("expected no sync time after a partial sync, got %v", syncedAt)
	}
}

func TestSyncPlayerMatches_QueuesPlayerWithoutLoweringPriority(t *testing.T) {
	st := storetest.New(t)
	riot := newFakeRiot()
	ctx := context.Background()
	if err := st.WriteLeagueSnapshot(ctx, "na1", []store.SnapshotEntry{{
		LeagueEntry: store.LeagueEntry{PUUID: "me", Tier: "CHALLENGER"}, RoutingRegion: "americas", Priority: ingest.PriorityChallenger,
	}}, true); err != nil {
		t.Fatal(err)
	}

	if _, err := ingest.SyncPlayerMatches(ctx, riotapitest.NewClient(t, riot), st, riotapi.PlatformNA1, "me", 20); err != nil {
		t.Fatal(err)
	}

	batch, err := st.NextQueueBatch(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(batch) != 1 || batch[0].Priority != ingest.PriorityChallenger {
		t.Errorf("expected me queued at challenger priority, got %+v", batch)
	}
}

func TestSyncPlayerMatches_DeadKeyIsAnError(t *testing.T) {
	st := storetest.New(t)
	riot := newFakeRiot()
	riot.status["/ids"] = http.StatusForbidden

	_, err := ingest.SyncPlayerMatches(context.Background(), riotapitest.NewClient(t, riot), st, riotapi.PlatformNA1, "me", 20)
	var keyExpired *riotapi.ErrKeyExpired
	if !errors.As(err, &keyExpired) {
		t.Errorf("expected ErrKeyExpired, got %v", err)
	}
}

// A PUUID Riot rejects (here 400, as for a malformed id) must not be stored
// or queued for crawling.
func TestSyncPlayerMatches_RejectedPUUIDIsNotStored(t *testing.T) {
	st := storetest.New(t)
	client := riotapitest.NewClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"status":{"message":"Bad Request - Exception decrypting junk"}}`, http.StatusBadRequest)
	}))
	ctx := context.Background()

	if _, err := ingest.SyncPlayerMatches(ctx, client, st, riotapi.PlatformNA1, "junk", 20); err == nil {
		t.Fatal("expected Riot's rejection as an error")
	}
	var accounts, queued int
	st.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM accounts), (SELECT count(*) FROM ingest_puuid_queue)`).Scan(&accounts, &queued)
	if accounts != 0 || queued != 0 {
		t.Errorf("accounts=%d queued=%d, want nothing stored", accounts, queued)
	}
}
