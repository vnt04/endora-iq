package explain

import (
	"strings"
	"testing"

	"github.com/apilens/apilens/internal/core"
)

func TestTruncate(t *testing.T) {
	tests := []struct {
		in   string
		n    int
		want string
	}{
		{"short", 10, "short"},
		{"exactfit", 8, "exactfit"},
		{"truncate me please", 10, "truncat..."},
		{"abc", 2, "ab"},
		{"abcdef", 3, "abc"},
	}
	for _, tt := range tests {
		if got := truncate(tt.in, tt.n); got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.in, tt.n, got, tt.want)
		}
	}
}

func TestIsHTMLReport(t *testing.T) {
	cases := map[string]bool{
		"r.html":      true,
		"r.HTM":       true,
		"report.json": false,
		"noext":       false,
	}
	for path, want := range cases {
		if got := isHTMLReport(path); got != want {
			t.Errorf("isHTMLReport(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestErrorNameOrUnknown(t *testing.T) {
	if got := errorNameOrUnknown("AssertionError"); got != "AssertionError" {
		t.Errorf("got %q", got)
	}
	if got := errorNameOrUnknown("   "); got != "Failed assertion" {
		t.Errorf("blank should map to default, got %q", got)
	}
}

func TestFormatEndpointContext(t *testing.T) {
	ep := core.Endpoint{
		Method:      "POST",
		Path:        "/login",
		Name:        "Login user",
		HasAuth:     true,
		AuthType:    "bearer",
		Description: "Authenticates\na user",
		RequestBody: `{"user":"x"}`,
	}
	got := formatEndpointContext(ep)
	for _, want := range []string{"POST /login", "Login user", "auth: bearer", "description:", "request body:"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "\na user") {
		t.Error("description should be flattened to one line")
	}
}

func TestFormatEndpointContext_FallbacksAndAuthYes(t *testing.T) {
	// Empty method/path -> label falls back to Name; auth without type -> "yes".
	ep := core.Endpoint{Name: "Healthcheck", HasAuth: true}
	got := formatEndpointContext(ep)
	if !strings.Contains(got, "Healthcheck") {
		t.Errorf("expected name fallback label, got:\n%s", got)
	}
	if !strings.Contains(got, "auth: yes") {
		t.Errorf("expected 'auth: yes' when type absent, got:\n%s", got)
	}
}

func TestBuildGroupPrompt_WithEndpointsAndContext(t *testing.T) {
	g := failureGroup{
		ErrorName:     "AssertionError",
		Count:         3,
		Endpoints:     []string{"GET https://api.test/users -> HTTP 401"},
		Assertions:    []string{"Status is 200"},
		SampleMessage: "expected 200 but got 401",
	}
	endpoints := []core.Endpoint{{Method: "GET", Path: "/users", Name: "List users", HasAuth: true, AuthType: "bearer"}}

	prompt := buildGroupPrompt(g, endpoints)
	for _, want := range []string{
		"AssertionError",
		"Affected endpoints:",
		"Failed assertions:",
		"Representative error message:",
		"Relevant endpoint definitions",
		"GET /users",
		"Root Cause:",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestLoadCollection(t *testing.T) {
	if loadCollection("") != nil {
		t.Error("empty path should yield nil")
	}
	if loadCollection("/no/such/file.json") != nil {
		t.Error("unparseable path should yield nil (best-effort)")
	}

	const c = `{
	  "info": {"name": "C", "schema": "collection"},
	  "item": [{"name": "Get", "request": {"method": "GET", "url": {"raw": "/x"}, "header": [{"key":"Authorization","value":"Bearer s3cret"}]}}]
	}`
	path := writeTempFile(t, "collection.json", c)
	col := loadCollection(path)
	if col == nil {
		t.Fatal("expected parsed collection")
	}
	if col.EndpointCount() != 1 {
		t.Fatalf("expected 1 endpoint, got %d", col.EndpointCount())
	}
	// Redaction is applied at load time.
	if v := col.Endpoints[0].Headers; len(v) == 1 && strings.Contains(v[0].Value, "s3cret") {
		t.Errorf("collection should be redacted at load, got header %q", v[0].Value)
	}
}
