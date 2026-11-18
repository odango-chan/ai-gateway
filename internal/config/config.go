package config

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var (
	labelPattern  = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
	headerPattern = regexp.MustCompile("^[!#$%&'*+.^_|~0-9A-Za-z-]+$")
	envPattern    = regexp.MustCompile("\\$\\{([A-Za-z_][A-Za-z0-9_]*)\\}")
)

type Duration time.Duration

func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode {
		return fmt.Errorf("duration must be a scalar string")
	}
	parsed, err := time.ParseDuration(node.Value)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", node.Value, err)
	}
	*d = Duration(parsed)
	return nil
}

func (d Duration) Value() time.Duration {
	return time.Duration(d)
}

type Config struct {
	Server      ServerConfig              `yaml:"server"`
	Limits      LimitsConfig              `yaml:"limits"`
	Upstreams   map[string]UpstreamConfig `yaml:"upstreams"`
	AccessToken string                    `yaml:"-"`
}

type ServerConfig struct {
	Listen         string `yaml:"listen"`
	AllowRemote    bool   `yaml:"allow_remote"`
	AccessTokenEnv string `yaml:"access_token_env"`
}

type LimitsConfig struct {
	MaxRequestBytes       int64    `yaml:"max_request_bytes"`
	MaxConcurrent         int      `yaml:"max_concurrent"`
	DialTimeout           Duration `yaml:"dial_timeout"`
	ResponseHeaderTimeout Duration `yaml:"response_header_timeout"`
	IdleConnTimeout       Duration `yaml:"idle_conn_timeout"`
	ShutdownTimeout       Duration `yaml:"shutdown_timeout"`
}

type UpstreamConfig struct {
	BaseURL string            `yaml:"base_url"`
	Headers map[string]string `yaml:"headers"`
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return Config{}, fmt.Errorf("multiple YAML documents are not supported")
		}
		return Config{}, fmt.Errorf("decode trailing configuration: %w", err)
	}

	cfg.applyDefaults()
	if err := cfg.expandHeaders(); err != nil {
		return Config{}, err
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (cfg *Config) applyDefaults() {
	if cfg.Server.Listen == "" {
		cfg.Server.Listen = "127.0.0.1:8787"
	}
	if cfg.Limits.MaxRequestBytes == 0 {
		cfg.Limits.MaxRequestBytes = 16 << 20
	}
	if cfg.Limits.MaxConcurrent == 0 {
		cfg.Limits.MaxConcurrent = 16
	}
	if cfg.Limits.DialTimeout == 0 {
		cfg.Limits.DialTimeout = Duration(10 * time.Second)
	}
	if cfg.Limits.ResponseHeaderTimeout == 0 {
		cfg.Limits.ResponseHeaderTimeout = Duration(120 * time.Second)
	}
	if cfg.Limits.IdleConnTimeout == 0 {
		cfg.Limits.IdleConnTimeout = Duration(90 * time.Second)
	}
	if cfg.Limits.ShutdownTimeout == 0 {
		cfg.Limits.ShutdownTimeout = Duration(10 * time.Second)
	}
}

func (cfg *Config) expandHeaders() error {
	for label, upstream := range cfg.Upstreams {
		expanded := make(map[string]string, len(upstream.Headers))
		for name, value := range upstream.Headers {
			result, err := expandEnvironment(value)
			if err != nil {
				return fmt.Errorf("upstream %q header %q: %w", label, name, err)
			}
			expanded[name] = result
		}
		upstream.Headers = expanded
		cfg.Upstreams[label] = upstream
	}
	return nil
}

func expandEnvironment(value string) (string, error) {
	var missing string
	result := envPattern.ReplaceAllStringFunc(value, func(match string) string {
		name := envPattern.FindStringSubmatch(match)[1]
		replacement, ok := os.LookupEnv(name)
		if !ok || strings.TrimSpace(replacement) == "" {
			missing = name
			return ""
		}
		return replacement
	})
	if missing != "" {
		return "", fmt.Errorf("environment variable %q is not set or empty", missing)
	}
	return result, nil
}

func (cfg *Config) Validate() error {
	if cfg.Server.Listen == "" {
		return fmt.Errorf("server.listen is required")
	}
	if cfg.Limits.MaxRequestBytes < 1 {
		return fmt.Errorf("limits.max_request_bytes must be greater than zero")
	}
	if cfg.Limits.MaxConcurrent < 1 {
		return fmt.Errorf("limits.max_concurrent must be greater than zero")
	}
	for name, value := range map[string]Duration{
		"dial_timeout":            cfg.Limits.DialTimeout,
		"response_header_timeout": cfg.Limits.ResponseHeaderTimeout,
		"idle_conn_timeout":       cfg.Limits.IdleConnTimeout,
		"shutdown_timeout":        cfg.Limits.ShutdownTimeout,
	} {
		if value.Value() <= 0 {
			return fmt.Errorf("limits.%s must be greater than zero", name)
		}
	}

	host, port, err := net.SplitHostPort(cfg.Server.Listen)
	if err != nil {
		return fmt.Errorf("server.listen must be host:port: %w", err)
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return fmt.Errorf("server.listen has an invalid port")
	}
	loopback := isLoopbackHost(host)
	if !loopback && !cfg.Server.AllowRemote {
		return fmt.Errorf("non-loopback listen address requires server.allow_remote: true")
	}
	if !loopback && cfg.Server.AccessTokenEnv == "" {
		return fmt.Errorf("non-loopback listen address requires server.access_token_env")
	}
	if cfg.Server.AccessTokenEnv != "" {
		token, ok := os.LookupEnv(cfg.Server.AccessTokenEnv)
		if !ok || strings.TrimSpace(token) == "" {
			return fmt.Errorf("environment variable %q must contain the shared gateway access token", cfg.Server.AccessTokenEnv)
		}
		cfg.AccessToken = token
	}

	if len(cfg.Upstreams) == 0 {
		return fmt.Errorf("at least one upstream must be configured")
	}
	for label, upstream := range cfg.Upstreams {
		if !labelPattern.MatchString(label) {
			return fmt.Errorf("invalid upstream label %q: use lowercase letters, digits, and hyphens", label)
		}
		parsed, err := url.Parse(upstream.BaseURL)
		if err != nil || parsed == nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return fmt.Errorf("upstream %q has an invalid base_url", label)
		}
		if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return fmt.Errorf("upstream %q base_url must not contain credentials, query, or fragment", label)
		}
		for name, value := range upstream.Headers {
			canonical := http.CanonicalHeaderKey(name)
			if canonical == "" || !headerPattern.MatchString(name) {
				return fmt.Errorf("upstream %q has invalid header name %q", label, name)
			}
			if isRestrictedHeader(canonical) {
				return fmt.Errorf("upstream %q may not override restricted header %q", label, canonical)
			}
			if strings.ContainsAny(value, "\r\n") {
				return fmt.Errorf("upstream %q header %q contains a newline", label, canonical)
			}
		}
	}
	return nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func isRestrictedHeader(name string) bool {
	switch http.CanonicalHeaderKey(name) {
	case "Host", "Content-Length", "Transfer-Encoding", "Connection", "Keep-Alive",
		"Proxy-Authenticate", "Proxy-Authorization", "Proxy-Connection", "Te", "Trailer",
		"Upgrade", "X-Gateway-Token":
		return true
	default:
		return false
	}
}
