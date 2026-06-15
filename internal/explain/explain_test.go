package explain

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apilens/apilens/internal/core"
	"github.com/apilens/apilens/internal/core/ai"
	"github.com/apilens/apilens/internal/core/config"
	"github.com/apilens/apilens/internal/core/redact"
	"github.com/apilens/apilens/internal/providers/fake"
)

// --- fixtures ----------------------------------------------------------------

const failingReportJSON = `{
  "collection": {"info": {"name": "Sample API"}},
  "run": {"executions": [
    {
      "item": {"name": "List users"},
      "request": {"method": "GET", "url": "https://api.example.com/users"},
      "response": {"code": 401, "responseTime": 12},
      "assertions": [
        {"assertion": "Status code is 200",
         "error": {"name": "AssertionError", "message": "expected response to have status code 200 but got 401"}}
      ]
    },
    {
      "item": {"name": "List orders"},
      "request": {"method": "GET", "url": "https://api.example.com/orders"},
      "response": {"code": 403, "responseTime": 15},
      "assertions": [
        {"assertion": "Status code is 200",
         "error": {"name": "AssertionError", "message": "expected response to have status code 200 but got 403"}}
      ]
    }
  ]}
}`

const passingReportJSON = `{
  "collection": {"info": {"name": "Healthy API"}},
  "run": {"executions": [
    {
      "item": {"name": "Ping"},
      "request": {"method": "GET", "url": "https://api.example.com/ping"},
      "response": {"code": 200, "responseTime": 5},
      "assertions": [
        {"assertion": "Status code is 200"},
        {"assertion": "Body is not empty"}
      ]
    }
  ]}
}`

const secretReportJSON = `{
  "collection": {"info": {"name": "Secret API"}},
  "run": {"executions": [
    {
      "item": {"name": "Login"},
      "request": {"method": "POST", "url": "https://api.example.com/login"},
      "response": {"code": 401},
      "assertions": [
        {"assertion": "Token accepted",
         "error": {"name": "AssertionError", "message": "token eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJhZG1pbiJ9.s3cr3tSignaturePart was rejected"}}
      ]
    }
  ]}
}`

func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture %q: %v", name, err)
	}
	return path
}

func readReport(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read report %q: %v", path, err)
	}
	return string(data)
}

// --- Run: with failures + AI -------------------------------------------------

func TestRun_WithFailures_WritesAnalysis(t *testing.T) {
	reportPath := writeTempFile(t, "report.json", failingReportJSON)
	outPath := filepath.Join(t.TempDir(), "nested", "failure-analysis.md")

	const canned = "**Root Cause:** The bearer token is missing or expired.\n" +
		"**Confidence:** 88%\n" +
		"**Evidence:**\n- All affected requests returned HTTP 401/403.\n" +
		"**Suggested Actions:**\n- Refresh the auth token before running the suite."

	prov := fake.New(canned)
	client := ai.NewWithProvider(prov, config.Default())

	err := Run(context.Background(), config.Default(), client, Options{
		ReportPath: reportPath,
		OutPath:    outPath,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	content := readReport(t, outPath)

	if !strings.Contains(content, "**Failed:** 2") {
		t.Errorf("report missing failure count; got:\n%s", content)
	}
	if !strings.Contains(content, "The bearer token is missing or expired") {
		t.Errorf("report missing canned root cause; got:\n%s", content)
	}
	if !strings.Contains(content, "AI Analysis") {
		t.Errorf("report missing AI Analysis section; got:\n%s", content)
	}
	// Two endpoints, one error signature -> exactly one AI call.
	if prov.Calls != 1 {
		t.Errorf("expected 1 AI call (one group), got %d", prov.Calls)
	}
	if prov.LastReq.System != systemPrompt {
		t.Errorf("unexpected system prompt: %q", prov.LastReq.System)
	}
}

// --- Run: secrets are redacted before prompting ------------------------------

func TestRun_RedactsSecretsInPrompt(t *testing.T) {
	reportPath := writeTempFile(t, "report.json", secretReportJSON)
	outPath := filepath.Join(t.TempDir(), "failure-analysis.md")

	prov := fake.New("**Root Cause:** auth.\n**Confidence:** 50%")
	client := ai.NewWithProvider(prov, config.Default())

	if err := Run(context.Background(), config.Default(), client, Options{ReportPath: reportPath, OutPath: outPath}); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if len(prov.LastReq.Messages) == 0 {
		t.Fatal("no message recorded by fake provider")
	}
	prompt := prov.LastReq.Messages[0].Content
	if strings.Contains(prompt, "eyJhbGciOiJIUzI1NiJ9") {
		t.Errorf("raw JWT leaked into prompt:\n%s", prompt)
	}
	if !strings.Contains(prompt, redact.Mask) {
		t.Errorf("expected redaction mask in prompt:\n%s", prompt)
	}
}

// --- Run: all tests passed ---------------------------------------------------

func TestRun_AllPassed(t *testing.T) {
	reportPath := writeTempFile(t, "report.json", passingReportJSON)
	outPath := filepath.Join(t.TempDir(), "failure-analysis.md")

	prov := fake.New("should not be called")
	client := ai.NewWithProvider(prov, config.Default())

	if err := Run(context.Background(), config.Default(), client, Options{ReportPath: reportPath, OutPath: outPath}); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	content := readReport(t, outPath)
	if !strings.Contains(content, "All tests passed") {
		t.Errorf("expected all-passed report; got:\n%s", content)
	}
	if !strings.Contains(content, "**Passed:** 2") {
		t.Errorf("expected passed count; got:\n%s", content)
	}
	if prov.Calls != 0 {
		t.Errorf("expected no AI calls when all tests pass, got %d", prov.Calls)
	}
}

// --- Run: nil client (no provider/API key) -----------------------------------

func TestRun_NilClient(t *testing.T) {
	reportPath := writeTempFile(t, "report.json", failingReportJSON)
	outPath := filepath.Join(t.TempDir(), "failure-analysis.md")

	if err := Run(context.Background(), config.Default(), nil, Options{ReportPath: reportPath, OutPath: outPath}); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	content := readReport(t, outPath)
	if !strings.Contains(content, "AssertionError") {
		t.Errorf("expected deterministic grouping with error name; got:\n%s", content)
	}
	if !strings.Contains(content, "AI analysis unavailable") {
		t.Errorf("expected AI-unavailable note; got:\n%s", content)
	}
	if !strings.Contains(content, "**Failed:** 2") {
		t.Errorf("expected failure count; got:\n%s", content)
	}
}

// --- Run: input validation / formats -----------------------------------------

func TestRun_EmptyReportPath(t *testing.T) {
	err := Run(context.Background(), config.Default(), nil, Options{})
	if err == nil {
		t.Fatal("expected error for empty report path")
	}
}

func TestRun_HTMLReportRejected(t *testing.T) {
	reportPath := writeTempFile(t, "report.html", "<html><body>newman report</body></html>")
	err := Run(context.Background(), config.Default(), nil, Options{ReportPath: reportPath})
	if err == nil {
		t.Fatal("expected error for HTML report")
	}
	if !strings.Contains(err.Error(), "JSON") {
		t.Errorf("expected guidance to use JSON reporter; got: %v", err)
	}
}

func TestRun_DefaultOutPath(t *testing.T) {
	reportPath := writeTempFile(t, "report.json", failingReportJSON)

	// Run in a temp working directory so the default file lands somewhere clean.
	dir := t.TempDir()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(prev) }()

	if err := Run(context.Background(), config.Default(), nil, Options{ReportPath: reportPath}); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, defaultOutPath)); err != nil {
		t.Errorf("expected default output file %q: %v", defaultOutPath, err)
	}
}

// --- groupFailures: clustering -----------------------------------------------

func TestGroupFailures_ClustersBySameErrorName(t *testing.T) {
	report := &core.NewmanReport{
		Executions: []core.Execution{
			{
				Name: "A", Method: "GET", URL: "/a", Status: 500,
				Assertions: []core.Assertion{
					{Name: "status", Failed: true, ErrorName: "AssertionError", ErrorMessage: "expected 200 but got 500"},
				},
			},
			{
				Name: "B", Method: "GET", URL: "/b", Status: 503,
				Assertions: []core.Assertion{
					{Name: "status", Failed: true, ErrorName: "AssertionError", ErrorMessage: "expected 200 but got 503"},
					{Name: "passing", Failed: false},
					{Name: "body", Failed: true, ErrorName: "JSONError", ErrorMessage: "Unexpected token < in JSON"},
				},
			},
		},
	}

	groups := groupFailures(report)
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d: %+v", len(groups), groups)
	}

	// Sorted by count desc: AssertionError (2) first, then JSONError (1).
	if groups[0].ErrorName != "AssertionError" || groups[0].Count != 2 {
		t.Errorf("expected AssertionError x2 first, got %q x%d", groups[0].ErrorName, groups[0].Count)
	}
	if len(groups[0].Endpoints) != 2 {
		t.Errorf("expected 2 affected endpoints in AssertionError group, got %d (%v)", len(groups[0].Endpoints), groups[0].Endpoints)
	}
	if groups[1].ErrorName != "JSONError" || groups[1].Count != 1 {
		t.Errorf("expected JSONError x1 second, got %q x%d", groups[1].ErrorName, groups[1].Count)
	}
}

func TestGroupFailures_NilReport(t *testing.T) {
	if g := groupFailures(nil); g != nil {
		t.Errorf("expected nil groups for nil report, got %v", g)
	}
}

// --- messageSignature --------------------------------------------------------

func TestMessageSignature(t *testing.T) {
	a := messageSignature("expected 200 but got 500")
	b := messageSignature("expected 200 but got 503")
	if a != b {
		t.Errorf("messages differing only in numbers should share a signature: %q vs %q", a, b)
	}
	if a != "expected # but got #" {
		t.Errorf("unexpected signature: %q", a)
	}
	if strings.ContainsAny(a, "0123456789") {
		t.Errorf("signature should not contain digits: %q", a)
	}
	if got := messageSignature(""); got != "" {
		t.Errorf("empty message should yield empty signature, got %q", got)
	}
	if messageSignature("Unexpected token < in JSON") == a {
		t.Error("distinct messages should not share a signature")
	}
}

// --- correlate ---------------------------------------------------------------

func TestCorrelate_MatchesByPath(t *testing.T) {
	col := &core.Collection{Endpoints: []core.Endpoint{
		{ID: "1", Name: "List users", Method: "GET", Path: "/users"},
		{ID: "2", Name: "List orders", Method: "GET", Path: "/orders"},
	}}
	g := failureGroup{Endpoints: []string{"GET https://api.example.com/users -> HTTP 401"}}

	eps := correlate(col, g)
	if len(eps) != 1 {
		t.Fatalf("expected 1 correlated endpoint, got %d", len(eps))
	}
	if eps[0].Name != "List users" {
		t.Errorf("expected 'List users', got %q", eps[0].Name)
	}

	if correlate(nil, g) != nil {
		t.Error("expected nil for nil collection")
	}
}
