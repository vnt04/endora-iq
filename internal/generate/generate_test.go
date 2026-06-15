package generate

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
	"github.com/apilens/apilens/internal/core/redact"
	"github.com/apilens/apilens/internal/providers/fake"
)

const fakeScript = "pm.test('status is 200', function(){ pm.response.to.have.status(200); });"

// testConfig returns a config that is valid for the fake provider.
func testConfig() config.Config {
	cfg := config.Default()
	cfg.APIKey = "test-key" // never used by the fake provider; set for realism
	return cfg
}

func TestParseEndpoint(t *testing.T) {
	tests := []struct {
		name       string
		spec       string
		wantMethod string
		wantPath   string
		wantErr    bool
	}{
		{name: "method and path", spec: "GET /users", wantMethod: "GET", wantPath: "/users"},
		{name: "nested path", spec: "POST /a/b", wantMethod: "POST", wantPath: "/a/b"},
		{name: "lower-case method", spec: "post /a/b", wantMethod: "POST", wantPath: "/a/b"},
		{name: "extra whitespace", spec: "  GET   /x  ", wantMethod: "GET", wantPath: "/x"},
		{name: "path only defaults to GET", spec: "/users", wantMethod: "GET", wantPath: "/users"},
		{name: "bare token gets leading slash", spec: "users", wantMethod: "GET", wantPath: "/users"},
		{name: "path with template param", spec: "DELETE /users/{id}", wantMethod: "DELETE", wantPath: "/users/{id}"},
		{name: "empty", spec: "", wantErr: true},
		{name: "whitespace only", spec: "   ", wantErr: true},
		{name: "invalid method", spec: "FOO /x", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			method, path, err := parseEndpoint(tt.spec)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseEndpoint(%q) = (%q, %q, nil); want error", tt.spec, method, path)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseEndpoint(%q) unexpected error: %v", tt.spec, err)
			}
			if method != tt.wantMethod || path != tt.wantPath {
				t.Fatalf("parseEndpoint(%q) = (%q, %q); want (%q, %q)",
					tt.spec, method, path, tt.wantMethod, tt.wantPath)
			}
		})
	}
}

func TestStripFences(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "no fences", in: "pm.test('x');", want: "pm.test('x');"},
		{name: "js fence", in: "```js\npm.test('x');\n```", want: "pm.test('x');"},
		{name: "javascript fence", in: "```javascript\npm.test('x');\n```", want: "pm.test('x');"},
		{name: "plain fence", in: "```\npm.test('x');\n```", want: "pm.test('x');"},
		{name: "trailing stray fence", in: "pm.test('x');\n```", want: "pm.test('x');"},
		{name: "surrounding whitespace", in: "\n\n```js\npm.test('x');\n```\n\n", want: "pm.test('x');"},
		{name: "empty", in: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stripFences(tt.in); got != tt.want {
				t.Fatalf("stripFences(%q) = %q; want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestNormalizeJSON(t *testing.T) {
	pretty := normalizeJSON([]byte(`{"b":1,"a":2}`))
	if !strings.Contains(pretty, "\n") {
		t.Fatalf("expected pretty-printed multi-line JSON, got %q", pretty)
	}
	// Invalid JSON is returned trimmed but unchanged.
	if got := normalizeJSON([]byte("  not json  ")); got != "not json" {
		t.Fatalf("normalizeJSON(non-json) = %q; want %q", got, "not json")
	}
	if got := normalizeJSON([]byte("   ")); got != "" {
		t.Fatalf("normalizeJSON(blank) = %q; want empty", got)
	}
}

func TestWithHeader(t *testing.T) {
	got := withHeader("pm.test('x');", "GET", "/users", 200)
	if !strings.HasPrefix(got, "//") {
		t.Fatalf("output must begin with a header comment, got: %q", got)
	}
	if !strings.Contains(got, "GET /users") || !strings.Contains(got, "200") {
		t.Fatalf("header missing endpoint/status: %q", got)
	}
	if !strings.Contains(got, "pm.test('x');") {
		t.Fatalf("body not preserved: %q", got)
	}
}

// writeResponseFile writes body to a temp file and returns its path.
func writeResponseFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "response.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write response fixture: %v", err)
	}
	return path
}

func TestRunHappyPath(t *testing.T) {
	provider := fake.New(fakeScript)
	client := ai.NewWithProvider(provider, testConfig())

	respPath := writeResponseFile(t, `{"id":1,"email":"a@b.com"}`)
	outPath := filepath.Join(t.TempDir(), "nested", "tests.js")

	err := Run(context.Background(), testConfig(), client, Options{
		ResponsePath: respPath,
		Endpoint:     "GET /users",
		Status:       200,
		OutputPath:   outPath,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("output file not written (MkdirAll should have created parent): %v", err)
	}
	out := string(data)
	if !strings.HasPrefix(out, "//") {
		t.Fatalf("output must begin with a header comment, got: %q", out)
	}
	if !strings.Contains(out, fakeScript) {
		t.Fatalf("output missing generated body: %q", out)
	}
	if strings.Contains(out, "```") {
		t.Fatalf("output must not contain markdown fences: %q", out)
	}

	// System prompt constraints reached the provider.
	if !strings.Contains(provider.LastReq.System, "expert API test engineer") {
		t.Fatalf("system prompt not forwarded: %q", provider.LastReq.System)
	}
	// User prompt embeds endpoint + expected status.
	user := provider.LastReq.Messages[0].Content
	if !strings.Contains(user, "GET /users") || !strings.Contains(user, "200") {
		t.Fatalf("user prompt missing endpoint/status: %q", user)
	}
}

func TestRunStripsFences(t *testing.T) {
	fenced := "```js\n" + fakeScript + "\n```"
	client := ai.NewWithProvider(fake.New(fenced), testConfig())

	respPath := writeResponseFile(t, `{"id":1}`)
	outPath := filepath.Join(t.TempDir(), "tests.js")

	if err := Run(context.Background(), testConfig(), client, Options{
		ResponsePath: respPath,
		Endpoint:     "POST /a/b",
		OutputPath:   outPath,
	}); err != nil {
		t.Fatalf("Run error: %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if strings.Contains(string(data), "```") {
		t.Fatalf("fences were not stripped: %q", string(data))
	}
	if !strings.Contains(string(data), fakeScript) {
		t.Fatalf("body lost during fence stripping: %q", string(data))
	}
}

func TestRunRedactsResponseBeforePrompt(t *testing.T) {
	provider := fake.New(fakeScript)
	client := ai.NewWithProvider(provider, testConfig())

	const secret = "eyJhbGc.payloadpart.signaturepart"
	respPath := writeResponseFile(t, `{"token":"`+secret+`","name":"alice"}`)

	if err := Run(context.Background(), testConfig(), client, Options{
		ResponsePath: respPath,
		Endpoint:     "GET /users",
		OutputPath:   filepath.Join(t.TempDir(), "tests.js"),
	}); err != nil {
		t.Fatalf("Run error: %v", err)
	}

	user := provider.LastReq.Messages[0].Content
	if strings.Contains(user, secret) {
		t.Fatalf("secret leaked into prompt: %q", user)
	}
	if !strings.Contains(user, redact.Mask) {
		t.Fatalf("expected redaction mask in prompt: %q", user)
	}
}

func TestRunDefaultStatus(t *testing.T) {
	provider := fake.New(fakeScript)
	client := ai.NewWithProvider(provider, testConfig())
	respPath := writeResponseFile(t, `{"id":1}`)

	if err := Run(context.Background(), testConfig(), client, Options{
		ResponsePath: respPath,
		Endpoint:     "GET /users",
		// Status omitted -> defaults to 200.
		OutputPath: filepath.Join(t.TempDir(), "tests.js"),
	}); err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if !strings.Contains(provider.LastReq.Messages[0].Content, "200") {
		t.Fatalf("default status 200 not used: %q", provider.LastReq.Messages[0].Content)
	}
}

func TestRunDefaultOutputPath(t *testing.T) {
	// Run in a temp dir so the default "generated-tests.js" lands somewhere safe.
	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })

	client := ai.NewWithProvider(fake.New(fakeScript), testConfig())
	respPath := writeResponseFile(t, `{"id":1}`)

	if err := Run(context.Background(), testConfig(), client, Options{
		ResponsePath: respPath,
		Endpoint:     "GET /users",
	}); err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, defaultOutputPath)); err != nil {
		t.Fatalf("default output file not written: %v", err)
	}
}

func TestRunMissingResponsePath(t *testing.T) {
	client := ai.NewWithProvider(fake.New(fakeScript), testConfig())

	// Empty path.
	if err := Run(context.Background(), testConfig(), client, Options{
		Endpoint:   "GET /users",
		OutputPath: filepath.Join(t.TempDir(), "tests.js"),
	}); err == nil {
		t.Fatal("expected error for empty ResponsePath")
	}

	// Non-existent path.
	missing := filepath.Join(t.TempDir(), "does-not-exist.json")
	if err := Run(context.Background(), testConfig(), client, Options{
		ResponsePath: missing,
		Endpoint:     "GET /users",
		OutputPath:   filepath.Join(t.TempDir(), "tests.js"),
	}); err == nil {
		t.Fatal("expected error for missing response file")
	}
}

func TestRunInvalidEndpoint(t *testing.T) {
	client := ai.NewWithProvider(fake.New(fakeScript), testConfig())
	respPath := writeResponseFile(t, `{"id":1}`)
	if err := Run(context.Background(), testConfig(), client, Options{
		ResponsePath: respPath,
		Endpoint:     "", // empty -> parse error
		OutputPath:   filepath.Join(t.TempDir(), "tests.js"),
	}); err == nil {
		t.Fatal("expected error for empty endpoint")
	}
}

func TestRunNilClient(t *testing.T) {
	respPath := writeResponseFile(t, `{"id":1}`)
	err := Run(context.Background(), testConfig(), nil, Options{
		ResponsePath: respPath,
		Endpoint:     "GET /users",
		OutputPath:   filepath.Join(t.TempDir(), "tests.js"),
	})
	if err == nil {
		t.Fatal("expected error for nil client")
	}
	if !errors.Is(err, core.ErrMissingAPIKey) {
		t.Fatalf("nil-client error should wrap ErrMissingAPIKey, got: %v", err)
	}
}

func TestRunAIError(t *testing.T) {
	client := ai.NewWithProvider(fake.NewError(errors.New("boom")), testConfig())
	respPath := writeResponseFile(t, `{"id":1}`)
	if err := Run(context.Background(), testConfig(), client, Options{
		ResponsePath: respPath,
		Endpoint:     "GET /users",
		OutputPath:   filepath.Join(t.TempDir(), "tests.js"),
	}); err == nil {
		t.Fatal("expected error when the AI provider fails")
	}
}

func TestRunEmptyAIResponse(t *testing.T) {
	// Provider returns only a fence, which strips to empty.
	client := ai.NewWithProvider(fake.New("```js\n```"), testConfig())
	respPath := writeResponseFile(t, `{"id":1}`)
	if err := Run(context.Background(), testConfig(), client, Options{
		ResponsePath: respPath,
		Endpoint:     "GET /users",
		OutputPath:   filepath.Join(t.TempDir(), "tests.js"),
	}); err == nil {
		t.Fatal("expected error when AI returns an empty script")
	}
}
