package setdata

import (
	"testing"
	"time"

	"tft-platform/internal/store"
)

func TestPatchOfGame(t *testing.T) {
	day := func(d int) time.Time { return time.Date(2026, 9, d, 12, 0, 0, 0, time.UTC) }
	cal := []store.PatchStart{
		{SetNumber: 18, TFTPatch: "18.1", StartsAt: day(1)},
		{SetNumber: 18, TFTPatch: "18.2", StartsAt: day(10)},
		{SetNumber: 18, TFTPatch: "18.3", StartsAt: day(23)},
	}
	cases := []struct {
		set     int
		at      time.Time
		version string
		want    string
	}{
		{18, day(5), "TFT Unreal Version ?.?.?.?", "18.1"},
		{18, day(10), "", "18.2"},
		{18, day(30), "", "18.3"},
		{18, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), "", ""}, // before the calendar
		{17, day(5), "Linux Version 16.15.802.4387 (Aug 03 2026) [PUBLIC] <Releases/16.15>", "16.15"}, // version wins; set 17 has no TFT numbering here
		{18, day(5), "Linux Version 16.18.1 [PUBLIC] <Releases/16.18>", "18.2"},
	}
	for _, c := range cases {
		if got := PatchOfGame(cal, c.set, c.at, c.version); got != c.want {
			t.Errorf("set %d at %s (%q) = %q, want %q", c.set, c.at.Format("Jan 2"), c.version, got, c.want)
		}
	}
}
