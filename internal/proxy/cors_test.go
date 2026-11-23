package proxy

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/odango-chan/ai-gateway/internal/config"
)

const testPagesOrigin = "https://odango-chan.github.io"

func corsTestHandler(t *testing.T, upstreamURL string) *Handler {
	t.Helper()
	cfg := testConfig(map[string]config.UpstreamConfig{"opencode-go": {BaseURL: upstreamURL}})
	cfg.Server.CORSAllowedOrigins = []string{testPagesOrigin}
	return newTestHandler(t, cfg)
}

func TestExactOriginPreflightAndPrivateNetwork(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("CORS preflight must never reach upstream")
	}))
	defer upstream.Close()
	handler := corsTestHandler(t, upstream.URL)
	r := httptest.NewRequest(http.MethodOptions, "/opencode-go/openai/v1/chat/completions", nil)
	r.Header.Set("Origin", testPagesOrigin)
	r.Header.Set("Access-Control-Request-Method", "POST")
	r.Header.Set("Access-Control-Request-Headers", "content-type, x-opencode-session, x-gateway-token")
	r.Header.Set("Access-Control-Request-Private-Network", "true")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Access-Control-Allow-Origin") != testPagesOrigin ||
		w.Header().Get("Access-Control-Allow-Private-Network") != "true" ||
		!strings.Contains(w.Header().Get("Access-Control-Allow-Headers"), "X-Opencode-Session") ||
		!strings.Contains(strings.Join(w.Header().Values("Vary"), ", "), "Origin") {
		t.Fatalf("wrong CORS response: %v", w.Header())
	}
	if w.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Fatal("CORS must not grant browser credentials")
	}
}

func TestCORSIsFailClosedAndCannotBeOverriddenByUpstream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Allow-Private-Network", "true")
		_, _ = io.WriteString(w, "ok")
	}))
	defer upstream.Close()
	handler := corsTestHandler(t, upstream.URL)
	makeRequest := func(method, path, origin string, preflight map[string]string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, nil)
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		for name, value := range preflight {
			r.Header.Set(name, value)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	for _, badOrigin := range []string{"https://evil.example", testPagesOrigin + ".evil.example", "null"} {
		w := makeRequest(http.MethodGet, "/opencode-go/v1/models", badOrigin, nil)
		if w.Code != http.StatusForbidden || w.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatalf("origin %q was not rejected: %d %v", badOrigin, w.Code, w.Header())
		}
	}
	for _, test := range []struct {
		name    string
		headers map[string]string
	}{
		{"disallowed method", map[string]string{"Access-Control-Request-Method": "DELETE"}},
		{"disallowed header", map[string]string{"Access-Control-Request-Method": "POST", "Access-Control-Request-Headers": "authorization"}},
		{"invalid PNA", map[string]string{"Access-Control-Request-Method": "POST", "Access-Control-Request-Private-Network": "false"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := makeRequest(http.MethodOptions, "/opencode-go/openai/v1/chat/completions", testPagesOrigin, test.headers)
			if w.Code != http.StatusForbidden || w.Header().Get("Access-Control-Allow-Private-Network") != "" {
				t.Fatalf("preflight policy was not enforced: %d %v", w.Code, w.Header())
			}
		})
	}
	unknown := makeRequest(http.MethodOptions, "/unknown/v1/chat/completions", testPagesOrigin,
		map[string]string{"Access-Control-Request-Method": "POST"})
	if unknown.Code != http.StatusNotFound {
		t.Fatalf("unknown preflight route must be 404, got %d", unknown.Code)
	}
	allowed := makeRequest(http.MethodPost, "/opencode-go/openai/v1/chat/completions", testPagesOrigin, nil)
	if allowed.Code != http.StatusOK ||
		allowed.Header().Get("Access-Control-Allow-Origin") != testPagesOrigin ||
		allowed.Header().Get("Access-Control-Allow-Credentials") != "" ||
		allowed.Header().Get("Access-Control-Allow-Private-Network") != "" {
		t.Fatalf("upstream overrode CORS policy: %d %v", allowed.Code, allowed.Header())
	}
	cli := makeRequest(http.MethodGet, "/opencode-go/v1/models", "", nil)
	if cli.Code != http.StatusOK || cli.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("CLI request must work without CORS grant: %d %v", cli.Code, cli.Header())
	}

	// With an empty allowlist, Origin-bearing POST is rejected (not just
	// hidden by the browser). Otherwise a cross-site simple request might
	// cause billable inference despite failing the CORS read.
	cfg := testConfig(map[string]config.UpstreamConfig{"opencode-go": {BaseURL: upstream.URL}})
	noCORS := newTestHandler(t, cfg)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/opencode-go/openai/v1/chat/completions", nil)
	r.Header.Set("Origin", testPagesOrigin)
	noCORS.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden || w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("disabled CORS should reject browser-origin requests: %d %v", w.Code, w.Header())
	}
}

func TestRemoteTokenRequiredAfterCORSPreflight(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Gateway-Token") != "" {
			t.Error("shared Gateway token must be stripped before upstream forwarding")
		}
		_, _ = io.WriteString(w, "ok")
	}))
	defer upstream.Close()
	cfg := testConfig(map[string]config.UpstreamConfig{"opencode-go": {BaseURL: upstream.URL}})
	cfg.Server.CORSAllowedOrigins = []string{testPagesOrigin}
	cfg.AccessToken = "private-shared-token"
	handler, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer handler.CloseIdleConnections()
	r := httptest.NewRequest(http.MethodOptions, "/opencode-go/v1/models", nil)
	r.Header.Set("Origin", testPagesOrigin)
	r.Header.Set("Access-Control-Request-Method", "GET")
	preflight := httptest.NewRecorder()
	handler.ServeHTTP(preflight, r)
	if preflight.Code != http.StatusNoContent {
		t.Fatalf("tokenless preflight should pass: %d", preflight.Code)
	}
	r = httptest.NewRequest(http.MethodGet, "/opencode-go/v1/models", nil)
	r.Header.Set("Origin", testPagesOrigin)
	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, r)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("expected token enforcement after preflight: %d", unauthorized.Code)
	}
	r.Header.Set("X-Gateway-Token", "private-shared-token")
	authorized := httptest.NewRecorder()
	handler.ServeHTTP(authorized, r)
	if authorized.Code != http.StatusOK || authorized.Header().Get("Access-Control-Allow-Origin") != testPagesOrigin {
		t.Fatalf("expected authenticated browser request: %d %v", authorized.Code, authorized.Header())
	}
}
