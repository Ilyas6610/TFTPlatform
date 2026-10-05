// Package storetest gives tests a *store.Store backed by a real Postgres
// with the current migrations applied. Each call gets its own fresh schema
// (dropped on cleanup), so tests and packages can run in parallel against
// one database. Tests are skipped unless TEST_DATABASE_URL is set, e.g.
// postgres://tft:tft@localhost:5432/tft_test?sslmode=disable.
package storetest

import (
	"context"
	"fmt"
	"math/rand"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"

	"tft-platform/internal/store"
)

func New(t *testing.T) *store.Store {
	t.Helper()
	baseURL := os.Getenv("TEST_DATABASE_URL")
	if baseURL == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping Postgres-backed test")
	}
	ctx := context.Background()

	admin, err := store.New(ctx, baseURL)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	schema := fmt.Sprintf("test_%d", rand.Int63())
	if _, err := admin.Pool.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		admin.Pool.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		admin.Close()
	})

	// pgx passes unrecognized URL params through as runtime parameters, so
	// every pooled connection gets this schema as its search_path.
	u, err := url.Parse(baseURL)
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()

	st, err := store.New(ctx, u.String())
	if err != nil {
		t.Fatalf("connect to test schema: %v", err)
	}
	t.Cleanup(st.Close)

	for _, path := range migrations(t) {
		sql, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read migration: %v", err)
		}
		if _, err := st.Pool.Exec(ctx, string(sql)); err != nil {
			t.Fatalf("apply %s: %v", filepath.Base(path), err)
		}
	}
	return st
}

func migrations(t *testing.T) []string {
	_, here, _, _ := runtime.Caller(0)
	paths, err := filepath.Glob(filepath.Join(filepath.Dir(here), "..", "..", "db", "migrations", "*.up.sql"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("find migrations: %v (found %d)", err, len(paths))
	}
	sort.Strings(paths)
	return paths
}
