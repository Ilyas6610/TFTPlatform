package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type Account struct {
	PUUID         string
	GameName      string
	TagLine       string
	RoutingRegion string
	LastFetchedAt *time.Time
}

// UpsertAccount inserts or refreshes an account row, keyed by puuid. Used
// both by the ingestion pipeline (bulk discovery) and by the API's
// live-fetch fallback (see internal/apiserver/handlers_profile.go), so a
// cache miss on read becomes a cache hit for subsequent lookups.
func (s *Store) UpsertAccount(ctx context.Context, a Account) error {
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO accounts (puuid, game_name, tag_line, routing_region, last_fetched_at, updated_at)
		VALUES ($1, $2, $3, $4, now(), now())
		ON CONFLICT (puuid) DO UPDATE SET
			game_name = EXCLUDED.game_name,
			tag_line = EXCLUDED.tag_line,
			routing_region = EXCLUDED.routing_region,
			last_fetched_at = now(),
			updated_at = now()
	`, a.PUUID, a.GameName, a.TagLine, a.RoutingRegion)
	return err
}

// UpsertAccountPUUIDOnly registers a PUUID discovered indirectly — e.g. as
// another participant in an ingested match — without a resolved Riot ID.
// game_name/tag_line stay NULL until a future account-v1 lookup backfills
// them. ON CONFLICT DO NOTHING so this never clobbers a name already
// resolved for that PUUID via UpsertAccount.
func (s *Store) UpsertAccountPUUIDOnly(ctx context.Context, puuid, routingRegion string) error {
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO accounts (puuid, routing_region)
		VALUES ($1, $2)
		ON CONFLICT (puuid) DO NOTHING
	`, puuid, routingRegion)
	return err
}

// SetAccountRiotID records a Riot ID resolved for an existing PUUID. Riot IDs
// can move between accounts (a name freed by one player and taken by
// another), so any other account still holding this ID — necessarily a stale
// record, given idx_accounts_riot_id — has it cleared first.
func (s *Store) SetAccountRiotID(ctx context.Context, puuid, gameName, tagLine string) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		UPDATE accounts SET game_name = NULL, tag_line = NULL, updated_at = now()
		WHERE game_name = $1 AND tag_line = $2 AND puuid <> $3
	`, gameName, tagLine, puuid); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE accounts SET game_name = $2, tag_line = $3, last_fetched_at = now(), updated_at = now()
		WHERE puuid = $1
	`, puuid, gameName, tagLine); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// GetAccountByRiotID looks up a cached account by Riot ID. Returns
// (nil, nil) on a cache miss (not an error) so callers can fall through to a
// live Riot API fetch. Matching ignores case, as Riot IDs do; if a name was
// held by several accounts over time, the most recently fetched wins.
func (s *Store) GetAccountByRiotID(ctx context.Context, gameName, tagLine string) (*Account, error) {
	var a Account
	err := s.Pool.QueryRow(ctx, `
		SELECT puuid, game_name, tag_line, routing_region, last_fetched_at
		FROM accounts
		WHERE lower(game_name) = lower($1) AND lower(tag_line) = lower($2)
		ORDER BY last_fetched_at DESC NULLS LAST
		LIMIT 1
	`, gameName, tagLine).Scan(&a.PUUID, &a.GameName, &a.TagLine, &a.RoutingRegion, &a.LastFetchedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}
