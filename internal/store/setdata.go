package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

type SetDataSnapshot struct {
	SetNumber int
	Version   string
	Patch     string
	Data      []byte // JSON-encoded setdata.SetData
	FetchedAt time.Time
}

// InsertSetDataSnapshot stores a snapshot unless one for the same version
// already exists, reporting whether it was inserted.
func (s *Store) InsertSetDataSnapshot(ctx context.Context, snap SetDataSnapshot) (bool, error) {
	tag, err := s.Pool.Exec(ctx, `
		INSERT INTO set_data_snapshots (set_number, version, patch, data)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (set_number, version) DO NOTHING
	`, snap.SetNumber, snap.Version, snap.Patch, snap.Data)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// ListSetDataSnapshots returns every snapshot for set, data included,
// unordered — callers sort by version (see setdata.LoadSnapshots).
func (s *Store) ListSetDataSnapshots(ctx context.Context, set int) ([]SetDataSnapshot, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT set_number, version, patch, data, fetched_at
		FROM set_data_snapshots
		WHERE set_number = $1
	`, set)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[SetDataSnapshot])
}

type SetTextOverrides struct {
	SetNumber int
	Source    string
	Version   string
	Data      []byte // JSON: apiName -> description template
	FetchedAt time.Time
}

// UpsertSetTextOverrides replaces the stored overrides for (set, source).
func (s *Store) UpsertSetTextOverrides(ctx context.Context, o SetTextOverrides) error {
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO set_text_overrides (set_number, source, version, data, fetched_at)
		VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (set_number, source) DO UPDATE SET
			version = EXCLUDED.version, data = EXCLUDED.data, fetched_at = now()
	`, o.SetNumber, o.Source, o.Version, o.Data)
	return err
}

// ListSetTextOverrides returns every source's overrides for set.
func (s *Store) ListSetTextOverrides(ctx context.Context, set int) ([]SetTextOverrides, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT set_number, source, version, data, fetched_at
		FROM set_text_overrides
		WHERE set_number = $1
		ORDER BY source
	`, set)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[SetTextOverrides])
}
