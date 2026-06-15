// Package coverage is the deterministic test-coverage engine (SRS §10). It
// statically analyzes a normalized collection and reports, per category, which
// checks are present and which are missing. It uses NO AI: every number here is
// computed in Go so the logic is unit-testable without API keys.
//
// # How it works
//
// Analyze builds a single lowercased text corpus from every endpoint's test
// scripts and request bodies, plus the set of declared/example response status
// codes and whether any endpoint declares authentication. Each coverage check
// (see [checks]) is a small, pure predicate over that corpus. A check is either
// "found" or "missing"; nothing is fuzzy or weighted at the check level, which
// keeps the engine deterministic and easy to reason about.
//
// # Categories and checks (SRS §10)
//
// The six SRS §10 categories and their fixed checklists are:
//
//	Authentication: Authentication Declared, 401 Unauthorized, 403 Forbidden,
//	                Authorization Failure, Expired Token
//	Error:          404 Not Found, 422 Unprocessable Entity, 429 Rate Limit,
//	                500 Server Error
//	Validation:     Schema Validation, Type Validation, Format Validation,
//	                Null Checks, Required Fields
//	Security:       Missing Headers, Invalid Payload, Invalid Format
//	Performance:    Response Time Assertion, Large Payload
//	Edge Case:      Boundary Values, Empty Payload, Missing Fields,
//	                Duplicate Data
//
// # Scoring
//
//	CategoryScore.Score = round(100 * found / total)  for that category.
//	Result.Overall      = round(mean(category scores)) — an UNWEIGHTED average of
//	                      the six category scores (each category counts equally,
//	                      regardless of how many checks it contains).
//	Result.RiskLevel    = "High" if Overall < 50, "Medium" if Overall < 75,
//	                      else "Low".
//
// A nil or empty collection yields an empty corpus, so every check is missing:
// Overall is 0 and RiskLevel is "High".
package coverage

import (
	"math"
	"strconv"
	"strings"

	"github.com/apilens/apilens/internal/core"
)

// CategoryScore is the coverage result for one category (Authentication, Error,
// Validation, Security, Performance, Edge Case).
type CategoryScore struct {
	Name    string   // category name
	Score   int      // 0-100
	Found   []string // checks detected
	Missing []string // checks not detected
}

// Result is the overall coverage analysis for a collection.
type Result struct {
	Overall       int             // 0-100
	Categories    []CategoryScore // per-category breakdown
	MissingChecks []string        // flat, de-duplicated list of all missing checks
	RiskLevel     string          // "Low" | "Medium" | "High"
}

// Coverage categories (SRS §10). Exported so reporting code can refer to them
// by name without re-declaring string literals.
const (
	CategoryAuthentication = "Authentication"
	CategoryError          = "Error"
	CategoryValidation     = "Validation"
	CategorySecurity       = "Security"
	CategoryPerformance    = "Performance"
	CategoryEdgeCase       = "Edge Case"
)

// categoryOrder fixes the output order of [Result.Categories] and the traversal
// order used to build [Result.MissingChecks], keeping results deterministic.
var categoryOrder = []string{
	CategoryAuthentication,
	CategoryError,
	CategoryValidation,
	CategorySecurity,
	CategoryPerformance,
	CategoryEdgeCase,
}

// Risk level labels and their Overall-score thresholds.
const (
	RiskHigh   = "High"
	RiskMedium = "Medium"
	RiskLow    = "Low"

	// riskHighBelow: Overall < 50 => High.
	riskHighBelow = 50
	// riskMediumBelow: Overall < 75 => Medium, otherwise Low.
	riskMediumBelow = 75
)

// corpus is the derived, read-only view of a collection that detectors run
// against. It is built once per Analyze call and never mutated.
type corpus struct {
	// text is the lowercased concatenation of every endpoint's test scripts and
	// request bodies. All keyword matching is substring matching over this.
	text string
	// statuses is the set of status codes declared by example/saved responses.
	statuses map[int]bool
	// hasAuth is true if any endpoint declares authentication (HasAuth).
	hasAuth bool
}

// has reports whether the corpus text contains any of the given (lowercased)
// substrings.
func (c corpus) has(subs ...string) bool {
	for _, s := range subs {
		if strings.Contains(c.text, s) {
			return true
		}
	}
	return false
}

// hasAll reports whether the corpus text contains every one of the given
// (lowercased) substrings. Used for multi-token signals such as
// "less than" + "ms".
func (c corpus) hasAll(subs ...string) bool {
	for _, s := range subs {
		if !strings.Contains(c.text, s) {
			return false
		}
	}
	return true
}

// statusCheck detects a status-code check: the code appears in the declared
// response statuses, OR the numeric code appears in the corpus text, OR any of
// the given semantic keywords appears in the text.
func (c corpus) statusCheck(code int, keywords ...string) bool {
	if c.statuses[code] {
		return true
	}
	if strings.Contains(c.text, strconv.Itoa(code)) {
		return true
	}
	return c.has(keywords...)
}

// check is one coverage signal: a human-readable name, the category it belongs
// to, and a pure predicate over the corpus. Keeping detectors in a flat table
// makes them trivial to test and extend.
type check struct {
	name     string
	category string
	match    func(c corpus) bool
}

// checks is the fixed checklist. Detectors are intentionally conservative about
// shared vocabulary so distinct checks do not fire on the same token (for
// example "Format Validation" keys on regex/pattern tokens, NOT the bare word
// "format", so it does not collide with the "Invalid Format" check).
var checks = []check{
	// --- Authentication -----------------------------------------------------
	// Authentication is declared anywhere in the collection (HasAuth) or an
	// auth scheme/header is referenced in scripts.
	{"Authentication Declared", CategoryAuthentication, func(c corpus) bool {
		return c.hasAuth || c.has("authorization", "bearer", "x-api-key", "apikey", "api key")
	}},
	// 401: status code, or "unauthorized" called out in a test.
	{"401 Unauthorized", CategoryAuthentication, func(c corpus) bool {
		return c.statusCheck(401, "unauthorized")
	}},
	// 403: status code, or "forbidden" called out in a test.
	{"403 Forbidden", CategoryAuthentication, func(c corpus) bool {
		return c.statusCheck(403, "forbidden")
	}},
	// Authorization failure: a negative auth-path scenario is described.
	{"Authorization Failure", CategoryAuthentication, func(c corpus) bool {
		return c.has("authorization failure", "auth failure", "access denied",
			"not authorized", "permission denied", "insufficient permission")
	}},
	// Expired token: token expiry / refresh scenario is exercised.
	{"Expired Token", CategoryAuthentication, func(c corpus) bool {
		return c.has("expired", "expiry", "token expir", "expired token", "expired jwt")
	}},

	// --- Error --------------------------------------------------------------
	{"404 Not Found", CategoryError, func(c corpus) bool {
		return c.statusCheck(404, "not found")
	}},
	{"422 Unprocessable Entity", CategoryError, func(c corpus) bool {
		return c.statusCheck(422, "unprocessable")
	}},
	{"429 Rate Limit", CategoryError, func(c corpus) bool {
		return c.statusCheck(429, "rate limit", "too many requests")
	}},
	{"500 Server Error", CategoryError, func(c corpus) bool {
		return c.statusCheck(500, "server error", "internal error", "internal server error")
	}},

	// --- Validation ---------------------------------------------------------
	// Schema validation: JSON-schema assertion libraries / schema matching.
	{"Schema Validation", CategoryValidation, func(c corpus) bool {
		return c.has("jsonschema", "json schema", "schema", "tv4", "ajv", "matchschema", "match schema")
	}},
	// Type validation: asserting a value's JS type.
	{"Type Validation", CategoryValidation, func(c corpus) bool {
		return c.has("typeof", "to.be.a(", "to.be.an(", ".is.a(", "instanceof", "be.a number", "be.a string")
	}},
	// Format validation: regex/pattern/format-shaped assertions on values.
	// Keys on specific tokens, NOT the bare word "format", to avoid colliding
	// with the "Invalid Format" check below.
	{"Format Validation", CategoryValidation, func(c corpus) bool {
		return c.has("to.match", ".match(", "regex", "regexp", "pattern", "uuid", "iso8601", "valid email")
	}},
	// Null checks: presence/absence of null/undefined values.
	{"Null Checks", CategoryValidation, func(c corpus) bool {
		return c.has("null", "undefined", "not.exist", "to.exist", "be.empty")
	}},
	// Required fields: required keys / properties are asserted present.
	{"Required Fields", CategoryValidation, func(c corpus) bool {
		return c.has("required", "to.have.property", "have.property", "have.keys",
			"have.all.keys", "have.ownproperty")
	}},

	// --- Security -----------------------------------------------------------
	// Missing headers: a request is exercised without an expected header.
	{"Missing Headers", CategorySecurity, func(c corpus) bool {
		return c.has("missing header", "header missing", "without header", "no header",
			"remove header", "missing-header")
	}},
	// Invalid payload: malformed/garbage request body is sent.
	{"Invalid Payload", CategorySecurity, func(c corpus) bool {
		return c.has("invalid payload", "bad payload", "invalid body", "invalid json",
			"malformed json", "malformed body", "invalid request body")
	}},
	// Invalid format: a field value in the wrong format is rejected. Keys on
	// "invalid/wrong/bad format" phrases (not bare "format"); disjoint from
	// "Format Validation" above.
	{"Invalid Format", CategorySecurity, func(c corpus) bool {
		return c.has("invalid format", "wrong format", "bad format", "invalid email", "invalid date")
	}},

	// --- Performance --------------------------------------------------------
	// Response-time assertion: explicit latency bound on the response.
	{"Response Time Assertion", CategoryPerformance, func(c corpus) bool {
		if c.has("responsetime", "response time") {
			return true
		}
		// "expect(...).to.be.below(500)" style: "less than"/"below"/"under" + "ms".
		return c.hasAll("less than", "ms") || c.hasAll("below", "ms") || c.hasAll("under", "ms")
	}},
	// Large payload: an oversized request body scenario.
	{"Large Payload", CategoryPerformance, func(c corpus) bool {
		return c.has("large payload", "large body", "big payload", "oversized",
			"payload too large", "request too large", "large file")
	}},

	// --- Edge Case ----------------------------------------------------------
	// Boundary values: min/max/limit/overflow scenarios.
	{"Boundary Values", CategoryEdgeCase, func(c corpus) bool {
		return c.has("boundary", "minimum", "maximum", "out of range", "out-of-range",
			"overflow", "edge case", "off-by-one", "off by one")
	}},
	// Empty payload: empty/blank request body scenario.
	{"Empty Payload", CategoryEdgeCase, func(c corpus) bool {
		return c.has("empty payload", "empty body", "empty request", "blank body", "no body")
	}},
	// Missing fields: required fields omitted from the request.
	{"Missing Fields", CategoryEdgeCase, func(c corpus) bool {
		return c.has("missing field", "missing fields", "field missing", "without field",
			"omit", "missing required")
	}},
	// Duplicate data: re-submission / uniqueness conflict scenario.
	{"Duplicate Data", CategoryEdgeCase, func(c corpus) bool {
		return c.has("duplicate", "already exists", "conflict", "unique constraint", "409")
	}},
}

// Analyze computes the coverage Result for a collection. It is pure: no I/O, no
// globals mutated, deterministic for a given input. A nil or empty collection
// produces an Overall of 0 and a RiskLevel of "High" with every check missing.
func Analyze(c *core.Collection) Result {
	cor := buildCorpus(c)

	categories := make([]CategoryScore, 0, len(categoryOrder))
	missingAll := make([]string, 0, len(checks))
	scoreSum := 0

	for _, cat := range categoryOrder {
		var found, missing []string
		for _, chk := range checks {
			if chk.category != cat {
				continue
			}
			if chk.match(cor) {
				found = append(found, chk.name)
			} else {
				missing = append(missing, chk.name)
			}
		}

		total := len(found) + len(missing)
		score := 0
		if total > 0 {
			score = int(math.Round(100 * float64(len(found)) / float64(total)))
		}

		categories = append(categories, CategoryScore{
			Name:    cat,
			Score:   score,
			Found:   found,
			Missing: missing,
		})
		scoreSum += score
		missingAll = append(missingAll, missing...)
	}

	overall := 0
	if len(categories) > 0 {
		overall = int(math.Round(float64(scoreSum) / float64(len(categories))))
	}

	return Result{
		Overall:       overall,
		Categories:    categories,
		MissingChecks: dedup(missingAll),
		RiskLevel:     riskLevel(overall),
	}
}

// buildCorpus derives the analysis corpus from a collection. It is nil-safe and
// lowercases all text up front so detectors do case-insensitive matching.
func buildCorpus(c *core.Collection) corpus {
	cor := corpus{statuses: make(map[int]bool)}
	if c == nil {
		return cor
	}

	var b strings.Builder
	for _, e := range c.Endpoints {
		if e.HasAuth {
			cor.hasAuth = true
		}
		if e.AuthType != "" {
			b.WriteString(strings.ToLower(e.AuthType))
			b.WriteByte('\n')
		}
		for _, s := range e.TestScripts {
			b.WriteString(strings.ToLower(s))
			b.WriteByte('\n')
		}
		if e.RequestBody != "" {
			b.WriteString(strings.ToLower(e.RequestBody))
			b.WriteByte('\n')
		}
		for _, r := range e.Responses {
			if r.Status != 0 {
				cor.statuses[r.Status] = true
			}
		}
	}
	cor.text = b.String()
	return cor
}

// riskLevel maps an overall 0-100 score to its risk band.
func riskLevel(overall int) string {
	switch {
	case overall < riskHighBelow:
		return RiskHigh
	case overall < riskMediumBelow:
		return RiskMedium
	default:
		return RiskLow
	}
}

// dedup returns the input with duplicate entries removed, preserving first-seen
// order. Returns nil for empty input.
func dedup(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
