package apiserver

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"

	"tft-platform/internal/store"
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
