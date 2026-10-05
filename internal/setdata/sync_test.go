package setdata

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"tft-platform/internal/store/storetest"
)

// fakeCDragon serves content-metadata.json and the TFT export per channel.
type fakeCDragon struct {
	mu       sync.Mutex
	channels map[string]struct {
		version string
		export  []byte
	}
}

func newFakeCDragon(t *testing.T) (*fakeCDragon, Source) {
	f := &fakeCDragon{channels: map[string]struct {
		version string
		export  []byte
	}{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, Source{BaseURL: srv.URL, HTTP: srv.Client()}
}

func (f *fakeCDragon) set(channel, version string, export []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.channels[channel] = struct {
		version string
		export  []byte
	}{version, export}
}

func (f *fakeCDragon) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	channel, path, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	c, ok := f.channels[channel]
	switch {
	case !ok:
		w.WriteHeader(http.StatusNotFound)
	case path == "content-metadata.json":
		fmt.Fprintf(w, `{"version":"%s+branch.releases.content.release"}`, c.version)
	case path == "cdragon/tft/en_us.json":
		w.Write(c.export)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func TestSync_StoresNewPatchesAndBuildsHistory(t *testing.T) {
	st := storetest.New(t)
	cd, src := newFakeCDragon(t)
	ctx := context.Background()

	cd.set("16.18", "16.18.100", defaultFixture().json(t))
	changed := defaultFixture()
	changed.akaliMana = 25
	cd.set("latest", "16.19.200", changed.json(t))

	// Backfill out of order: latest first, then the older archive.
	for _, channel := range []string{"latest", "16.18"} {
		result, err := Sync(ctx, src, st, 0, channel)
		if err != nil {
			t.Fatalf("sync %s: %v", channel, err)
		}
		if !result.Stored || result.SetNumber != 18 {
			t.Errorf("sync %s: expected set 18 stored, got %+v", channel, result)
		}
	}

	snaps, err := LoadSnapshots(ctx, st, 18)
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != 2 || snaps[0].Version != "16.18.100" || snaps[1].Version != "16.19.200" || snaps[1].Patch != "16.19" {
		t.Fatalf("expected snapshots ordered by version, got %+v", snaps)
	}
	if mana := snaps[1].Units[0].Stats["mana"]; mana != 25 {
		t.Errorf("expected latest snapshot to have mana 25, got %v", mana)
	}
}

func TestSync_SkipsKnownVersionAndUnchangedBuilds(t *testing.T) {
	st := storetest.New(t)
	cd, src := newFakeCDragon(t)
	ctx := context.Background()

	cd.set("latest", "16.19.200", defaultFixture().json(t))
	if r, err := Sync(ctx, src, st, 18, "latest"); err != nil || !r.Stored {
		t.Fatalf("first sync: %+v, %v", r, err)
	}

	if r, err := Sync(ctx, src, st, 18, "latest"); err != nil || r.Stored {
		t.Errorf("same version again: expected not stored, got %+v, %v", r, err)
	}

	// A hotfix build that doesn't touch this set's data.
	cd.set("latest", "16.19.201", defaultFixture().json(t))
	if r, err := Sync(ctx, src, st, 18, "latest"); err != nil || r.Stored {
		t.Errorf("identical new build: expected not stored, got %+v, %v", r, err)
	}

	snaps, err := LoadSnapshots(ctx, st, 18)
	if err != nil || len(snaps) != 1 {
		t.Errorf("expected a single snapshot, got %d (err %v)", len(snaps), err)
	}
}

func TestSync_FetchErrors(t *testing.T) {
	st := storetest.New(t)
	_, src := newFakeCDragon(t)

	if _, err := Sync(context.Background(), src, st, 18, "16.01"); err == nil {
		t.Error("expected an error for a missing channel")
	}
}
