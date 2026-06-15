package parser

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/apilens/apilens/internal/core"
)

const newmanReport = `{
  "collection": {"info": {"name": "Run Report"}},
  "run": {
    "executions": [
      {
        "item": {"name": "List"},
        "request": {"method": "get", "url": "https://api.example.com/list"},
        "response": {"code": 200, "responseTime": 12},
        "assertions": [
          {"assertion": "Status is 200"},
          {"assertion": "Has body"}
        ]
      },
      {
        "item": {"name": "Create"},
        "request": {"method": "post", "url": {"raw": "https://api.example.com/create"}},
        "response": {"code": 500, "responseTime": 30},
        "assertions": [
          {"assertion": "Status is 201", "error": {"name": "AssertionError", "message": "expected 201 got 500"}}
        ]
      }
    ]
  }
}`

func TestParseNewmanReport(t *testing.T) {
	path := writeTemp(t, "report.json", newmanReport)
	r, err := ParseNewmanReport(path)
	if err != nil {
		t.Fatalf("ParseNewmanReport: %v", err)
	}

	if r.CollectionName != "Run Report" {
		t.Errorf("name = %q", r.CollectionName)
	}
	if r.Total != 3 || r.Failed != 1 || r.Passed != 2 {
		t.Errorf("total/failed/passed = %d/%d/%d, want 3/1/2", r.Total, r.Failed, r.Passed)
	}
	if !r.HasFailures() {
		t.Error("expected HasFailures true")
	}
	if len(r.Executions) != 2 {
		t.Fatalf("expected 2 executions, got %d", len(r.Executions))
	}
	if r.Executions[0].Method != "GET" || r.Executions[0].URL != "https://api.example.com/list" {
		t.Errorf("exec0 = %s %s", r.Executions[0].Method, r.Executions[0].URL)
	}
	create := r.Executions[1]
	if create.Method != "POST" || create.URL != "https://api.example.com/create" {
		t.Errorf("exec1 = %s %s (url object form)", create.Method, create.URL)
	}
	if len(create.Assertions) != 1 || !create.Assertions[0].Failed {
		t.Fatalf("expected 1 failed assertion, got %+v", create.Assertions)
	}
	if create.Assertions[0].ErrorName != "AssertionError" {
		t.Errorf("error name = %q", create.Assertions[0].ErrorName)
	}
}

func TestParseNewmanReport_StatsFallback(t *testing.T) {
	// No per-assertion detail: fall back to run-level stats.
	const c = `{
	  "collection": {"info": {"name": "Stats"}},
	  "run": {"executions": [], "stats": {"assertions": {"total": 10, "failed": 4}}}
	}`
	path := writeTemp(t, "stats.json", c)
	r, err := ParseNewmanReport(path)
	if err != nil {
		t.Fatalf("ParseNewmanReport: %v", err)
	}
	if r.Total != 10 || r.Failed != 4 || r.Passed != 6 {
		t.Errorf("total/failed/passed = %d/%d/%d, want 10/4/6", r.Total, r.Failed, r.Passed)
	}
}

func TestParseNewmanReport_Errors(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		if _, err := ParseNewmanReport(filepath.Join(t.TempDir(), "nope.json")); err == nil {
			t.Error("expected error for missing file")
		}
	})
	t.Run("malformed json", func(t *testing.T) {
		path := writeTemp(t, "bad.json", `{not valid`)
		_, err := ParseNewmanReport(path)
		if !errors.Is(err, core.ErrUnsupportedFormat) {
			t.Errorf("expected ErrUnsupportedFormat, got %v", err)
		}
	})
}

func TestParseNewman_Alias(t *testing.T) {
	path := writeTemp(t, "report.json", newmanReport)
	r, err := ParseNewman(path)
	if err != nil {
		t.Fatalf("ParseNewman: %v", err)
	}
	if r.Total != 3 {
		t.Errorf("alias should match ParseNewmanReport, got total %d", r.Total)
	}
}
