// Package review implements SRS Feature C: the AI collection review. It produces
// prioritized, actionable recommendations for a collection or spec.
//
// The pipeline is deliberately split into a deterministic stage and an AI stage.
// First, every endpoint is classified in pure Go (see classify.go) into
// untested, weak, or covered, and a production-risk estimate is rolled up from
// the coverage engine. Then a REDACTED summary of that analysis is handed to the
// AI client for free-form recommendations. The deterministic findings are always
// written, so the report is useful even with no API key.
package review

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
	"github.com/apilens/apilens/pkg/logging"
	"github.com/apilens/apilens/pkg/tui"
)

// defaultOutPath is where the review is written when Options.OutPath is empty.
const defaultOutPath = "review.md"

// Options configures a review run; it mirrors the `apilens review` flags.
type Options struct {
	InputPath string // positional arg: collection or spec path
	OutPath   string // --out: destination file (default review.md)
}

// Run is the feature entry point. It parses the input collection, classifies its
// endpoints deterministically, asks the AI client for recommendations from a
// redacted summary, writes review.md, and prints a terminal summary.
func Run(ctx context.Context, cfg config.Config, client *ai.Client, opts Options) error {
	if strings.TrimSpace(opts.InputPath) == "" {
		return fmt.Errorf("review: input path is required")
	}
	outPath := strings.TrimSpace(opts.OutPath)
	if outPath == "" {
		outPath = defaultOutPath
	}

	c, err := parser.Parse(opts.InputPath)
	if err != nil {
		return fmt.Errorf("review: parse %q: %w", opts.InputPath, err)
	}
	if c == nil {
		c = &core.Collection{}
	}

	cov := coverage.Analyze(c)
	cl := classifyCollection(c, cov)

	// AI recommendations are built from a REDACTED copy and degrade gracefully:
	// a nil client or a provider error yields a documented note, not a failure.
	recommendations, aiNote := generateRecommendations(ctx, client, c, cov, cl)

	md := renderMarkdown(c, cov, cl, recommendations, aiNote)
	if err := writeReport(outPath, md); err != nil {
		return fmt.Errorf("review: %w", err)
	}

	printSummary(c, cov, cl, outPath)
	return nil
}

// generateRecommendations runs the AI Review step. It returns the recommendation
// text on success, or an empty string plus a human-readable note explaining why
// AI output is unavailable. It never returns an error: the deterministic report
// is valuable on its own.
func generateRecommendations(ctx context.Context, client *ai.Client, c *core.Collection, cov coverage.Result, cl Classification) (recommendations, note string) {
	if client == nil {
		return "", "no AI client configured (set the provider API key to enable AI recommendations)"
	}

	// SECURITY (SRS §20): redact BEFORE building any prompt.
	redacted := redact.Collection(c)
	user := buildReviewPrompt(redacted, cov, cl)

	var result string
	runErr := tui.RunWithSpinner(ctx, "Reviewing collection with AI...", func(ctx context.Context) error {
		out, err := client.Review(ctx, systemPrompt, user)
		if err != nil {
			return err
		}
		result = out
		return nil
	})
	if runErr != nil {
		logging.L().Warn().Err(runErr).Msg("review: AI recommendations unavailable")
		return "", fmt.Sprintf("AI request failed: %v", runErr)
	}
	if strings.TrimSpace(result) == "" {
		return "", "the AI provider returned an empty response"
	}
	return result, ""
}

// writeReport creates any parent directories and writes content to path.
func writeReport(path, content string) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create output dir %q: %w", dir, err)
		}
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write report %q: %w", path, err)
	}
	return nil
}

// printSummary writes the terminal summary (counts + risk) to stdout.
func printSummary(c *core.Collection, cov coverage.Result, cl Classification, outPath string) {
	fmt.Println("ApiLens Review")
	fmt.Printf("  Endpoints:       %d\n", c.EndpointCount())
	fmt.Printf("  Untested:        %d\n", len(cl.Untested))
	fmt.Printf("  Weak (tested):   %d\n", len(cl.Weak))
	fmt.Printf("  Coverage score:  %d/100\n", cov.Overall)
	fmt.Printf("  Production risk: %s\n", cl.Risk)
	fmt.Printf("  Report:          %s\n", outPath)
}
