package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestLoadConfigDefaults(t *testing.T) {
	clearConfigEnv(t)

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.addr != ":8080" {
		t.Fatalf("expected default addr :8080, got %q", cfg.addr)
	}
	if cfg.reacherURL != "http://reacher:8080" {
		t.Fatalf("expected default reacher URL, got %q", cfg.reacherURL)
	}
	if cfg.httpTimeout != 30*time.Second {
		t.Fatalf("expected default timeout 30s, got %s", cfg.httpTimeout)
	}
	if cfg.maxBatch != defaultMaxBatchSize {
		t.Fatalf("expected default max batch %d, got %d", defaultMaxBatchSize, cfg.maxBatch)
	}
	if cfg.rateLimit != defaultRateLimitPerMinute {
		t.Fatalf("expected default rate limit %d, got %d", defaultRateLimitPerMinute, cfg.rateLimit)
	}
}

func TestLoadConfigOverrides(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("PORT", "9000")
	t.Setenv("REACHER_BACKEND_URL", "http://example.test/")
	t.Setenv("HTTP_TIMEOUT_SECONDS", "7")
	t.Setenv("MAX_BATCH_SIZE", "3")
	t.Setenv("RATE_LIMIT_PER_MINUTE", "4")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.addr != ":9000" {
		t.Fatalf("expected addr :9000, got %q", cfg.addr)
	}
	if cfg.reacherURL != "http://example.test" {
		t.Fatalf("expected trimmed reacher URL, got %q", cfg.reacherURL)
	}
	if cfg.httpTimeout != 7*time.Second {
		t.Fatalf("expected timeout 7s, got %s", cfg.httpTimeout)
	}
	if cfg.maxBatch != 3 {
		t.Fatalf("expected max batch 3, got %d", cfg.maxBatch)
	}
	if cfg.rateLimit != 4 {
		t.Fatalf("expected rate limit 4, got %d", cfg.rateLimit)
	}
}

func TestLoadConfigRejectsInvalidIntegers(t *testing.T) {
	tests := []struct {
		name string
		key  string
		val  string
		want string
	}{
		{name: "timeout not integer", key: "HTTP_TIMEOUT_SECONDS", val: "bad", want: "HTTP_TIMEOUT_SECONDS must be a positive integer"},
		{name: "timeout zero", key: "HTTP_TIMEOUT_SECONDS", val: "0", want: "HTTP_TIMEOUT_SECONDS must be a positive integer"},
		{name: "max batch negative", key: "MAX_BATCH_SIZE", val: "-1", want: "MAX_BATCH_SIZE must be a positive integer"},
		{name: "rate limit zero", key: "RATE_LIMIT_PER_MINUTE", val: "0", want: "RATE_LIMIT_PER_MINUTE must be a positive integer"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearConfigEnv(t)
			t.Setenv(tt.key, tt.val)

			_, err := loadConfig()
			if err == nil || err.Error() != tt.want {
				t.Fatalf("expected error %q, got %v", tt.want, err)
			}
		})
	}
}

func TestHandleHealth(t *testing.T) {
	srv := newTestServer("")

	res := performRequest(srv, http.MethodGet, "/healthz", "")

	assertStatus(t, res, http.StatusOK)
	assertJSONBody(t, res.Body.String(), healthResponse{Status: "ok"})
}

func TestHandleDocs(t *testing.T) {
	srv := newTestServer("")

	res := performRequest(srv, http.MethodGet, "/", "")

	assertStatus(t, res, http.StatusOK)
	if got := res.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("expected HTML content type, got %q", got)
	}
	if !strings.Contains(res.Body.String(), "@scalar/api-reference") {
		t.Fatal("expected Scalar API reference script")
	}
}

func TestHandleOpenAPI(t *testing.T) {
	srv := newTestServer("")

	res := performRequest(srv, http.MethodGet, "/openapi.json", "")

	assertStatus(t, res, http.StatusOK)
	var spec map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &spec); err != nil {
		t.Fatalf("expected valid OpenAPI JSON: %v", err)
	}
	if spec["openapi"] != "3.1.0" {
		t.Fatalf("unexpected OpenAPI version: %v", spec["openapi"])
	}
	assertOpenAPIPath(t, spec, "/healthz")
	assertOpenAPIPath(t, spec, "/v1/check")
	assertOpenAPIPath(t, spec, "/v1/check/batch")
}

func TestHandleCheckForwardsToReacher(t *testing.T) {
	var received reacherRequest
	reacher := newReacherStub(t, func(w http.ResponseWriter, r *http.Request) {
		assertReacherRequest(t, r, &received)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"input":"person@example.com","is_reachable":"unknown"}`))
	})

	srv := newTestServer(reacher.URL)
	res := performRequest(srv, http.MethodPost, "/v1/check", `{"email":"person@example.com"}`)

	assertStatus(t, res, http.StatusOK)
	if received.ToEmail != "person@example.com" {
		t.Fatalf("expected forwarded email, got %q", received.ToEmail)
	}
	var got checkResponse
	decodeResponse(t, res, &got)
	if got.Email != "person@example.com" {
		t.Fatalf("expected response email, got %q", got.Email)
	}
	if !json.Valid(got.Result) {
		t.Fatal("expected raw Reacher result to remain valid JSON")
	}
}

func TestHandleCheckForwardsProxy(t *testing.T) {
	var received reacherRequest
	reacher := newReacherStub(t, func(w http.ResponseWriter, r *http.Request) {
		assertReacherRequest(t, r, &received)
		_, _ = w.Write([]byte(`{"input":"person@example.com","is_reachable":"safe"}`))
	})

	srv := newTestServer(reacher.URL)
	body := `{"email":"person@example.com","proxy":{"host":"proxy.example.com","port":1080,"username":"user","password":"secret"}}`
	res := performRequest(srv, http.MethodPost, "/v1/check", body)

	assertStatus(t, res, http.StatusOK)
	if received.Proxy == nil {
		t.Fatal("expected proxy to be forwarded")
	}
	if received.Proxy.Host != "proxy.example.com" || received.Proxy.Port != 1080 {
		t.Fatalf("unexpected proxy: %+v", received.Proxy)
	}
	if received.Proxy.Username != "user" || received.Proxy.Password != "secret" {
		t.Fatalf("unexpected proxy credentials: %+v", received.Proxy)
	}
}

func TestHandleCheckRejectsBadRequests(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "malformed json", body: `{`, want: "invalid JSON request body"},
		{name: "unknown field", body: `{"email":"person@example.com","extra":true}`, want: "invalid JSON request body"},
		{name: "multiple json values", body: `{"email":"person@example.com"} {"email":"other@example.com"}`, want: "request body must contain a single JSON object"},
		{name: "missing email", body: `{}`, want: "email is required"},
		{name: "invalid email", body: `{"email":"not-an-email"}`, want: "email must be a valid address"},
		{name: "display name email", body: `{"email":"Person <person@example.com>"}`, want: "email must be a valid address"},
		{name: "proxy missing host", body: `{"email":"person@example.com","proxy":{"port":1080}}`, want: "proxy.host is required when proxy is provided"},
		{name: "proxy bad port", body: `{"email":"person@example.com","proxy":{"host":"proxy.example.com","port":70000}}`, want: "proxy.port must be between 1 and 65535"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newTestServer("")
			res := performRequest(srv, http.MethodPost, "/v1/check", tt.body)

			assertStatus(t, res, http.StatusBadRequest)
			assertJSONError(t, res.Body.String(), tt.want)
		})
	}
}

func TestHandleCheckBackendFailures(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{
			name: "backend error status",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "backend failed", http.StatusInternalServerError)
			},
		},
		{
			name: "backend invalid json",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`not-json`))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reacher := newReacherStub(t, tt.handler)
			srv := newTestServer(reacher.URL)

			res := performRequest(srv, http.MethodPost, "/v1/check", `{"email":"person@example.com"}`)

			assertStatus(t, res, http.StatusBadGateway)
			assertJSONError(t, res.Body.String(), "email verification backend unavailable")
		})
	}
}

func TestHandleCheckBackendTimeout(t *testing.T) {
	reacher := newReacherStub(t, func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(50 * time.Millisecond)
		_, _ = w.Write([]byte(`{"is_reachable":"unknown"}`))
	})
	srv := newTestServer(reacher.URL)
	srv.client.Timeout = 5 * time.Millisecond

	res := performRequest(srv, http.MethodPost, "/v1/check", `{"email":"person@example.com"}`)

	assertStatus(t, res, http.StatusGatewayTimeout)
	assertJSONError(t, res.Body.String(), "email verification backend timed out")
}

func TestHandleBatchCheckForwardsEmailsInOrder(t *testing.T) {
	var received []string
	reacher := newReacherStub(t, func(w http.ResponseWriter, r *http.Request) {
		var payload reacherRequest
		assertReacherRequest(t, r, &payload)
		received = append(received, payload.ToEmail)
		_, _ = w.Write([]byte(`{"is_reachable":"unknown"}`))
	})

	srv := newTestServer(reacher.URL)
	res := performRequest(srv, http.MethodPost, "/v1/check/batch", `{"emails":["first@example.com","second@example.com"]}`)

	assertStatus(t, res, http.StatusOK)
	if strings.Join(received, ",") != "first@example.com,second@example.com" {
		t.Fatalf("unexpected forwarded order: %v", received)
	}
	var got batchCheckResponse
	decodeResponse(t, res, &got)
	if len(got.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(got.Results))
	}
}

func TestHandleBatchCheckRejectsBadRequests(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "empty list", body: `{"emails":[]}`, want: "emails must contain at least one address"},
		{name: "invalid email", body: `{"emails":["valid@example.com","bad"]}`, want: `"bad": email must be a valid address`},
		{name: "proxy missing host", body: `{"emails":["person@example.com"],"proxy":{"port":1080}}`, want: "proxy.host is required when proxy is provided"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newTestServer("")
			res := performRequest(srv, http.MethodPost, "/v1/check/batch", tt.body)

			assertStatus(t, res, http.StatusBadRequest)
			assertJSONError(t, res.Body.String(), tt.want)
		})
	}
}

func TestHandleBatchCheckRejectsTooManyEmails(t *testing.T) {
	srv := newTestServer("")
	srv.cfg.maxBatch = 2

	res := performRequest(srv, http.MethodPost, "/v1/check/batch", `{"emails":["a@example.com","b@example.com","c@example.com"]}`)

	assertStatus(t, res, http.StatusBadRequest)
	assertJSONError(t, res.Body.String(), "emails cannot contain more than 2 addresses")
}

func TestRateLimitHeaders(t *testing.T) {
	srv := newTestServer("")

	res := performRequestFromIP(srv, http.MethodGet, "/healthz", "", "192.0.2.10:12345")

	assertStatus(t, res, http.StatusOK)
	assertHeader(t, res, "X-RateLimit-Limit", "60")
	assertHeader(t, res, "X-RateLimit-Remaining", "59")
	if got := headerValue(res, "X-RateLimit-Reset"); got == "" {
		t.Fatal("expected rate limit reset header")
	}
}

func TestRateLimitExceeded(t *testing.T) {
	srv := newTestServer("")

	var res *httptest.ResponseRecorder
	for i := 0; i < defaultRateLimitPerMinute+1; i++ {
		res = performRequestFromIP(srv, http.MethodGet, "/healthz", "", "192.0.2.20:12345")
	}

	assertStatus(t, res, http.StatusTooManyRequests)
	assertHeader(t, res, "X-RateLimit-Limit", "60")
	assertHeader(t, res, "X-RateLimit-Remaining", "0")
	if got := res.Header().Get("Retry-After"); got == "" {
		t.Fatal("expected Retry-After header")
	}
	assertJSONError(t, res.Body.String(), "rate limit exceeded")
}

func TestRateLimitIsPerIP(t *testing.T) {
	limiter := newRateLimiter(1, time.Minute)
	now := time.Unix(1746392401, 0)

	first := limiter.allow("192.0.2.1", now)
	second := limiter.allow("198.51.100.1", now)

	if !first.allowed || !second.allowed {
		t.Fatalf("expected separate IPs to be allowed: first=%+v second=%+v", first, second)
	}
}

func TestRateLimitResetsAtFixedWindow(t *testing.T) {
	limiter := newRateLimiter(1, time.Minute)
	firstWindow := time.Unix(1746392459, 0)
	nextWindow := time.Unix(1746392460, 0)

	first := limiter.allow("192.0.2.1", firstWindow)
	blocked := limiter.allow("192.0.2.1", firstWindow)
	reset := limiter.allow("192.0.2.1", nextWindow)

	if !first.allowed {
		t.Fatal("expected first request to be allowed")
	}
	if blocked.allowed {
		t.Fatal("expected second request in same fixed window to be blocked")
	}
	if !reset.allowed {
		t.Fatal("expected request in next fixed window to be allowed")
	}
	if reset.remaining != 0 {
		t.Fatalf("expected remaining 0 after one request with limit 1, got %d", reset.remaining)
	}
}

func TestClientIP(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		want       string
	}{
		{name: "host port", remoteAddr: "192.0.2.1:12345", want: "192.0.2.1"},
		{name: "ipv6 host port", remoteAddr: "[2001:db8::1]:12345", want: "2001:db8::1"},
		{name: "raw value fallback", remoteAddr: "not-host-port", want: "not-host-port"},
		{name: "empty", remoteAddr: "", want: "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remoteAddr

			if got := clientIP(req); got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func clearConfigEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"PORT", "REACHER_BACKEND_URL", "HTTP_TIMEOUT_SECONDS", "MAX_BATCH_SIZE", "RATE_LIMIT_PER_MINUTE"} {
		t.Setenv(key, "")
	}
}

func newReacherStub(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
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

func performRequest(srv *server, method string, path string, body string) *httptest.ResponseRecorder {
	return performRequestFromIP(srv, method, path, body, "192.0.2.100:12345")
}

func performRequestFromIP(srv *server, method string, path string, body string, remoteAddr string) *httptest.ResponseRecorder {
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.RemoteAddr = remoteAddr
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	res := httptest.NewRecorder()

	srv.routes().ServeHTTP(res, req)
	return res
}

func assertReacherRequest(t *testing.T, r *http.Request, dst *reacherRequest) {
	t.Helper()
	if r.Method != http.MethodPost {
		t.Fatalf("expected POST to reacher, got %s", r.Method)
	}
	if r.URL.Path != "/v0/check_email" {
		t.Fatalf("unexpected reacher path: %s", r.URL.Path)
	}
	if got := r.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("expected JSON content type, got %q", got)
	}
	if got := r.Header.Get("Accept"); got != "application/json" {
		t.Fatalf("expected JSON accept header, got %q", got)
	}
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		t.Fatalf("decode reacher request: %v", err)
	}
}

func assertOpenAPIPath(t *testing.T, spec map[string]any, path string) {
	t.Helper()
	paths, ok := spec["paths"].(map[string]any)
	if !ok {
		t.Fatal("OpenAPI spec missing paths object")
	}
	if _, ok := paths[path]; !ok {
		t.Fatalf("OpenAPI spec missing path %s", path)
	}
}

func assertStatus(t *testing.T, res *httptest.ResponseRecorder, want int) {
	t.Helper()
	if res.Code != want {
		t.Fatalf("expected status %d, got %d: %s", want, res.Code, res.Body.String())
	}
}

func assertHeader(t *testing.T, res *httptest.ResponseRecorder, key string, want string) {
	t.Helper()
	if got := headerValue(res, key); got != want {
		t.Fatalf("expected header %s=%q, got %q", key, want, got)
	}
}

func assertJSONBody[T comparable](t *testing.T, body string, want T) {
	t.Helper()
	var got T
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got != want {
		t.Fatalf("expected response %+v, got %+v", want, got)
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

func decodeResponse(t *testing.T, res *httptest.ResponseRecorder, dst any) {
	t.Helper()
	if err := json.Unmarshal(res.Body.Bytes(), dst); err != nil {
		t.Fatalf("decode response: %v", err)
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

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}

func BenchmarkRateLimiterAllow(b *testing.B) {
	limiter := newRateLimiter(b.N+1, time.Minute)
	now := time.Unix(1746392400, 0)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		limiter.allow(strconv.Itoa(i%100), now)
	}
}
