package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/apilens/apilens/internal/core"
)

func TestDefault(t *testing.T) {
	d := Default()
	if d.Provider != defaultProvider {
		t.Errorf("provider = %q, want %q", d.Provider, defaultProvider)
	}
	if d.Model != defaultModel {
		t.Errorf("model = %q, want %q", d.Model, defaultModel)
	}
	if d.ReportDir != defaultReportDir || d.DocsDir != defaultDocsDir || d.MonitorDir != defaultMonitorDir {
		t.Errorf("dirs = %q/%q/%q", d.ReportDir, d.DocsDir, d.MonitorDir)
	}
	if d.Temperature != defaultTemperature {
		t.Errorf("temperature = %g, want %g", d.Temperature, defaultTemperature)
	}
	if d.MaxTokens != defaultMaxTokens {
		t.Errorf("max_tokens = %d, want %d", d.MaxTokens, defaultMaxTokens)
	}
	if d.Timeout != defaultTimeout {
		t.Errorf("timeout = %s, want %s", d.Timeout, defaultTimeout)
	}
	if d.APIKey != "" || d.BaseURL != "" {
		t.Errorf("expected empty APIKey/BaseURL, got %q/%q", d.APIKey, d.BaseURL)
	}
}

func TestEnvVarName(t *testing.T) {
	tests := []struct {
		provider string
		want     string
	}{
		{"anthropic", "ANTHROPIC_API_KEY"},
		{"  Anthropic ", "ANTHROPIC_API_KEY"},
		{"openai", "OPENAI_API_KEY"},
		{"OpenAI", "OPENAI_API_KEY"},
		{"gemini", "GEMINI_API_KEY"},
		{"openrouter", "OPENROUTER_API_KEY"},
		{"unknown", ""},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			got := Config{Provider: tt.provider}.EnvVarName()
			if got != tt.want {
				t.Errorf("EnvVarName(%q) = %q, want %q", tt.provider, got, tt.want)
			}
		})
	}
}

func TestRequireAPIKey(t *testing.T) {
	t.Run("present", func(t *testing.T) {
		c := Config{Provider: "anthropic", APIKey: "sk-123"}
		if err := c.RequireAPIKey(); err != nil {
			t.Errorf("expected nil, got %v", err)
		}
	})

	t.Run("missing known provider", func(t *testing.T) {
		c := Config{Provider: "openai"}
		err := c.RequireAPIKey()
		if !errors.Is(err, core.ErrMissingAPIKey) {
			t.Fatalf("expected ErrMissingAPIKey, got %v", err)
		}
		if got := err.Error(); !contains(got, "OPENAI_API_KEY") {
			t.Errorf("expected env var name in message, got %q", got)
		}
	})

	t.Run("missing unknown provider", func(t *testing.T) {
		c := Config{Provider: "mystery"}
		err := c.RequireAPIKey()
		if !errors.Is(err, core.ErrMissingAPIKey) {
			t.Fatalf("expected ErrMissingAPIKey, got %v", err)
		}
		if got := err.Error(); !contains(got, "mystery") {
			t.Errorf("expected provider name in message, got %q", got)
		}
	})

	t.Run("whitespace-only key treated as empty", func(t *testing.T) {
		c := Config{Provider: "anthropic", APIKey: "   "}
		if err := c.RequireAPIKey(); !errors.Is(err, core.ErrMissingAPIKey) {
			t.Errorf("expected ErrMissingAPIKey for blank key, got %v", err)
		}
	})
}

func TestLoad_FromFile(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "env-key-123")
	yaml := `provider: openai
model: gpt-4o
report_dir: out/reports
docs_dir: out/docs
monitor_dir: out/monitor
temperature: 0.5
max_tokens: 1000
timeout: 30s
base_url: https://proxy.example.com
`
	path := writeYAML(t, yaml)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Provider != "openai" || cfg.Model != "gpt-4o" {
		t.Errorf("provider/model = %q/%q", cfg.Provider, cfg.Model)
	}
	if cfg.ReportDir != "out/reports" || cfg.DocsDir != "out/docs" || cfg.MonitorDir != "out/monitor" {
		t.Errorf("dirs = %q/%q/%q", cfg.ReportDir, cfg.DocsDir, cfg.MonitorDir)
	}
	if cfg.Temperature != 0.5 || cfg.MaxTokens != 1000 {
		t.Errorf("temperature/max_tokens = %g/%d", cfg.Temperature, cfg.MaxTokens)
	}
	if cfg.Timeout != 30*time.Second {
		t.Errorf("timeout = %s, want 30s", cfg.Timeout)
	}
	if cfg.BaseURL != "https://proxy.example.com" {
		t.Errorf("base_url = %q", cfg.BaseURL)
	}
	if cfg.APIKey != "env-key-123" {
		t.Errorf("APIKey = %q, want env-key-123 (resolved from env)", cfg.APIKey)
	}
}

func TestLoad_EmptyPathUsesDefaults(t *testing.T) {
	chdirTemp(t)
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load(\"\"): %v", err)
	}
	if cfg.Provider != defaultProvider {
		t.Errorf("provider = %q, want default %q", cfg.Provider, defaultProvider)
	}
	if cfg.MaxTokens != defaultMaxTokens {
		t.Errorf("max_tokens = %d, want default", cfg.MaxTokens)
	}
}

func TestLoad_EnvOverride(t *testing.T) {
	chdirTemp(t)
	t.Setenv("APILENS_PROVIDER", "gemini")
	t.Setenv("APILENS_MODEL", "gemini-2.0")
	t.Setenv("GEMINI_API_KEY", "g-key")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Provider != "gemini" {
		t.Errorf("provider = %q, want gemini (env override)", cfg.Provider)
	}
	if cfg.Model != "gemini-2.0" {
		t.Errorf("model = %q, want gemini-2.0 (env override)", cfg.Model)
	}
	if cfg.APIKey != "g-key" {
		t.Errorf("APIKey = %q, want g-key", cfg.APIKey)
	}
}

func TestLoad_ZeroTimeoutFallsBackToDefault(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "k")
	path := writeYAML(t, "provider: anthropic\nmax_tokens: 10\ntimeout: 0s\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Timeout != defaultTimeout {
		t.Errorf("timeout = %s, want default %s", cfg.Timeout, defaultTimeout)
	}
}

func TestLoad_ExplicitMissingFileIsError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.yaml")
	if _, err := Load(missing); err == nil {
		t.Fatal("expected error for explicit missing file")
	}
}

func TestLoad_ValidationErrors(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{"unknown provider", "provider: wat\nmax_tokens: 10\n", "unknown provider"},
		{"zero max_tokens", "provider: anthropic\nmax_tokens: 0\n", "max_tokens"},
		{"negative temperature", "provider: anthropic\nmax_tokens: 10\ntemperature: -1\n", "temperature"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeYAML(t, tt.yaml)
			_, err := Load(path)
			if err == nil {
				t.Fatalf("expected validation error containing %q", tt.want)
			}
			if !contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want substring %q", err.Error(), tt.want)
			}
		})
	}
}

func TestLoad_MalformedYAMLIsError(t *testing.T) {
	path := writeYAML(t, "provider: : : bad\n\t- nope\n")
	if _, err := Load(path); err == nil {
		t.Fatal("expected error for malformed yaml")
	}
}

// --- helpers -----------------------------------------------------------------

func writeYAML(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "apilens.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	return path
}

func chdirTemp(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
