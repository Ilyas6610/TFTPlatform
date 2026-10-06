package setdata

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const plannerFixture = `{
  "TFTSet17": [{"character_id": "TFT17_Akali", "team_planner_code": 13}, {"character_id": "TFT17_Enemy_Aatrox", "team_planner_code": 0}],
  "TFTSet18": [{"character_id": "DA_18_Ahri", "team_planner_code": 1001}, {"character_id": "DA_18_Ashe", "team_planner_code": 1008}, {"character_id": "", "team_planner_code": 5}],
  "TFTSet4_Stage2": [{"character_id": "TFT4_Ahri", "team_planner_code": 3}],
  "TFTSet": [], "TFTSet018": []
}`

func TestParsePlannerCodes(t *testing.T) {
	codes, err := parsePlannerCodes([]byte(plannerFixture))
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != 2 {
		t.Fatalf("sets = %v, want only TFTSet17 and TFTSet18", codes)
	}
	if got := codes[18]["DA_18_Ahri"]; got != 1001 || codes[18]["DA_18_Ashe"] != 1008 || len(codes[18]) != 2 {
		t.Errorf("set 18 = %v", codes[18])
	}
	if _, ok := codes[17]["TFT17_Enemy_Aatrox"]; ok || codes[17]["TFT17_Akali"] != 13 {
		t.Errorf("set 17 = %v (code 0 means not plannable)", codes[17])
	}
}

func TestFetchPlannerCodes(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.Write([]byte(plannerFixture))
	}))
	defer srv.Close()
	ctx := context.Background()
	codes, err := FetchPlannerCodes(ctx, Source{BaseURL: srv.URL, HTTP: srv.Client()})
	if err != nil || codes[18]["DA_18_Ahri"] != 1001 {
		t.Fatalf("codes = %v, err = %v", codes, err)
	}
	if !strings.HasSuffix(path, "tftchampions-teamplanner.json") {
		t.Errorf("fetched %q", path)
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "no", 500) }))
	defer bad.Close()
	if _, err := FetchPlannerCodes(ctx, Source{BaseURL: bad.URL, HTTP: bad.Client()}); err == nil {
		t.Error("want an error for a failing source")
	}

	big := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"TFTSet18": [` + strings.Repeat(` `, maxPlannerCodesBytes+10) + `]}`))
	}))
	defer big.Close()
	if _, err := FetchPlannerCodes(ctx, Source{BaseURL: big.URL, HTTP: &http.Client{Timeout: 10 * time.Second}}); err == nil {
		t.Error("want an error for an oversized response")
	}
}
