package apiserver

import (
	"context"
	"fmt"
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

// maxSetNumber bounds the {set} path parameter; TFT is on set 18.
const maxSetNumber = 100

// setBundle is everything the set endpoints serve for one set, computed
// once per resultCacheTTL: snapshots (oldest first) with reward tables and
// text overrides attached, and the generated patch notes. Handlers only
// read it.
type setBundle struct {
	snaps    []setdata.Snapshot
	versions []SetVersionResponse // newest first
	patches  []PatchNotesResponse // newest first
}

// loadSet parses {set} and returns its bundle, writing the error response
// itself if it can't.
func (s *Server) loadSet(w http.ResponseWriter, r *http.Request) (*setBundle, bool) {
	set, err := strconv.Atoi(r.PathValue("set"))
	if err != nil || set <= 0 || set > maxSetNumber {
		writeError(w, http.StatusBadRequest, "invalid_set", fmt.Sprintf("set must be a number from 1 to %d", maxSetNumber))
		return nil, false
	}
	v, err := s.sets.get(r.Context(), "set|"+strconv.Itoa(set), func(ctx context.Context) (any, error) {
		return s.buildSetBundle(ctx, set)
	})
	if err != nil {
		writeDBError(w, r, err)
		return nil, false
	}
	b := v.(*setBundle)
	if len(b.snaps) == 0 {
		writeError(w, http.StatusNotFound, "no_set_data", "no data has been synced for this set yet")
		return nil, false
	}
	// Snapshots change only when a sync stores a new patch.
	w.Header().Set("Cache-Control", "public, max-age=300")
	return b, true
}

func (s *Server) buildSetBundle(ctx context.Context, set int) (*setBundle, error) {
	snaps, err := setdata.LoadSnapshots(ctx, s.Store, set)
	if err != nil {
		return nil, err
	}
	b := &setBundle{snaps: snaps, patches: []PatchNotesResponse{}}

	// Patch notes diff the snapshots as stored, before anything is
	// attached: overrides apply to the newest version only and would show
	// up as changes.
	for i := len(snaps) - 1; i > 0; i-- {
		cur, prev := snaps[i], snaps[i-1]
		changes := setdata.Diff(prev.SetData, cur.SetData)
		if changes == nil {
			changes = []setdata.Change{}
		}
		tftPatch := setdata.TFTPatch(cur.SetNumber, cur.Patch)
		b.patches = append(b.patches, PatchNotesResponse{
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

	overrides, err := setdata.LoadTextOverrides(ctx, s.Store, set)
	if err != nil {
		return nil, err
	}
	for i := len(snaps) - 1; i >= 0; i-- {
		setdata.AttachRewards(snaps[i].SetData)
		setdata.AttachTextOverrides(snaps[i].SetData, overrides)
		b.versions = append(b.versions, SetVersionResponse{
			Version:   snaps[i].Version,
			Patch:     snaps[i].Patch,
			FetchedAt: snaps[i].FetchedAt.Format(time.RFC3339),
		})
	}
	return b, nil
}

// handleSetData serves GET /api/v1/sets/{set}/data: the set's units,
// traits, augments and items as of the newest stored patch, or of
// ?version=<version>.
func (s *Server) handleSetData(w http.ResponseWriter, r *http.Request) {
	b, ok := s.loadSet(w, r)
	if !ok {
		return
	}

	chosen := b.snaps[len(b.snaps)-1]
	if v := r.URL.Query().Get("version"); v != "" {
		found := false
		for _, snap := range b.snaps {
			if snap.Version == v {
				chosen, found = snap, true
			}
		}
		if !found {
			writeError(w, http.StatusNotFound, "unknown_version", "no snapshot for that version")
			return
		}
	}
	writeJSON(w, http.StatusOK, SetDataResponse{
		SetData:   chosen.SetData,
		Patch:     chosen.Patch,
		FetchedAt: chosen.FetchedAt.Format(time.RFC3339),
		Versions:  b.versions,
	})
}

// handleSetPatches serves GET /api/v1/sets/{set}/patches: generated patch
// notes — the number changes between each stored snapshot and the one
// before it — newest first. The oldest snapshot is the baseline and has no
// notes of its own.
func (s *Server) handleSetPatches(w http.ResponseWriter, r *http.Request) {
	b, ok := s.loadSet(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, b.patches)
}
