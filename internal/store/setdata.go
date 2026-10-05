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
