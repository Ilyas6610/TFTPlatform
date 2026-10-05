package riotapi

import (
	"context"
	"fmt"
	"net/url"
)

type Account struct {
	PUUID    string `json:"puuid"`
	GameName string `json:"gameName"`
	TagLine  string `json:"tagLine"`
}

// GetAccountByRiotID resolves a Riot ID (gameName#tagLine) to a PUUID via the
// region-routed (NOT platform-routed) account-v1 endpoint.
func (c *Client) GetAccountByRiotID(ctx context.Context, routing RoutingRegion, gameName, tagLine string) (*Account, error) {
	u := fmt.Sprintf("%s/riot/account/v1/accounts/by-riot-id/%s/%s",
		routingHost(routing), url.PathEscape(gameName), url.PathEscape(tagLine))

	var account Account
	if err := c.do(ctx, "account-v1.get-by-riot-id", u, &account); err != nil {
		return nil, err
	}
	return &account, nil
}
