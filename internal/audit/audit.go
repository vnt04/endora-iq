// Package audit implements SRS Feature B: the AI collection auditor. It combines
// deterministic coverage/quality scoring with an AI narrative of gaps and risk,
// and emits audit-report.md / audit-report.json.
//
// Subscore mapping (coverage.Result -> scoring.Subscores):
//
//	Coverage      <- cov.Overall (the engine's blended coverage score)
//	Validation    <- the "Validation" coverage category score
//	Security      <- the "Security" coverage category score
//	ErrorHandling <- the "Error" coverage category score
//	Documentation <- deterministic ratio of endpoints carrying a non-empty
//	                 Description, scaled to 0-100 (computed here, not by AI)
//
// Every number in the report is computed in deterministic Go. The AI is used
// only for the qualitative narrative, and only when a client and API key are
// available; otherwise the scores are still emitted with a clear note.
package audit

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
	"github.com/apilens/apilens/internal/coverage"
	"github.com/apilens/apilens/internal/parser"
	"github.com/apilens/apilens/internal/scoring"
	"github.com/apilens/apilens/pkg/tui"
)

// Output format identifiers for Options.Format.
const (
	formatMD   = "md"
	formatJSON = "json"
	formatBoth = "both"
)

// Report artifact file names written into the output directory.
const (
	jsonFileName = "audit-report.json"
	mdFileName   = "audit-report.md"
)

// fallbackOutDir is used when neither Options.OutDir nor cfg.ReportDir is set.
const fallbackOutDir = "reports"

// unavailableNote is the AI-analysis placeholder when no narrative could be
// produced because there is no client/API key.
const unavailableNote = "AI narrative unavailable (no API key)."

// Options configures an audit run; it mirrors the `apilens audit` flags.
type Options struct {
	InputPath string // positional arg: collection.json or OpenAPI spec
	OutDir    string // --out-dir: directory for reports
	Format    string // --format: "md" | "json" | "both"
}

// Run is the feature entry point. It parses the input collection, computes
// deterministic coverage and quality scores, optionally enriches the result
// with an AI risk narrative, and writes audit-report.{md,json} honoring
// opts.Format.
func Run(ctx context.Context, cfg config.Config, client *ai.Client, opts Options) error {
	format, err := normalizeFormat(opts.Format)
	if err != nil {
		return err
	}

	outDir := strings.TrimSpace(opts.OutDir)
	if outDir == "" {
		outDir = strings.TrimSpace(cfg.ReportDir)
	}
	if outDir == "" {
		outDir = fallbackOutDir
	}

	collection, err := parser.Parse(opts.InputPath)
	if err != nil {
		return fmt.Errorf("audit: parse input %q: %w", opts.InputPath, err)
	}

	cov := coverage.Analyze(collection)
	subs := deriveSubscores(collection, cov)
	score := scoring.Compute(subs)

	aiAnalysis, aiUsed := analyze(ctx, cfg, client, collection, cov, score)

	report := buildReport(collection, cov, score, aiAnalysis, cfg, aiUsed)

	written, err := writeOutputs(outDir, format, report)
	if err != nil {
		return fmt.Errorf("audit: %w", err)
	}

	printSummary(report, written)
	return nil
}

// deriveSubscores maps the deterministic coverage Result into scoring inputs.
// See the package doc for the authoritative mapping.
func deriveSubscores(c *core.Collection, cov coverage.Result) scoring.Subscores {
	return scoring.Subscores{
		Coverage:      cov.Overall,
		Validation:    categoryScore(cov, "Validation"),
		Security:      categoryScore(cov, "Security"),
		ErrorHandling: categoryScore(cov, "Error"),
		Documentation: documentationScore(c),
	}
}

// analyze produces the AI risk narrative. It returns the narrative text and
// whether the AI was actually used. When no client or API key is available it
// skips the call gracefully and returns the unavailable note. The AI call is
// wrapped in a TUI spinner; any failure degrades to a note rather than aborting
// the audit (the scores are always emitted).
func analyze(ctx context.Context, cfg config.Config, client *ai.Client, c *core.Collection, cov coverage.Result, score scoring.Score) (string, bool) {
	if client == nil || strings.TrimSpace(cfg.APIKey) == "" {
		return unavailableNote, false
	}

	system, user := buildAuditPrompt(redact.Collection(c), cov, score)

	var out string
	runErr := tui.RunWithSpinner(ctx, "Auditing collection with AI...", func(spinCtx context.Context) error {
		res, err := client.Analyze(spinCtx, system, user)
		if err != nil {
			return err
		}
		out = res
		return nil
	})
	if runErr != nil {
		return fmt.Sprintf("AI narrative unavailable: %s", core.FormatUserError(runErr)), false
	}

	out = strings.TrimSpace(out)
	if out == "" {
		return unavailableNote, false
	}
	return out, true
}

// normalizeFormat validates and canonicalizes the requested output format,
// defaulting an empty value to "both".
func normalizeFormat(f string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(f)) {
	case "", formatBoth:
		return formatBoth, nil
	case formatMD:
		return formatMD, nil
	case formatJSON:
		return formatJSON, nil
	default:
		return "", fmt.Errorf("audit: unknown format %q (want %q, %q, or %q)", f, formatMD, formatJSON, formatBoth)
	}
}

// writeOutputs creates outDir (and parents) and writes the report artifacts
// selected by format. It returns the paths written, in write order.
func writeOutputs(outDir, format string, r Report) ([]string, error) {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, fmt.Errorf("create out dir %q: %w", outDir, err)
	}

	var written []string
	if format == formatJSON || format == formatBoth {
		p := filepath.Join(outDir, jsonFileName)
		data, err := renderJSON(r)
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			return nil, fmt.Errorf("write %q: %w", p, err)
		}
		written = append(written, p)
	}
	if format == formatMD || format == formatBoth {
		p := filepath.Join(outDir, mdFileName)
		if err := os.WriteFile(p, []byte(renderMarkdown(r)), 0o644); err != nil {
			return nil, fmt.Errorf("write %q: %w", p, err)
		}
		written = append(written, p)
	}
	return written, nil
}

// printSummary prints a concise human-readable summary to stdout.
func printSummary(r Report, written []string) {
	fmt.Printf("ApiLens Audit - %s\n", orDash(r.Collection))
	fmt.Printf("  Endpoints:      %d\n", r.EndpointCount)
	fmt.Printf("  Coverage Score: %d/100\n", r.Coverage.Overall)
	fmt.Printf("  Quality Score:  %d/100 (Grade %s)\n", r.Quality.Value, orDash(r.Quality.Grade))
	fmt.Printf("  Risk Level:     %s\n", orDash(r.RiskLevel))
	for _, p := range written {
		fmt.Printf("  Wrote:          %s\n", p)
	}
}
