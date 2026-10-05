package apiserver

import (
	"net/http"
	"time"

	"tft-platform/internal/riotapi"
	"tft-platform/internal/store"
)

// profileStaleAfter bounds how long a cached profile is served before the
// next request triggers a live refresh.
const profileStaleAfter = 1 * time.Hour

// handlePlayerProfile serves GET /api/v1/players/{region}/{name}/{tag}.
// Postgres is checked first; on a cache miss or stale entry, it falls
// through to a live Riot fetch using the server's shared *riotapi.Client —
// the same instance (and rate limiter) the ingestion pipeline uses — and
// persists the result so subsequent lookups are cache hits.
func (s *Server) handlePlayerProfile(w http.ResponseWriter, r *http.Request) {
	platform := riotapi.PlatformRegion(r.PathValue("region"))
	gameName := r.PathValue("name")
	tagLine := r.PathValue("tag")

	routing, err := riotapi.RoutingForPlatform(platform)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_region", err.Error())
		return
	}

	ctx := r.Context()

	account, err := s.Store.GetAccountByRiotID(ctx, gameName, tagLine)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}

	var summoner *store.Summoner
	if account != nil {
		summoner, err = s.Store.GetSummonerByPUUID(ctx, account.PUUID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
	}

	if isFresh(account, summoner) {
		writeJSON(w, http.StatusOK, PlayerProfileResponse{
			PUUID:          account.PUUID,
			GameName:       account.GameName,
			TagLine:        account.TagLine,
			PlatformRegion: summoner.PlatformRegion,
			SummonerLevel:  summoner.SummonerLevel,
			ProfileIconID:  summoner.ProfileIconID,
			Source:         "cache",
		})
		return
	}

	riotAccount, err := s.Riot.GetAccountByRiotID(ctx, routing, gameName, tagLine)
	if err != nil {
		writeRiotError(w, err)
		return
	}
	riotSummoner, err := s.Riot.GetTFTSummonerByPUUID(ctx, platform, riotAccount.PUUID)
	if err != nil {
		writeRiotError(w, err)
		return
	}

	if err := s.Store.UpsertAccount(ctx, store.Account{
		PUUID:         riotAccount.PUUID,
		GameName:      riotAccount.GameName,
		TagLine:       riotAccount.TagLine,
		RoutingRegion: string(routing),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	if err := s.Store.UpsertSummoner(ctx, store.Summoner{
		PUUID:          riotAccount.PUUID,
		PlatformRegion: string(platform),
		SummonerID:     riotSummoner.ID,
		ProfileIconID:  riotSummoner.ProfileIconID,
		SummonerLevel:  riotSummoner.SummonerLevel,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, PlayerProfileResponse{
		PUUID:          riotAccount.PUUID,
		GameName:       riotAccount.GameName,
		TagLine:        riotAccount.TagLine,
		PlatformRegion: string(platform),
		SummonerLevel:  riotSummoner.SummonerLevel,
		ProfileIconID:  riotSummoner.ProfileIconID,
		Source:         "live",
	})
}

func isFresh(account *store.Account, summoner *store.Summoner) bool {
	if account == nil || summoner == nil {
		return false
	}
	if account.LastFetchedAt == nil || time.Since(*account.LastFetchedAt) >= profileStaleAfter {
		return false
	}
	if summoner.LastFetchedAt == nil || time.Since(*summoner.LastFetchedAt) >= profileStaleAfter {
		return false
	}
	return true
}
