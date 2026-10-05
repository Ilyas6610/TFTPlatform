package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type Summoner struct {
	PUUID          string
	PlatformRegion string
	SummonerID     string
	ProfileIconID  int
	SummonerLevel  int
	LastFetchedAt  *time.Time
}

func (s *Store) UpsertSummoner(ctx context.Context, sm Summoner) error {
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO summoners (puuid, platform_region, summoner_id, profile_icon_id, summoner_level, last_fetched_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, now(), now())
		ON CONFLICT (puuid) DO UPDATE SET
			platform_region = EXCLUDED.platform_region,
			summoner_id = EXCLUDED.summoner_id,
			profile_icon_id = EXCLUDED.profile_icon_id,
			summoner_level = EXCLUDED.summoner_level,
			last_fetched_at = now(),
			updated_at = now()
	`, sm.PUUID, sm.PlatformRegion, sm.SummonerID, sm.ProfileIconID, sm.SummonerLevel)
	return err
}

// GetSummonerByPUUID returns (nil, nil) on a cache miss, not an error.
func (s *Store) GetSummonerByPUUID(ctx context.Context, puuid string) (*Summoner, error) {
	var sm Summoner
	err := s.Pool.QueryRow(ctx, `
		SELECT puuid, platform_region, summoner_id, profile_icon_id, summoner_level, last_fetched_at
		FROM summoners
		WHERE puuid = $1
	`, puuid).Scan(&sm.PUUID, &sm.PlatformRegion, &sm.SummonerID, &sm.ProfileIconID, &sm.SummonerLevel, &sm.LastFetchedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &sm, nil
}
