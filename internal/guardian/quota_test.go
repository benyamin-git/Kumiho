package guardian

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestQuotaPrefersHeadHeaders(t *testing.T) {
	var headCalls, getCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/fpn/token" {
			http.NotFound(w, r)
			return
		}
		switch r.Method {
		case http.MethodHead:
			headCalls.Add(1)
			w.Header().Set("X-Quota-Limit", "1000")
			w.Header().Set("X-Quota-Remaining", "750")
			w.Header().Set("X-Quota-Reset", "1790000000")
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			getCalls.Add(1)
			http.Error(w, "HEAD should be enough", http.StatusInternalServerError)
		default:
			http.Error(w, "unexpected method", http.StatusMethodNotAllowed)
		}
	}))
	defer srv.Close()

	c := NewClient()
	c.BaseURL = srv.URL
	q, err := c.Quota(context.Background(), "token")
	if err != nil {
		t.Fatal(err)
	}
	if headCalls.Load() != 1 || getCalls.Load() != 0 {
		t.Fatalf("quota calls: HEAD=%d GET=%d", headCalls.Load(), getCalls.Load())
	}
	if q.Limit == nil || *q.Limit != 1000 ||
		q.Remaining == nil || *q.Remaining != 750 ||
		q.Reset == nil || *q.Reset != 1790000000 {
		t.Fatalf("quota = %+v", q)
	}
}

func TestQuotaFallsBackToPass(t *testing.T) {
	var getCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/fpn/token" {
			http.NotFound(w, r)
			return
		}
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "unexpected method", http.StatusMethodNotAllowed)
			return
		}
		getCalls.Add(1)
		w.Header().Set("X-Quota-Limit", "2000")
		w.Header().Set("X-Quota-Remaining", "50")
		w.Header().Set("X-Quota-Reset", "1790000100")
		_ = json.NewEncoder(w).Encode(map[string]any{"token": "pass-token", "expires_at": 1790000200})
	}))
	defer srv.Close()

	c := NewClient()
	c.BaseURL = srv.URL
	q, err := c.Quota(context.Background(), "token")
	if err != nil {
		t.Fatal(err)
	}
	if getCalls.Load() != 1 {
		t.Fatalf("GET calls = %d, want 1", getCalls.Load())
	}
	if q.Limit == nil || *q.Limit != 2000 ||
		q.Remaining == nil || *q.Remaining != 50 ||
		q.Reset == nil || *q.Reset != 1790000100 {
		t.Fatalf("quota from pass = %+v", q)
	}
}

func TestQuotaFallsBackWhenHeadHasNoQuotaHeaders(t *testing.T) {
	var getCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		getCalls.Add(1)
		w.Header().Set("X-Quota-Remaining", "12")
		_ = json.NewEncoder(w).Encode(map[string]any{"token": "pass-token"})
	}))
	defer srv.Close()

	c := NewClient()
	c.BaseURL = srv.URL
	q, err := c.Quota(context.Background(), "token")
	if err != nil {
		t.Fatal(err)
	}
	if getCalls.Load() != 1 || q.Remaining == nil || *q.Remaining != 12 {
		t.Fatalf("quota = %+v, GET calls = %d", q, getCalls.Load())
	}
}

func TestQuotaUnauthorized(t *testing.T) {
	var getCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"message":"invalid token"}`)
			return
		}
		getCalls.Add(1)
	}))
	defer srv.Close()

	c := NewClient()
	c.BaseURL = srv.URL
	_, err := c.Quota(context.Background(), "token")
	if err == nil || !IsUnauthorized(err) {
		t.Fatalf("quota error = %v, want unauthorized", err)
	}
	if getCalls.Load() != 0 {
		t.Fatalf("unauthorized HEAD fell back to GET %d times", getCalls.Load())
	}
}

func TestQuotaRateLimitedSetsZero(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := NewClient()
	c.BaseURL = srv.URL
	q, err := c.Quota(context.Background(), "token")
	if err != nil {
		t.Fatalf("429 quota error = %v", err)
	}
	if q == nil || q.Remaining == nil || *q.Remaining != 0 {
		t.Fatalf("429 quota = %+v", q)
	}
}
