package setdata

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// plannerCodesPath is CommunityDragon's list of the shop champions the
// in-game Team Planner knows, with the number each one is encoded as in a
// Team Planner code. The game's own client data isn't published anywhere
// else; the file is keyed by set ("TFTSet18").
const plannerCodesPath = "latest/plugins/rcp-be-lol-game-data/global/default/v1/tftchampions-teamplanner.json"

// maxPlannerCodesBytes caps the download; the file is ~65 KB.
const maxPlannerCodesBytes = 4 << 20

// PlannerCodes maps TFT set number -> champion apiName -> Team Planner code.
type PlannerCodes map[int]map[string]int

// FetchPlannerCodes downloads and parses the Team Planner champion codes for
// every set the file lists. Champions without a code (enemy-only units) are
// left out.
func FetchPlannerCodes(ctx context.Context, src Source) (PlannerCodes, error) {
	url := strings.TrimRight(src.BaseURL, "/") + "/" + plannerCodesPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := src.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: status %d", url, resp.StatusCode)
	}
	body, err := readLimited(resp.Body, maxPlannerCodesBytes, url)
	if err != nil {
		return nil, err
	}
	return parsePlannerCodes(body)
}

func parsePlannerCodes(body []byte) (PlannerCodes, error) {
	var raw map[string][]struct {
		CharacterID string `json:"character_id"`
		Code        int    `json:"team_planner_code"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode planner codes: %w", err)
	}
	out := PlannerCodes{}
	for key, champs := range raw {
		var set int
		if _, err := fmt.Sscanf(key, "TFTSet%d", &set); err != nil || set <= 0 || key != fmt.Sprintf("TFTSet%d", set) {
			continue // e.g. a stage variant like "TFTSet4_Stage2"
		}
		codes := map[string]int{}
		for _, c := range champs {
			if c.CharacterID != "" && c.Code > 0 {
				codes[c.CharacterID] = c.Code
			}
		}
		out[set] = codes
	}
	return out, nil
}
