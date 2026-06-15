package audit

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apilens/apilens/internal/core"
	"github.com/apilens/apilens/internal/core/ai"
	"github.com/apilens/apilens/internal/core/config"
	"github.com/apilens/apilens/internal/coverage"
	"github.com/apilens/apilens/internal/providers/fake"
	"github.com/apilens/apilens/internal/scoring"
)

// sampleCollection is a small Postman v2.1 collection used across tests. It has
// three endpoints: one documented + authed + tested, one undocumented POST with
// a secret body, and one whose raw URL carries a secret query parameter (to
// exercise redaction in the AI prompt).
const sampleCollection = `{
  "info": {
    "name": "Sample API",
    "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json",
    "description": "A sample collection."
  },
  "item": [
    {
      "name": "Get Users",
      "request": {
        "method": "GET",
        "description": "List all users.",
        "header": [
          {"key": "Authorization", "value": "Bearer eyJhbGciOiJIUzI1Ni.payload.sig"}
        ],
        "url": {"raw": "https://api.example.com/users", "path": ["users"]}
      },
      "event": [
        {"listen": "test", "script": {"exec": ["pm.test('status', function(){ pm.response.to.have.status(200); });"]}}
      ]
    },
    {
      "name": "Create User",
      "request": {
        "method": "POST",
        "body": {"mode": "raw", "raw": "{\"password\": \"hunter2\"}"},
        "url": {"raw": "https://api.example.com/users", "path": ["users"]}
      }
    },
    {
      "name": "Health",
      "request": {
        "method": "GET",
        "url": "https://api.example.com?api_key=SECRETKEY123"
      }
    }
  ]
}`

// writeSample writes the sample collection into dir and returns its path.
func writeSample(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, "collection.json")
	if err := os.WriteFile(p, []byte(sampleCollection), 0o644); err != nil {
		t.Fatalf("write sample collection: %v", err)
	}
	return p
}

// testConfig returns a Default config with an API key set so the AI path runs.
func testConfig() config.Config {
	cfg := config.Default()
	cfg.APIKey = "test-key"
	return cfg
}

func TestRun_BothFormats(t *testing.T) {
	dir := t.TempDir()
	input := writeSample(t, dir)
	outDir := filepath.Join(dir, "out")

	prov := fake.New("## Risk Analysis\nLooks risky.\n\n1. Add 401 tests.")
	cfg := testConfig()
	client := ai.NewWithProvider(prov, cfg)

	err := Run(context.Background(), cfg, client, Options{
		InputPath: input,
		OutDir:    outDir,
		Format:    "both",
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	jsonPath := filepath.Join(outDir, jsonFileName)
	mdPath := filepath.Join(outDir, mdFileName)
	assertExists(t, jsonPath)
	assertExists(t, mdPath)

	// AI was actually invoked.
	if prov.Calls != 1 {
		t.Fatalf("expected fake provider to be called once, got %d", prov.Calls)
	}

	// JSON parses and carries the scores + identity fields.
	var rep Report
	raw := readFile(t, jsonPath)
	if err := json.Unmarshal([]byte(raw), &rep); err != nil {
		t.Fatalf("audit-report.json does not parse: %v", err)
	}
	if rep.Collection != "Sample API" {
		t.Errorf("collection = %q, want %q", rep.Collection, "Sample API")
	}
	if rep.Source != string(core.SourcePostman) {
		t.Errorf("source = %q, want %q", rep.Source, core.SourcePostman)
	}
	if rep.EndpointCount != 3 {
		t.Errorf("endpoint_count = %d, want 3", rep.EndpointCount)
	}
	if !strings.Contains(rep.AIAnalysis, "Looks risky.") {
		t.Errorf("ai_analysis missing narrative: %q", rep.AIAnalysis)
	}
	if !strings.Contains(rep.GeneratedBy, "apilens audit") {
		t.Errorf("generated_by = %q", rep.GeneratedBy)
	}
	// Score-bearing keys are present in the serialized JSON.
	for _, key := range []string{`"coverage"`, `"quality"`, "Overall", "Subscores", "Grade", "Value", `"risk_level"`, `"missing_checks"`} {
		if !strings.Contains(raw, key) {
			t.Errorf("audit-report.json missing key %s", key)
		}
	}

	// MD renders the headline score labels and the AI narrative.
	md := readFile(t, mdPath)
	for _, want := range []string{"# ApiLens Audit Report", "Coverage Score", "Quality Score", "Risk Level", "## AI Analysis", "Looks risky."} {
		if !strings.Contains(md, want) {
			t.Errorf("audit-report.md missing %q", want)
		}
	}
}

func TestRun_PromptIsRedacted(t *testing.T) {
	dir := t.TempDir()
	input := writeSample(t, dir)

	prov := fake.New("ok")
	cfg := testConfig()
	client := ai.NewWithProvider(prov, cfg)

	if err := Run(context.Background(), cfg, client, Options{InputPath: input, OutDir: filepath.Join(dir, "out"), Format: "json"}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(prov.LastReq.Messages) == 0 {
		t.Fatal("no message recorded by fake provider")
	}
	user := prov.LastReq.Messages[0].Content
	if strings.Contains(user, "SECRETKEY123") {
		t.Errorf("prompt leaked secret query value:\n%s", user)
	}
	if !strings.Contains(user, "Sample API") {
		t.Errorf("prompt missing expected collection summary:\n%s", user)
	}
	// The redacted raw URL secret should appear masked.
	if !strings.Contains(user, "api_key="+maskValue) {
		t.Errorf("prompt did not contain masked secret; got:\n%s", user)
	}
	if prov.LastReq.System == "" {
		t.Error("system prompt was empty")
	}
}

func TestRun_JSONOnly(t *testing.T) {
	dir := t.TempDir()
	input := writeSample(t, dir)
	outDir := filepath.Join(dir, "out")

	cfg := testConfig()
	client := ai.NewWithProvider(fake.New("analysis"), cfg)

	if err := Run(context.Background(), cfg, client, Options{InputPath: input, OutDir: outDir, Format: "json"}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	assertExists(t, filepath.Join(outDir, jsonFileName))
	if _, err := os.Stat(filepath.Join(outDir, mdFileName)); !os.IsNotExist(err) {
		t.Errorf("md file should not exist for json-only format, stat err = %v", err)
	}
}

func TestRun_MDOnly(t *testing.T) {
	dir := t.TempDir()
	input := writeSample(t, dir)
	outDir := filepath.Join(dir, "out")

	cfg := testConfig()
	client := ai.NewWithProvider(fake.New("analysis"), cfg)

	if err := Run(context.Background(), cfg, client, Options{InputPath: input, OutDir: outDir, Format: "md"}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	assertExists(t, filepath.Join(outDir, mdFileName))
	if _, err := os.Stat(filepath.Join(outDir, jsonFileName)); !os.IsNotExist(err) {
		t.Errorf("json file should not exist for md-only format, stat err = %v", err)
	}
}

func TestRun_NilClientUnavailableNote(t *testing.T) {
	dir := t.TempDir()
	input := writeSample(t, dir)
	outDir := filepath.Join(dir, "out")

	// nil client => skip AI, still emit scores with the unavailable note.
	if err := Run(context.Background(), config.Default(), nil, Options{InputPath: input, OutDir: outDir, Format: "both"}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	md := readFile(t, filepath.Join(outDir, mdFileName))
	if !strings.Contains(md, unavailableNote) {
		t.Errorf("md missing unavailable note; got:\n%s", md)
	}

	var rep Report
	if err := json.Unmarshal([]byte(readFile(t, filepath.Join(outDir, jsonFileName))), &rep); err != nil {
		t.Fatalf("json parse: %v", err)
	}
	if rep.AIAnalysis != unavailableNote {
		t.Errorf("ai_analysis = %q, want %q", rep.AIAnalysis, unavailableNote)
	}
	if strings.Contains(rep.GeneratedBy, "(") {
		t.Errorf("generated_by should not name a provider without AI: %q", rep.GeneratedBy)
	}
}

func TestRun_NoAPIKeySkipsAI(t *testing.T) {
	dir := t.TempDir()
	input := writeSample(t, dir)
	outDir := filepath.Join(dir, "out")

	cfg := config.Default() // no APIKey
	prov := fake.New("should not be called")
	client := ai.NewWithProvider(prov, cfg)

	if err := Run(context.Background(), cfg, client, Options{InputPath: input, OutDir: outDir, Format: "both"}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if prov.Calls != 0 {
		t.Errorf("AI should be skipped without an API key, got %d calls", prov.Calls)
	}
	md := readFile(t, filepath.Join(outDir, mdFileName))
	if !strings.Contains(md, unavailableNote) {
		t.Errorf("md missing unavailable note")
	}
}

func TestRun_AIErrorDegradesGracefully(t *testing.T) {
	dir := t.TempDir()
	input := writeSample(t, dir)
	outDir := filepath.Join(dir, "out")

	cfg := testConfig()
	client := ai.NewWithProvider(fake.NewError(errProviderDown), cfg)

	// AI failure must not abort the audit; scores are still written.
	if err := Run(context.Background(), cfg, client, Options{InputPath: input, OutDir: outDir, Format: "both"}); err != nil {
		t.Fatalf("Run should not fail on AI error: %v", err)
	}

	var rep Report
	if err := json.Unmarshal([]byte(readFile(t, filepath.Join(outDir, jsonFileName))), &rep); err != nil {
		t.Fatalf("json parse: %v", err)
	}
	if !strings.Contains(rep.AIAnalysis, "AI narrative unavailable") {
		t.Errorf("ai_analysis should note unavailability, got %q", rep.AIAnalysis)
	}
}

func TestRun_DefaultsOutDirToReportDir(t *testing.T) {
	dir := t.TempDir()
	input := writeSample(t, dir)

	cfg := testConfig()
	cfg.ReportDir = filepath.Join(dir, "configured-reports")
	client := ai.NewWithProvider(fake.New("ok"), cfg)

	// Empty OutDir => fall back to cfg.ReportDir.
	if err := Run(context.Background(), cfg, client, Options{InputPath: input, Format: "json"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	assertExists(t, filepath.Join(cfg.ReportDir, jsonFileName))
}

func TestRun_DefaultFormatIsBoth(t *testing.T) {
	dir := t.TempDir()
	input := writeSample(t, dir)
	outDir := filepath.Join(dir, "out")

	cfg := testConfig()
	client := ai.NewWithProvider(fake.New("ok"), cfg)

	// Empty Format => both files.
	if err := Run(context.Background(), cfg, client, Options{InputPath: input, OutDir: outDir}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	assertExists(t, filepath.Join(outDir, jsonFileName))
	assertExists(t, filepath.Join(outDir, mdFileName))
}

func TestRun_UnknownFormatErrors(t *testing.T) {
	dir := t.TempDir()
	input := writeSample(t, dir)

	cfg := testConfig()
	client := ai.NewWithProvider(fake.New("ok"), cfg)

	err := Run(context.Background(), cfg, client, Options{InputPath: input, OutDir: dir, Format: "pdf"})
	if err == nil {
		t.Fatal("expected error for unknown format")
	}
	if !strings.Contains(err.Error(), "unknown format") {
		t.Errorf("error = %v, want it to mention unknown format", err)
	}
}

func TestRun_ParseErrorPropagates(t *testing.T) {
	cfg := testConfig()
	client := ai.NewWithProvider(fake.New("ok"), cfg)

	err := Run(context.Background(), cfg, client, Options{InputPath: filepath.Join(t.TempDir(), "missing.json"), OutDir: t.TempDir(), Format: "json"})
	if err == nil {
		t.Fatal("expected parse error for missing input")
	}
}

func TestNormalizeFormat(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"", formatBoth, false},
		{"both", formatBoth, false},
		{"BOTH", formatBoth, false},
		{" md ", formatMD, false},
		{"json", formatJSON, false},
		{"yaml", "", true},
	}
	for _, tc := range tests {
		got, err := normalizeFormat(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("normalizeFormat(%q) expected error", tc.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("normalizeFormat(%q) error: %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("normalizeFormat(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestDocumentationScore(t *testing.T) {
	tests := []struct {
		name string
		c    *core.Collection
		want int
	}{
		{"nil collection", nil, 0},
		{"no endpoints", &core.Collection{}, 0},
		{
			"all documented",
			&core.Collection{Endpoints: []core.Endpoint{{Description: "a"}, {Description: "b"}}},
			100,
		},
		{
			"none documented",
			&core.Collection{Endpoints: []core.Endpoint{{}, {Description: "   "}}},
			0,
		},
		{
			"one of three rounds to 33",
			&core.Collection{Endpoints: []core.Endpoint{{Description: "x"}, {}, {}}},
			33,
		},
		{
			"two of three rounds to 67",
			&core.Collection{Endpoints: []core.Endpoint{{Description: "x"}, {Description: "y"}, {}}},
			67,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := documentationScore(tc.c); got != tc.want {
				t.Errorf("documentationScore = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestCategoryScore(t *testing.T) {
	cov := coverage.Result{Categories: []coverage.CategoryScore{
		{Name: "Validation", Score: 40},
		{Name: "Security", Score: 70},
		{Name: "Error", Score: 55},
	}}
	if got := categoryScore(cov, "Validation"); got != 40 {
		t.Errorf("Validation = %d, want 40", got)
	}
	if got := categoryScore(cov, "error"); got != 55 {
		t.Errorf("Error (case-insensitive) = %d, want 55", got)
	}
	if got := categoryScore(cov, "Performance"); got != 0 {
		t.Errorf("missing category = %d, want 0", got)
	}
}

func TestRiskLevelOf(t *testing.T) {
	if got := riskLevelOf(coverage.Result{RiskLevel: "High"}, scoring.Score{Value: 95}); got != "High" {
		t.Errorf("explicit risk = %q, want High", got)
	}
	tests := []struct {
		value int
		want  string
	}{
		{90, "Low"},
		{60, "Medium"},
		{59, "High"},
	}
	for _, tc := range tests {
		if got := riskLevelOf(coverage.Result{}, scoring.Score{Value: tc.value}); got != tc.want {
			t.Errorf("riskLevelOf(value=%d) = %q, want %q", tc.value, got, tc.want)
		}
	}
}

func TestDeriveSubscores(t *testing.T) {
	c := &core.Collection{Endpoints: []core.Endpoint{{Description: "doc"}, {}}}
	cov := coverage.Result{
		Overall: 67,
		Categories: []coverage.CategoryScore{
			{Name: "Validation", Score: 40},
			{Name: "Security", Score: 70},
			{Name: "Error", Score: 55},
		},
	}
	got := deriveSubscores(c, cov)
	want := scoring.Subscores{Coverage: 67, Validation: 40, Security: 70, ErrorHandling: 55, Documentation: 50}
	if got != want {
		t.Errorf("deriveSubscores = %+v, want %+v", got, want)
	}
}

func TestBuildAuditPrompt(t *testing.T) {
	c := &core.Collection{
		Name:   "My API",
		Source: core.SourcePostman,
		Endpoints: []core.Endpoint{
			{Method: "GET", Path: "/users", HasAuth: true, AuthType: "bearer", TestScripts: []string{"x"}},
			{Method: "POST", Path: "/users"},
		},
	}
	cov := coverage.Result{
		Overall:       60,
		RiskLevel:     "Medium",
		MissingChecks: []string{"401 Unauthorized", "429 Rate Limit"},
		Categories:    []coverage.CategoryScore{{Name: "Security", Score: 50}},
	}
	system, user := buildAuditPrompt(c, cov, scoring.Score{Value: 72, Grade: "B"})

	if !strings.Contains(system, "auditor") {
		t.Errorf("system prompt missing role framing")
	}
	for _, want := range []string{"My API", "GET /users", "auth: bearer", "tests: 1", "auth: none", "401 Unauthorized", "Security: 50/100", "60/100", "72/100"} {
		if !strings.Contains(user, want) {
			t.Errorf("user prompt missing %q; got:\n%s", want, user)
		}
	}
}

// --- small helpers ----------------------------------------------------------

// maskValue is the redaction placeholder, mirrored here to avoid importing the
// redact package solely for a constant in assertions.
const maskValue = "***REDACTED***"

// errProviderDown is a canned provider failure for the AI-error test.
var errProviderDown = stubErr("provider down")

type stubErr string

func (e stubErr) Error() string { return string(e) }

func assertExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file %q to exist: %v", path, err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}
	return string(data)
}
