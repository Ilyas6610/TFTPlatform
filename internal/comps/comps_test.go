package comps

import (
	"slices"
	"testing"
)

func board(placement int, units ...string) Board {
	b := Board{Placement: placement}
	for _, id := range units {
		b.Units = append(b.Units, Unit{ID: id, Star: 2})
	}
	return b
}

// withItems gives the first unit on the board an item set.
func withItems(b Board, items ...string) Board {
	b.Units[0].Items = items
	return b
}

func TestBuild_AnchorsOnTheMostPlayedExactBoard(t *testing.T) {
	core := []string{"A", "B", "C", "D", "E", "F", "G", "H"}
	var boards []Board
	for i := 0; i < 5; i++ { // the exact core board, 5 times
		boards = append(boards, withItems(board(i%8+1, core...), "IE", "GR", "KF"))
	}
	for i := 0; i < 3; i++ { // level 9: + Z
		boards = append(boards, board(1, append(slices.Clone(core), "Z")...))
	}
	for i := 0; i < 2; i++ { // swap H -> Y
		boards = append(boards, board(6, "A", "B", "C", "D", "E", "F", "G", "Y"))
	}
	// An unrelated comp.
	for i := 0; i < 6; i++ {
		boards = append(boards, board(4, "P", "Q", "R", "S", "T", "U", "V", "W"))
	}

	comps := Build(boards, Options{})
	if len(comps) != 2 {
		t.Fatalf("expected 2 comps, got %d", len(comps))
	}
	c := comps[0]
	if c.Boards != 10 || c.BoardStats.Boards != 5 {
		t.Errorf("comp boards %d (want 10), exact board %d (want 5)", c.Boards, c.BoardStats.Boards)
	}
	var ids []string
	for _, u := range c.Board {
		ids = append(ids, u.ID)
	}
	slices.Sort(ids)
	if !slices.Equal(ids, core) {
		t.Errorf("anchor board = %v, want %v", ids, core)
	}
	// The itemized unit leads, with its exact build.
	if c.Board[0].ID != "A" || !slices.Equal(c.Board[0].Items, []string{"GR", "IE", "KF"}) {
		t.Errorf("carry = %+v", c.Board[0])
	}
	if len(c.Variants) != 2 || !slices.Equal(c.Variants[0].Add, []string{"Z"}) || len(c.Variants[0].Remove) != 0 ||
		c.Variants[0].Boards != 3 || c.Variants[0].AvgPlacement != 1 {
		t.Errorf("first variant should be level-9 +Z: %+v", c.Variants)
	}
	if v := c.Variants[1]; !slices.Equal(v.Add, []string{"Y"}) || !slices.Equal(v.Remove, []string{"H"}) {
		t.Errorf("second variant should swap H for Y: %+v", v)
	}
	// Z is on 3 of 10 boards and Y on 2: both are flex at the 15% bar.
	if len(c.Flex) != 2 || c.Flex[0].ID != "Z" || c.Flex[0].Frequency != 0.3 {
		t.Errorf("flex = %+v", c.Flex)
	}
	if c.PlayRate != 10.0/16 {
		t.Errorf("play rate = %v", c.PlayRate)
	}
}

func TestBuild_DissimilarBoardsDontMerge(t *testing.T) {
	var boards []Board
	for i := 0; i < 5; i++ {
		boards = append(boards, board(1, "A", "B", "C", "D", "E", "F", "G", "H"))
		// Shares 4 of 12 units with the first: Jaccard 0.33.
		boards = append(boards, board(8, "A", "B", "C", "D", "W", "X", "Y", "Z"))
	}
	if comps := Build(boards, Options{}); len(comps) != 2 {
		t.Errorf("expected 2 separate comps, got %d", len(comps))
	}
}

func TestBuild_SmallCompsAreDropped(t *testing.T) {
	boards := []Board{board(1, "A", "B"), board(2, "A", "B"), board(3, "X", "Y")}
	if comps := Build(boards, Options{MinBoards: 2}); len(comps) != 1 || comps[0].Boards != 2 {
		t.Errorf("expected only the 2-board comp, got %+v", comps)
	}
	if comps := Build(nil, Options{}); len(comps) != 0 {
		t.Errorf("no boards should give no comps")
	}
}

func TestCommonBuild(t *testing.T) {
	cases := []struct {
		builds map[string]int
		want   string
	}{
		{map[string]int{"": 50, "A,B,C": 10}, "A,B,C"},           // "no items" never wins
		{map[string]int{"A,B,C": 2}, ""},                         // fewer than 3 copies
		{map[string]int{"": 100, "A,B,C": 3}, ""},                // under 5% of copies
		{map[string]int{"A,B": 5, "A,B,C": 5}, "A,B,C"},          // ties prefer fuller builds
		{map[string]int{"X,Y,Z": 4, "A,B,C": 4, "": 1}, "A,B,C"}, // then a stable order
	}
	for _, c := range cases {
		if got := commonBuild(c.builds, 0.05); got != c.want {
			t.Errorf("commonBuild(%v) = %q, want %q", c.builds, got, c.want)
		}
	}
}
