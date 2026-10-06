package setdata

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"time"

	"tft-platform/internal/store"
)

// Patch calendar: Set 18's matches report no game version ("TFT Unreal
// Version ?.?.?.?"), so a game's patch comes from its date. Each patch's
// start is taken from Riot's official patch notes: their publish date plus a
// day, since TFT patches deploy the day after the notes (regions roll out
// over a few hours, so games right at a patch boundary may be off).

// patchDeployDelay is how long after the notes a patch is taken to be live.
const patchDeployDelay = 24 * time.Hour

var datePublished = regexp.MustCompile(`"datePublished":"([^"]+)"`)

// NotesFetcher returns a patch notes page's publish date.
type NotesFetcher func(ctx context.Context, url string) (time.Time, error)

// FetchNotesDate reads datePublished from a Riot patch notes page.
func FetchNotesDate(ctx context.Context, url string) (time.Time, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return time.Time{}, err
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return time.Time{}, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return time.Time{}, fmt.Errorf("fetch %s: status %d", url, resp.StatusCode)
	}
	body, err := readLimited(resp.Body, 8<<20, url)
	if err != nil {
		return time.Time{}, err
	}
	m := datePublished.FindSubmatch(body)
	if m == nil {
		return time.Time{}, fmt.Errorf("%s: no datePublished", url)
	}
	return time.Parse(time.RFC3339, string(m[1]))
}

// SyncPatchCalendar records the start of every patch of set that has a
// stored snapshot and isn't in the calendar yet. Patches whose notes can't
// be read are skipped (and retried on the next sync). It returns how many
// patches it added.
func SyncPatchCalendar(ctx context.Context, st *store.Store, set int, fetch NotesFetcher) (int, error) {
	snaps, err := LoadSnapshots(ctx, st, set)
	if err != nil {
		return 0, err
	}
	known, err := st.PatchCalendar(ctx)
	if err != nil {
		return 0, err
	}
	have := map[string]bool{}
	for _, p := range known {
		if p.SetNumber == set {
			have[p.TFTPatch] = true
		}
	}
	added := 0
	var firstErr error
	for _, snap := range snaps {
		tftPatch := TFTPatch(set, snap.Patch)
		if tftPatch == "" || have[tftPatch] {
			continue
		}
		url := OfficialNotesURL(tftPatch)
		published, err := fetch(ctx, url)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if err := st.PutPatchStart(ctx, store.PatchStart{
			SetNumber: set, TFTPatch: tftPatch, GamePatch: snap.Patch,
			StartsAt: published.Add(patchDeployDelay), SourceURL: url,
		}); err != nil {
			return added, err
		}
		have[tftPatch] = true
		added++
	}
	return added, firstErr
}

var releasePatch = regexp.MustCompile(`<Releases/(\d+\.\d+)>`)

// PatchOfGame names the patch a game was played on: from its game version
// when Riot reports one (sets before 18: "16.15", or "17.x" style when the
// set's TFT numbering is known), else from the calendar by date. "" if
// unknown.
func PatchOfGame(cal []store.PatchStart, set int, playedAt time.Time, gameVersion string) string {
	if m := releasePatch.FindStringSubmatch(gameVersion); m != nil {
		if p := TFTPatch(set, m[1]); p != "" {
			return p
		}
		return m[1]
	}
	patch := ""
	for _, p := range cal { // oldest first
		if p.SetNumber != set || p.StartsAt.After(playedAt) {
			continue
		}
		patch = p.TFTPatch
	}
	return patch
}
