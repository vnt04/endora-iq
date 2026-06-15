// Package explain implements SRS Feature D: the AI failure analyzer. It parses a
// Newman report, deterministically groups related failures, asks the AI to
// predict a root cause / confidence / evidence / fixes per group, and writes a
// failure-analysis.md report.
package explain

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/apilens/apilens/internal/core"
	"github.com/apilens/apilens/internal/core/ai"
	"github.com/apilens/apilens/internal/core/config"
	"github.com/apilens/apilens/internal/core/redact"
	"github.com/apilens/apilens/internal/parser"
	"github.com/apilens/apilens/pkg/tui"
)

// Options configures a failure analysis; it mirrors the `apilens explain` flags.
type Options struct {
	ReportPath     string // positional arg: Newman JSON/HTML report
	CollectionPath string // --collection: optional collection for correlation
	OutPath        string // --out: destination file (default failure-analysis.md)
}

const (
	defaultOutPath = "failure-analysis.md"

	// systemPrompt frames every Explain call (SRS §12).
	systemPrompt = "You are an expert at diagnosing API test failures."

	// maxAIGroups caps how many failure groups are sent to the LLM, keeping
	// prompt volume bounded on large/noisy runs. Remaining groups are still
	// reported deterministically.
	maxAIGroups = 8

	// maxCorrelatedEndpoints caps how many collection endpoints are attached as
	// context to a single group prompt.
	maxCorrelatedEndpoints = 3

	noteAIUnavailable   = "_AI analysis unavailable: no AI provider/API key configured. Showing deterministic grouping only._"
	noteGroupCapReached = "_Not analyzed by AI: failure-group cap reached to keep prompt sizes reasonable. Deterministic grouping is shown above._"
)

// Run is the feature entry point. It parses the Newman report at opts.ReportPath,
// groups failures, optionally calls the AI per group, and writes the analysis to
// opts.OutPath (default failure-analysis.md). A nil client (or one without an API
// key upstream) degrades to a deterministic-only report.
func Run(ctx context.Context, cfg config.Config, client *ai.Client, opts Options) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(opts.ReportPath) == "" {
		return fmt.Errorf("explain: report path is required")
	}

	outPath := strings.TrimSpace(opts.OutPath)
	if outPath == "" {
		outPath = defaultOutPath
	}

	report, err := parser.ParseNewman(opts.ReportPath)
	if err != nil {
		if isHTMLReport(opts.ReportPath) {
			return fmt.Errorf(
				"explain: HTML reports are not machine-readable; re-run newman with the JSON reporter "+
					"(newman run ... -r json --reporter-json-export report.json) and pass the JSON file: %w",
				err)
		}
		return fmt.Errorf("explain: parse report %q: %w", opts.ReportPath, err)
	}

	// Optional correlation collection. Failure to parse it is non-fatal: the
	// analysis still proceeds without endpoint context.
	collection := loadCollection(opts.CollectionPath)

	// Clean path: no failures at all.
	if !report.HasFailures() {
		if werr := writeReport(outPath, buildAllPassedMarkdown(report)); werr != nil {
			return werr
		}
		printAllPassedSummary(report, outPath)
		return nil
	}

	groups := groupFailures(report)
	analyses := make([]string, len(groups))
	aiAvailable := client != nil && len(groups) > 0

	if aiAvailable {
		if rerr := analyzeGroups(ctx, client, collection, groups, analyses); rerr != nil {
			return rerr
		}
	} else {
		for i := range analyses {
			analyses[i] = noteAIUnavailable
		}
	}

	if werr := writeReport(outPath, buildMarkdown(report, groups, analyses, aiAvailable)); werr != nil {
		return werr
	}
	printSummary(report, groups, outPath, aiAvailable)
	return nil
}

// analyzeGroups runs the per-group AI calls inside a single spinner. It writes
// each group's response into analyses[i]. Context cancellation aborts; a
// per-group provider error degrades that one group rather than the whole run.
func analyzeGroups(ctx context.Context, client *ai.Client, collection *core.Collection, groups []failureGroup, analyses []string) error {
	label := fmt.Sprintf("Analyzing %d failure group(s) with AI...", min(len(groups), maxAIGroups))
	runErr := tui.RunWithSpinner(ctx, label, func(ctx context.Context) error {
		for i := range groups {
			if i >= maxAIGroups {
				analyses[i] = noteGroupCapReached
				continue
			}
			endpoints := correlate(collection, groups[i])
			user := buildGroupPrompt(groups[i], endpoints)
			out, aerr := client.Explain(ctx, systemPrompt, user)
			if aerr != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				analyses[i] = fmt.Sprintf("_AI analysis failed for this group (%s)._", redact.String(aerr.Error()))
				continue
			}
			if out = strings.TrimSpace(out); out == "" {
				out = "_AI returned an empty analysis for this group._"
			}
			analyses[i] = out
		}
		return nil
	})
	if runErr != nil {
		return fmt.Errorf("explain: ai analysis: %w", runErr)
	}
	return nil
}

// loadCollection parses and redacts the optional correlation collection. Any
// error is reported as a warning and yields nil (correlation is best-effort).
func loadCollection(path string) *core.Collection {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	c, err := parser.Parse(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "explain: warning: ignoring collection %q for correlation: %v\n", path, err)
		return nil
	}
	// Redact once, here, so every downstream prompt uses scrubbed endpoints.
	return redact.Collection(c)
}

// writeReport creates the parent directory if needed and writes content.
func writeReport(path, content string) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("explain: create output dir %q: %w", dir, err)
		}
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("explain: write report %q: %w", path, err)
	}
	return nil
}

// isHTMLReport reports whether path looks like a Newman HTML report.
func isHTMLReport(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".html", ".htm":
		return true
	default:
		return false
	}
}

// printSummary writes the terminal summary for a run with failures.
func printSummary(report *core.NewmanReport, groups []failureGroup, outPath string, aiAvailable bool) {
	fmt.Printf("Analyzed %d failed assertion(s) across %d group(s).\n", report.Failed, len(groups))
	if !aiAvailable {
		fmt.Println("AI analysis was skipped (no provider/API key); deterministic grouping only.")
	}
	fmt.Printf("Failure analysis written to %s\n", outPath)
}

// printAllPassedSummary writes the terminal summary when nothing failed.
func printAllPassedSummary(report *core.NewmanReport, outPath string) {
	fmt.Printf("All tests passed (%d/%d assertions). Report written to %s\n", report.Passed, report.Total, outPath)
}
