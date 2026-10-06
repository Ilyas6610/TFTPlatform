package advice

import (
	"testing"

	"tft-platform/internal/store"
)

func unit(id string, games int, avg float64, builds ...store.MetaBuild) store.MetaUnit {
	if builds == nil {
		builds = []store.MetaBuild{}
	}
	return store.MetaUnit{ID: id, PlacementStats: store.PlacementStats{Boards: games, AvgPlacement: avg}, Builds: builds}
}

func build(games int, avg float64, items ...string) store.MetaBuild {
	return store.MetaBuild{Items: items, PlacementStats: store.PlacementStats{Boards: games, AvgPlacement: avg}}
}

func TestUnits_RelativeToTheirEdge(t *testing.T) {
	// A strong player (3.5 vs everyone 4.5): expected = everyone's - 1.
	meta := &store.MetaResult{Units: []store.MetaUnit{
		unit("Weak", 500, 4.5), unit("Strong", 500, 4.5), unit("AsUsual", 500, 4.5),
		unit("Lucky", 500, 4.5), unit("Rare", 10, 4.5),
	}}
	player := &store.MetaResult{Units: []store.MetaUnit{
		unit("Weak", 20, 5.0),    // 1.5 worse than expected 3.5: noted, though better than everyone
		unit("Strong", 20, 2.0),  // 1.5 better
		unit("AsUsual", 20, 3.7), // within the gap
		unit("Lucky", 4, 1.0),    // too few games
		unit("Rare", 20, 6.0),    // everyone's figure too thin
	}}
	a := Build(player, meta, 3.5, 4.5, Defaults)
	if a.Edge != -1 {
		t.Errorf("edge = %v, want -1", a.Edge)
	}
	if len(a.Weak) != 1 || a.Weak[0].Unit != "Weak" || a.Weak[0].Expected != 3.5 {
		t.Errorf("weak = %+v, want Weak against 3.5", a.Weak)
	}
	if len(a.Strong) != 1 || a.Strong[0].Unit != "Strong" {
		t.Errorf("strong = %+v, want Strong", a.Strong)
	}
}

func TestUnits_NoiseIsNotANote(t *testing.T) {
	meta := &store.MetaResult{Units: []store.MetaUnit{unit("A", 500, 4.5)}}
	// 1.0 worse over 6 games is within ~1.5 standard errors (0.93 each).
	player := &store.MetaResult{Units: []store.MetaUnit{unit("A", 6, 5.5)}}
	if a := Build(player, meta, 4.5, 4.5, Defaults); len(a.Weak) != 0 {
		t.Errorf("weak = %+v, want none from 6 games", a.Weak)
	}
}

func TestBuilds(t *testing.T) {
	meta := &store.MetaResult{Units: []store.MetaUnit{
		unit("Jinx", 1000, 4.4,
			build(300, 4.6, "IE", "LW", "RB"),  // theirs
			build(200, 3.9, "GS", "IE", "LW"),  // clearly better
			build(15, 3.0, "BT", "IE", "LW"),   // better but too thin to trust
			build(250, 4.45, "DB", "IE", "LW"), // barely better
		),
		unit("Vi", 1000, 4.4, build(300, 4.4, "A", "B", "C"), build(300, 4.35, "D", "E", "F")),
		unit("Ahri", 1000, 4.4, build(300, 4.4, "X", "Y", "Z"), build(300, 3.0, "P", "Q", "R")),
	}}
	player := &store.MetaResult{Units: []store.MetaUnit{
		unit("Jinx", 10, 4.0, build(6, 4.0, "IE", "LW", "RB")),
		unit("Vi", 10, 4.0, build(6, 4.0, "A", "B", "C")),   // no clearly better build
		unit("Ahri", 10, 4.0, build(2, 4.0, "X", "Y", "Z")), // not their usual: 2 games
	}}
	a := Build(player, meta, 4.0, 4.4, Defaults)
	if len(a.Builds) != 1 {
		t.Fatalf("builds = %+v, want only Jinx", a.Builds)
	}
	b := a.Builds[0]
	if b.Unit != "Jinx" || b.TheirsMeta.AvgPlacement != 4.6 || b.Better.AvgPlacement != 3.9 || b.Theirs.Boards != 6 {
		t.Errorf("jinx = %+v, want IE/LW/RB (4.6) vs GS/IE/LW (3.9)", b)
	}
}
