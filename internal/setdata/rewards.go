package setdata

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
)

// Reward tables for augments with random or listed rewards. Set 18 moved
// this logic out of the game data CommunityDragon exports, so these are
// curated by hand from a cited community source (see rewards/*.json) rather
// than extracted. They're attached when serving set data, not stored in
// snapshots, so editing them never shows up as a patch-note change.
//
//go:embed rewards/*.json
var rewardFiles embed.FS

type Rewards struct {
	SourceName string        `json:"sourceName"`
	SourceURL  string        `json:"sourceUrl"`
	Updated    string        `json:"updated"` // date the source last updated the table
	Tables     []RewardTable `json:"tables"`
}

type RewardTable struct {
	Title   string     `json:"title"`
	Columns []string   `json:"columns"`
	Rows    [][]string `json:"rows"`
	Notes   []string   `json:"notes,omitempty"`
}

type rewardFile struct {
	Set    int `json:"set"`
	Source struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	} `json:"source"`
	Augments []struct {
		APINames  []string      `json:"apiNames"`
		SourceURL string        `json:"sourceUrl"`
		Updated   string        `json:"updated"`
		Tables    []RewardTable `json:"tables"`
	} `json:"augments"`
}

// rewardsBySet maps set number -> augment apiName -> rewards.
var rewardsBySet = mustLoadRewards(rewardFiles)

func mustLoadRewards(fsys fs.FS) map[int]map[string]*Rewards {
	out, err := loadRewards(fsys)
	if err != nil {
		panic(err)
	}
	return out
}

func loadRewards(fsys fs.FS) (map[int]map[string]*Rewards, error) {
	paths, err := fs.Glob(fsys, "rewards/*.json")
	if err != nil {
		return nil, err
	}
	out := map[int]map[string]*Rewards{}
	for _, path := range paths {
		raw, err := fs.ReadFile(fsys, path)
		if err != nil {
			return nil, err
		}
		var f rewardFile
		if err := json.Unmarshal(raw, &f); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if f.Set == 0 || f.Source.Name == "" {
			return nil, fmt.Errorf("%s: set and source.name are required", path)
		}
		bySet := out[f.Set]
		if bySet == nil {
			bySet = map[string]*Rewards{}
			out[f.Set] = bySet
		}
		for i, a := range f.Augments {
			if len(a.APINames) == 0 || a.SourceURL == "" || len(a.Tables) == 0 {
				return nil, fmt.Errorf("%s: augment %d needs apiNames, sourceUrl and tables", path, i)
			}
			for _, t := range a.Tables {
				for r, row := range t.Rows {
					if len(row) != len(t.Columns) {
						return nil, fmt.Errorf("%s: %v table %q row %d has %d cells for %d columns", path, a.APINames, t.Title, r, len(row), len(t.Columns))
					}
				}
			}
			rw := &Rewards{SourceName: f.Source.Name, SourceURL: a.SourceURL, Updated: a.Updated, Tables: a.Tables}
			for _, api := range a.APINames {
				if _, dup := bySet[api]; dup {
					return nil, fmt.Errorf("%s: %s listed twice", path, api)
				}
				bySet[api] = rw
			}
		}
	}
	return out, nil
}

// AttachRewards sets Rewards on d's augments that have a curated table.
func AttachRewards(d *SetData) {
	bySet := rewardsBySet[d.SetNumber]
	for i := range d.Augments {
		d.Augments[i].Rewards = bySet[d.Augments[i].APIName]
	}
}
