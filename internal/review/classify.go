package review

import (
	"regexp"
	"sort"
	"strings"

	"github.com/apilens/apilens/internal/core"
	"github.com/apilens/apilens/internal/coverage"
)

// Risk levels, sharing the vocabulary of coverage.Result.RiskLevel.
const (
	RiskLow    = "Low"
	RiskMedium = "Medium"
	RiskHigh   = "High"
)

// Weakness reasons, phrased to mirror the SRS §11 example output.
const (
	reasonUntested     = "No tests written"
	reasonNoNegative   = "No negative tests"
	reasonNoAuthTest   = "No authorization tests"
	reasonNoValidation = "No validation tests"
)

// Assertion-content heuristics. The coverage engine scores the collection as a
// whole; here we look at each endpoint's own test scripts to decide which kinds
// of checks it personally exercises.
var (
	// reNegative: any 4xx/5xx status assertion or a negative-path keyword.
	reNegative = regexp.MustCompile(`(?i)\b[45]\d\d\b|\b(invalid|error|fail|failure|reject|negative|unauthorized|forbidden|not\s*found|bad\s*request|empty|missing|malformed)\b`)
	// reAuthCheck: an auth-failure status (401/403) or auth keyword.
	reAuthCheck = regexp.MustCompile(`(?i)\b(401|403)\b|\b(unauthori[sz]ed|forbidden|auth)\b`)
	// reValidation: a validation-failure status (400/422) or validation keyword.
	reValidation = regexp.MustCompile(`(?i)\b(400|422)\b|\b(valid|validation|schema|required|missing|malformed)\b`)
)

// EndpointReview is the deterministic test-posture classification of a single
// endpoint. Everything here is computed in pure Go (no AI), so the rules are
// fully unit-testable without an API key or network access.
type EndpointReview struct {
	Endpoint   core.Endpoint
	TestCount  int      // len(Endpoint.TestScripts)
	Tested     bool     // TestCount > 0
	Weaknesses []string // ordered, human-readable gaps; empty when adequately covered
	Risk       string   // RiskLow | RiskMedium | RiskHigh
}

// IsUntested reports whether the endpoint carries no test scripts at all.
func (r EndpointReview) IsUntested() bool { return !r.Tested }

// IsWeak reports whether the endpoint has tests but is missing one or more
// expected check categories (negative / authorization / validation).
func (r EndpointReview) IsWeak() bool { return r.Tested && len(r.Weaknesses) > 0 }

// NeedsAttention reports whether the endpoint is untested or weak.
func (r EndpointReview) NeedsAttention() bool { return r.IsUntested() || r.IsWeak() }

// classifyEndpoint computes the EndpointReview for one endpoint.
//
// Classification rules:
//   - UNTESTED  : len(TestScripts) == 0. Sole weakness is "No tests written".
//   - WEAK      : has tests, but at least one expected category is absent —
//     negative tests (always expected), authorization tests (expected when the
//     endpoint declares auth), and validation tests (expected when it accepts
//     input: a request body, query params, or a mutating verb).
//   - COVERED   : has tests and no missing expected category (no weaknesses).
func classifyEndpoint(e core.Endpoint) EndpointReview {
	r := EndpointReview{
		Endpoint:  e,
		TestCount: len(e.TestScripts),
		Tested:    len(e.TestScripts) > 0,
	}

	if !r.Tested {
		r.Weaknesses = []string{reasonUntested}
		r.Risk = untestedRisk(e)
		return r
	}

	scripts := strings.Join(e.TestScripts, "\n")
	if !reNegative.MatchString(scripts) {
		r.Weaknesses = append(r.Weaknesses, reasonNoNegative)
	}
	if expectsAuthTest(e) && !reAuthCheck.MatchString(scripts) {
		r.Weaknesses = append(r.Weaknesses, reasonNoAuthTest)
	}
	if expectsValidationTest(e) && !reValidation.MatchString(scripts) {
		r.Weaknesses = append(r.Weaknesses, reasonNoValidation)
	}
	r.Risk = testedRisk(e, r.Weaknesses)
	return r
}

// isMutating reports whether the HTTP verb changes server state.
func isMutating(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case "POST", "PUT", "PATCH", "DELETE":
		return true
	default:
		return false
	}
}

// acceptsInput reports whether an endpoint takes caller-supplied data worth
// validating: a request body, any query params, or a mutating verb.
func acceptsInput(e core.Endpoint) bool {
	return strings.TrimSpace(e.RequestBody) != "" || len(e.QueryParams) > 0 || isMutating(e.Method)
}

// expectsAuthTest reports whether the endpoint should have an auth-failure test.
func expectsAuthTest(e core.Endpoint) bool { return e.HasAuth }

// expectsValidationTest reports whether the endpoint should have a validation
// test (it accepts input that could be malformed).
func expectsValidationTest(e core.Endpoint) bool { return acceptsInput(e) }

// untestedRisk grades an endpoint that has no tests at all.
func untestedRisk(e core.Endpoint) string {
	if isMutating(e.Method) || e.HasAuth {
		return RiskHigh
	}
	return RiskMedium
}

// testedRisk grades an endpoint that has tests but possible gaps.
func testedRisk(e core.Endpoint, weaknesses []string) string {
	if len(weaknesses) == 0 {
		return RiskLow
	}
	if isMutating(e.Method) || e.HasAuth {
		return RiskHigh
	}
	return RiskMedium
}

// Classification is the deterministic review of an entire collection.
type Classification struct {
	Endpoints []EndpointReview // one per collection endpoint, in source order
	Untested  []EndpointReview // IsUntested(), in source order
	Weak      []EndpointReview // IsWeak(), in source order
	Risk      string           // overall production-risk estimate
}

// classifyCollection classifies every endpoint and rolls the results up into an
// overall production-risk estimate. The coverage Result supplies the headline
// risk; when it is absent (e.g. an empty collection) a count-based fallback is
// derived so the report always carries a risk level.
func classifyCollection(c *core.Collection, cov coverage.Result) Classification {
	var cl Classification
	if c == nil {
		cl.Risk = overallRisk(cov, cl)
		return cl
	}

	cl.Endpoints = make([]EndpointReview, 0, len(c.Endpoints))
	for _, e := range c.Endpoints {
		er := classifyEndpoint(e)
		cl.Endpoints = append(cl.Endpoints, er)
		switch {
		case er.IsUntested():
			cl.Untested = append(cl.Untested, er)
		case er.IsWeak():
			cl.Weak = append(cl.Weak, er)
		}
	}
	cl.Risk = overallRisk(cov, cl)
	return cl
}

// overallRisk reuses the coverage engine's RiskLevel when present, otherwise
// derives a risk from the proportion of flagged endpoints.
func overallRisk(cov coverage.Result, cl Classification) string {
	if r := strings.TrimSpace(cov.RiskLevel); r != "" {
		return r
	}
	total := len(cl.Endpoints)
	if total == 0 {
		return RiskLow
	}
	flagged := len(cl.Untested) + len(cl.Weak)
	ratio := float64(flagged) / float64(total)
	switch {
	case ratio >= 0.5 || hasHighRiskEndpoint(cl):
		return RiskHigh
	case flagged > 0:
		return RiskMedium
	default:
		return RiskLow
	}
}

// hasHighRiskEndpoint reports whether any flagged endpoint is itself High risk.
func hasHighRiskEndpoint(cl Classification) bool {
	for _, er := range append(append([]EndpointReview{}, cl.Untested...), cl.Weak...) {
		if er.Risk == RiskHigh {
			return true
		}
	}
	return false
}

// Attention returns the combined untested+weak endpoints, ordered by descending
// risk and then by endpoint id, for the deterministic report section.
func (cl Classification) Attention() []EndpointReview {
	out := make([]EndpointReview, 0, len(cl.Untested)+len(cl.Weak))
	out = append(out, cl.Untested...)
	out = append(out, cl.Weak...)
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := riskRank(out[i].Risk), riskRank(out[j].Risk)
		if ri != rj {
			return ri > rj
		}
		return out[i].Endpoint.ID < out[j].Endpoint.ID
	})
	return out
}

// riskRank maps a risk label to a sortable rank (High highest).
func riskRank(risk string) int {
	switch risk {
	case RiskHigh:
		return 3
	case RiskMedium:
		return 2
	case RiskLow:
		return 1
	default:
		return 0
	}
}
