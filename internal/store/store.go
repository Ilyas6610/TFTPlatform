// Package store provides Postgres access. Queries are hand-written against
// pgx rather than generated (e.g. via sqlc) since the schema is still
// evolving milestone by milestone; codegen can be introduced later if query
// volume grows enough to justify it.
package store

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	Pool *pgxpool.Pool
}

// Options tunes the connection pool; the zero value keeps pgx's defaults
// (no statement timeout, max(4, NumCPU) connections).
type Options struct {
	// StatementTimeout makes Postgres cancel any statement running longer
	// than this, so a slow query can't hold a pool connection indefinitely
	// (the API serves public analytics queries from one shared pool).
	StatementTimeout time.Duration
	// MaxConns caps pool connections.
	MaxConns int32
}

func New(ctx context.Context, databaseURL string) (*Store, error) {
	return NewWithOptions(ctx, databaseURL, Options{})
}

// NewWithOptions is New with pool settings.
func NewWithOptions(ctx context.Context, databaseURL string, opts Options) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	if opts.StatementTimeout > 0 {
		cfg.ConnConfig.RuntimeParams["statement_timeout"] = strconv.FormatInt(opts.StatementTimeout.Milliseconds(), 10)
	}
	if opts.MaxConns > 0 {
		cfg.MaxConns = opts.MaxConns
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect to postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return &Store{Pool: pool}, nil
}

func (s *Store) Close() {
	s.Pool.Close()
}
