package audit

import (
	"fmt"
	"strings"

	"github.com/apilens/apilens/internal/core"
	"github.com/apilens/apilens/internal/coverage"
	"github.com/apilens/apilens/internal/scoring"
)

// maxEndpointsInPrompt caps how many endpoints are listed in the AI prompt to
// keep the request compact for large collections; the remainder is summarized
// as a truncation note.
const maxEndpointsInPrompt = 200

// auditSystemPrompt frames the model as an API testing quality auditor and
// constrains it to insight + prioritized actions over a redacted summary.
const auditSystemPrompt = `You are ApiLens, an expert API testing quality auditor.
You receive a REDACTED summary of an API collection together with its
deterministically-computed coverage and quality scores. All secrets have already
been masked as ***REDACTED***; never request, guess, or reconstruct them.

Produce, in clean and concise Markdown:
1. A short qualitative RISK ANALYSIS (2-4 sentences) of the collection's testing
   quality, grounded in the specific gaps shown.
2. A PRIORITIZED, numbered list of concrete, actionable recommendations that
   close the most important coverage, validation, security, and error-handling
   gaps first.

Do not restate the raw score tables; add insight a reader cannot get from the
numbers alone.`

// buildAuditPrompt composes the system and user prompts from a REDACTED
// collection plus the computed coverage and quality scores. The caller must
// pass a collection already run through redact.Collection.
func buildAuditPrompt(c *core.Collection, cov coverage.Result, score scoring.Score) (system, user string) {
	var b strings.Builder

	name, source, count := "(unnamed)", "unknown", 0
	if c != nil {
		if strings.TrimSpace(c.Name) != "" {
			name = c.Name
		}
		if strings.TrimSpace(string(c.Source)) != "" {
			source = string(c.Source)
		}
		count = c.EndpointCount()
	}
	fmt.Fprintf(&b, "Collection: %s\nSource: %s\nEndpoints: %d\n\n", name, source, count)

	writePromptEndpoints(&b, c)
	writePromptScores(&b, cov, score)
	writePromptCategories(&b, cov.Categories)
	writePromptMissing(&b, cov.MissingChecks)

	return auditSystemPrompt, strings.TrimSpace(b.String())
}

// writePromptEndpoints lists endpoints with method/path/auth/test presence.
func writePromptEndpoints(b *strings.Builder, c *core.Collection) {
	b.WriteString("Endpoints:\n")
	if c == nil || len(c.Endpoints) == 0 {
		b.WriteString("- (none)\n\n")
		return
	}

	shown := len(c.Endpoints)
	if shown > maxEndpointsInPrompt {
		shown = maxEndpointsInPrompt
	}
	for i := 0; i < shown; i++ {
		e := c.Endpoints[i]
		method := e.Method
		if strings.TrimSpace(method) == "" {
			method = "?"
		}
		path := e.Path
		if strings.TrimSpace(path) == "" {
			path = e.RawURL
		}
		fmt.Fprintf(b, "- %s %s (auth: %s, tests: %d)\n", method, orDash(path), authLabel(e), len(e.TestScripts))
	}
	if remaining := len(c.Endpoints) - shown; remaining > 0 {
		fmt.Fprintf(b, "- ... and %d more endpoints (truncated)\n", remaining)
	}
	b.WriteString("\n")
}

// writePromptScores writes the deterministic headline scores.
func writePromptScores(b *strings.Builder, cov coverage.Result, score scoring.Score) {
	fmt.Fprintf(b, "Computed coverage score: %d/100\n", cov.Overall)
	fmt.Fprintf(b, "Computed quality score: %d/100 (grade %s)\n", score.Value, orDash(score.Grade))
	fmt.Fprintf(b, "Risk level: %s\n\n", orDash(cov.RiskLevel))
}

// writePromptCategories writes the per-category coverage scores.
func writePromptCategories(b *strings.Builder, cats []coverage.CategoryScore) {
	if len(cats) == 0 {
		return
	}
	b.WriteString("Coverage by category:\n")
	for _, cat := range cats {
		fmt.Fprintf(b, "- %s: %d/100\n", orDash(cat.Name), cat.Score)
	}
	b.WriteString("\n")
}

// writePromptMissing writes the flat list of missing checks (the gaps).
func writePromptMissing(b *strings.Builder, missing []string) {
	if len(missing) == 0 {
		return
	}
	b.WriteString("Missing checks:\n")
	for _, m := range missing {
		fmt.Fprintf(b, "- %s\n", m)
	}
	b.WriteString("\n")
}

// authLabel describes an endpoint's auth posture for the prompt.
func authLabel(e core.Endpoint) string {
	if !e.HasAuth {
		return "none"
	}
	if t := strings.TrimSpace(e.AuthType); t != "" {
		return t
	}
	return "yes"
}
