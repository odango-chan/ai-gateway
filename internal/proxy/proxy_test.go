package proxy

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/odango-chan/ai-gateway/internal/config"
)

func testConfig(upstreams map[string]config.UpstreamConfig) config.Config {
	return config.Config{
		Server: config.ServerConfig{Listen: "127.0.0.1:8787"},
		Limits: config.LimitsConfig{
			MaxRequestBytes:      1 << 20,
			MaxConcurrent:         8,
			DialTimeout:           config.Duration(2 * time.Second),
			ResponseHeaderTimeout: config.Duration(2 * time.Second),
			IdleConnTimeout:       config.Duration(2 * time.Second),
			ShutdownTimeout:       config.Duration(2 * time.Second),
		},
		Upstreams: upstreams,
	}
}

func newTestHandler(t *testing.T, cfg config.Config) *Handler {
	t.Helper()
	handler, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(handler.CloseIdleConnections)
	return handler
}

func TestHealthEndpoint(t *testing.T) {
	handler := newTestHandler(t, testConfig(map[string]config.UpstreamConfig{
		"local": {BaseURL: "http://127.0.0.1:8000"},
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if recorder.Code != http.StatusOK || recorder.Body.String() != "ok\n" {
		t.Fatalf("unexpected health response: %d %q", recorder.Code, recorder.Body.String())
	}
}

func TestTransparentForwardingAndConfiguredAuthorization(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method was not preserved: %s", r.Method)
		}
		if r.URL.Path != "/inference/v1/chat/completions" {
			t.Errorf("path was not joined correctly: %s", r.URL.Path)
		}
		if r.URL.RawQuery != "stream=true&n=2" {
			t.Errorf("query was not preserved: %s", r.URL.RawQuery)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer upstream-secret" {
			t.Errorf("upstream authorization was not injected: %q", got)
		}
		if got := r.Header.Get("Proxy-Authorization"); got != "" {
			t.Errorf("proxy authorization was forwarded: %q", got)
		}
		if got := r.Header.Get("X-Opencode-Session"); got != "session-123" {
			t.Errorf("session header was not preserved: %q", got)
		}
		if got := r.Header.Get("X-Gateway-Token"); got != "" {
			t.Errorf("gateway token leaked upstream: %q", got)
		}
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("X-Upstream-Response", "preserved")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(body)
	}))
	defer upstream.Close()

	cfg := testConfig(map[string]config.UpstreamConfig{
		"opencode-go": {
			BaseURL: upstream.URL + "/inference",
			Headers: map[string]string{"Authorization": "Bearer upstream-secret"},
		},
	})
	handler := newTestHandler(t, cfg)
	request := httptest.NewRequest(http.MethodPost, "/opencode-go/v1/chat/completions?stream=true&n=2", strings.NewReader(`{"model":"example","stream":true}`))
	request.Header.Set("Authorization", "Bearer caller-secret")
	request.Header.Set("Proxy-Authorization", "Basic proxy-secret")
	request.Header.Set("X-Opencode-Session", "session-123")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}
	if recorder.Body.String() != `{"model":"example","stream":true}` {
		t.Fatalf("request body was not preserved: %q", recorder.Body.String())
	}
	if recorder.Header().Get("X-Upstream-Response") != "preserved" {
		t.Fatal("upstream response header was not preserved")
	}
}

func TestSelfHostedUpstreamForwardsCallerAuthorizationWhenNoOverride(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer local-model-token" {
			t.Errorf("caller authorization was not preserved: %q", got)
		}
		_, _ = io.WriteString(w, "ok")
	}))
	defer upstream.Close()

	handler := newTestHandler(t, testConfig(map[string]config.UpstreamConfig{
		"local-qwen": {BaseURL: upstream.URL},
	}))
	request := httptest.NewRequest(http.MethodGet, "/local-qwen/v1/models", nil)
	request.Header.Set("Authorization", "Bearer local-model-token")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}
}

func TestUnknownRouteIsNotFound(t *testing.T) {
	handler := newTestHandler(t, testConfig(map[string]config.UpstreamConfig{
		"local": {BaseURL: "http://127.0.0.1:8000"},
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/unknown/v1/models", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}
}

func TestRequestBodyLimit(t *testing.T) {
	cfg := testConfig(map[string]config.UpstreamConfig{
		"local": {BaseURL: "http://127.0.0.1:8000"},
	})
	cfg.Limits.MaxRequestBytes = 4
	handler := newTestHandler(t, cfg)
	request := httptest.NewRequest(http.MethodPost, "/local/v1/chat/completions", strings.NewReader("12345"))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestRemoteModeRequiresSharedToken(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Gateway-Token"); got != "" {
			t.Errorf("gateway token leaked upstream: %q", got)
		}
		_, _ = io.WriteString(w, "ok")
	}))
	defer upstream.Close()

	cfg := testConfig(map[string]config.UpstreamConfig{"local": {BaseURL: upstream.URL}})
	cfg.Server.AllowRemote = true
	cfg.AccessToken = "shared-token"
	handler := newTestHandler(t, cfg)

	unauthorized := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/local/", nil)
	handler.ServeHTTP(unauthorized, request)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", unauthorized.Code)
	}

	authorized := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/local/", nil)
	request.Header.Set("X-Gateway-Token", "shared-token")
	handler.ServeHTTP(authorized, request)
	if authorized.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", authorized.Code)
	}
}

func TestSSEIsForwardedWithoutBuffering(t *testing.T) {
	release := make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		_, _ = io.WriteString(w, "data: first\n\n")
		flusher.Flush()
		<-release
		_, _ = io.WriteString(w, "data: second\n\n")
		flusher.Flush()
	}))
	defer upstream.Close()

	handler := newTestHandler(t, testConfig(map[string]config.UpstreamConfig{"local": {BaseURL: upstream.URL}}))
	proxyServer := httptest.NewServer(handler)
	defer proxyServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, proxyServer.URL+"/local/v1/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if line != "data: first\n" {
		t.Fatalf("first event was not streamed promptly: %q", line)
	}
	separator, err := reader.ReadString('\n')
	if err != nil || separator != "\n" {
		t.Fatalf("first event separator mismatch: %q, %v", separator, err)
	}
	close(release)
	second, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if second != "data: second\n" {
		t.Fatalf("second event mismatch: %q", second)
	}
}

func TestClientCancellationPropagatesUpstream(t *testing.T) {
	cancelled := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(cancelled)
	}))
	defer upstream.Close()

	handler := newTestHandler(t, testConfig(map[string]config.UpstreamConfig{"local": {BaseURL: upstream.URL}}))
	proxyServer := httptest.NewServer(handler)
	defer proxyServer.Close()

	ctx, cancel := context.WithCancel(context.Background())
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, proxyServer.URL+"/local/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	cancel()
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("client cancellation did not reach upstream")
	}
}

func TestConcurrencyLimit(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		_, _ = io.WriteString(w, "ok")
	}))
	defer upstream.Close()

	cfg := testConfig(map[string]config.UpstreamConfig{"local": {BaseURL: upstream.URL}})
	cfg.Limits.MaxConcurrent = 1
	handler := newTestHandler(t, cfg)
	proxyServer := httptest.NewServer(handler)
	defer proxyServer.Close()

	client := &http.Client{Timeout: 2 * time.Second}
	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		response, err := client.Get(proxyServer.URL + "/local/one")
		if err == nil {
			_ = response.Body.Close()
		}
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("first request did not reach the upstream")
	}

	response, err := client.Get(proxyServer.URL + "/local/two")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 while saturated, got %d", response.StatusCode)
	}

	close(release)
	select {
	case <-firstDone:
	case <-time.After(2 * time.Second):
		t.Fatal("first request did not finish")
	}
}
