package store

import "context"

// DistinctSetNumbers returns every TFT set number represented in ingested
// matches, so the aggregator can recompute meta stats for each one without
// the caller needing to know set numbers in advance.
func (s *Store) DistinctSetNumbers(ctx context.Context) ([]int, error) {
	rows, err := s.Pool.Query(ctx, `SELECT DISTINCT tft_set_number FROM matches WHERE tft_set_number IS NOT NULL ORDER BY tft_set_number`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []int
	for rows.Next() {
		var n int
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// RecomputeUnitStats rebuilds meta_unit_stats for setNumber from every unit
// appearance across match_participants. This does one pass over the data in
// SQL rather than looping in Go — the whole point is to keep this off the
// request path and safe to run as a periodic batch. Returns the number of
// unit rows written.
func (s *Store) RecomputeUnitStats(ctx context.Context, setNumber int) (int, error) {
	tag, err := s.Pool.Exec(ctx, `
		WITH set_totals AS (
			SELECT count(*) AS total_games
			FROM match_participants mp
			JOIN matches m USING (match_id)
			WHERE m.tft_set_number = $1
		),
		unit_rows AS (
			SELECT elem->>'character_id' AS character_id, mp.placement
			FROM match_participants mp
			JOIN matches m USING (match_id)
			CROSS JOIN LATERAL jsonb_array_elements(mp.units) AS elem
			WHERE m.tft_set_number = $1
		)
		INSERT INTO meta_unit_stats (tft_set_number, character_id, tier_context, games_played, avg_placement, top4_rate, win_rate, pick_rate, computed_at)
		SELECT
			$1, ur.character_id, 'ALL',
			count(*),
			avg(ur.placement),
			avg((ur.placement <= 4)::int::numeric),
			avg((ur.placement = 1)::int::numeric),
			count(*)::numeric / st.total_games,
			now()
		FROM unit_rows ur, set_totals st
		GROUP BY ur.character_id, st.total_games
		ON CONFLICT (tft_set_number, character_id, tier_context) DO UPDATE SET
			games_played = EXCLUDED.games_played,
			avg_placement = EXCLUDED.avg_placement,
			top4_rate = EXCLUDED.top4_rate,
			win_rate = EXCLUDED.win_rate,
			pick_rate = EXCLUDED.pick_rate,
			computed_at = EXCLUDED.computed_at
	`, setNumber)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

// RecomputeTraitStats rebuilds meta_trait_stats for setNumber. Only traits
// with tier_current > 0 count — an inactive trait (0 units short of the
// first breakpoint) shouldn't be scored as if it contributed to the result.
func (s *Store) RecomputeTraitStats(ctx context.Context, setNumber int) (int, error) {
	tag, err := s.Pool.Exec(ctx, `
		WITH trait_rows AS (
			SELECT elem->>'name' AS trait_name, (elem->>'tier_current')::int AS trait_tier, mp.placement
			FROM match_participants mp
			JOIN matches m USING (match_id)
			CROSS JOIN LATERAL jsonb_array_elements(mp.traits) AS elem
			WHERE m.tft_set_number = $1 AND (elem->>'tier_current')::int > 0
		)
		INSERT INTO meta_trait_stats (tft_set_number, trait_name, trait_tier, tier_context, games_played, avg_placement, top4_rate, win_rate, computed_at)
		SELECT
			$1, trait_name, trait_tier, 'ALL',
			count(*),
			avg(placement),
			avg((placement <= 4)::int::numeric),
			avg((placement = 1)::int::numeric),
			now()
		FROM trait_rows
		GROUP BY trait_name, trait_tier
		ON CONFLICT (tft_set_number, trait_name, trait_tier, tier_context) DO UPDATE SET
			games_played = EXCLUDED.games_played,
			avg_placement = EXCLUDED.avg_placement,
			top4_rate = EXCLUDED.top4_rate,
			win_rate = EXCLUDED.win_rate,
			computed_at = EXCLUDED.computed_at
	`, setNumber)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

// RecomputeAugmentStats rebuilds meta_augment_stats for setNumber.
// participant.augments is a flat array of augment ID strings per Riot's
// schema (unlike units/traits, which are arrays of objects), and can be
// entirely absent for game modes/versions that don't carry augments —
// participants with a null augments column are simply excluded.
func (s *Store) RecomputeAugmentStats(ctx context.Context, setNumber int) (int, error) {
	tag, err := s.Pool.Exec(ctx, `
		WITH augment_rows AS (
			SELECT elem AS augment_id, mp.placement
			FROM match_participants mp
			JOIN matches m USING (match_id)
			CROSS JOIN LATERAL jsonb_array_elements_text(mp.augments) AS elem
			WHERE m.tft_set_number = $1 AND mp.augments IS NOT NULL
		)
		INSERT INTO meta_augment_stats (tft_set_number, augment_id, tier_context, games_played, avg_placement, top4_rate, win_rate, computed_at)
		SELECT
			$1, augment_id, 'ALL',
			count(*),
			avg(placement),
			avg((placement <= 4)::int::numeric),
			avg((placement = 1)::int::numeric),
			now()
		FROM augment_rows
		GROUP BY augment_id
		ON CONFLICT (tft_set_number, augment_id, tier_context) DO UPDATE SET
			games_played = EXCLUDED.games_played,
			avg_placement = EXCLUDED.avg_placement,
			top4_rate = EXCLUDED.top4_rate,
			win_rate = EXCLUDED.win_rate,
			computed_at = EXCLUDED.computed_at
	`, setNumber)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

func (s *Store) StartMetaComputeRun(ctx context.Context, setNumber int) (int64, error) {
	var id int64
	err := s.Pool.QueryRow(ctx, `INSERT INTO meta_compute_runs (tft_set_number) VALUES ($1) RETURNING id`, setNumber).Scan(&id)
	return id, err
}

func (s *Store) FinishMetaComputeRun(ctx context.Context, id int64, status string, matchesScanned int) error {
	_, err := s.Pool.Exec(ctx, `
		UPDATE meta_compute_runs SET finished_at = now(), status = $2, matches_scanned = $3 WHERE id = $1
	`, id, status, matchesScanned)
	return err
}

type UnitStat struct {
	CharacterID  string
	GamesPlayed  int
	AvgPlacement float64
	Top4Rate     float64
	WinRate      float64
	PickRate     float64
}

func (s *Store) GetUnitStats(ctx context.Context, setNumber int) ([]UnitStat, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT character_id, games_played, avg_placement::float8, top4_rate::float8, win_rate::float8, pick_rate::float8
		FROM meta_unit_stats
		WHERE tft_set_number = $1
		ORDER BY avg_placement ASC
	`, setNumber)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []UnitStat
	for rows.Next() {
		var u UnitStat
		if err := rows.Scan(&u.CharacterID, &u.GamesPlayed, &u.AvgPlacement, &u.Top4Rate, &u.WinRate, &u.PickRate); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

type TraitStat struct {
	TraitName    string
	TraitTier    int
	GamesPlayed  int
	AvgPlacement float64
	Top4Rate     float64
	WinRate      float64
}

func (s *Store) GetTraitStats(ctx context.Context, setNumber int) ([]TraitStat, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT trait_name, trait_tier, games_played, avg_placement::float8, top4_rate::float8, win_rate::float8
		FROM meta_trait_stats
		WHERE tft_set_number = $1
		ORDER BY avg_placement ASC
	`, setNumber)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []TraitStat
	for rows.Next() {
		var t TraitStat
		if err := rows.Scan(&t.TraitName, &t.TraitTier, &t.GamesPlayed, &t.AvgPlacement, &t.Top4Rate, &t.WinRate); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

type AugmentStat struct {
	AugmentID    string
	GamesPlayed  int
	AvgPlacement float64
	Top4Rate     float64
	WinRate      float64
}

func (s *Store) GetAugmentStats(ctx context.Context, setNumber int) ([]AugmentStat, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT augment_id, games_played, avg_placement::float8, top4_rate::float8, win_rate::float8
		FROM meta_augment_stats
		WHERE tft_set_number = $1
		ORDER BY avg_placement ASC
	`, setNumber)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AugmentStat
	for rows.Next() {
		var a AugmentStat
		if err := rows.Scan(&a.AugmentID, &a.GamesPlayed, &a.AvgPlacement, &a.Top4Rate, &a.WinRate); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
