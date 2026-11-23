package proxy

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/odango-chan/ai-gateway/internal/config"
)

type Handler struct {
	proxies         map[string]*httputil.ReverseProxy
	maxRequestBytes int64
	maxConcurrent   chan struct{}
	accessToken     string
	corsOrigins     map[string]struct{}
	logger          *slog.Logger
	transport       *http.Transport
}

func New(cfg config.Config, logger *slog.Logger) (*Handler, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}

	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: cfg.Limits.DialTimeout.Value(), KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       cfg.Limits.IdleConnTimeout.Value(),
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: cfg.Limits.ResponseHeaderTimeout.Value(),
		ExpectContinueTimeout: time.Second,
	}

	handler := &Handler{
		proxies:         make(map[string]*httputil.ReverseProxy, len(cfg.Upstreams)),
		maxRequestBytes: cfg.Limits.MaxRequestBytes,
		maxConcurrent:   make(chan struct{}, cfg.Limits.MaxConcurrent),
		accessToken:     cfg.AccessToken,
		corsOrigins:     make(map[string]struct{}, len(cfg.Server.CORSAllowedOrigins)),
		logger:          logger,
		transport:       transport,
	}

	for _, origin := range cfg.Server.CORSAllowedOrigins {
		handler.corsOrigins[origin] = struct{}{}
	}

	for label, upstream := range cfg.Upstreams {
		target, err := url.Parse(upstream.BaseURL)
		if err != nil {
			transport.CloseIdleConnections()
			return nil, fmt.Errorf("parse upstream %q: %w", label, err)
		}
		headers := make(http.Header, len(upstream.Headers))
		for name, value := range upstream.Headers {
			headers.Set(name, value)
		}
		_, replaceAuthorization := headers[http.CanonicalHeaderKey("Authorization")]
		reverseProxy := &httputil.ReverseProxy{
			Transport: transport,
			// Upstreams are never allowed to grant browser access to this Gateway.
			// Gateway's exact-origin policy is authoritative even when disabled.
			ModifyResponse: func(response *http.Response) error {
				for _, name := range []string{
					"Access-Control-Allow-Origin",
					"Access-Control-Allow-Methods",
					"Access-Control-Allow-Headers",
					"Access-Control-Allow-Credentials",
					"Access-Control-Allow-Private-Network",
					"Access-Control-Expose-Headers",
					"Access-Control-Max-Age",
				} {
					response.Header.Del(name)
				}
				return nil
			},
			Rewrite: func(request *httputil.ProxyRequest) {
				request.SetURL(target)
				if replaceAuthorization {
					request.Out.Header.Del("Authorization")
				}
				request.Out.Header.Del("Proxy-Authorization")
				request.Out.Header.Del("X-Gateway-Token")
				for name, values := range headers {
					for _, value := range values {
						request.Out.Header.Set(name, value)
					}
				}
			},
			ErrorHandler: func(w http.ResponseWriter, r *http.Request, proxyErr error) {
				var maxBytesError *http.MaxBytesError
				if errors.As(proxyErr, &maxBytesError) {
					http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
					return
				}
				if errors.Is(proxyErr, context.Canceled) {
					return
				}
				logger.Warn("upstream request failed", "upstream", label)
				http.Error(w, "upstream request failed", http.StatusBadGateway)
			},
		}
		handler.proxies[label] = reverseProxy
	}
	return handler, nil
}

func (h *Handler) CloseIdleConnections() {
	h.transport.CloseIdleConnections()
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if len(h.corsOrigins) > 0 {
		w.Header().Add("Vary", "Origin")
		if origin := r.Header.Get("Origin"); origin != "" {
			if _, ok := h.corsOrigins[origin]; !ok || len(r.Header.Values("Origin")) != 1 {
				http.Error(w, "origin not allowed", http.StatusForbidden)
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				h.serveCORSPreflight(w, r)
				return
			}
		}
	}
	if r.URL.Path == "/healthz" {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok\n")
		return
	}

	label, routedURL, ok := routeURL(r.URL, h.proxies)
	if !ok {
		http.NotFound(w, r)
		return
	}

	if h.accessToken != "" {
		provided := r.Header.Get("X-Gateway-Token")
		if provided == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(h.accessToken)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}
	if r.ContentLength > h.maxRequestBytes {
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		return
	}

	select {
	case h.maxConcurrent <- struct{}{}:
		defer func() { <-h.maxConcurrent }()
	default:
		w.Header().Set("Retry-After", "1")
		http.Error(w, "gateway is busy", http.StatusServiceUnavailable)
		return
	}

	outgoing := r.Clone(r.Context())
	outgoing.URL = routedURL
	recorder := &statusWriter{ResponseWriter: w}
	if r.Body != nil && r.Body != http.NoBody {
		outgoing.Body = http.MaxBytesReader(recorder, r.Body, h.maxRequestBytes)
	}

	started := time.Now()
	h.proxies[label].ServeHTTP(recorder, outgoing)
	if recorder.status == 0 {
		recorder.status = http.StatusOK
	}
	h.logger.Info("request completed",
		"method", r.Method,
		"upstream", label,
		"status", recorder.status,
		"duration_ms", time.Since(started).Milliseconds(),
	)
}

// serveCORSPreflight handles only known Gateway endpoints. Browser preflight
// has no access token; the subsequent actual request must still authenticate.
func (h *Handler) serveCORSPreflight(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Vary", "Access-Control-Request-Method")
	w.Header().Add("Vary", "Access-Control-Request-Headers")
	w.Header().Add("Vary", "Access-Control-Request-Private-Network")
	if r.URL.Path != "/healthz" {
		if _, _, ok := routeURL(r.URL, h.proxies); !ok {
			http.NotFound(w, r)
			return
		}
	}
	method := r.Header.Get("Access-Control-Request-Method")
	if method != http.MethodGet && method != http.MethodPost && method != http.MethodOptions {
		http.Error(w, "CORS method not allowed", http.StatusForbidden)
		return
	}
	requestedHeaders := r.Header.Get("Access-Control-Request-Headers")
	if requestedHeaders != "" {
		for _, rawHeader := range strings.Split(requestedHeaders, ",") {
			switch strings.ToLower(strings.TrimSpace(rawHeader)) {
			case "content-type", "x-opencode-session", "x-gateway-token":
			default:
				http.Error(w, "CORS header not allowed", http.StatusForbidden)
				return
			}
		}
	}
	privateNetwork := r.Header.Get("Access-Control-Request-Private-Network")
	if privateNetwork != "" && privateNetwork != "true" {
		http.Error(w, "invalid private-network preflight", http.StatusForbidden)
		return
	}
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Opencode-Session, X-Gateway-Token")
	if privateNetwork == "true" {
		w.Header().Set("Access-Control-Allow-Private-Network", "true")
	}
	w.WriteHeader(http.StatusNoContent)
}

func routeURL(in *url.URL, proxies map[string]*httputil.ReverseProxy) (string, *url.URL, bool) {
	path := in.Path
	if !strings.HasPrefix(path, "/") || path == "/" {
		return "", nil, false
	}
	trimmed := strings.TrimPrefix(path, "/")
	label, _, _ := strings.Cut(trimmed, "/")
	reverseProxy, exists := proxies[label]
	if !exists || reverseProxy == nil {
		return "", nil, false
	}

	prefix := "/" + label
	if path != prefix && !strings.HasPrefix(path, prefix+"/") {
		return "", nil, false
	}

	out := *in
	if path == prefix {
		out.Path = "/"
	} else {
		out.Path = strings.TrimPrefix(path, prefix)
	}
	escaped := in.EscapedPath()
	candidate := "/"
	if escaped != prefix {
		candidate = strings.TrimPrefix(escaped, prefix)
	}
	if decoded, err := url.PathUnescape(candidate); err == nil && decoded == out.Path && candidate != out.Path {
		out.RawPath = candidate
	} else {
		out.RawPath = ""
	}
	return label, &out, true
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	// Informational responses (except 101 Switching Protocols) do not
	// commit the final response status. ReverseProxy may forward 103 Early
	// Hints before the upstream's final status.
	if status >= 100 && status < 200 && status != http.StatusSwitchingProtocols {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(data)
}

func (w *statusWriter) Flush() {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *statusWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
