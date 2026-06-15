package review

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apilens/apilens/internal/core"
	"github.com/apilens/apilens/internal/core/ai"
	"github.com/apilens/apilens/internal/core/config"
	"github.com/apilens/apilens/internal/coverage"
	"github.com/apilens/apilens/internal/providers/fake"
)

const secretToken = "secret-token-do-not-leak-123"

// sampleCollection is a minimal Postman v2.1 collection with exactly one tested
// endpoint (GET, positive-only) and one untested endpoint (POST, with auth and
// a request body). The POST carries a secret Authorization header so tests can
// assert it never reaches the prompt or the report.
const sampleCollection = `{
  "info": {
    "name": "Sample API",
    "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"
  },
  "item": [
    {
      "name": "Create User",
      "request": {
        "method": "POST",
        "url": { "raw": "https://api.example.com/users", "path": ["users"] },
        "auth": { "type": "bearer" },
        "header": [{ "key": "Authorization", "value": "Bearer ` + secretToken + `" }],
        "body": { "mode": "raw", "raw": "{\"email\":\"a@b.com\"}" }
      },
      "event": []
    },
    {
      "name": "List Users",
      "request": {
        "method": "GET",
        "url": { "raw": "https://api.example.com/users", "path": ["users"] }
      },
      "event": [
        {
          "listen": "test",
          "script": { "exec": ["pm.test('status is 200', function () { pm.response.to.have.status(200); });"] }
        }
      ]
    }
  ]
}`

// writeSampleCollection writes sampleCollection into dir and returns its path.
func writeSampleCollection(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "collection.json")
	if err := os.WriteFile(path, []byte(sampleCollection), 0o644); err != nil {
		t.Fatalf("write collection: %v", err)
	}
	return path
}

func TestClassifyEndpoint(t *testing.T) {
	tests := []struct {
		name           string
		endpoint       core.Endpoint
		wantUntested   bool
		wantWeak       bool
		wantWeaknesses []string
		wantRisk       string
	}{
		{
			name:           "untested mutating endpoint is high risk",
			endpoint:       core.Endpoint{Method: "POST", HasAuth: true, RequestBody: "{}"},
			wantUntested:   true,
			wantWeak:       false,
			wantWeaknesses: []string{reasonUntested},
			wantRisk:       RiskHigh,
		},
		{
			name:           "untested read endpoint is medium risk",
			endpoint:       core.Endpoint{Method: "GET"},
			wantUntested:   true,
			wantWeak:       false,
			wantWeaknesses: []string{reasonUntested},
			wantRisk:       RiskMedium,
		},
		{
			name: "tested but positive-only read is weak (no negative)",
			endpoint: core.Endpoint{
				Method:      "GET",
				TestScripts: []string{"pm.test('ok', function(){ pm.response.to.have.status(200); });"},
			},
			wantUntested:   false,
			wantWeak:       true,
			wantWeaknesses: []string{reasonNoNegative},
			wantRisk:       RiskMedium,
		},
		{
			name: "tested mutating endpoint missing only auth checks",
			endpoint: core.Endpoint{
				Method:      "POST",
				HasAuth:     true,
				RequestBody: "{}",
				// 400 supplies both a negative path and a validation status,
				// but there is no 401/403 or auth keyword.
				TestScripts: []string{"pm.response.to.have.status(400);"},
			},
			wantUntested:   false,
			wantWeak:       true,
			wantWeaknesses: []string{reasonNoAuthTest},
			wantRisk:       RiskHigh,
		},
		{
			name: "fully covered endpoint has no weaknesses",
			endpoint: core.Endpoint{
				Method:      "POST",
				HasAuth:     true,
				RequestBody: "{}",
				TestScripts: []string{
					"pm.response.to.have.status(200);",
					"pm.response.to.have.status(401);",
					"pm.response.to.have.status(422);",
				},
			},
			wantUntested:   false,
			wantWeak:       false,
			wantWeaknesses: nil,
			wantRisk:       RiskLow,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyEndpoint(tt.endpoint)

			if got.IsUntested() != tt.wantUntested {
				t.Errorf("IsUntested() = %v, want %v", got.IsUntested(), tt.wantUntested)
			}
			if got.IsWeak() != tt.wantWeak {
				t.Errorf("IsWeak() = %v, want %v", got.IsWeak(), tt.wantWeak)
			}
			if got.Risk != tt.wantRisk {
				t.Errorf("Risk = %q, want %q", got.Risk, tt.wantRisk)
			}
			if !equalStrings(got.Weaknesses, tt.wantWeaknesses) {
				t.Errorf("Weaknesses = %v, want %v", got.Weaknesses, tt.wantWeaknesses)
			}
		})
	}
}

func TestClassifyCollection(t *testing.T) {
	c := &core.Collection{
		Name: "Sample",
		Endpoints: []core.Endpoint{
			{ID: "POST /users", Method: "POST", HasAuth: true, RequestBody: "{}"}, // untested -> High
			{
				ID:          "GET /users",
				Method:      "GET",
				TestScripts: []string{"pm.response.to.have.status(200);"}, // weak -> Medium
			},
		},
	}

	cl := classifyCollection(c, coverage.Result{}) // empty coverage -> count-based fallback

	if len(cl.Untested) != 1 {
		t.Fatalf("Untested = %d, want 1", len(cl.Untested))
	}
	if len(cl.Weak) != 1 {
		t.Fatalf("Weak = %d, want 1", len(cl.Weak))
	}
	if cl.Risk != RiskHigh {
		t.Errorf("overall Risk = %q, want %q", cl.Risk, RiskHigh)
	}

	attention := cl.Attention()
	if len(attention) != 2 {
		t.Fatalf("Attention len = %d, want 2", len(attention))
	}
	if attention[0].Risk != RiskHigh {
		t.Errorf("Attention[0].Risk = %q, want High (risk-sorted)", attention[0].Risk)
	}
}

func TestClassifyCollectionPrefersCoverageRisk(t *testing.T) {
	c := &core.Collection{Endpoints: []core.Endpoint{{Method: "GET"}}}
	cl := classifyCollection(c, coverage.Result{RiskLevel: "Low"})
	if cl.Risk != "Low" {
		t.Errorf("Risk = %q, want Low (from coverage)", cl.Risk)
	}
}

func TestRunWithFakeProvider(t *testing.T) {
	dir := t.TempDir()
	in := writeSampleCollection(t, dir)
	out := filepath.Join(dir, "out", "review.md")

	const marker = "AI_REVIEW_RECOMMENDATIONS_MARKER"
	prov := fake.New("### Weak Endpoints\n" + marker + "\nAdd 401 tests.\n")
	client := ai.NewWithProvider(prov, config.Default())

	if err := Run(context.Background(), config.Default(), client, Options{InputPath: in, OutPath: out}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	md := string(data)

	wantSubstrings := []string{
		"# ApiLens Review",
		"## High Risk / Untested Endpoints",
		"POST /users",  // the untested endpoint must be listed
		reasonUntested, // "No tests written"
		"## Recommendations",
		marker, // AI output is embedded
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(md, want) {
			t.Errorf("review.md missing %q\n--- report ---\n%s", want, md)
		}
	}

	// SECURITY: the secret must never appear in the report nor in the prompt.
	if strings.Contains(md, secretToken) {
		t.Errorf("review.md leaked secret %q", secretToken)
	}
	if prov.Calls != 1 {
		t.Errorf("provider Calls = %d, want 1", prov.Calls)
	}
	if prov.LastReq.System != systemPrompt {
		t.Errorf("system prompt = %q, want %q", prov.LastReq.System, systemPrompt)
	}
	if len(prov.LastReq.Messages) == 0 {
		t.Fatal("provider received no messages")
	}
	userPrompt := prov.LastReq.Messages[0].Content
	if strings.Contains(userPrompt, secretToken) {
		t.Errorf("prompt leaked secret %q:\n%s", secretToken, userPrompt)
	}
	if !strings.Contains(userPrompt, "POST /users") {
		t.Errorf("prompt missing endpoint summary:\n%s", userPrompt)
	}
}

func TestRunNilClient(t *testing.T) {
	dir := t.TempDir()
	in := writeSampleCollection(t, dir)
	out := filepath.Join(dir, "review.md")

	if err := Run(context.Background(), config.Default(), nil, Options{InputPath: in, OutPath: out}); err != nil {
		t.Fatalf("Run with nil client: %v", err)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	md := string(data)

	// Deterministic sections must still be present.
	for _, want := range []string{"## High Risk / Untested Endpoints", "POST /users", reasonUntested} {
		if !strings.Contains(md, want) {
			t.Errorf("review.md missing deterministic content %q", want)
		}
	}
	// AI section degrades to a documented note.
	if !strings.Contains(md, "AI recommendations unavailable") {
		t.Errorf("review.md missing AI-unavailable note:\n%s", md)
	}
	if !strings.Contains(md, "no AI client configured") {
		t.Errorf("review.md missing nil-client reason:\n%s", md)
	}
}

func TestRunDefaultsAIErrorToNote(t *testing.T) {
	dir := t.TempDir()
	in := writeSampleCollection(t, dir)
	out := filepath.Join(dir, "review.md")

	client := ai.NewWithProvider(fake.NewError(errors.New("boom")), config.Default())
	if err := Run(context.Background(), config.Default(), client, Options{InputPath: in, OutPath: out}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	md := readFile(t, out)
	if !strings.Contains(md, "AI recommendations unavailable") {
		t.Errorf("expected graceful AI-error note, got:\n%s", md)
	}
	if !strings.Contains(md, "AI request failed") {
		t.Errorf("expected AI failure reason, got:\n%s", md)
	}
}

func TestRunRequiresInput(t *testing.T) {
	err := Run(context.Background(), config.Default(), nil, Options{})
	if err == nil {
		t.Fatal("expected error for empty input path")
	}
	if !strings.Contains(err.Error(), "input path is required") {
		t.Errorf("error = %v, want input-path message", err)
	}
}

// --- helpers ---

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}
	return string(data)
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
