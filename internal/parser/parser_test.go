package parser

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/apilens/apilens/internal/core"
)

func TestParse_DispatchPostman(t *testing.T) {
	path := writeTemp(t, "collection.json", postmanCollection)
	col, err := Parse(path)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if col.Source != core.SourcePostman {
		t.Errorf("source = %q, want postman", col.Source)
	}
	if col.EndpointCount() == 0 {
		t.Error("expected endpoints from postman dispatch")
	}
}

func TestParse_DispatchOpenAPIYAML(t *testing.T) {
	path := writeTemp(t, "spec.yaml", openapiYAML)
	col, err := Parse(path)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if col.Source != core.SourceOpenAPI {
		t.Errorf("source = %q, want openapi", col.Source)
	}
}

func TestParse_DispatchOpenAPIJSON(t *testing.T) {
	const spec = `{
	  "openapi": "3.0.0",
	  "info": {"title": "JSON Spec"},
	  "paths": {"/x": {"get": {"responses": {"200": {"description": "ok"}}}}}
	}`
	path := writeTemp(t, "spec.json", spec)
	col, err := Parse(path)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if col.Source != core.SourceOpenAPI || col.Name != "JSON Spec" {
		t.Errorf("source/name = %q / %q", col.Source, col.Name)
	}
}

func TestParse_Errors(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		if _, err := Parse(filepath.Join(t.TempDir(), "nope.json")); err == nil {
			t.Error("expected error for missing file")
		}
	})
	t.Run("unrecognized json", func(t *testing.T) {
		path := writeTemp(t, "x.json", `{"foo": "bar"}`)
		_, err := Parse(path)
		if !errors.Is(err, core.ErrUnsupportedFormat) {
			t.Errorf("expected ErrUnsupportedFormat, got %v", err)
		}
	})
	t.Run("plain text", func(t *testing.T) {
		path := writeTemp(t, "x.txt", "just some text\n")
		_, err := Parse(path)
		if !errors.Is(err, core.ErrUnsupportedFormat) {
			t.Errorf("expected ErrUnsupportedFormat, got %v", err)
		}
	})
}

func TestDetectCollectionFormat(t *testing.T) {
	tests := []struct {
		name string
		data string
		want core.SourceType
	}{
		{"openapi key", `{"openapi":"3.0.0"}`, core.SourceOpenAPI},
		{"swagger key", `{"swagger":"2.0"}`, core.SourceOpenAPI},
		{"postman schema", `{"info":{"schema":"...collection..."}}`, core.SourcePostman},
		{"postman items", `{"item":[{"name":"x"}]}`, core.SourcePostman},
		{"yaml openapi", "openapi: 3.0.0\ninfo:\n  title: x\n", core.SourceOpenAPI},
		{"yaml swagger", "swagger: \"2.0\"\n", core.SourceOpenAPI},
		{"unknown", `{"hello":"world"}`, core.SourceType("")},
		{"empty items not postman", `{"item":[]}`, core.SourceType("")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := detectCollectionFormat([]byte(tt.data)); got != tt.want {
				t.Errorf("detectCollectionFormat = %q, want %q", got, tt.want)
			}
		})
	}
}
