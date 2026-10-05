package apiserver

import (
	"net/http"
	"strconv"
	"time"

	"tft-platform/internal/setdata"
)

type SetVersionResponse struct {
	Version   string `json:"version"`
	Patch     string `json:"patch"`
	FetchedAt string `json:"fetchedAt"`
}

type SetDataResponse struct {
	*setdata.SetData
	Patch     string `json:"patch"`
	FetchedAt string `json:"fetchedAt"`
	// Versions lists every stored snapshot of the set, newest first.
	Versions []SetVersionResponse `json:"versions"`
}

type PatchNotesResponse struct {
	Patch string `json:"patch"`
	// TFTPatch is Riot's TFT numbering for Patch ("18.3"), and
	// OfficialNotesURL its official notes; both empty when unknown.
	TFTPatch         string           `json:"tftPatch,omitempty"`
	OfficialNotesURL string           `json:"officialNotesUrl,omitempty"`
	Version          string           `json:"version"`
	PreviousPatch    string           `json:"previousPatch"`
	PreviousVersion  string           `json:"previousVersion"`
	FetchedAt        string           `json:"fetchedAt"`
	Changes          []setdata.Change `json:"changes"`
}

// loadSetSnapshots parses {set} and loads its snapshots, writing the error
// response itself if there are none.
func (s *Server) loadSetSnapshots(w http.ResponseWriter, r *http.Request) ([]setdata.Snapshot, bool) {
	set, err := strconv.Atoi(r.PathValue("set"))
	if err != nil || set <= 0 {
		writeError(w, http.StatusBadRequest, "invalid_set", "set must be a positive number")
		return nil, false
	}
	snaps, err := setdata.LoadSnapshots(r.Context(), s.Store, set)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return nil, false
	}
	if len(snaps) == 0 {
		writeError(w, http.StatusNotFound, "no_set_data", "no data has been synced for this set yet")
		return nil, false
	}
	return snaps, true
}

// handleSetData serves GET /api/v1/sets/{set}/data: the set's units,
// traits, augments and items as of the newest stored patch, or of
// ?version=<version>.
func (s *Server) handleSetData(w http.ResponseWriter, r *http.Request) {
	snaps, ok := s.loadSetSnapshots(w, r)
	if !ok {
		return
	}

	chosen := snaps[len(snaps)-1]
	if v := r.URL.Query().Get("version"); v != "" {
		found := false
		for _, snap := range snaps {
			if snap.Version == v {
				chosen, found = snap, true
			}
		}
		if !found {
			writeError(w, http.StatusNotFound, "unknown_version", "no snapshot for version "+v)
			return
		}
	}

	setdata.AttachRewards(chosen.SetData)
	overrides, err := setdata.LoadTextOverrides(r.Context(), s.Store, chosen.SetNumber)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	setdata.AttachTextOverrides(chosen.SetData, overrides)
	resp := SetDataResponse{SetData: chosen.SetData, Patch: chosen.Patch, FetchedAt: chosen.FetchedAt.Format(time.RFC3339)}
	for i := len(snaps) - 1; i >= 0; i-- {
		resp.Versions = append(resp.Versions, SetVersionResponse{
			Version:   snaps[i].Version,
			Patch:     snaps[i].Patch,
			FetchedAt: snaps[i].FetchedAt.Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleSetPatches serves GET /api/v1/sets/{set}/patches: generated patch
// notes — the number changes between each stored snapshot and the one
// before it — newest first. The oldest snapshot is the baseline and has no
// notes of its own.
func (s *Server) handleSetPatches(w http.ResponseWriter, r *http.Request) {
	snaps, ok := s.loadSetSnapshots(w, r)
	if !ok {
		return
	}

	out := []PatchNotesResponse{}
	for i := len(snaps) - 1; i > 0; i-- {
		cur, prev := snaps[i], snaps[i-1]
		changes := setdata.Diff(prev.SetData, cur.SetData)
		if changes == nil {
			changes = []setdata.Change{}
		}
		tftPatch := setdata.TFTPatch(cur.SetNumber, cur.Patch)
		out = append(out, PatchNotesResponse{
			Patch:            cur.Patch,
			TFTPatch:         tftPatch,
			OfficialNotesURL: setdata.OfficialNotesURL(tftPatch),
			Version:          cur.Version,
			PreviousPatch:    prev.Patch,
			PreviousVersion:  prev.Version,
			FetchedAt:        cur.FetchedAt.Format(time.RFC3339),
			Changes:          changes,
		})
	}
	writeJSON(w, http.StatusOK, out)
}
