package apiserver

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestWriteDBError_CancelledQueryIsBusyNotBroken(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/explore", nil)

	rec := httptest.NewRecorder()
	writeDBError(rec, req, fmt.Errorf("explore summary: %w", &pgconn.PgError{Code: "57014", Message: "canceling statement due to statement timeout"}))
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") == "" {
		t.Errorf("statement timeout: got %d, Retry-After %q; want 503 with Retry-After", rec.Code, rec.Header().Get("Retry-After"))
	}
	if strings.Contains(rec.Body.String(), "statement") {
		t.Errorf("response leaks the database message: %s", rec.Body)
	}

	rec = httptest.NewRecorder()
	writeDBError(rec, req, errors.New("connection refused"))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("other database errors stay 500, got %d", rec.Code)
	}
}
