package apiserver

// PlayerProfileResponse is decoupled from the store/riotapi row types so the
// frontend contract doesn't leak DB or Riot-payload shape changes directly.
type PlayerProfileResponse struct {
	PUUID          string `json:"puuid"`
	GameName       string `json:"gameName"`
	TagLine        string `json:"tagLine"`
	PlatformRegion string `json:"platformRegion"`
	SummonerLevel  int    `json:"summonerLevel"`
	ProfileIconID  int    `json:"profileIconId"`
	// Source is "cache" when served from Postgres, "live" when freshly
	// fetched from Riot on this request — lets the frontend distinguish an
	// instant cached result from a slower live lookup.
	Source string `json:"source"`
}

type errorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}
