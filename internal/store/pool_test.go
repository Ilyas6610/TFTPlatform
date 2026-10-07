package store_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"tft-platform/internal/store"
)

func TestNewWithOptions_StatementTimeoutAndMaxConns(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	st, err := store.NewWithOptions(ctx, url, store.Options{StatementTimeout: 200 * time.Millisecond, MaxConns: 3})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if got := st.Pool.Config().MaxConns; got != 3 {
		t.Errorf("MaxConns = %d, want 3", got)
	}
	start := time.Now()
	_, err = st.Pool.Exec(ctx, `SELECT pg_sleep(5)`)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "57014" { // query_canceled
		t.Fatalf("a slow statement should be cancelled by the timeout, got %v", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Errorf("cancelled after %v, want about 200ms", time.Since(start))
	}
	// The pool still works afterwards.
	var one int
	if err := st.Pool.QueryRow(ctx, `SELECT 1`).Scan(&one); err != nil || one != 1 {
		t.Errorf("pool unusable after a timeout: %v", err)
	}
}

func TestNew_HasNoStatementTimeoutByDefault(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	st, err := store.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var v string
	if err := st.Pool.QueryRow(context.Background(), `SHOW statement_timeout`).Scan(&v); err != nil || v != "0" {
		t.Errorf("statement_timeout = %q (%v), want 0 for binaries that don't set one", v, err)
	}
}
