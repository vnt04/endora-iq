package monitor

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	yaml "go.yaml.in/yaml/v3"

	"github.com/apilens/apilens/internal/core/config"
)

// --- pure mapper ---------------------------------------------------------

func TestModeToCron(t *testing.T) {
	tests := []struct {
		name      string
		mode      string
		wantCron  string
		wantKnown bool
	}{
		{"15min", "15min", cron15Min, true},
		{"30min", "30min", cron30Min, true},
		{"hourly", "hourly", cronHourly, true},
		{"daily", "daily", cronDaily, true},
		{"case insensitive", "HOURLY", cronHourly, true},
		{"whitespace tolerant", "  daily  ", cronDaily, true},
		{"unknown defaults hourly", "weekly", cronHourly, false},
		{"empty defaults hourly", "", cronHourly, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotCron, gotKnown := modeToCron(tc.mode)
			if gotCron != tc.wantCron {
				t.Errorf("cron = %q, want %q", gotCron, tc.wantCron)
			}
			if gotKnown != tc.wantKnown {
				t.Errorf("known = %v, want %v", gotKnown, tc.wantKnown)
			}
		})
	}
}

func TestNormalizeNotify(t *testing.T) {
	tests := []struct {
		name        string
		in          []string
		wantValid   []string
		wantUnknown []string
	}{
		{"empty", nil, nil, nil},
		{
			name:      "canonical order regardless of input order",
			in:        []string{"email", "slack"},
			wantValid: []string{"slack", "email"},
		},
		{
			name:      "case and whitespace + dedupe",
			in:        []string{"Slack", " slack ", "DISCORD"},
			wantValid: []string{"slack", "discord"},
		},
		{
			name:        "unknown channels reported and skipped",
			in:          []string{"slack", "carrier-pigeon", "sms"},
			wantValid:   []string{"slack"},
			wantUnknown: []string{"carrier-pigeon", "sms"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotValid, gotUnknown := normalizeNotify(tc.in)
			if !reflect.DeepEqual(gotValid, tc.wantValid) {
				t.Errorf("valid = %v, want %v", gotValid, tc.wantValid)
			}
			if !reflect.DeepEqual(gotUnknown, tc.wantUnknown) {
				t.Errorf("unknown = %v, want %v", gotUnknown, tc.wantUnknown)
			}
		})
	}
}

// --- Run end-to-end ------------------------------------------------------

func runMonitor(t *testing.T, opts Options) string {
	t.Helper()
	if opts.OutDir == "" {
		opts.OutDir = t.TempDir()
	}
	if err := Run(context.Background(), config.Default(), nil, opts); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	return opts.OutDir
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func mustExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
}

func TestRunGeneratesWorkflowAndDirs(t *testing.T) {
	out := runMonitor(t, Options{Mode: "hourly", Notify: []string{"slack", "discord"}})

	wfPath := filepath.Join(out, ".github", "workflows", "apilens-monitor.yml")
	mustExist(t, wfPath)
	wf := readFile(t, wfPath)

	wantContains := []string{
		"name: ApiLens API Monitor",
		"schedule:",
		"cron: '0 * * * *'",
		"workflow_dispatch:",
		"npm install -g newman",
		"newman run ./collection.json",
		"./environment.json",
		"actions/upload-artifact@v4",
		"Notify Slack on failure",
		"SLACK_WEBHOOK_URL: ${{ secrets.SLACK_WEBHOOK_URL }}",
		"Notify Discord on failure",
		"DISCORD_WEBHOOK_URL: ${{ secrets.DISCORD_WEBHOOK_URL }}",
		"if: failure()",
	}
	for _, want := range wantContains {
		if !strings.Contains(wf, want) {
			t.Errorf("workflow missing %q\n---\n%s", want, wf)
		}
	}

	// Channels that were not requested must not appear.
	for _, notWant := range []string{"TELEGRAM_BOT_TOKEN", "dawidd6/action-send-mail", "SMTP_HOST"} {
		if strings.Contains(wf, notWant) {
			t.Errorf("workflow unexpectedly contains %q", notWant)
		}
	}

	// reports/ and alerts/ created with .gitkeep, plus alerts/config.yaml.
	mustExist(t, filepath.Join(out, "reports", ".gitkeep"))
	mustExist(t, filepath.Join(out, "alerts", ".gitkeep"))
	mustExist(t, filepath.Join(out, "alerts", "config.yaml"))

	cfg := readFile(t, filepath.Join(out, "alerts", "config.yaml"))
	for _, want := range []string{"mode: hourly", "SLACK_WEBHOOK_URL", "DISCORD_WEBHOOK_URL"} {
		if !strings.Contains(cfg, want) {
			t.Errorf("config.yaml missing %q\n---\n%s", want, cfg)
		}
	}
}

func TestRunWorkflowIsValidYAML(t *testing.T) {
	out := runMonitor(t, Options{Mode: "15min", Notify: []string{"slack", "discord", "telegram", "email"}})

	wf := readFile(t, filepath.Join(out, ".github", "workflows", "apilens-monitor.yml"))
	// Unmarshal into `any` so the test does not depend on how the parser
	// resolves the GitHub Actions `on:` key (YAML 1.1 bool vs 1.2 string).
	var parsed any
	if err := yaml.Unmarshal([]byte(wf), &parsed); err != nil {
		t.Fatalf("workflow is not valid YAML: %v\n---\n%s", err, wf)
	}
	if !strings.Contains(wf, "jobs:") {
		t.Errorf("workflow missing top-level 'jobs:' key")
	}

	cfgPath := filepath.Join(out, "alerts", "config.yaml")
	var cfgParsed any
	if err := yaml.Unmarshal([]byte(readFile(t, cfgPath)), &cfgParsed); err != nil {
		t.Fatalf("config.yaml is not valid YAML: %v", err)
	}
}

func TestRunAllChannelsAndUnknownSkipped(t *testing.T) {
	out := runMonitor(t, Options{
		Mode:   "daily",
		Notify: []string{"telegram", "email", "carrier-pigeon"},
	})
	wf := readFile(t, filepath.Join(out, ".github", "workflows", "apilens-monitor.yml"))

	for _, want := range []string{
		"cron: '0 0 * * *'",
		"Notify Telegram on failure",
		"TELEGRAM_BOT_TOKEN: ${{ secrets.TELEGRAM_BOT_TOKEN }}",
		"TELEGRAM_CHAT_ID: ${{ secrets.TELEGRAM_CHAT_ID }}",
		"Notify Email on failure",
		"dawidd6/action-send-mail@v3",
		"${{ secrets.SMTP_HOST }}",
	} {
		if !strings.Contains(wf, want) {
			t.Errorf("workflow missing %q", want)
		}
	}
	if strings.Contains(strings.ToLower(wf), "carrier") {
		t.Errorf("unknown channel leaked into workflow")
	}

	// config.yaml should mark requested channels enabled and others disabled.
	cfg := readFile(t, filepath.Join(out, "alerts", "config.yaml"))
	if !strings.Contains(cfg, "telegram:\n    enabled: true") {
		t.Errorf("config.yaml should enable telegram\n---\n%s", cfg)
	}
	if !strings.Contains(cfg, "slack:\n    enabled: false") {
		t.Errorf("config.yaml should disable slack\n---\n%s", cfg)
	}
}

func TestRunUnknownModeDefaultsHourly(t *testing.T) {
	out := runMonitor(t, Options{Mode: "weekly"})
	wf := readFile(t, filepath.Join(out, ".github", "workflows", "apilens-monitor.yml"))
	if !strings.Contains(wf, "cron: '0 * * * *'") {
		t.Errorf("unknown mode should fall back to hourly cron\n---\n%s", wf)
	}
}

func TestRunNoNotifyChannels(t *testing.T) {
	out := runMonitor(t, Options{Mode: "hourly"})
	wf := readFile(t, filepath.Join(out, ".github", "workflows", "apilens-monitor.yml"))
	// Core steps still present; no notification steps.
	if !strings.Contains(wf, "Run Newman") {
		t.Errorf("workflow missing Newman step")
	}
	if strings.Contains(wf, "on failure") {
		t.Errorf("workflow should have no notification steps when none requested")
	}
	mustExist(t, filepath.Join(out, "alerts", "config.yaml"))
}

func TestRunDefaultOutDirIsCwd(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir) // empty OutDir must default to "." (the cwd)

	if err := Run(context.Background(), config.Default(), nil, Options{Mode: "hourly"}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	mustExist(t, filepath.Join(dir, ".github", "workflows", "apilens-monitor.yml"))
	mustExist(t, filepath.Join(dir, "reports", ".gitkeep"))
	mustExist(t, filepath.Join(dir, "alerts", ".gitkeep"))
}

func TestModeCronMappingsAppearInWorkflow(t *testing.T) {
	cases := map[string]string{
		"15min":  cron15Min,
		"30min":  cron30Min,
		"hourly": cronHourly,
		"daily":  cronDaily,
	}
	for mode, wantCron := range cases {
		t.Run(mode, func(t *testing.T) {
			out := runMonitor(t, Options{Mode: mode})
			wf := readFile(t, filepath.Join(out, ".github", "workflows", "apilens-monitor.yml"))
			if !strings.Contains(wf, "cron: '"+wantCron+"'") {
				t.Errorf("workflow for mode %q should contain cron %q", mode, wantCron)
			}
		})
	}
}
