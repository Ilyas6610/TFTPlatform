package apiserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"tft-platform/internal/setdata"
	"tft-platform/internal/store"
	"tft-platform/internal/store/storetest"
)

func storeSetSnapshot(t *testing.T, st *store.Store, version string, akaliMana float64) {
	t.Helper()
	data := setdata.SetData{
		SetNumber: 18,
		Version:   version,
		Units: []setdata.Unit{{
			APIName: "DA_18_Akali", Name: "Akali", Cost: 1, Traits: []string{"Adaptor"},
			Stats: map[string]float64{"mana": akaliMana},
		}},
	}
	b, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.InsertSetDataSnapshot(context.Background(), store.SetDataSnapshot{
		SetNumber: 18, Version: version, Patch: setdata.PatchOf(version), Data: b,
	}); err != nil {
		t.Fatal(err)
	}
}

func get[T any](t *testing.T, s *Server, path string) (int, T) {
	t.Helper()
	rec := httptest.NewRecorder()
	NewRouter(s).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	var out T
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
	}
	return rec.Code, out
}

func newSetServer(t *testing.T) *Server {
	s := &Server{Store: storetest.New(t)}
	// Stored out of order on purpose: responses must order by version.
	storeSetSnapshot(t, s.Store, "16.19.200", 25)
	storeSetSnapshot(t, s.Store, "16.17.100", 30)
	storeSetSnapshot(t, s.Store, "16.18.150", 30)
	return s
}

func TestSetData_ServesLatestWithVersionList(t *testing.T) {
	s := newSetServer(t)

	code, resp := get[SetDataResponse](t, s, "/api/v1/sets/18/data")

	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if resp.Version != "16.19.200" || resp.Patch != "16.19" || resp.Units[0].Stats["mana"] != 25 {
		t.Errorf("expected latest snapshot, got version %s patch %s", resp.Version, resp.Patch)
	}
	var versions []string
	for _, v := range resp.Versions {
		versions = append(versions, v.Version)
	}
	if want := []string{"16.19.200", "16.18.150", "16.17.100"}; len(versions) != 3 || versions[0] != want[0] || versions[2] != want[2] {
		t.Errorf("expected versions newest first %v, got %v", want, versions)
	}
}

func TestSetData_SpecificVersion(t *testing.T) {
	s := newSetServer(t)

	code, resp := get[SetDataResponse](t, s, "/api/v1/sets/18/data?version=16.17.100")
	if code != http.StatusOK || resp.Version != "16.17.100" || resp.Units[0].Stats["mana"] != 30 {
		t.Errorf("expected the 16.17 snapshot, got %d %s", code, resp.Version)
	}

	if code, _ := get[SetDataResponse](t, s, "/api/v1/sets/18/data?version=1.2.3"); code != http.StatusNotFound {
		t.Errorf("unknown version: expected 404, got %d", code)
	}
}

func TestSetData_Errors(t *testing.T) {
	s := newSetServer(t)
	for path, want := range map[string]int{
		"/api/v1/sets/17/data":    http.StatusNotFound,
		"/api/v1/sets/abc/data":   http.StatusBadRequest,
		"/api/v1/sets/0/patches":  http.StatusBadRequest,
		"/api/v1/sets/17/patches": http.StatusNotFound,
	} {
		if code, _ := get[json.RawMessage](t, s, path); code != want {
			t.Errorf("%s: expected %d, got %d", path, want, code)
		}
	}
}

func TestSetPatches_DiffsConsecutiveVersionsNewestFirst(t *testing.T) {
	s := newSetServer(t)

	code, notes := get[[]PatchNotesResponse](t, s, "/api/v1/sets/18/patches")

	if code != http.StatusOK || len(notes) != 2 {
		t.Fatalf("expected notes for 2 patches (oldest is the baseline), got %d %d", code, len(notes))
	}
	latest, older := notes[0], notes[1]
	if latest.Patch != "16.19" || latest.PreviousPatch != "16.18" || older.Patch != "16.18" || older.PreviousPatch != "16.17" {
		t.Errorf("unexpected ordering: %+v / %+v", latest, older)
	}
	if len(latest.Changes) != 1 || latest.Changes[0].Field != "mana" || *latest.Changes[0].Old != 30 || *latest.Changes[0].New != 25 {
		t.Errorf("expected Akali mana 30 -> 25, got %+v", latest.Changes)
	}
	if older.Changes == nil || len(older.Changes) != 0 {
		t.Errorf("expected an empty (non-null) change list for 16.18, got %+v", older.Changes)
	}
}
