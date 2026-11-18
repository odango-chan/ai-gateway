package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadDefaultsAndExpandsEnvironmentHeaders(t *testing.T) {
	t.Setenv("TEST_PROVIDER_KEY", "secret-value")
	yaml := "upstreams:\n" +
		"  opencode-go:\n" +
		"    base_url: https://example.test/v1\n" +
		"    headers:\n" +
		"      Authorization: \"Bearer " + "$" + "{TEST_PROVIDER_KEY}\"\n"
	cfg, err := Load(writeConfig(t, yaml))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Listen != "127.0.0.1:8787" {
		t.Fatalf("unexpected listen address: %q", cfg.Server.Listen)
	}
	if cfg.Limits.MaxConcurrent != 16 {
		t.Fatalf("unexpected concurrency default: %d", cfg.Limits.MaxConcurrent)
	}
	if got := cfg.Upstreams["opencode-go"].Headers["Authorization"]; got != "Bearer secret-value" {
		t.Fatalf("environment expansion failed: %q", got)
	}
	if cfg.Limits.ResponseHeaderTimeout.Value() != 120*time.Second {
		t.Fatalf("unexpected response header timeout: %s", cfg.Limits.ResponseHeaderTimeout.Value())
	}
}

func TestRemoteListenRequiresSharedToken(t *testing.T) {
	t.Setenv("GATEWAY_TEST_TOKEN", "")
	path := writeConfig(t, `server:
  listen: "0.0.0.0:8787"
  allow_remote: true
  access_token_env: GATEWAY_TEST_TOKEN
upstreams:
  local:
    base_url: http://127.0.0.1:8000
`)
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "GATEWAY_TEST_TOKEN") {
		t.Fatalf("expected missing shared token error, got %v", err)
	}
	t.Setenv("GATEWAY_TEST_TOKEN", "shared-secret")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AccessToken != "shared-secret" {
		t.Fatal("shared token was not loaded")
	}
}

func TestRemoteListenCannotBeEnabledWithoutExplicitOptIn(t *testing.T) {
	path := writeConfig(t, `server:
  listen: "0.0.0.0:8787"
upstreams:
  local:
    base_url: http://127.0.0.1:8000
`)
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "allow_remote") {
		t.Fatalf("expected remote listen opt-in error, got %v", err)
	}
}

func TestRejectsUnsafeUpstreamConfiguration(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{
			name: "invalid scheme",
			yaml: `upstreams:
  bad:
    base_url: file:///etc/passwd
`,
		},
		{
			name: "query in base URL",
			yaml: `upstreams:
  bad:
    base_url: https://example.test/v1?token=secret
`,
		},
		{
			name: "restricted header",
			yaml: `upstreams:
  bad:
    base_url: https://example.test
    headers:
      Host: attacker.test
`,
		},
		{
			name: "invalid label",
			yaml: `upstreams:
  Bad_Label:
    base_url: https://example.test
`,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := Load(writeConfig(t, testCase.yaml)); err == nil {
				t.Fatal("expected configuration to be rejected")
			}
		})
	}
}

func TestRejectsUnknownConfigurationFields(t *testing.T) {
	path := writeConfig(t, `upstreams:
  local:
    base_url: http://127.0.0.1:8000
    arbitrary_target: https://attacker.test
`)
	if _, err := Load(path); err == nil {
		t.Fatal("expected unknown field to be rejected")
	}
}

func TestRejectsMissingEnvironmentVariable(t *testing.T) {
	yaml := "upstreams:\n" +
		"  hosted:\n" +
		"    base_url: https://example.test\n" +
		"    headers:\n" +
		"      Authorization: \"Bearer " + "$" + "{AI_GATEWAY_TEST_MISSING_4A9B}\"\n"
	if _, err := Load(writeConfig(t, yaml)); err == nil || !strings.Contains(err.Error(), "AI_GATEWAY_TEST_MISSING_4A9B") {
		t.Fatalf("expected missing environment variable error, got %v", err)
	}
}

func TestExampleConfigurationLoads(t *testing.T) {
	t.Setenv("OPENCODE_GO_API_KEY", "test-only-key")
	path := filepath.Join("..", "..", "configs", "config.example.yaml")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Upstreams["opencode-go"].BaseURL; got != "https://opencode.ai/inference/go" {
		t.Fatalf("unexpected OpenCode Go base URL: %q", got)
	}
	if got := cfg.Upstreams["opencode-go"].Headers["Authorization"]; got != "Bearer test-only-key" {
		t.Fatalf("OpenCode Go API key was not expanded: %q", got)
	}
}
