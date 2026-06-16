// Package cmd wires the ApiLens cobra command tree: the root command, its
// persistent flags (config/provider/model/verbose), and one subcommand per SRS
// §7 feature. Feature logic lives in the internal/* packages; the commands here
// only parse flags, build each feature's Options, and dispatch to its Run.
package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/apilens/apilens/internal/core"
	"github.com/apilens/apilens/internal/core/ai"
	"github.com/apilens/apilens/internal/core/config"
	"github.com/apilens/apilens/pkg/logging"
)

// Persistent flag values, bound in init below.
var (
	cfgPath      string
	providerFlag string
	modelFlag    string
	verboseFlag  bool
)

// cfg is the resolved configuration for the current invocation. It is populated
// by the root PersistentPreRunE before any subcommand RunE executes.
var cfg config.Config

// rootCmd is the `apilens` root command.
var rootCmd = &cobra.Command{
	Use:   "apilens",
	Short: "AI-Powered API Quality Platform for Developers",
	Long: "ApiLens is a CLI-first AI quality layer for API testing. It works on top of\n" +
		"existing tools (Postman, Newman, OpenAPI) to generate tests, audit coverage,\n" +
		"review collections, explain failures, build docs, and scaffold monitoring.",
	// We format errors ourselves in Execute; let cobra stay quiet.
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
		logging.Setup(verboseFlag)

		loaded, err := config.Load(cfgPath)
		if err != nil {
			return err
		}

		// Apply CLI overrides, then re-resolve the API key from the (possibly
		// changed) provider's environment variable. The key is never read from
		// the config file.
		if v := strings.TrimSpace(providerFlag); v != "" {
			loaded.Provider = v
		}
		if v := strings.TrimSpace(modelFlag); v != "" {
			loaded.Model = v
		}
		if name := loaded.EnvVarName(); name != "" {
			loaded.APIKey = strings.TrimSpace(os.Getenv(name))
		}

		cfg = loaded
		logging.L().Debug().
			Str("provider", cfg.Provider).
			Str("model", cfg.Model).
			Msg("configuration resolved")
		return nil
	},
}

// Execute runs the root command. It installs a signal-aware context so
// long-running AI calls can be cancelled with Ctrl+C, and prints a friendly,
// redacted error message before exiting non-zero on failure.
func Execute() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := rootCmd.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", core.FormatUserError(err))
		os.Exit(1)
	}
}

// newClient builds the AI client for commands that strictly require an LLM
// (e.g. generate). It checks for the provider API key first so the user gets
// actionable guidance instead of a transport error.
func newClient() (*ai.Client, error) {
	if err := cfg.RequireAPIKey(); err != nil {
		return nil, err
	}
	client, err := ai.New(cfg)
	if err != nil {
		return nil, err
	}
	return client, nil
}

// optionalClient builds an AI client when an API key is configured, otherwise
// it returns (nil, nil). Commands that degrade gracefully without AI (audit,
// review, explain) use this so their deterministic analysis still runs offline;
// the feature layer treats a nil client as "skip the AI narrative".
func optionalClient() (*ai.Client, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		logging.L().Warn().Msg("no API key configured; producing deterministic output only (AI narrative skipped)")
		return nil, nil
	}
	return ai.New(cfg)
}

func init() {
	pf := rootCmd.PersistentFlags()
	pf.StringVarP(&cfgPath, "config", "c", "", "path to apilens.yaml (default ./apilens.yaml)")
	pf.StringVarP(&providerFlag, "provider", "p", "", "override AI provider (anthropic|openai|gemini|openrouter)")
	pf.StringVarP(&modelFlag, "model", "m", "", "override AI model")
	pf.BoolVarP(&verboseFlag, "verbose", "v", false, "enable verbose (debug) logging")
}
