// Package monitor implements SRS Feature G: the monitoring generator. It
// scaffolds a GitHub Actions workflow plus report/alert folders so a collection
// can be run on a schedule with Newman and alert on failure. This feature does
// NOT use AI; callers pass a nil ai.Client.
package monitor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog"

	"github.com/apilens/apilens/internal/core/ai"
	"github.com/apilens/apilens/internal/core/config"
	"github.com/apilens/apilens/pkg/logging"
)

// Scaffolding constants.
const (
	defaultMode      = "hourly"
	workflowFileName = "apilens-monitor.yml"

	// Cron expressions for each supported monitoring mode.
	cron15Min  = "*/15 * * * *"
	cron30Min  = "*/30 * * * *"
	cronHourly = "0 * * * *"
	cronDaily  = "0 0 * * *"
)

// channelSpec describes one notification channel and the GitHub Actions
// repository secrets it requires. It is the single source of truth shared by
// validation, the alerts config template, and the next-steps log output.
type channelSpec struct {
	name    string
	desc    string
	secrets []string
}

// channelOrder is the canonical, deterministic ordering for emitted channels.
var channelOrder = []string{"slack", "discord", "telegram", "email"}

// knownChannels maps each supported channel to its required secrets.
var knownChannels = map[string]channelSpec{
	"slack": {
		name:    "slack",
		desc:    "Slack incoming webhook",
		secrets: []string{"SLACK_WEBHOOK_URL"},
	},
	"discord": {
		name:    "discord",
		desc:    "Discord channel webhook",
		secrets: []string{"DISCORD_WEBHOOK_URL"},
	},
	"telegram": {
		name:    "telegram",
		desc:    "Telegram bot message",
		secrets: []string{"TELEGRAM_BOT_TOKEN", "TELEGRAM_CHAT_ID"},
	},
	"email": {
		name:    "email",
		desc:    "SMTP email (dawidd6/action-send-mail)",
		secrets: []string{"SMTP_HOST", "SMTP_PORT", "SMTP_USERNAME", "SMTP_PASSWORD", "SMTP_TO", "SMTP_FROM"},
	},
}

// Options configures monitoring scaffolding; it mirrors the `apilens monitor
// init` flags.
type Options struct {
	OutDir string   // --out-dir: output directory (default ".")
	Mode   string   // --mode: "15min" | "30min" | "hourly" | "daily"
	Notify []string // --notify: "email" | "slack" | "discord" | "telegram"
}

// modeToCron maps a monitoring mode to its cron expression. Matching is
// case-insensitive and whitespace-tolerant. Unknown modes fall back to hourly
// and report known=false so the caller can warn.
func modeToCron(mode string) (cron string, known bool) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "15min":
		return cron15Min, true
	case "30min":
		return cron30Min, true
	case "hourly":
		return cronHourly, true
	case "daily":
		return cronDaily, true
	default:
		return cronHourly, false
	}
}

// normalizeNotify lower-cases, trims, validates and de-duplicates the requested
// channels, returning the recognised ones in canonical order plus any unknown
// names (already normalised) for the caller to warn about.
func normalizeNotify(in []string) (valid, unknown []string) {
	seen := make(map[string]bool, len(in))
	for _, raw := range in {
		name := strings.ToLower(strings.TrimSpace(raw))
		if name == "" {
			continue
		}
		if _, ok := knownChannels[name]; !ok {
			unknown = append(unknown, name)
			continue
		}
		seen[name] = true
	}
	for _, name := range channelOrder {
		if seen[name] {
			valid = append(valid, name)
		}
	}
	return valid, unknown
}

// Run is the feature entry point for `apilens monitor init`. It writes a valid
// GitHub Actions workflow plus the reports/ and alerts/ scaffolding under
// opts.OutDir. The ai client is unused.
//
// OutDir defaults to "." (the repository root). We intentionally do NOT use
// cfg.MonitorDir here: GitHub Actions only discovers workflows under
// "<repo-root>/.github/workflows", so the workflow must be rooted at the repo
// root, not under cfg.MonitorDir (whose default "monitor" is not a valid
// workflow location).
func Run(_ context.Context, _ config.Config, _ *ai.Client, opts Options) error {
	log := logging.L()

	outDir := strings.TrimSpace(opts.OutDir)
	if outDir == "" {
		outDir = "."
	}

	mode := strings.ToLower(strings.TrimSpace(opts.Mode))
	if mode == "" {
		mode = defaultMode
	}
	cron, known := modeToCron(mode)
	if !known {
		log.Warn().
			Str("mode", opts.Mode).
			Str("fallback", defaultMode).
			Msg("monitor: unknown mode; defaulting to hourly")
		mode = defaultMode
	}

	channels, unknown := normalizeNotify(opts.Notify)
	for _, u := range unknown {
		log.Warn().Str("channel", u).Msg("monitor: unknown notification channel; skipping")
	}

	wfPath, err := writeWorkflow(outDir, workflowData{Cron: cron, Mode: mode, Notify: channels})
	if err != nil {
		return err
	}
	log.Info().
		Str("workflow", wfPath).
		Str("mode", mode).
		Str("cron", cron).
		Msg("monitor: wrote GitHub Actions workflow")

	if err := scaffoldFolders(outDir, log); err != nil {
		return err
	}

	cfgPath, err := writeAlertsConfig(outDir, mode, cron, channels)
	if err != nil {
		return err
	}
	log.Info().
		Str("config", cfgPath).
		Strs("channels", channels).
		Msg("monitor: wrote alerts config template")

	logNextSteps(log, channels, wfPath)
	return nil
}

// writeWorkflow renders and writes the workflow to
// <outDir>/.github/workflows/apilens-monitor.yml, creating parent dirs.
func writeWorkflow(outDir string, data workflowData) (string, error) {
	wfDir := filepath.Join(outDir, ".github", "workflows")
	if err := os.MkdirAll(wfDir, 0o755); err != nil {
		return "", fmt.Errorf("monitor: create workflow dir %q: %w", wfDir, err)
	}
	content, err := renderWorkflow(data)
	if err != nil {
		return "", fmt.Errorf("monitor: render workflow: %w", err)
	}
	wfPath := filepath.Join(wfDir, workflowFileName)
	if err := os.WriteFile(wfPath, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("monitor: write workflow %q: %w", wfPath, err)
	}
	return wfPath, nil
}

// scaffoldFolders creates <outDir>/reports and <outDir>/alerts, each with a
// .gitkeep so the (otherwise empty) directories are committable.
func scaffoldFolders(outDir string, log *zerolog.Logger) error {
	for _, name := range []string{"reports", "alerts"} {
		dir := filepath.Join(outDir, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("monitor: create dir %q: %w", dir, err)
		}
		keep := filepath.Join(dir, ".gitkeep")
		if err := os.WriteFile(keep, []byte{}, 0o644); err != nil {
			return fmt.Errorf("monitor: write %q: %w", keep, err)
		}
		log.Info().Str("dir", dir).Msg("monitor: created scaffold directory")
	}
	return nil
}

// writeAlertsConfig renders and writes <outDir>/alerts/config.yaml, the
// notification config template documenting each channel's required secrets.
func writeAlertsConfig(outDir, mode, cron string, channels []string) (string, error) {
	content, err := renderAlertsConfig(mode, cron, channels)
	if err != nil {
		return "", fmt.Errorf("monitor: render alerts config: %w", err)
	}
	cfgPath := filepath.Join(outDir, "alerts", "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("monitor: write alerts config %q: %w", cfgPath, err)
	}
	return cfgPath, nil
}

// logNextSteps prints actionable guidance: which repository secrets to set for
// each enabled channel, and a reminder to customise the collection paths.
func logNextSteps(log *zerolog.Logger, channels []string, wfPath string) {
	log.Info().
		Str("workflow", wfPath).
		Msg("monitor: next step — edit ./collection.json and ./environment.json paths in the workflow's Newman step")

	if len(channels) == 0 {
		log.Info().Msg("monitor: no notification channels enabled; add one with --notify slack,discord,telegram,email")
		return
	}
	for _, name := range channels {
		spec := knownChannels[name]
		log.Info().
			Str("channel", name).
			Strs("secrets", spec.secrets).
			Msg("monitor: set these GitHub Actions repository secrets (gh secret set <NAME>)")
	}
}
