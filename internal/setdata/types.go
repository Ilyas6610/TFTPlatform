// Package setdata extracts a TFT set's static game data (units, traits,
// augments, items) from CommunityDragon's export, renders its description
// templates with real numbers, and diffs snapshots between patches to
// produce number-level patch notes.
//
// Snapshots are stored per CommunityDragon content version (see sync.go), so
// re-running a sync after a patch or hotfix records the new numbers and the
// diff against the previous version becomes that patch's notes.
package setdata

// SetData is one patch's normalized data for a set — what the API serves and
// what patch notes are diffed from.
type SetData struct {
	SetNumber int       `json:"setNumber"`
	Version   string    `json:"version"` // e.g. "16.19.8230722"
	Units     []Unit    `json:"units"`
	Traits    []Trait   `json:"traits"`
	Augments  []Augment `json:"augments"`
	Items     []Item    `json:"items"`
}

type Unit struct {
	APIName string             `json:"apiName"`
	Name    string             `json:"name"`
	Icon    string             `json:"icon,omitempty"` // CommunityDragon URL; see iconURL
	Cost    int                `json:"cost"`
	Traits  []string           `json:"traits"` // trait display names
	Stats   map[string]float64 `json:"stats"`
	Ability Ability            `json:"ability"`
}

type Ability struct {
	Name string `json:"name"`
	// Desc is the rendered description; star-scaled values read "a/b/c".
	// Values the game computes at runtime (e.g. "@MagicDamageCalc1@") aren't
	// in the export and render as "?".
	Desc string `json:"desc"`
	// Values are the ability's variables by name, indexed by star level
	// (1-3), so index 0 is unused.
	Values map[string][]float64 `json:"values"`
}

type Trait struct {
	APIName     string       `json:"apiName"`
	Name        string       `json:"name"`
	Icon        string       `json:"icon,omitempty"`
	Desc        string       `json:"desc"` // rendered, without the breakpoint rows
	Breakpoints []Breakpoint `json:"breakpoints"`
}

type Breakpoint struct {
	MinUnits int                `json:"minUnits"`
	MaxUnits int                `json:"maxUnits"`
	Style    int                `json:"style"` // 1 bronze, 3 silver, 4 unique, 5 gold, 6 prismatic
	Text     string             `json:"text,omitempty"`
	Values   map[string]float64 `json:"values"`
}

type Augment struct {
	APIName string             `json:"apiName"`
	Name    string             `json:"name"`
	Icon    string             `json:"icon,omitempty"`
	Desc    string             `json:"desc"`
	Tier    int                `json:"tier"` // 1 silver, 2 gold, 3 prismatic; 0 if unknown
	Traits  []string           `json:"traits,omitempty"`
	Values  map[string]float64 `json:"values"`
}

type Item struct {
	APIName string `json:"apiName"`
	Name    string `json:"name"`
	Icon    string `json:"icon,omitempty"`
	Desc    string `json:"desc"`
	// Kind is "component", "completed", "emblem", "artifact", "radiant",
	// "consumable", "special" (set-specific, e.g. Set 18's DA_* items) or
	// "other".
	Kind string `json:"kind"`
	// Variant distinguishes same-named versions of a special item:
	// "", "upgraded", "prismatic" or "charm".
	Variant     string             `json:"variant,omitempty"`
	Composition []string           `json:"composition,omitempty"`
	Values      map[string]float64 `json:"values"`
}
