package explain

import (
	"fmt"
	"strings"

	"github.com/apilens/apilens/internal/core"
	"github.com/apilens/apilens/internal/core/redact"
)

// promptInstructions is the trailing instruction block appended to every group
// prompt. It pins the response to the SRS §12 shape so each section maps cleanly
// into the report.
const promptInstructions = `Diagnose the single most likely root cause of this failure cluster. Respond in Markdown using exactly these four labelled sections:

**Root Cause:** one concise sentence naming the underlying cause.
**Confidence:** an integer percentage from 0 to 100 (for example "84%") reflecting your certainty.
**Evidence:** 1-4 bullet points citing the specific data above that supports the root cause.
**Suggested Actions:** 1-4 bullet points with concrete, actionable fixes (for example: update assertions, fix schema validation, refresh auth).

Base every claim only on the data provided. Do not invent endpoints or values.`

// buildGroupPrompt renders the user prompt for a single failure group. All
// free-form, potentially secret-bearing text (URLs, messages, assertion names)
// is passed through redact.String first; endpoints supplied here are expected to
// already be redacted (see correlate / redact.Collection).
func buildGroupPrompt(g failureGroup, endpoints []core.Endpoint) string {
	var b strings.Builder

	b.WriteString("A cluster of related API test failures was detected during a Newman test run.\n\n")
	// ErrorName is a JS exception class name in practice, but a custom test script
	// could embed data in it — redact for parity with the other prompt fields.
	b.WriteString(fmt.Sprintf("Error type: %s\n", redact.String(errorNameOrUnknown(g.ErrorName))))
	b.WriteString(fmt.Sprintf("Number of failing assertions in this cluster: %d\n\n", g.Count))

	if len(g.Endpoints) > 0 {
		b.WriteString("Affected endpoints:\n")
		for _, e := range g.Endpoints {
			b.WriteString(fmt.Sprintf("- %s\n", redact.String(e)))
		}
		b.WriteString("\n")
	}

	if len(g.Assertions) > 0 {
		b.WriteString("Failed assertions:\n")
		for _, a := range g.Assertions {
			b.WriteString(fmt.Sprintf("- %s\n", redact.String(a)))
		}
		b.WriteString("\n")
	}

	if msg := strings.TrimSpace(g.SampleMessage); msg != "" {
		b.WriteString("Representative error message:\n")
		b.WriteString(redact.String(msg))
		b.WriteString("\n\n")
	}

	if len(endpoints) > 0 {
		b.WriteString("Relevant endpoint definitions from the collection (already redacted):\n")
		for _, ep := range endpoints {
			b.WriteString(formatEndpointContext(ep))
		}
		b.WriteString("\n")
	}

	b.WriteString(promptInstructions)
	return b.String()
}

// formatEndpointContext renders a compact, already-redacted endpoint summary for
// the prompt's correlation context.
func formatEndpointContext(ep core.Endpoint) string {
	var b strings.Builder
	label := strings.TrimSpace(ep.Method + " " + ep.Path)
	if label == "" {
		label = strings.TrimSpace(ep.Name)
	}
	b.WriteString(fmt.Sprintf("- %s", label))
	if name := strings.TrimSpace(ep.Name); name != "" && name != label {
		b.WriteString(fmt.Sprintf(" (%s)", name))
	}
	b.WriteString("\n")
	if ep.HasAuth {
		auth := ep.AuthType
		if auth == "" {
			auth = "yes"
		}
		b.WriteString(fmt.Sprintf("  - auth: %s\n", auth))
	}
	if d := strings.TrimSpace(ep.Description); d != "" {
		b.WriteString(fmt.Sprintf("  - description: %s\n", truncate(oneLine(d), 200)))
	}
	if body := strings.TrimSpace(ep.RequestBody); body != "" {
		b.WriteString(fmt.Sprintf("  - request body: %s\n", truncate(oneLine(body), 300)))
	}
	return b.String()
}

// correlate finds up to maxCorrelatedEndpoints collection endpoints that look
// related to the group's affected endpoints. The collection is expected to be
// pre-redacted; matching is best-effort by name or path substring. A nil
// collection yields nil.
func correlate(collection *core.Collection, g failureGroup) []core.Endpoint {
	if collection == nil {
		return nil
	}
	out := make([]core.Endpoint, 0, maxCorrelatedEndpoints)
	seen := make(map[string]bool)
	for _, ep := range collection.Endpoints {
		if !endpointMatchesGroup(ep, g) {
			continue
		}
		key := ep.ID
		if key == "" {
			key = ep.Method + " " + ep.Path
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, ep)
		if len(out) >= maxCorrelatedEndpoints {
			break
		}
	}
	return out
}

// endpointMatchesGroup reports whether ep's name or path appears in any of the
// group's affected-endpoint labels.
func endpointMatchesGroup(ep core.Endpoint, g failureGroup) bool {
	name := strings.ToLower(strings.TrimSpace(ep.Name))
	path := strings.ToLower(strings.TrimSpace(ep.Path))
	for _, label := range g.Endpoints {
		l := strings.ToLower(label)
		if name != "" && strings.Contains(l, name) {
			return true
		}
		if path != "" && strings.Contains(l, path) {
			return true
		}
	}
	return false
}
