package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/mail"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const defaultMaxBatchSize = 25
const defaultRateLimitPerMinute = 60

type config struct {
	addr        string
	reacherURL  string
	httpTimeout time.Duration
	maxBatch    int
	rateLimit   int
}

type server struct {
	cfg     config
	client  *http.Client
	log     *slog.Logger
	limiter *rateLimiter
}

type checkRequest struct {
	Email string `json:"email"`
	Proxy *proxy `json:"proxy,omitempty"`
}

type batchCheckRequest struct {
	Emails []string `json:"emails"`
	Proxy  *proxy   `json:"proxy,omitempty"`
}

type proxy struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

type reacherRequest struct {
	ToEmail string `json:"to_email"`
	Proxy   *proxy `json:"proxy,omitempty"`
}

type checkResponse struct {
	Email  string          `json:"email"`
	Result json.RawMessage `json:"result"`
}

type batchCheckResponse struct {
	Results []checkResponse `json:"results"`
}

type healthResponse struct {
	Status string `json:"status"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "configuration error: %v\n", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	srv := &server{
		cfg: cfg,
		client: &http.Client{
			Timeout: cfg.httpTimeout,
		},
		log:     logger,
		limiter: newRateLimiter(cfg.rateLimit, time.Minute),
	}

	httpServer := &http.Server{
		Addr:         cfg.addr,
		Handler:      srv.routes(),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: cfg.httpTimeout + 5*time.Second,
		IdleTimeout:  60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("api listening", "addr", cfg.addr, "reacher_url", cfg.reacherURL)
		errCh <- httpServer.ListenAndServe()
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-stop:
		logger.Info("shutdown requested", "signal", sig.String())
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server failed", "error", err)
			os.Exit(1)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}
}

func loadConfig() (config, error) {
	port := env("PORT", "8080")
	timeoutSeconds, err := strconv.Atoi(env("HTTP_TIMEOUT_SECONDS", "120"))
	if err != nil || timeoutSeconds <= 0 {
		return config{}, errors.New("HTTP_TIMEOUT_SECONDS must be a positive integer")
	}

	maxBatch, err := strconv.Atoi(env("MAX_BATCH_SIZE", strconv.Itoa(defaultMaxBatchSize)))
	if err != nil || maxBatch <= 0 {
		return config{}, errors.New("MAX_BATCH_SIZE must be a positive integer")
	}

	rateLimit, err := strconv.Atoi(env("RATE_LIMIT_PER_MINUTE", strconv.Itoa(defaultRateLimitPerMinute)))
	if err != nil || rateLimit <= 0 {
		return config{}, errors.New("RATE_LIMIT_PER_MINUTE must be a positive integer")
	}

	return config{
		addr:        ":" + port,
		reacherURL:  strings.TrimRight(env("REACHER_BACKEND_URL", "http://reacher:8080"), "/"),
		httpTimeout: time.Duration(timeoutSeconds) * time.Second,
		maxBatch:    maxBatch,
		rateLimit:   rateLimit,
	}, nil
}

func env(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.handleDocs)
	mux.HandleFunc("GET /openapi.json", s.handleOpenAPI)
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("POST /v1/check", s.handleCheck)
	mux.HandleFunc("POST /v1/check/batch", s.handleBatchCheck)
	return s.withLogging(s.withRateLimit(mux))
}

func (s *server) handleDocs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte(scalarDocsHTML)); err != nil {
		s.log.Error("failed to write docs response", "error", err)
	}
}

func (s *server) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte(openAPISpec)); err != nil {
		s.log.Error("failed to write openapi response", "error", err)
	}
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, healthResponse{Status: "ok"})
}

func (s *server) handleCheck(w http.ResponseWriter, r *http.Request) {
	var req checkRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	email := strings.TrimSpace(req.Email)
	if err := validateEmail(email); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateProxy(req.Proxy); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	result, err := s.checkEmail(r.Context(), email, req.Proxy)
	if err != nil {
		s.log.Error("reacher request failed", "error", err)
		writeError(w, http.StatusBadGateway, "email verification backend unavailable")
		return
	}

	writeJSON(w, http.StatusOK, checkResponse{Email: email, Result: result})
}

func (s *server) handleBatchCheck(w http.ResponseWriter, r *http.Request) {
	var req batchCheckRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	if len(req.Emails) == 0 {
		writeError(w, http.StatusBadRequest, "emails must contain at least one address")
		return
	}
	if len(req.Emails) > s.cfg.maxBatch {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("emails cannot contain more than %d addresses", s.cfg.maxBatch))
		return
	}
	if err := validateProxy(req.Proxy); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	results := make([]checkResponse, 0, len(req.Emails))
	for _, rawEmail := range req.Emails {
		email := strings.TrimSpace(rawEmail)
		if err := validateEmail(email); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("%q: %s", rawEmail, err.Error()))
			return
		}

		result, err := s.checkEmail(r.Context(), email, req.Proxy)
		if err != nil {
			s.log.Error("reacher request failed", "email", email, "error", err)
			writeError(w, http.StatusBadGateway, "email verification backend unavailable")
			return
		}

		results = append(results, checkResponse{Email: email, Result: result})
	}

	writeJSON(w, http.StatusOK, batchCheckResponse{Results: results})
}

func (s *server) checkEmail(ctx context.Context, email string, proxy *proxy) (json.RawMessage, error) {
	payload := reacherRequest{ToEmail: email, Proxy: proxy}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.reacherURL+"/v0/check_email", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("reacher returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	if !json.Valid(respBody) {
		return nil, errors.New("reacher returned invalid JSON")
	}

	return json.RawMessage(respBody), nil
}

func validateEmail(email string) error {
	if email == "" {
		return errors.New("email is required")
	}
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email {
		return errors.New("email must be a valid address")
	}
	return nil
}

func validateProxy(p *proxy) error {
	if p == nil {
		return nil
	}
	if strings.TrimSpace(p.Host) == "" {
		return errors.New("proxy.host is required when proxy is provided")
	}
	if p.Port <= 0 || p.Port > 65535 {
		return errors.New("proxy.port must be between 1 and 65535")
	}
	return nil
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON request body")
		return false
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		writeError(w, http.StatusBadRequest, "request body must contain a single JSON object")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		slog.Error("failed to write response", "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorResponse{Error: message})
}

func (s *server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		s.log.Info("request completed",
			"method", r.Method,
			"path", r.URL.Path,
			"status", recorder.status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

func (s *server) withRateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		result := s.limiter.allow(clientIP(r), time.Now())
		setHeader(w, "X-RateLimit-Limit", strconv.Itoa(result.limit))
		setHeader(w, "X-RateLimit-Remaining", strconv.Itoa(result.remaining))
		setHeader(w, "X-RateLimit-Reset", strconv.FormatInt(result.reset.Unix(), 10))

		if !result.allowed {
			retryAfter := int(time.Until(result.reset).Seconds())
			if retryAfter < 1 {
				retryAfter = 1
			}
			w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
			writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}

		next.ServeHTTP(w, r)
	})
}

func setHeader(w http.ResponseWriter, key string, value string) {
	w.Header()[key] = []string{value}
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	if r.RemoteAddr == "" {
		return "unknown"
	}
	return r.RemoteAddr
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

type rateLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	clients map[string]rateWindow
}

type rateWindow struct {
	start time.Time
	count int
}

type rateLimitResult struct {
	allowed   bool
	limit     int
	remaining int
	reset     time.Time
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{
		limit:   limit,
		window:  window,
		clients: make(map[string]rateWindow),
	}
}

func (l *rateLimiter) allow(key string, now time.Time) rateLimitResult {
	l.mu.Lock()
	defer l.mu.Unlock()

	windowStart := now.Truncate(l.window)
	current := l.clients[key]
	if current.start.IsZero() || !current.start.Equal(windowStart) {
		current = rateWindow{start: windowStart}
	}

	reset := windowStart.Add(l.window)
	if current.count >= l.limit {
		l.clients[key] = current
		l.cleanup(windowStart)
		return rateLimitResult{
			allowed:   false,
			limit:     l.limit,
			remaining: 0,
			reset:     reset,
		}
	}

	current.count++
	l.clients[key] = current
	l.cleanup(windowStart)

	remaining := l.limit - current.count
	if remaining < 0 {
		remaining = 0
	}
	return rateLimitResult{
		allowed:   true,
		limit:     l.limit,
		remaining: remaining,
		reset:     reset,
	}
}

func (l *rateLimiter) cleanup(currentWindow time.Time) {
	for key, window := range l.clients {
		if window.start.Before(currentWindow) {
			delete(l.clients, key)
		}
	}
}
