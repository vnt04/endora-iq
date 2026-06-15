package review

import (
	"fmt"
	"strings"

	"github.com/apilens/apilens/internal/core"
	"github.com/apilens/apilens/internal/coverage"
)

// systemPrompt is the fixed reviewer persona sent as the system message.
const systemPrompt = "You are a senior API QA reviewer. Be specific and actionable."

// renderMarkdown builds the review.md body (SRS §11): a header, a deterministic
// summary, a deterministic "High Risk / Untested Endpoints" section, the
// collection-wide missing checks, and finally the AI "Recommendations" section.
//
// When recommendations is empty the aiNote explains why AI output is missing so
// the report still documents the gap rather than silently omitting it.
func renderMarkdown(c *core.Collection, cov coverage.Result, cl Classification, recommendations, aiNote string) string {
	var b strings.Builder

	b.WriteString("# ApiLens Review\n\n")
	b.WriteString(fmt.Sprintf("**Collection:** %s  \n", collectionName(c)))
	if c != nil && c.Source != "" {
		b.WriteString(fmt.Sprintf("**Source:** %s  \n", c.Source))
	}
	b.WriteString("\n")

	writeSummary(&b, c, cov, cl)
	writeAttention(&b, cl)
	writeMissingChecks(&b, cov)
	writeRecommendations(&b, recommendations, aiNote)

	return b.String()
}

// writeSummary renders the deterministic at-a-glance counts and risk.
func writeSummary(b *strings.Builder, c *core.Collection, cov coverage.Result, cl Classification) {
	b.WriteString("## Summary\n\n")
	b.WriteString(fmt.Sprintf("- Total endpoints: %d\n", c.EndpointCount()))
	b.WriteString(fmt.Sprintf("- Untested endpoints: %d\n", len(cl.Untested)))
	b.WriteString(fmt.Sprintf("- Weak endpoints (tested, with gaps): %d\n", len(cl.Weak)))
	b.WriteString(fmt.Sprintf("- Coverage score: %d/100\n", cov.Overall))
	b.WriteString(fmt.Sprintf("- Production risk: %s\n\n", cl.Risk))
}

// writeAttention renders the deterministic high-risk / untested section.
func writeAttention(b *strings.Builder, cl Classification) {
	b.WriteString("## High Risk / Untested Endpoints\n\n")
	attention := cl.Attention()
	if len(attention) == 0 {
		b.WriteString("_No untested or weak endpoints detected; every endpoint has adequate test coverage._\n\n")
		return
	}
	for _, er := range attention {
		b.WriteString(fmt.Sprintf("### %s — %s\n", endpointLabel(er.Endpoint), er.Risk))
		for _, w := range er.Weaknesses {
			b.WriteString(fmt.Sprintf("- %s\n", w))
		}
		b.WriteString("\n")
	}
}

// writeMissingChecks renders the collection-wide missing coverage checks, if any.
func writeMissingChecks(b *strings.Builder, cov coverage.Result) {
	if len(cov.MissingChecks) == 0 {
		return
	}
	b.WriteString("## Missing Coverage Checks\n\n")
	for _, m := range cov.MissingChecks {
		b.WriteString(fmt.Sprintf("- %s\n", m))
	}
	b.WriteString("\n")
}

// writeRecommendations renders the AI section, or a graceful unavailable note.
func writeRecommendations(b *strings.Builder, recommendations, aiNote string) {
	b.WriteString("## Recommendations\n\n")
	if rec := strings.TrimSpace(recommendations); rec != "" {
		b.WriteString(rec)
		b.WriteString("\n")
		return
	}
	note := strings.TrimSpace(aiNote)
	if note == "" {
		note = "AI recommendations are unavailable"
	}
	b.WriteString(fmt.Sprintf("_AI recommendations unavailable: %s. The deterministic findings above remain valid._\n", note))
}

// buildReviewPrompt assembles the user prompt from an ALREADY-REDACTED
// collection. It emits only non-sensitive structure (method, path, auth scheme,
// test count) plus the deterministic weakness labels — never raw bodies,
// headers, or URLs.
func buildReviewPrompt(redacted *core.Collection, cov coverage.Result, cl Classification) string {
	var b strings.Builder

	b.WriteString("Review the following API collection's test quality and produce actionable recommendations.\n\n")
	b.WriteString(fmt.Sprintf("Collection: %s\n", collectionName(redacted)))
	if redacted != nil && redacted.Source != "" {
		b.WriteString(fmt.Sprintf("Source: %s\n", redacted.Source))
	}
	b.WriteString(fmt.Sprintf("Total endpoints: %d\n", redacted.EndpointCount()))
	b.WriteString(fmt.Sprintf("Coverage score: %d/100 (production risk: %s)\n", cov.Overall, cl.Risk))
	if len(cov.MissingChecks) > 0 {
		b.WriteString(fmt.Sprintf("Collection-wide missing checks: %s\n", strings.Join(cov.MissingChecks, ", ")))
	}

	b.WriteString("\nEndpoints (method/path | auth | test-count | detected gaps):\n")
	var eps []core.Endpoint
	if redacted != nil {
		eps = redacted.Endpoints
	}
	for i, e := range eps {
		gaps := "none"
		if i < len(cl.Endpoints) && len(cl.Endpoints[i].Weaknesses) > 0 {
			gaps = strings.Join(cl.Endpoints[i].Weaknesses, ", ")
		}
		b.WriteString(fmt.Sprintf("- %s | auth: %s | tests: %d | gaps: %s\n",
			endpointLabel(e), authLabel(e), len(e.TestScripts), gaps))
	}

	b.WriteString(promptInstructions)
	return b.String()
}

// promptInstructions is the fixed task description appended to every prompt.
const promptInstructions = `
Provide your review in markdown with these subsections:
- "Weak Endpoints": for each, the exact assertions/tests to add.
- "Untested Endpoints": the minimum set of tests each one needs.
- "Missing Scenarios": cross-cutting test cases the collection lacks.
- "Improvements": concrete, prioritized changes.
- "Production Risk": a short justification of the overall risk level.
Refer to endpoints by METHOD and path. Be specific and actionable; do not invent endpoints.`

// collectionName returns a display name, falling back when none is set.
func collectionName(c *core.Collection) string {
	if c != nil {
		if n := strings.TrimSpace(c.Name); n != "" {
			return n
		}
	}
	return "(unnamed collection)"
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

// endpointLabel renders a stable, human-readable label for an endpoint,
// preferring "METHOD path" and degrading to name or id.
func endpointLabel(e core.Endpoint) string {
	method := strings.ToUpper(strings.TrimSpace(e.Method))
	path := strings.TrimSpace(e.Path)
	if path == "" {
		path = strings.TrimSpace(e.RawURL)
	}
	switch {
	case method != "" && path != "":
		return method + " " + path
	case strings.TrimSpace(e.Name) != "":
		return strings.TrimSpace(e.Name)
	case method != "":
		return method
	default:
		return e.ID
	}
}
