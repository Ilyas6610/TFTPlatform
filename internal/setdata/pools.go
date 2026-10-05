package setdata

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// Pools are the apiNames Riot's set definition actually includes, read from
// the TFT map data (game/data/maps/shipping/map22/map22.bin.json). The
// export's per-set "items" list is the union of every list the set pulls
// in — including the shared Common_Items pool with debug items, legacy
// copies and other modes' items — so it can't tell what's live. The set's
// own lists can: for Set 18, Set18_Items (156), Set18_Items_Augments (250)
// and TFTSet18_Items_Charms (343, the Wisps).
type Pools struct {
	Augments []string
	Items    []string
	Wisps    []string
}

func (p *Pools) has(list []string, api string) bool {
	for _, a := range list {
		if a == api {
			return true
		}
	}
	return false
}

// FetchPools downloads channel's map22 data (~70 MB) and returns set's
// pools, or nil if the set defines no set-specific lists (older sets).
func (s Source) FetchPools(ctx context.Context, channel string, set int) (*Pools, error) {
	url := strings.TrimRight(s.BaseURL, "/") + "/" + channel + "/game/data/maps/shipping/map22/map22.bin.json"
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
	// Streamed, but still capped (the file is ~70 MB): a truncated stream
	// fails to parse rather than reading without end.
	return parsePools(io.LimitReader(resp.Body, maxMapBytes), set)
}

const maxMapBytes = 500 << 20

// parsePools streams the map data — one top-level object of ~19k entries —
// keeping only set definitions, item lists and item names, so the whole
// file is never held in memory.
func parsePools(r io.Reader, set int) (*Pools, error) {
	dec := json.NewDecoder(r)
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, fmt.Errorf("map data: expected an object (%v)", err)
	}

	type entry struct {
		Type      string   `json:"__type"`
		Name      string   `json:"name"`
		MName     string   `json:"mName"`
		ItemLists []string `json:"itemLists"`
		MItems    []string `json:"mItems"`
	}
	type itemList struct {
		name  string
		items []string
	}
	var setLists []string
	lists := map[string]itemList{}
	itemNames := map[string]string{} // entry key (and its hash) -> item apiName
	setName := fmt.Sprintf("TFTSet%d", set)

	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("map data: %w", err)
		}
		key, _ := keyTok.(string)
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, fmt.Errorf("map data entry %s: %w", key, err)
		}
		// Entries that aren't objects (or don't fit entry) aren't ones we want.
		var e entry
		if err := json.Unmarshal(raw, &e); err != nil {
			continue
		}
		switch e.Type {
		case "TFTSetData":
			if e.Name == setName {
				setLists = e.ItemLists
			}
		case "TFTItemList":
			lists[key] = itemList{name: e.Name, items: e.MItems}
		case "TftItemData":
			if e.MName != "" {
				itemNames[key] = e.MName
				itemNames[varHash(key)] = e.MName
			}
		}
	}
	if setLists == nil {
		return nil, fmt.Errorf("map data: %s not found", setName)
	}

	setTag := strconv.Itoa(set)
	var p Pools
	found := false
	for _, ref := range setLists {
		l, ok := lists[ref]
		// Only the set's own lists ("Set18_Items", "TFTSet18_Items_Charms");
		// shared lists ("Common_Items", "..._Set17") are skipped.
		if !ok || !strings.Contains(l.name, "Set"+setTag+"_") {
			continue
		}
		var names []string
		for _, ref := range l.items {
			if n, ok := itemNames[ref]; ok {
				names = append(names, n)
			}
		}
		found = true
		switch {
		case strings.Contains(l.name, "Charms"):
			p.Wisps = append(p.Wisps, names...)
		case strings.Contains(l.name, "Augments"):
			p.Augments = append(p.Augments, names...)
		default:
			p.Items = append(p.Items, names...)
		}
	}
	if !found {
		return nil, nil
	}
	return &p, nil
}
