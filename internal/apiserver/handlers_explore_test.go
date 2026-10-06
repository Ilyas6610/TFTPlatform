package apiserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
	"time"

	"tft-platform/internal/store"
	"tft-platform/internal/store/storetest"
)

func TestParseExploreFilter(t *testing.T) {
	q, _ := url.ParseQuery("set=18&queue=1100&queue=1090&level=8-&unit=DA_18_Akali_AD*2:DA_InfinityEdge,DA_JeweledGauntlet&unit=DA_Amumu18&item=DA_Warmogs&trait=DA_18_Elderwood*5&trait=DA_Primal18&trait=DA_Juggernaut18*4-5")
	f, err := parseExploreFilter(q)
	if err != nil {
		t.Fatal(err)
	}
	want := store.ExploreFilter{
		Set: 18, Queues: []int{1100, 1090}, LevelMin: 8,
		Units: []store.UnitCond{
			{ID: "DA_18_Akali_AD", MinStar: 2, Items: []string{"DA_InfinityEdge", "DA_JeweledGauntlet"}},
			{ID: "DA_Amumu18"},
		},
		Items:  []string{"DA_Warmogs"},
		Traits: []store.TraitCond{{ID: "DA_18_Elderwood", MinUnits: 5}, {ID: "DA_Primal18"}, {ID: "DA_Juggernaut18", MinUnits: 4, MaxUnits: 5}},
	}
	if !reflect.DeepEqual(f, want) {
		t.Errorf("got  %+v\nwant %+v", f, want)
	}
}

func TestParseExploreFilter_Levels(t *testing.T) {
	for in, want := range map[string][2]int{"7-10": {7, 10}, "8-": {8, 0}, "-6": {0, 6}, "9": {9, 9}} {
		q := url.Values{"set": {"18"}, "level": {in}}
		f, err := parseExploreFilter(q)
		if err != nil || f.LevelMin != want[0] || f.LevelMax != want[1] {
			t.Errorf("level=%s: got %d-%d (err %v), want %v", in, f.LevelMin, f.LevelMax, err, want)
		}
	}
}

func TestParseExploreFilter_Rejects(t *testing.T) {
	for _, raw := range []string{
		"",                          // no set
		"set=0",                     // bad set
		"set=18&queue=x",            // bad queue
		"set=18&level=9-2",          // inverted
		"set=18&level=11",           // out of range
		"set=18&unit=Akali*5",       // star out of range
		"set=18&unit=Ak'ali",        // bad id
		"set=18&unit=A:I1,I2,I3,I4", // too many items
		"set=18&unit=A:bad id",      // bad item id
		"set=18&trait=T*0",          // unit count out of range
		"set=18&trait=T*5-3",        // inverted unit range
		"set=18&item=x%3Bdrop",      // bad item id (";" encoded, as a browser sends it)
		"set=18&unit=A&unit=B&unit=C&unit=D&unit=E&unit=F&unit=G", // too many units
	} {
		q, _ := url.ParseQuery(raw)
		if _, err := parseExploreFilter(q); err == nil {
			t.Errorf("%q: expected an error", raw)
		}
	}
}

func TestMetaBuilds_RejectsBoardConditions(t *testing.T) {
	s := &Server{} // validation runs before the store is touched
	for _, q := range []string{"set=18&unit=A", "set=18&item=I", "set=18&trait=T", "set=0"} {
		rec := httptest.NewRecorder()
		NewRouter(s).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/meta/builds?"+q, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400", q, rec.Code)
		}
	}
}

func TestMetaComps_RejectsBoardConditions(t *testing.T) {
	s := &Server{} // validation runs before the store is touched
	for _, q := range []string{"set=18&unit=A", "set=18&item=I", "set=18&trait=T", "set=18&level=8-", "set=0"} {
		rec := httptest.NewRecorder()
		NewRouter(s).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/meta/comps?"+q, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400", q, rec.Code)
		}
	}
}

// seedBoards stores one ranked set-18 match whose players all ran units at
// level 9, placing 1..len(boards).
func seedBoards(t *testing.T, st *store.Store, matchID string, boards [][]string) {
	t.Helper()
	ctx := context.Background()
	parts := map[string]store.MatchParticipant{}
	for i, ids := range boards {
		puuid := fmt.Sprintf("%s-p%d", matchID, i)
		if err := st.UpsertAccountPUUIDOnly(ctx, puuid, "americas"); err != nil {
			t.Fatal(err)
		}
		var units []map[string]any
		for _, id := range ids {
			units = append(units, map[string]any{"character_id": id, "tier": 2, "itemNames": []string{}})
		}
		unitsJSON, _ := json.Marshal(units)
		parts[puuid] = store.MatchParticipant{PUUID: puuid, Placement: i + 1, Level: 9,
			Units: unitsJSON, Traits: []byte(`[]`), RawParticipant: []byte(`{}`)}
	}
	if err := st.InsertMatchWithParticipants(ctx, store.Match{
		MatchID: matchID, RoutingRegion: "americas", GameDatetime: time.Now(), GameVersion: "x",
		TFTSetNumber: 18, QueueID: 1100, TFTGameType: "standard", RawPayload: []byte(`{}`),
	}, parts); err != nil {
		t.Fatal(err)
	}
}

func TestMetaComps_ShapeAndCache(t *testing.T) {
	s := &Server{Store: storetest.New(t)}
	comp := []string{"A", "B", "C", "D", "E", "F", "G", "H"}
	seedBoards(t, s.Store, "NA1_1", [][]string{comp, comp, comp, comp, comp, comp})

	get := func() MetaCompsResponse {
		t.Helper()
		rec := httptest.NewRecorder()
		NewRouter(s).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/meta/comps?set=18&queue=1100", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("got %d: %s", rec.Code, rec.Body)
		}
		var res MetaCompsResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatal(err)
		}
		return res
	}

	res := get()
	if res.Boards != 6 || len(res.Comps) != 1 {
		t.Fatalf("got %d boards, %d comps; want 6 boards in 1 comp", res.Boards, len(res.Comps))
	}
	c := res.Comps[0]
	if c.Boards != 6 || len(c.Board) != len(comp) || c.BoardStats.Boards != 6 || c.Variants == nil || c.Flex == nil {
		t.Errorf("unexpected comp %+v", c)
	}

	// Within the TTL the same result is served, even after new matches.
	seedBoards(t, s.Store, "NA1_2", [][]string{comp})
	if again := get(); again.Boards != 6 {
		t.Errorf("got %d boards, want the cached 6", again.Boards)
	}
}

func TestSetNumberIsBounded(t *testing.T) {
	s := &Server{} // rejected before the store is touched
	for _, path := range []string{"/api/v1/meta/comps?set=999999999", "/api/v1/explore/options?set=101", "/api/v1/explore?set=101&unit=A"} {
		rec := httptest.NewRecorder()
		NewRouter(s).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400", path, rec.Code)
		}
	}
}

func exploreBoards(t *testing.T, h http.Handler, query string) int {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/explore?"+query, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rec.Code, rec.Body)
	}
	var res struct {
		Summary struct{ Boards int } `json:"summary"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	return res.Summary.Boards
}

func TestExplore_ResultIsCachedPerSearch(t *testing.T) {
	s := &Server{Store: storetest.New(t)}
	h := NewRouter(s)
	seedBoards(t, s.Store, "NA1_1", [][]string{{"A", "B"}, {"A"}})

	if n := exploreBoards(t, h, "set=18&queue=1100&unit=A"); n != 2 {
		t.Fatalf("got %d boards, want 2", n)
	}
	seedBoards(t, s.Store, "NA1_2", [][]string{{"A"}})
	if n := exploreBoards(t, h, "set=18&queue=1100&unit=A"); n != 2 {
		t.Errorf("got %d boards, want the cached 2", n)
	}
	// A different search isn't served from that entry.
	if n := exploreBoards(t, h, "set=18&queue=1100&unit=B"); n != 1 {
		t.Errorf("unit B: got %d boards, want 1", n)
	}
}

func TestExplore_CacheLifetimeIsConfigurable(t *testing.T) {
	s := &Server{Store: storetest.New(t), StatsCacheTTL: 100 * time.Millisecond}
	h := NewRouter(s)
	if s.stats.lifetime() != 100*time.Millisecond || s.meta.lifetime() != 100*time.Millisecond || s.explore.lifetime() != 100*time.Millisecond {
		t.Fatalf("lifetimes %v %v %v", s.stats.lifetime(), s.meta.lifetime(), s.explore.lifetime())
	}
	seedBoards(t, s.Store, "NA1_1", [][]string{{"A"}})
	if n := exploreBoards(t, h, "set=18&queue=1100&unit=A"); n != 1 {
		t.Fatalf("got %d, want 1", n)
	}
	seedBoards(t, s.Store, "NA1_2", [][]string{{"A"}})
	time.Sleep(150 * time.Millisecond)
	if n := exploreBoards(t, h, "set=18&queue=1100&unit=A"); n != 2 {
		t.Errorf("got %d after the TTL, want a fresh 2", n)
	}

	def := &Server{}
	NewRouter(def)
	if def.stats.lifetime() != DefaultStatsCacheTTL || DefaultStatsCacheTTL != 10*time.Minute {
		t.Errorf("default lifetime %v, want 10m", def.stats.lifetime())
	}
}

func TestExploreCacheKey_QueueOrderDoesNotMatter(t *testing.T) {
	a := exploreCacheKey(store.ExploreFilter{Set: 18, Queues: []int{1100, 1090}})
	b := exploreCacheKey(store.ExploreFilter{Set: 18, Queues: []int{1090, 1100, 1100}})
	c := exploreCacheKey(store.ExploreFilter{Set: 18, Queues: []int{1100}})
	if a != b || a == c {
		t.Errorf("keys %q %q %q", a, b, c)
	}
}
