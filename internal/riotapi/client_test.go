package riotapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func testClient() *Client {
	return NewClient(EnvKeySource{Key: "test-key"}, NewRateLimiter(1000, 1000))
}

func TestClient_Do_SuccessDecodesBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Riot-Token"); got != "test-key" {
			t.Errorf("expected X-Riot-Token header to be set, got %q", got)
		}
		w.Header().Set("X-App-Rate-Limit", "20:1,100:120")
		w.Header().Set("X-App-Rate-Limit-Count", "1:1,1:120")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"puuid":"abc123"}`))
	}))
	defer srv.Close()

	var out struct {
		PUUID string `json:"puuid"`
	}
	if err := testClient().do(context.Background(), "test.method", srv.URL, &out); err != nil {
		t.Fatalf("do: %v", err)
	}
	if out.PUUID != "abc123" {
		t.Fatalf("expected puuid=abc123, got %q", out.PUUID)
	}
}

func TestClient_Do_404MapsToErrNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	err := testClient().do(context.Background(), "test.method", srv.URL, nil)
	if _, ok := err.(*ErrNotFound); !ok {
		t.Fatalf("expected *ErrNotFound, got %T: %v", err, err)
	}
}

func TestClient_Do_403MapsToErrKeyExpired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	err := testClient().do(context.Background(), "test.method", srv.URL, nil)
	if _, ok := err.(*ErrKeyExpired); !ok {
		t.Fatalf("expected *ErrKeyExpired, got %T: %v", err, err)
	}
}

func TestClient_Do_429MapsToErrRateLimitedWithoutRetrying(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Retry-After", "1")
		w.Header().Set("X-Rate-Limit-Type", "application")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	err := testClient().do(context.Background(), "test.method", srv.URL, nil)
	rlErr, ok := err.(*ErrRateLimited)
	if !ok {
		t.Fatalf("expected *ErrRateLimited, got %T: %v", err, err)
	}
	if rlErr.RetryAfter != time.Second {
		t.Errorf("expected RetryAfter=1s, got %s", rlErr.RetryAfter)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("expected exactly 1 call (429 must not be silently retried by do()), got %d", got)
	}
}

func TestClient_Do_500RetriesThenSucceeds(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	if err := testClient().do(context.Background(), "test.method", srv.URL, nil); err != nil {
		t.Fatalf("expected eventual success after retries, got: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Errorf("expected 3 calls (2 failures + 1 success), got %d", got)
	}
}

func TestClient_Do_400NeverRetries(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	if err := testClient().do(context.Background(), "test.method", srv.URL, nil); err == nil {
		t.Fatal("expected an error for 400")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("expected exactly 1 call (400 must never be retried), got %d", got)
	}
}
