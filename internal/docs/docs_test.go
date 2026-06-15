package docs

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apilens/apilens/internal/core"
	"github.com/apilens/apilens/internal/core/config"
)

// disableTools forces hasCommand to report every external tool as absent, so
// Run takes the built-in fallback paths and never touches the network. The
// original lookup is restored at test end.
func disableTools(t *testing.T) {
	t.Helper()
	orig := execLookPath
	execLookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	t.Cleanup(func() { execLookPath = orig })
}

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return p
}

const postmanCollection = `{
  "info": {
    "name": "Petstore",
    "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"
  },
  "variable": [{"key": "baseUrl", "value": "https://api.example.com"}],
  "item": [
    {
      "name": "List Users",
      "request": {
        "method": "GET",
        "header": [{"key": "Authorization", "value": "Bearer secrettoken123"}],
        "url": {"raw": "{{baseUrl}}/users?limit=10", "path": ["users"], "query": [{"key": "limit", "value": "10"}]}
      },
      "response": [{"name": "OK", "code": 200}]
    },
    {
      "name": "Get User",
      "request": {
        "method": "GET",
        "url": {"raw": "{{baseUrl}}/users/{id}", "path": ["users", "{id}"]}
      },
      "response": [{"name": "Not Found", "code": 404}]
    }
  ]
}`

const openapiSpec = `openapi: 3.0.0
info:
  title: Mini API
  version: 1.0.0
paths:
  /ping:
    get:
      summary: Ping
      responses:
        '200':
          description: OK
`

func TestRun_PostmanFallback(t *testing.T) {
	disableTools(t)
	dir := t.TempDir()
	in := writeFile(t, dir, "collection.json", postmanCollection)
	out := filepath.Join(dir, "site")

	err := Run(context.Background(), config.Default(), nil, Options{
		InputPath: in,
		OutDir:    out,
		Title:     "Petstore Docs",
		Theme:     "#ff0000",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	spec := readFile(t, filepath.Join(out, defaultOpenAPIFile))
	if strings.TrimSpace(spec) == "" {
		t.Fatal("openapi.yaml is empty")
	}
	for _, want := range []string{"openapi:", "/users", "/users/{id}", "Petstore Docs", "https://api.example.com"} {
		if !strings.Contains(spec, want) {
			t.Errorf("openapi.yaml missing %q\n---\n%s", want, spec)
		}
	}
	// Security: the redacted-before-emit path plus the structural-only emitter
	// must never leak the bearer token into generated docs.
	if strings.Contains(spec, "secrettoken123") {
		t.Errorf("openapi.yaml leaked a secret token")
	}

	html := readFile(t, filepath.Join(out, defaultIndexFile))
	for _, want := range []string{"redoc", "./openapi.yaml", "Petstore Docs", "#ff0000"} {
		if !strings.Contains(html, want) {
			t.Errorf("index.html missing %q", want)
		}
	}
	if strings.Contains(html, "secrettoken123") {
		t.Errorf("index.html leaked a secret token")
	}
}

func TestRun_OpenAPINormalize(t *testing.T) {
	disableTools(t)
	dir := t.TempDir()
	in := writeFile(t, dir, "spec.yaml", openapiSpec)
	out := filepath.Join(dir, "docs")

	if err := Run(context.Background(), config.Default(), nil, Options{InputPath: in, OutDir: out}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	spec := readFile(t, filepath.Join(out, defaultOpenAPIFile))
	for _, want := range []string{"openapi:", "/ping", "Mini API"} {
		if !strings.Contains(spec, want) {
			t.Errorf("normalized openapi.yaml missing %q", want)
		}
	}

	html := readFile(t, filepath.Join(out, defaultIndexFile))
	// Title falls back to the collection name when --title is empty.
	for _, want := range []string{"redoc", "./openapi.yaml", "Mini API"} {
		if !strings.Contains(html, want) {
			t.Errorf("index.html missing %q", want)
		}
	}
}

func TestRun_DefaultsToDocsDir(t *testing.T) {
	disableTools(t)
	dir := t.TempDir()
	in := writeFile(t, dir, "spec.yaml", openapiSpec)

	cfg := config.Default()
	cfg.DocsDir = filepath.Join(dir, "configured-docs")

	if err := Run(context.Background(), cfg, nil, Options{InputPath: in, OutDir: ""}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !fileNonEmpty(filepath.Join(cfg.DocsDir, defaultOpenAPIFile)) {
		t.Errorf("expected openapi.yaml under cfg.DocsDir %q", cfg.DocsDir)
	}
}

func TestRun_RequiresInput(t *testing.T) {
	disableTools(t)
	err := Run(context.Background(), config.Default(), nil, Options{InputPath: "  "})
	if err == nil {
		t.Fatal("expected error for empty input path")
	}
}

func TestRun_Deploy(t *testing.T) {
	disableTools(t)
	dir := t.TempDir()
	in := writeFile(t, dir, "spec.yaml", openapiSpec)
	out := filepath.Join(dir, "docs")

	if err := Run(context.Background(), config.Default(), nil, Options{InputPath: in, OutDir: out, Deploy: true}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	wf := readFile(t, filepath.Join(dir, ".github", "workflows", "apilens-docs.yml"))
	for _, want := range []string{"upload-pages-artifact", "deploy-pages", `path: "docs"`} {
		if !strings.Contains(wf, want) {
			t.Errorf("workflow missing %q", want)
		}
	}
}

func TestEmitMinimalOpenAPI(t *testing.T) {
	col := &core.Collection{
		Name:        "Sample",
		Description: "Sample description",
		BaseURL:     "https://api.example.com",
		Endpoints: []core.Endpoint{
			{
				Method:      "GET",
				Path:        "/users/{id}",
				Name:        "Get User",
				QueryParams: []core.Param{{Name: "verbose", In: core.InQuery}},
				Responses:   []core.ExampleResponse{{Status: 200}, {Status: 404}},
				Tags:        []string{"users"},
			},
			{
				Method: "POST",
				Path:   "users", // missing leading slash, normalized
				Name:   "Create User",
			},
		},
	}

	data, err := emitMinimalOpenAPI(col, Options{Title: "My Docs"})
	if err != nil {
		t.Fatalf("emitMinimalOpenAPI: %v", err)
	}
	got := string(data)

	for _, want := range []string{
		"openapi: 3.0",
		"title: My Docs",
		"version: 1.0.0",
		"description: Sample description",
		"https://api.example.com",
		"/users/{id}",
		"/users:",
		"get:",
		"post:",
		"responses:",
		`"404"`,
		"verbose",
		"in: path", // auto-added {id} path param
		"in: query",
		"tags:",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("emitted YAML missing %q\n---\n%s", want, got)
		}
	}
}

func TestEmitMinimalOpenAPI_NilAndEmpty(t *testing.T) {
	data, err := emitMinimalOpenAPI(nil, Options{})
	if err != nil {
		t.Fatalf("emit(nil): %v", err)
	}
	got := string(data)
	for _, want := range []string{"openapi:", "title: " + defaultTitle, "paths:"} {
		if !strings.Contains(got, want) {
			t.Errorf("emit(nil) missing %q\n%s", want, got)
		}
	}
}

func TestWriteRedocHTML(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "index.html")
	if err := writeRedocHTML(&core.Collection{Name: "Fallback"}, Options{
		Title: "Branded",
		Logo:  "https://cdn.example.com/logo.png",
		Theme: "#00aa55",
	}, p); err != nil {
		t.Fatalf("writeRedocHTML: %v", err)
	}
	html := readFile(t, p)
	for _, want := range []string{
		"redoc.standalone.js",
		"Redoc.init",
		"./openapi.yaml",
		"Branded",
		"logo.png",
		"#00aa55",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("html missing %q\n---\n%s", want, html)
		}
	}
}

func TestSanitizeThemeColor(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", defaultThemeColor},
		{"#1976d2", "#1976d2"},
		{"rgb(10, 20, 30)", "rgb(10, 20, 30)"},
		{"steelblue", "steelblue"},
		{`"><script>alert(1)</script>`, defaultThemeColor},
		{strings.Repeat("a", 99), defaultThemeColor},
	}
	for _, c := range cases {
		if got := sanitizeThemeColor(c.in); got != c.want {
			t.Errorf("sanitizeThemeColor(%q)=%q, want %q", c.in, got, c.want)
		}
	}
}

func TestNormalizePath(t *testing.T) {
	cases := map[string]string{
		"":            "/",
		"users":       "/users",
		"/users/{id}": "/users/{id}",
		"/users/:id":  "/users/{id}",
	}
	for in, want := range cases {
		if got := normalizePath(in); got != want {
			t.Errorf("normalizePath(%q)=%q, want %q", in, got, want)
		}
	}
}

func TestResponseKey(t *testing.T) {
	cases := []struct {
		in   core.ExampleResponse
		want string
	}{
		{core.ExampleResponse{Status: 200}, "200"},
		{core.ExampleResponse{Status: 0, Name: "201"}, "201"},
		{core.ExampleResponse{Status: 0, Name: "OK"}, "default"},
		{core.ExampleResponse{}, "default"},
	}
	for _, c := range cases {
		if got := responseKey(c.in); got != c.want {
			t.Errorf("responseKey(%+v)=%q, want %q", c.in, got, c.want)
		}
	}
}

func TestHasCommand(t *testing.T) {
	disableTools(t)
	if hasCommand("npx") {
		t.Error("hasCommand should be false when lookup fails")
	}
	if hasCommand("") {
		t.Error("hasCommand(\"\") should be false")
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}
