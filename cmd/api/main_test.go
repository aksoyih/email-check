package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHandleHealth(t *testing.T) {
	srv := newTestServer("")
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	res := httptest.NewRecorder()

	srv.routes().ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, res.Code)
	}
	assertJSONErrorAbsent(t, res.Body.String())
}

func TestHandleDocs(t *testing.T) {
	srv := newTestServer("")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	res := httptest.NewRecorder()

	srv.routes().ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, res.Code)
	}
	if got := res.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("expected HTML content type, got %q", got)
	}
	if !strings.Contains(res.Body.String(), "@scalar/api-reference") {
		t.Fatal("expected Scalar API reference script")
	}
}

func TestHandleOpenAPI(t *testing.T) {
	srv := newTestServer("")
	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	res := httptest.NewRecorder()

	srv.routes().ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, res.Code)
	}
	var spec map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &spec); err != nil {
		t.Fatalf("expected valid OpenAPI JSON: %v", err)
	}
	if spec["openapi"] != "3.1.0" {
		t.Fatalf("unexpected OpenAPI version: %v", spec["openapi"])
	}
}

func TestRateLimitHeaders(t *testing.T) {
	srv := newTestServer("")
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.RemoteAddr = "192.0.2.10:12345"
	res := httptest.NewRecorder()

	srv.routes().ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, res.Code)
	}
	if got := headerValue(res, "X-RateLimit-Limit"); got != "60" {
		t.Fatalf("expected rate limit 60, got %q", got)
	}
	if got := headerValue(res, "X-RateLimit-Remaining"); got != "59" {
		t.Fatalf("expected remaining 59, got %q", got)
	}
	if got := headerValue(res, "X-RateLimit-Reset"); got == "" {
		t.Fatal("expected rate limit reset header")
	}
}

func TestRateLimitExceeded(t *testing.T) {
	srv := newTestServer("")

	var res *httptest.ResponseRecorder
	for i := 0; i < defaultRateLimitPerMinute+1; i++ {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		req.RemoteAddr = "192.0.2.20:12345"
		res = httptest.NewRecorder()
		srv.routes().ServeHTTP(res, req)
	}

	if res.Code != http.StatusTooManyRequests {
		t.Fatalf("expected status %d, got %d", http.StatusTooManyRequests, res.Code)
	}
	if got := headerValue(res, "X-RateLimit-Remaining"); got != "0" {
		t.Fatalf("expected remaining 0, got %q", got)
	}
	if got := res.Header().Get("Retry-After"); got == "" {
		t.Fatal("expected Retry-After header")
	}
	assertJSONError(t, res.Body.String(), "rate limit exceeded")
}

func TestHandleCheckRejectsInvalidEmail(t *testing.T) {
	srv := newTestServer("")
	req := httptest.NewRequest(http.MethodPost, "/v1/check", strings.NewReader(`{"email":"not-an-email"}`))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()

	srv.routes().ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, res.Code)
	}
	assertJSONError(t, res.Body.String(), "email must be a valid address")
}

func TestHandleCheckForwardsToReacher(t *testing.T) {
	var received reacherRequest
	reacher := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0/check_email" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("decode reacher request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"input":"person@example.com","is_reachable":"unknown"}`))
	}))
	defer reacher.Close()

	srv := newTestServer(reacher.URL)
	req := httptest.NewRequest(http.MethodPost, "/v1/check", strings.NewReader(`{"email":"person@example.com"}`))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()

	srv.routes().ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, res.Code, res.Body.String())
	}
	if received.ToEmail != "person@example.com" {
		t.Fatalf("expected forwarded email, got %q", received.ToEmail)
	}
}

func newTestServer(reacherURL string) *server {
	return &server{
		cfg: config{
			addr:        ":0",
			reacherURL:  strings.TrimRight(reacherURL, "/"),
			httpTimeout: time.Second,
			maxBatch:    defaultMaxBatchSize,
			rateLimit:   defaultRateLimitPerMinute,
		},
		client:  &http.Client{Timeout: time.Second},
		log:     noopLogger(),
		limiter: newRateLimiter(defaultRateLimitPerMinute, time.Minute),
	}
}

func assertJSONError(t *testing.T, body string, want string) {
	t.Helper()
	var got errorResponse
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if got.Error != want {
		t.Fatalf("expected error %q, got %q", want, got.Error)
	}
}

func assertJSONErrorAbsent(t *testing.T, body string) {
	t.Helper()
	var got errorResponse
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Error != "" {
		t.Fatalf("unexpected error: %q", got.Error)
	}
}

func noopLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func headerValue(res *httptest.ResponseRecorder, key string) string {
	values := res.Header()[key]
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
