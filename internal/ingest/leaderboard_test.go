package ingest_test

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"

	"tft-platform/internal/ingest"
	"tft-platform/internal/riotapi"
	"tft-platform/internal/riotapi/riotapitest"
	"tft-platform/internal/store"
	"tft-platform/internal/store/storetest"
)

// fakeRiot serves tft/league-v1 apex tiers, account-v1 by-puuid, and
// tft/match-v1 ids + matches from in-memory data. Any path listed in status
// gets that status instead.
type fakeRiot struct {
	mu       sync.Mutex
	tiers    map[string][]riotapi.LeagueEntry // "challenger" -> entries
	accounts map[string]riotapi.Account
	matchIDs map[string][]string // puuid -> recent match ids, newest first
	matches  map[string]string   // match id -> raw match JSON
	status   map[string]int      // path suffix -> forced status
	requests []string
}

func newFakeRiot() *fakeRiot {
	return &fakeRiot{
		tiers:    map[string][]riotapi.LeagueEntry{},
		accounts: map[string]riotapi.Account{},
		matchIDs: map[string][]string{},
		matches:  map[string]string{},
		status:   map[string]int{},
	}
}

func (f *fakeRiot) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, r.URL.Path)

	for suffix, code := range f.status {
		if strings.HasSuffix(r.URL.Path, suffix) {
			w.WriteHeader(code)
			return
		}
	}
	if tier, ok := strings.CutPrefix(r.URL.Path, "/tft/league/v1/"); ok {
		json.NewEncoder(w).Encode(riotapi.LeagueList{Tier: strings.ToUpper(tier), Entries: f.tiers[tier]})
		return
	}
	if rest, ok := strings.CutPrefix(r.URL.Path, "/tft/match/v1/matches/"); ok {
		if puuid, ok := strings.CutPrefix(rest, "by-puuid/"); ok {
			json.NewEncoder(w).Encode(f.matchIDs[strings.TrimSuffix(puuid, "/ids")])
			return
		}
		if m, ok := f.matches[rest]; ok {
			w.Write([]byte(m))
			return
		}
	}
	if puuid, ok := strings.CutPrefix(r.URL.Path, "/riot/account/v1/accounts/by-puuid/"); ok {
		if a, ok := f.accounts[puuid]; ok {
			json.NewEncoder(w).Encode(a)
			return
		}
	}
	w.WriteHeader(http.StatusNotFound)
}

func (f *fakeRiot) requestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

func leaderboard(t *testing.T, st *store.Store) []store.LeaderboardEntry {
	t.Helper()
	entries, err := st.GetLeaderboard(context.Background(), "na1", 100)
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func puuids(entries []store.LeaderboardEntry) []string {
	var out []string
	for _, e := range entries {
		out = append(out, e.PUUID)
	}
	return out
}

func TestSeedLeaderboard_WritesAllTiersAndPrunesDropouts(t *testing.T) {
	st := storetest.New(t)
	riot := newFakeRiot()
	client := riotapitest.NewClient(t, riot)
	ctx := context.Background()

	riot.tiers["challenger"] = []riotapi.LeagueEntry{{PUUID: "c1", LeaguePoints: 1500}}
	riot.tiers["grandmaster"] = []riotapi.LeagueEntry{{PUUID: "g1", LeaguePoints: 700}, {PUUID: ""}}
	riot.tiers["master"] = []riotapi.LeagueEntry{{PUUID: "m1", LeaguePoints: 100}, {PUUID: "m2", LeaguePoints: 50}}

	result, err := ingest.SeedLeaderboard(ctx, client, st, riotapi.PlatformNA1)
	if err != nil {
		t.Fatal(err)
	}
	if result.RequestsMade != 3 || result.PlayersSeeded != 4 || result.Stopped != "" {
		t.Errorf("unexpected result (entries without a puuid must be skipped): %+v", result)
	}
	entries := leaderboard(t, st)
	if got, want := puuids(entries), []string{"c1", "g1", "m1", "m2"}; !slices.Equal(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	if entries[0].Tier != "CHALLENGER" || entries[1].Tier != "GRANDMASTER" {
		t.Errorf("unexpected tiers: %s, %s", entries[0].Tier, entries[1].Tier)
	}

	// m2 falls out of master.
	riot.tiers["master"] = []riotapi.LeagueEntry{{PUUID: "m1", LeaguePoints: 120}}
	if _, err := ingest.SeedLeaderboard(ctx, client, st, riotapi.PlatformNA1); err != nil {
		t.Fatal(err)
	}
	if got, want := puuids(leaderboard(t, st)), []string{"c1", "g1", "m1"}; !slices.Equal(got, want) {
		t.Errorf("expected m2 pruned: want %v, got %v", want, got)
	}
}

func TestSeedLeaderboard_RateLimitedKeepsFetchedTiersWithoutPruning(t *testing.T) {
	st := storetest.New(t)
	riot := newFakeRiot()
	client := riotapitest.NewClient(t, riot)
	ctx := context.Background()

	riot.tiers["challenger"] = []riotapi.LeagueEntry{{PUUID: "c1", LeaguePoints: 1500}}
	riot.tiers["master"] = []riotapi.LeagueEntry{{PUUID: "m1", LeaguePoints: 100}}
	if _, err := ingest.SeedLeaderboard(ctx, client, st, riotapi.PlatformNA1); err != nil {
		t.Fatal(err)
	}

	riot.tiers["challenger"] = []riotapi.LeagueEntry{{PUUID: "c1", LeaguePoints: 1600}}
	riot.status["/grandmaster"] = http.StatusTooManyRequests
	result, err := ingest.SeedLeaderboard(ctx, client, st, riotapi.PlatformNA1)
	if err != nil {
		t.Fatal(err)
	}
	if result.Stopped != "rate_limited" || result.RequestsMade != 2 {
		t.Errorf("expected stop at grandmaster after 2 requests, got %+v", result)
	}

	entries := leaderboard(t, st)
	if got, want := puuids(entries), []string{"c1", "m1"}; !slices.Equal(got, want) {
		t.Fatalf("expected master tier kept after partial seed: want %v, got %v", want, got)
	}
	if entries[0].LeaguePoints != 1600 {
		t.Errorf("expected challenger tier updated to 1600 LP, got %d", entries[0].LeaguePoints)
	}
}

func TestSeedLeaderboard_UnknownPlatform(t *testing.T) {
	st := storetest.New(t)
	riot := newFakeRiot()
	if _, err := ingest.SeedLeaderboard(context.Background(), riotapitest.NewClient(t, riot), st, "nope"); err == nil {
		t.Fatal("expected error for unknown platform")
	}
	if n := riot.requestCount(); n != 0 {
		t.Errorf("expected no Riot requests, got %d", n)
	}
}

func seedUnnamed(t *testing.T, st *store.Store, ids ...string) {
	t.Helper()
	var entries []store.SnapshotEntry
	for i, id := range ids {
		entries = append(entries, store.SnapshotEntry{
			LeagueEntry:   store.LeagueEntry{PUUID: id, Tier: "CHALLENGER", LeaguePoints: 1000 - i},
			RoutingRegion: "americas",
		})
	}
	if err := st.WriteLeagueSnapshot(context.Background(), "na1", entries, true); err != nil {
		t.Fatal(err)
	}
}

func TestResolveNames_StoresNamesAndSkipsUnknownPUUIDs(t *testing.T) {
	st := storetest.New(t)
	riot := newFakeRiot()
	ctx := context.Background()
	seedUnnamed(t, st, "a", "gone", "b")
	riot.accounts["a"] = riotapi.Account{PUUID: "a", GameName: "Alice", TagLine: "NA1"}
	riot.accounts["b"] = riotapi.Account{PUUID: "b", GameName: "Bob", TagLine: "NA2"}

	result, err := ingest.ResolveNames(ctx, riotapitest.NewClient(t, riot), st, riotapi.PlatformNA1, []string{"a", "gone", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if result.RequestsMade != 3 || result.Resolved != 2 || result.Stopped != "" {
		t.Errorf("unexpected result: %+v", result)
	}

	unresolved, err := st.UnresolvedLeaderboardPUUIDs(ctx, "na1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(unresolved, []string{"gone"}) {
		t.Errorf("expected only gone unresolved, got %v", unresolved)
	}
	if a, _ := st.GetAccountByRiotID(ctx, "Bob", "NA2"); a == nil || a.PUUID != "b" {
		t.Errorf("expected Bob#NA2 stored for b, got %+v", a)
	}
}

func TestResolveNames_StopsOnRateLimitOrDeadKey(t *testing.T) {
	for status, reason := range map[int]string{
		http.StatusTooManyRequests: "rate_limited",
		http.StatusForbidden:       "key_expired",
	} {
		t.Run(reason, func(t *testing.T) {
			st := storetest.New(t)
			riot := newFakeRiot()
			seedUnnamed(t, st, "a", "b", "c")
			riot.accounts["a"] = riotapi.Account{PUUID: "a", GameName: "Alice", TagLine: "NA1"}
			riot.status["/b"] = status

			result, err := ingest.ResolveNames(context.Background(), riotapitest.NewClient(t, riot), st, riotapi.PlatformNA1, []string{"a", "b", "c"})
			if err != nil {
				t.Fatal(err)
			}
			if result.Stopped != reason || result.Resolved != 1 || result.RequestsMade != 2 {
				t.Errorf("expected stop at b with reason %q, got %+v", reason, result)
			}
		})
	}
}

func TestResolveNames_SEAPlatformUsesAsiaAccountRouting(t *testing.T) {
	st := storetest.New(t)
	var host string
	client := riotapitest.NewClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host = r.Header.Get(riotapitest.OriginalHostHeader)
		w.WriteHeader(http.StatusNotFound)
	}))

	if _, err := ingest.ResolveNames(context.Background(), client, st, riotapi.PlatformSG2, []string{"x"}); err != nil {
		t.Fatal(err)
	}
	if host != "asia.api.riotgames.com" {
		t.Errorf("expected asia routing for sg2, got %q", host)
	}
}
