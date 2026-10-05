package setdata

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"tft-platform/internal/store"
)

// Source fetches CommunityDragon exports. Channel is "latest" or a patch
// archive like "16.18" (raw.communitydragon.org/<channel>/...).
type Source struct {
	BaseURL string
	HTTP    *http.Client
}

func DefaultSource() Source {
	// The TFT export is ~25 MB.
	return Source{BaseURL: "https://raw.communitydragon.org", HTTP: &http.Client{Timeout: 3 * time.Minute}}
}

// Fetch returns channel's TFT export and its content version, normalized
// from "16.19.8230722+branch.releases-16-19.content.release" to
// "16.19.8230722".
func (s Source) Fetch(ctx context.Context, channel string) (raw []byte, version string, err error) {
	meta, err := s.get(ctx, channel+"/content-metadata.json")
	if err != nil {
		return nil, "", err
	}
	var m struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(meta, &m); err != nil || m.Version == "" {
		return nil, "", fmt.Errorf("content-metadata for %s: unreadable version", channel)
	}
	version, _, _ = strings.Cut(m.Version, "+")

	raw, err = s.get(ctx, channel+"/cdragon/tft/en_us.json")
	if err != nil {
		return nil, "", err
	}
	return raw, version, nil
}

func (s Source) get(ctx context.Context, path string) ([]byte, error) {
	url := strings.TrimRight(s.BaseURL, "/") + "/" + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: status %d", url, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// PatchOf returns the "major.minor" patch of a version ("16.19.8230722" ->
// "16.19").
func PatchOf(version string) string {
	parts := strings.SplitN(version, ".", 3)
	if len(parts) < 2 {
		return version
	}
	return parts[0] + "." + parts[1]
}

// CompareVersions orders versions numerically by dot-separated segment.
func CompareVersions(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		var x, y int
		if i < len(as) {
			x, _ = strconv.Atoi(as[i])
		}
		if i < len(bs) {
			y, _ = strconv.Atoi(bs[i])
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

type SyncResult struct {
	SetNumber int
	Version   string
	// Stored is false when the version was already stored, or when its data
	// is identical to the previous stored version (e.g. a hotfix build that
	// didn't touch this set).
	Stored bool
}

// Sync fetches channel's export and stores set's data as a new snapshot.
// set 0 means the newest set in the export.
func Sync(ctx context.Context, src Source, st *store.Store, set int, channel string) (SyncResult, error) {
	raw, version, err := src.Fetch(ctx, channel)
	if err != nil {
		return SyncResult{}, err
	}
	if set == 0 {
		if set, err = LatestSet(raw); err != nil {
			return SyncResult{}, err
		}
	}
	result := SyncResult{SetNumber: set, Version: version}

	data, err := Extract(raw, set, version)
	if err != nil {
		return result, err
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return result, err
	}

	snaps, err := LoadSnapshots(ctx, st, set)
	if err != nil {
		return result, err
	}
	if prev := previousSnapshot(snaps, version); prev != nil && sameContent(prev, data) {
		return result, nil
	}

	result.Stored, err = st.InsertSetDataSnapshot(ctx, store.SetDataSnapshot{
		SetNumber: set,
		Version:   version,
		Patch:     PatchOf(version),
		Data:      encoded,
	})
	return result, err
}

// Snapshot is a stored SetData with its storage metadata.
type Snapshot struct {
	*SetData
	Patch     string
	FetchedAt time.Time
}

// LoadSnapshots returns set's stored snapshots, oldest version first.
func LoadSnapshots(ctx context.Context, st *store.Store, set int) ([]Snapshot, error) {
	rows, err := st.ListSetDataSnapshots(ctx, set)
	if err != nil {
		return nil, err
	}
	out := make([]Snapshot, 0, len(rows))
	for _, r := range rows {
		var d SetData
		if err := json.Unmarshal(r.Data, &d); err != nil {
			return nil, fmt.Errorf("decode snapshot %s: %w", r.Version, err)
		}
		out = append(out, Snapshot{SetData: &d, Patch: r.Patch, FetchedAt: r.FetchedAt})
	}
	sort.Slice(out, func(i, j int) bool { return CompareVersions(out[i].Version, out[j].Version) < 0 })
	return out, nil
}

// previousSnapshot returns the newest snapshot older than version.
func previousSnapshot(snaps []Snapshot, version string) *SetData {
	var prev *SetData
	for _, s := range snaps {
		if CompareVersions(s.Version, version) < 0 {
			prev = s.SetData
		}
	}
	return prev
}

func sameContent(a, b *SetData) bool {
	ac, bc := *a, *b
	ac.Version, bc.Version = "", ""
	aj, _ := json.Marshal(ac)
	bj, _ := json.Marshal(bc)
	return bytes.Equal(aj, bj)
}
