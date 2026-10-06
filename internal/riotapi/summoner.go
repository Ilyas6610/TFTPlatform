package riotapi

import (
	"context"
	"fmt"
)

type Summoner struct {
	PUUID         string `json:"puuid"`
	ID            string `json:"id"` // encrypted summonerId
	ProfileIconID int    `json:"profileIconId"`
	SummonerLevel int    `json:"summonerLevel"`
}

// GetTFTSummonerByPUUID fetches TFT summoner data via the platform-routed
// (NOT region-routed) tft/summoner-v1 endpoint.
func (c *Client) GetTFTSummonerByPUUID(ctx context.Context, platform PlatformRegion, puuid string) (*Summoner, error) {
	seg, err := pathSegment(puuid)
	if err != nil {
		return nil, err
	}
	u := fmt.Sprintf("%s/tft/summoner/v1/summoners/by-puuid/%s", platformHost(platform), seg)

	var s Summoner
	if err := c.do(ctx, "tft-summoner-v1.get-by-puuid", u, &s); err != nil {
		return nil, err
	}
	return &s, nil
}
