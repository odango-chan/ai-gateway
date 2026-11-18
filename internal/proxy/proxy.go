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
		logger:          logger,
		transport:       transport,
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
