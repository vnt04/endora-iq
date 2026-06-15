// Package config defines the ApiLens configuration model and loader.
//
// Configuration is sourced, in increasing precedence, from built-in defaults,
// an apilens.yaml file, and APILENS_-prefixed environment variables. The API
// key is deliberately never read from the config file: it is resolved from a
// provider-specific environment variable (see [Config.EnvVarName]) so secrets
// never live on disk.
package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"

	"github.com/apilens/apilens/internal/core"
)

// Default values mirroring the apilens.yaml example in the SRS.
const (
	defaultProvider    = "anthropic"
	defaultModel       = "claude-sonnet-4"
	defaultReportDir   = "reports"
	defaultDocsDir     = "docs"
	defaultMonitorDir  = "monitor"
	defaultTemperature = 0.1
	defaultMaxTokens   = 4000
	defaultTimeout     = 120 * time.Second

	envPrefix      = "APILENS"
	configBaseName = "apilens"
)

// Config holds all runtime settings. Fields map to apilens.yaml keys (shown in
// the struct tags / Load mapping) except APIKey, which is resolved from the
// environment.
type Config struct {
	Provider   string
	Model      string
	ReportDir  string
	DocsDir    string
	MonitorDir string

	Temperature float64
	MaxTokens   int
	Timeout     time.Duration

	// APIKey is resolved from the provider's environment variable, never from
	// the config file. It may be empty; only AI commands require it.
	APIKey string

	// BaseURL optionally overrides the provider's default API base URL.
	BaseURL string
}

// Default returns a Config populated with the built-in defaults.
func Default() Config {
	return Config{
		Provider:    defaultProvider,
		Model:       defaultModel,
		ReportDir:   defaultReportDir,
		DocsDir:     defaultDocsDir,
		MonitorDir:  defaultMonitorDir,
		Temperature: defaultTemperature,
		MaxTokens:   defaultMaxTokens,
		Timeout:     defaultTimeout,
	}
}

// EnvVarName returns the name of the environment variable that holds the API
// key for the configured provider, or "" if the provider is unknown.
func (c Config) EnvVarName() string {
	switch strings.ToLower(strings.TrimSpace(c.Provider)) {
	case "anthropic":
		return "ANTHROPIC_API_KEY"
	case "openai":
		return "OPENAI_API_KEY"
	case "gemini":
		return "GEMINI_API_KEY"
	case "openrouter":
		return "OPENROUTER_API_KEY"
	default:
		return ""
	}
}

// RequireAPIKey returns ErrMissingAPIKey (wrapped with guidance) if no API key
// is set. Call this at the start of any command that needs to reach an LLM.
func (c Config) RequireAPIKey() error {
	if strings.TrimSpace(c.APIKey) == "" {
		name := c.EnvVarName()
		if name == "" {
			return fmt.Errorf("%w for provider %q", core.ErrMissingAPIKey, c.Provider)
		}
		return fmt.Errorf("%w: set %s", core.ErrMissingAPIKey, name)
	}
	return nil
}

// Load reads configuration from the given path (or ./apilens.yaml when path is
// empty), applies APILENS_ environment overrides, resolves the provider API key
// from the environment, validates the result, and returns it.
//
// A missing config file is not an error when path is empty (defaults are used);
// an explicitly provided path that cannot be read is an error.
func Load(path string) (Config, error) {
	v := viper.New()
	def := Default()

	v.SetDefault("provider", def.Provider)
	v.SetDefault("model", def.Model)
	v.SetDefault("report_dir", def.ReportDir)
	v.SetDefault("docs_dir", def.DocsDir)
	v.SetDefault("monitor_dir", def.MonitorDir)
	v.SetDefault("temperature", def.Temperature)
	v.SetDefault("max_tokens", def.MaxTokens)
	v.SetDefault("timeout", def.Timeout)
	v.SetDefault("base_url", "")

	explicit := strings.TrimSpace(path) != ""
	if explicit {
		v.SetConfigFile(path)
	} else {
		v.SetConfigName(configBaseName)
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
	}

	v.SetEnvPrefix(envPrefix)
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	if err := v.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		// A missing file is fine only when no explicit path was requested.
		if explicit || !errors.As(err, &notFound) {
			return Config{}, fmt.Errorf("read config: %w", err)
		}
	}

	cfg := Config{
		Provider:    v.GetString("provider"),
		Model:       v.GetString("model"),
		ReportDir:   v.GetString("report_dir"),
		DocsDir:     v.GetString("docs_dir"),
		MonitorDir:  v.GetString("monitor_dir"),
		Temperature: v.GetFloat64("temperature"),
		MaxTokens:   v.GetInt("max_tokens"),
		Timeout:     v.GetDuration("timeout"),
		BaseURL:     v.GetString("base_url"),
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = def.Timeout
	}

	// Resolve the API key from the environment only.
	if name := cfg.EnvVarName(); name != "" {
		cfg.APIKey = strings.TrimSpace(os.Getenv(name))
	}

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// validate checks invariants that must hold regardless of API-key presence.
func (c Config) validate() error {
	if c.EnvVarName() == "" {
		return fmt.Errorf("unknown provider %q (supported: anthropic, openai, gemini, openrouter)", c.Provider)
	}
	if c.MaxTokens <= 0 {
		return fmt.Errorf("max_tokens must be greater than 0, got %d", c.MaxTokens)
	}
	if c.Temperature < 0 {
		return fmt.Errorf("temperature must be non-negative, got %g", c.Temperature)
	}
	return nil
}
