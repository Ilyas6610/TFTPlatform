package riotapi

import (
	"context"
	"encoding/json"
	"fmt"
)

// GetTFTMatchIDsByPUUID returns up to count recent match IDs for puuid via
// the region-routed tft/match-v1 endpoint.
func (c *Client) GetTFTMatchIDsByPUUID(ctx context.Context, routing RoutingRegion, puuid string, count int) ([]string, error) {
	seg, err := pathSegment(puuid)
	if err != nil {
		return nil, err
	}
	u := fmt.Sprintf("%s/tft/match/v1/matches/by-puuid/%s/ids?count=%d", routingHost(routing), seg, count)

	var ids []string
	if err := c.do(ctx, "tft-match-v1.get-ids-by-puuid", u, &ids); err != nil {
		return nil, err
	}
	return ids, nil
}

// TFTMatchParticipant covers the fields normalized into indexed DB columns
// (see internal/db/migrations/0004_match_participants.up.sql). Units/traits/
// augments are decoded as raw JSON rather than fixed structs because their
// shape changes every TFT set (~every 4 months) — callers store them
// verbatim in JSONB and the aggregation job parses them at query time.
type TFTMatchParticipant struct {
	PUUID                string          `json:"puuid"`
	Placement            int             `json:"placement"`
	Level                int             `json:"level"`
	LastRound            int             `json:"last_round"`
	PlayersEliminated    int             `json:"players_eliminated"`
	TotalDamageToPlayers int             `json:"total_damage_to_players"`
	GoldLeft             int             `json:"gold_left"`
	TimeEliminated       float64         `json:"time_eliminated"`
	Units                json.RawMessage `json:"units"`
	Traits               json.RawMessage `json:"traits"`
	Augments             json.RawMessage `json:"augments"`
	Companion            json.RawMessage `json:"companion"`
}

type TFTMatchInfo struct {
	GameDatetime int64                 `json:"game_datetime"` // epoch millis
	GameLength   float64               `json:"game_length"`
	GameVersion  string                `json:"game_version"`
	QueueID      int                   `json:"queue_id"`
	TFTGameType  string                `json:"tft_game_type"`
	TFTSetNumber int                   `json:"tft_set_number"`
	Participants []TFTMatchParticipant `json:"participants"`
}

type TFTMatch struct {
	Metadata struct {
		MatchID      string   `json:"match_id"`
		Participants []string `json:"participants"`
	} `json:"metadata"`
	Info TFTMatchInfo `json:"info"`
}

// GetTFTMatch fetches full match detail via the region-routed tft/match-v1
// endpoint. It returns both the decoded TFTMatch (for normalized columns)
// and the raw JSON bytes (stored verbatim as matches.raw_payload — the
// permanent source of truth we can re-derive normalized data from even after
// a set's schema changes).
func (c *Client) GetTFTMatch(ctx context.Context, routing RoutingRegion, matchID string) (*TFTMatch, []byte, error) {
	seg, err := pathSegment(matchID)
	if err != nil {
		return nil, nil, err
	}
	u := fmt.Sprintf("%s/tft/match/v1/matches/%s", routingHost(routing), seg)

	var raw json.RawMessage
	if err := c.do(ctx, "tft-match-v1.get-match", u, &raw); err != nil {
		return nil, nil, err
	}

	var match TFTMatch
	if err := json.Unmarshal(raw, &match); err != nil {
		return nil, nil, fmt.Errorf("decode tft match: %w", err)
	}
	return &match, raw, nil
}
