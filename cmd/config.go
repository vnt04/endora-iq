package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/apilens/apilens/internal/core/config"
	"github.com/apilens/apilens/internal/core/redact"
)

// defaultConfigFile is the file written by `apilens config init`.
const defaultConfigFile = "apilens.yaml"

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage ApiLens configuration",
	Long:  "Initialize a default apilens.yaml or inspect the resolved configuration.",
}

var configInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Write a default apilens.yaml to the current directory",
	Args:  cobra.NoArgs,
	RunE: func(_ *cobra.Command, _ []string) error {
		if _, err := os.Stat(defaultConfigFile); err == nil {
			return fmt.Errorf("%s already exists; remove it first to regenerate", defaultConfigFile)
		}
		if err := os.WriteFile(defaultConfigFile, []byte(defaultConfigYAML()), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", defaultConfigFile, err)
		}
		fmt.Printf("wrote %s\n", defaultConfigFile)
		return nil
	},
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Print the resolved configuration (secrets redacted)",
	Args:  cobra.NoArgs,
	RunE: func(_ *cobra.Command, _ []string) error {
		fmt.Print(renderConfig(cfg))
		return nil
	},
}

// defaultConfigYAML renders the built-in defaults as the SRS §8 apilens.yaml.
func defaultConfigYAML() string {
	d := config.Default()
	return fmt.Sprintf(`# ApiLens configuration. The API key is read from the provider's environment
# variable (e.g. ANTHROPIC_API_KEY) and is never stored here.
provider: %s
model: %s
report_dir: %s
docs_dir: %s
monitor_dir: %s
temperature: %g
max_tokens: %d
`, d.Provider, d.Model, d.ReportDir, d.DocsDir, d.MonitorDir, d.Temperature, d.MaxTokens)
}

// renderConfig prints the resolved config with the API key masked.
func renderConfig(c config.Config) string {
	apiKey := "(not set)"
	if strings.TrimSpace(c.APIKey) != "" {
		apiKey = redact.Mask
	}
	return fmt.Sprintf(`provider: %s
model: %s
report_dir: %s
docs_dir: %s
monitor_dir: %s
temperature: %g
max_tokens: %d
timeout: %s
base_url: %s
api_key: %s
`, c.Provider, c.Model, c.ReportDir, c.DocsDir, c.MonitorDir,
		c.Temperature, c.MaxTokens, c.Timeout, c.BaseURL, apiKey)
}

func init() {
	configCmd.AddCommand(configInitCmd)
	configCmd.AddCommand(configShowCmd)
	rootCmd.AddCommand(configCmd)
}
