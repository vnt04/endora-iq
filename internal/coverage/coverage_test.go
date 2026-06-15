package coverage

import (
	"slices"
	"strings"
	"testing"

	"github.com/apilens/apilens/internal/core"
)

// containsAll reports the first element of want not present in got, or "" if all
// are present. Used to assert membership in MissingChecks/Found lists.
func firstMissing(got, want []string) string {
	for _, w := range want {
		if !slices.Contains(got, w) {
			return w
		}
	}
	return ""
}

// categoryByName returns the CategoryScore with the given name (or a zero value
// and false).
func categoryByName(r Result, name string) (CategoryScore, bool) {
	for _, c := range r.Categories {
		if c.Name == name {
			return c, true
		}
	}
	return CategoryScore{}, false
}

func TestAnalyze_NilAndEmptyCollection(t *testing.T) {
	cases := []struct {
		name string
		in   *core.Collection
	}{
		{"nil collection", nil},
		{"empty collection", &core.Collection{Name: "empty"}},
		{"endpoints without signals", &core.Collection{Endpoints: []core.Endpoint{
			{Method: "GET", Path: "/ping"},
		}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Analyze(tc.in)

			if got.Overall != 0 {
				t.Errorf("Overall = %d, want 0", got.Overall)
			}
			if got.RiskLevel != RiskHigh {
				t.Errorf("RiskLevel = %q, want %q", got.RiskLevel, RiskHigh)
			}
			if len(got.Categories) != len(categoryOrder) {
				t.Fatalf("Categories = %d, want %d", len(got.Categories), len(categoryOrder))
			}
			// Every check must be missing; nothing found.
			if len(got.MissingChecks) != len(checks) {
				t.Errorf("MissingChecks = %d, want %d (all checks)", len(got.MissingChecks), len(checks))
			}
			for _, c := range got.Categories {
				if c.Score != 0 {
					t.Errorf("category %q Score = %d, want 0", c.Name, c.Score)
				}
				if len(c.Found) != 0 {
					t.Errorf("category %q Found = %v, want empty", c.Name, c.Found)
				}
			}
		})
	}
}

func TestAnalyze_CategoryOrderAndNames(t *testing.T) {
	got := Analyze(&core.Collection{})
	if len(got.Categories) != len(categoryOrder) {
		t.Fatalf("got %d categories, want %d", len(got.Categories), len(categoryOrder))
	}
	for i, want := range categoryOrder {
		if got.Categories[i].Name != want {
			t.Errorf("Categories[%d].Name = %q, want %q", i, got.Categories[i].Name, want)
		}
	}
}

// richCollection exercises every detector so the result is a perfect score.
func richCollection() *core.Collection {
	script := strings.Join([]string{
		"pm.test('unauthorized -> access denied', () => {});",
		"expired token / expired jwt handling",
		"pm.expect(j).to.have.property('id');",                            // required fields
		"typeof j.id === 'number';",                                       // type validation
		"jsonschema schema validation",                                    // schema validation
		"pm.expect(j.email).to.match(/.+@.+/); regex pattern valid email", // format validation
		"pm.expect(j.deleted).to.be.null; undefined check",                // null checks
		"missing header / without header scenario",                        // missing headers
		"invalid payload, malformed json sent",                            // invalid payload
		"invalid format, invalid email rejected",                          // invalid format
		"responsetime less than 200 ms (below threshold)",                 // response time
		"large payload oversized body",                                    // large payload
		"boundary minimum maximum overflow",                               // boundary values
		"empty payload / empty body",                                      // empty payload
		"missing field omitted from request",                              // missing fields
		"duplicate already exists conflict",                               // duplicate data
		"rate limit too many requests, not found, unprocessable, server error",
	}, "\n")

	return &core.Collection{
		Name:   "rich",
		Source: core.SourcePostman,
		Endpoints: []core.Endpoint{
			{
				Method:      "POST",
				Path:        "/users",
				HasAuth:     true,
				AuthType:    "bearer",
				TestScripts: []string{script},
				Responses: []core.ExampleResponse{
					{Status: 200}, {Status: 401}, {Status: 403},
					{Status: 404}, {Status: 422}, {Status: 429}, {Status: 500},
				},
			},
		},
	}
}

func TestAnalyze_RichCollectionPerfectScore(t *testing.T) {
	got := Analyze(richCollection())

	if got.Overall != 100 {
		t.Errorf("Overall = %d, want 100", got.Overall)
	}
	if got.RiskLevel != RiskLow {
		t.Errorf("RiskLevel = %q, want %q", got.RiskLevel, RiskLow)
	}
	if len(got.MissingChecks) != 0 {
		t.Errorf("MissingChecks = %v, want none", got.MissingChecks)
	}
	for _, c := range got.Categories {
		if c.Score != 100 {
			t.Errorf("category %q Score = %d, want 100 (missing: %v)", c.Name, c.Score, c.Missing)
		}
	}
}

func TestAnalyze_HappyPathGETsLowCoverage(t *testing.T) {
	// Only happy-path GETs with a basic 200 + property assertion: no auth, no
	// error statuses, minimal validation.
	col := &core.Collection{
		Name:   "happy",
		Source: core.SourcePostman,
		Endpoints: []core.Endpoint{
			{
				Method: "GET",
				Path:   "/users",
				TestScripts: []string{
					"pm.test('ok', () => { pm.response.to.have.status(200); });",
					"pm.expect(jsonData).to.have.property('id');",
				},
				Responses: []core.ExampleResponse{{Status: 200}},
			},
			{
				Method:      "GET",
				Path:        "/users/{id}",
				TestScripts: []string{"pm.response.to.have.status(200);"},
				Responses:   []core.ExampleResponse{{Status: 200}},
			},
		},
	}

	got := Analyze(col)

	if got.RiskLevel != RiskHigh {
		t.Errorf("RiskLevel = %q, want %q (Overall=%d)", got.RiskLevel, RiskHigh, got.Overall)
	}
	if got.Overall >= riskHighBelow {
		t.Errorf("Overall = %d, want < %d", got.Overall, riskHighBelow)
	}

	// Specific high-value checks must be reported missing.
	wantMissing := []string{
		"401 Unauthorized", "403 Forbidden", "500 Server Error",
		"429 Rate Limit", "Empty Payload", "Response Time Assertion",
		"Authentication Declared",
	}
	if m := firstMissing(got.MissingChecks, wantMissing); m != "" {
		t.Errorf("expected %q in MissingChecks, got %v", m, got.MissingChecks)
	}

	// "Required Fields" was satisfied by to.have.property, so the Validation
	// category should have found it (and NOT list it as missing).
	val, ok := categoryByName(got, CategoryValidation)
	if !ok {
		t.Fatal("Validation category missing")
	}
	if !slices.Contains(val.Found, "Required Fields") {
		t.Errorf("Validation.Found = %v, want to contain %q", val.Found, "Required Fields")
	}
	if slices.Contains(got.MissingChecks, "Required Fields") {
		t.Errorf("Required Fields should not be missing: %v", got.MissingChecks)
	}
}

func TestAnalyze_MediumRiskBand(t *testing.T) {
	// Tuned to land in the Medium band ([50,75)): partial auth + error coverage,
	// partial validation, strong performance/edge signals.
	script := strings.Join([]string{
		"pm.expect(j).to.have.property('id');", // required fields
		"typeof j.id;",                         // type validation
		"pm.expect(j.deleted).to.be.null;",     // null checks
		"invalid payload sent",                 // invalid payload (security)
		"invalid format rejected",              // invalid format (security)
		"responsetime asserted",                // response time (perf)
		"large payload scenario",               // large payload (perf)
		"boundary values exercised",            // boundary (edge)
		"duplicate already exists",             // duplicate (edge)
		"empty payload posted",                 // empty payload (edge)
	}, "\n")

	col := &core.Collection{
		Endpoints: []core.Endpoint{
			{
				Method:      "POST",
				Path:        "/things",
				HasAuth:     true, // Authentication Declared
				TestScripts: []string{script},
				Responses: []core.ExampleResponse{
					{Status: 401}, {Status: 403}, // auth: 401 + 403
					{Status: 404}, {Status: 422}, {Status: 500}, // error: 3 of 4 (no 429)
				},
			},
		},
	}

	got := Analyze(col)

	if got.RiskLevel != RiskMedium {
		t.Errorf("RiskLevel = %q, want %q (Overall=%d)", got.RiskLevel, RiskMedium, got.Overall)
	}
	if got.Overall < riskHighBelow || got.Overall >= riskMediumBelow {
		t.Errorf("Overall = %d, want in [%d,%d)", got.Overall, riskHighBelow, riskMediumBelow)
	}

	// 429 was deliberately omitted and must surface as missing.
	if !slices.Contains(got.MissingChecks, "429 Rate Limit") {
		t.Errorf("want %q in MissingChecks, got %v", "429 Rate Limit", got.MissingChecks)
	}
}

func TestRiskLevelThresholds(t *testing.T) {
	tests := []struct {
		overall int
		want    string
	}{
		{0, RiskHigh},
		{49, RiskHigh},
		{50, RiskMedium}, // High/Medium boundary
		{60, RiskMedium},
		{74, RiskMedium},
		{75, RiskLow}, // Medium/Low boundary
		{90, RiskLow},
		{100, RiskLow},
	}
	for _, tt := range tests {
		if got := riskLevel(tt.overall); got != tt.want {
			t.Errorf("riskLevel(%d) = %q, want %q", tt.overall, got, tt.want)
		}
	}
}

// TestDetectors exercises each detector's positive path individually, so a
// regression in any single check's keyword set is caught.
func TestDetectors(t *testing.T) {
	tests := []struct {
		name  string // check name
		text  string // corpus text (will be lowercased)
		stats map[int]bool
		auth  bool
		want  bool
	}{
		// Authentication
		{name: "Authentication Declared", auth: true, want: true},
		{name: "Authentication Declared", text: "Authorization: Bearer x", want: true},
		{name: "Authentication Declared", text: "no auth here", want: false},
		{name: "401 Unauthorized", stats: map[int]bool{401: true}, want: true},
		{name: "401 Unauthorized", text: "expect Unauthorized", want: true},
		{name: "401 Unauthorized", text: "code 401 returned", want: true},
		{name: "401 Unauthorized", text: "happy path", want: false},
		{name: "403 Forbidden", text: "must be forbidden", want: true},
		{name: "Authorization Failure", text: "access denied path", want: true},
		{name: "Authorization Failure", text: "permission denied", want: true},
		{name: "Authorization Failure", text: "all good", want: false},
		{name: "Expired Token", text: "expired jwt rejected", want: true},
		{name: "Expired Token", text: "fresh token", want: false},

		// Error
		{name: "404 Not Found", text: "resource not found", want: true},
		{name: "422 Unprocessable Entity", stats: map[int]bool{422: true}, want: true},
		{name: "429 Rate Limit", text: "too many requests", want: true},
		{name: "429 Rate Limit", text: "rate limit hit", want: true},
		{name: "500 Server Error", text: "internal server error", want: true},
		{name: "500 Server Error", text: "ok", want: false},

		// Validation
		{name: "Schema Validation", text: "tv4 jsonschema check", want: true},
		{name: "Schema Validation", text: "nothing", want: false},
		{name: "Type Validation", text: "typeof value", want: true},
		{name: "Type Validation", text: "to.be.a('string')", want: true},
		{name: "Format Validation", text: "pm.expect(x).to.match(/re/)", want: true},
		{name: "Format Validation", text: "regex pattern", want: true},
		{name: "Format Validation", text: "invalid format", want: false}, // disjoint from Invalid Format
		{name: "Null Checks", text: "value is null", want: true},
		{name: "Null Checks", text: "no nullable here word", want: true}, // contains "null"
		{name: "Required Fields", text: "to.have.property('id')", want: true},
		{name: "Required Fields", text: "required field present", want: true},

		// Security
		{name: "Missing Headers", text: "request without header", want: true},
		{name: "Missing Headers", text: "all headers set", want: false},
		{name: "Invalid Payload", text: "malformed json body", want: true},
		{name: "Invalid Payload", text: "valid body", want: false},
		{name: "Invalid Format", text: "invalid email supplied", want: true},
		{name: "Invalid Format", text: "wrong format", want: true},

		// Performance
		{name: "Response Time Assertion", text: "pm.response.responseTime", want: true},
		{name: "Response Time Assertion", text: "less than 300 ms", want: true},
		{name: "Response Time Assertion", text: "below 500 ms budget", want: true},
		{name: "Response Time Assertion", text: "fast", want: false},
		{name: "Large Payload", text: "oversized request", want: true},
		{name: "Large Payload", text: "small", want: false},

		// Edge Case
		{name: "Boundary Values", text: "test the boundary", want: true},
		{name: "Boundary Values", text: "overflow case", want: true},
		{name: "Empty Payload", text: "empty body posted", want: true},
		{name: "Missing Fields", text: "missing field id", want: true},
		{name: "Duplicate Data", text: "duplicate entry", want: true},
		{name: "Duplicate Data", text: "409 conflict", want: true},
		{name: "Duplicate Data", text: "unique value", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name+"/"+tt.text, func(t *testing.T) {
			c := corpus{text: strings.ToLower(tt.text), statuses: tt.stats, hasAuth: tt.auth}
			if c.statuses == nil {
				c.statuses = map[int]bool{}
			}
			got := evalCheck(t, tt.name, c)
			if got != tt.want {
				t.Errorf("check %q on %q = %v, want %v", tt.name, tt.text, got, tt.want)
			}
		})
	}
}

func TestBuildCorpus(t *testing.T) {
	col := &core.Collection{
		Endpoints: []core.Endpoint{
			{
				HasAuth:     true,
				AuthType:    "Bearer",
				TestScripts: []string{"PM.RESPONSE.to.have.STATUS(200)", "second SCRIPT"},
				RequestBody: `{"Name":"X"}`,
				Responses:   []core.ExampleResponse{{Status: 200}, {Status: 0}, {Status: 404}},
			},
		},
	}

	c := buildCorpus(col)

	if !c.hasAuth {
		t.Error("hasAuth = false, want true")
	}
	if !c.statuses[200] || !c.statuses[404] {
		t.Errorf("statuses = %v, want 200 and 404", c.statuses)
	}
	if c.statuses[0] {
		t.Error("status 0 should be ignored")
	}
	// Text is lowercased and includes scripts, auth type, and request body.
	for _, want := range []string{"pm.response", "second script", "bearer", `"name":"x"`} {
		if !strings.Contains(c.text, want) {
			t.Errorf("corpus text missing %q; got %q", want, c.text)
		}
	}
}

func TestBuildCorpus_Nil(t *testing.T) {
	c := buildCorpus(nil)
	if c.text != "" {
		t.Errorf("text = %q, want empty", c.text)
	}
	if c.hasAuth {
		t.Error("hasAuth = true, want false")
	}
	if len(c.statuses) != 0 {
		t.Errorf("statuses = %v, want empty", c.statuses)
	}
}

func TestDedup(t *testing.T) {
	if got := dedup(nil); got != nil {
		t.Errorf("dedup(nil) = %v, want nil", got)
	}
	got := dedup([]string{"a", "b", "a", "c", "b"})
	want := []string{"a", "b", "c"}
	if !slices.Equal(got, want) {
		t.Errorf("dedup = %v, want %v", got, want)
	}
}

// TestAnalyze_FoundPlusMissingEqualsTotal guards the invariant that every check
// is classified exactly once per category.
func TestAnalyze_FoundPlusMissingEqualsTotal(t *testing.T) {
	got := Analyze(richCollection())
	total := 0
	for _, c := range got.Categories {
		total += len(c.Found) + len(c.Missing)
	}
	if total != len(checks) {
		t.Errorf("found+missing across categories = %d, want %d", total, len(checks))
	}
}

// evalCheck runs the named detector against the corpus, failing the test if the
// name is unknown.
func evalCheck(t *testing.T, name string, c corpus) bool {
	t.Helper()
	for _, chk := range checks {
		if chk.name == name {
			return chk.match(c)
		}
	}
	t.Fatalf("unknown check %q", name)
	return false
}
