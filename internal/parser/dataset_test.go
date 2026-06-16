package parser

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/apilens/apilens/internal/core"
)

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %q: %v", name, err)
	}
	return path
}

func TestDetectFormat(t *testing.T) {
	tests := map[string]string{
		"data.csv":    "csv",
		"DATA.CSV":    "csv",
		"data.json":   "json",
		"data.jsonl":  "jsonl",
		"data.ndjson": "jsonl",
		"data.txt":    "txt",
		"noext":       "",
	}
	for path, want := range tests {
		if got := DetectFormat(path); got != want {
			t.Errorf("DetectFormat(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestLoadDataset_CSV(t *testing.T) {
	path := writeTemp(t, "rows.csv", "name, age ,city\nbob,30,NYC\nalice,,LA\nextra,1\n")
	rows, err := LoadDataset(path)
	if err != nil {
		t.Fatalf("LoadDataset: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d: %v", len(rows), rows)
	}
	if rows[0]["name"] != "bob" || rows[0]["age"] != "30" || rows[0]["city"] != "NYC" {
		t.Errorf("row0 = %v", rows[0])
	}
	if rows[1]["age"] != "" {
		t.Errorf("expected empty age, got %q", rows[1]["age"])
	}
	// Ragged row: missing trailing cell becomes empty.
	if rows[2]["city"] != "" {
		t.Errorf("expected empty trailing cell, got %q", rows[2]["city"])
	}
	if rows[2]["name"] != "extra" || rows[2]["age"] != "1" {
		t.Errorf("ragged row = %v", rows[2])
	}
}

func TestLoadDataset_CSVEmpty(t *testing.T) {
	path := writeTemp(t, "empty.csv", "")
	rows, err := LoadDataset(path)
	if err != nil {
		t.Fatalf("LoadDataset: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 rows, got %d", len(rows))
	}
}

func TestLoadDataset_JSONArray(t *testing.T) {
	path := writeTemp(t, "rows.json", `[
		{"s":"hi","n":42,"f":3.5,"b":true,"nil":null,"obj":{"a":1},"arr":[1,2]}
	]`)
	rows, err := LoadDataset(path)
	if err != nil {
		t.Fatalf("LoadDataset: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	r := rows[0]
	checks := map[string]string{
		"s":   "hi",
		"n":   "42",
		"f":   "3.5",
		"b":   "true",
		"nil": "",
		"obj": `{"a":1}`,
		"arr": "[1,2]",
	}
	for k, want := range checks {
		if r[k] != want {
			t.Errorf("row[%q] = %q, want %q", k, r[k], want)
		}
	}
}

func TestLoadDataset_JSONSingleObject(t *testing.T) {
	path := writeTemp(t, "one.json", `{"k":"v"}`)
	rows, err := LoadDataset(path)
	if err != nil {
		t.Fatalf("LoadDataset: %v", err)
	}
	if len(rows) != 1 || rows[0]["k"] != "v" {
		t.Errorf("expected single-row dataset, got %v", rows)
	}
}

func TestLoadDataset_JSONEmpty(t *testing.T) {
	path := writeTemp(t, "blank.json", "   ")
	rows, err := LoadDataset(path)
	if err != nil {
		t.Fatalf("LoadDataset: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("expected empty dataset, got %v", rows)
	}
}

func TestLoadDataset_JSONL(t *testing.T) {
	path := writeTemp(t, "rows.jsonl", "{\"a\":\"1\"}\n\n{\"a\":\"2\",\"b\":\"x\"}\n")
	rows, err := LoadDataset(path)
	if err != nil {
		t.Fatalf("LoadDataset: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows (blank line skipped), got %d", len(rows))
	}
	if rows[0]["a"] != "1" || rows[1]["b"] != "x" {
		t.Errorf("rows = %v", rows)
	}
}

func TestLoadDataset_Errors(t *testing.T) {
	t.Run("unsupported extension", func(t *testing.T) {
		path := writeTemp(t, "data.txt", "whatever")
		_, err := LoadDataset(path)
		if !errors.Is(err, core.ErrUnsupportedFormat) {
			t.Errorf("expected ErrUnsupportedFormat, got %v", err)
		}
	})
	t.Run("missing file", func(t *testing.T) {
		_, err := LoadDataset(filepath.Join(t.TempDir(), "nope.csv"))
		if err == nil {
			t.Error("expected error for missing file")
		}
	})
	t.Run("malformed json", func(t *testing.T) {
		path := writeTemp(t, "bad.json", `[{"a":}]`)
		if _, err := LoadDataset(path); err == nil {
			t.Error("expected error for malformed json array")
		}
	})
	t.Run("malformed jsonl", func(t *testing.T) {
		path := writeTemp(t, "bad.jsonl", "{\"a\":1}\n{not json}\n")
		if _, err := LoadDataset(path); err == nil {
			t.Error("expected error for malformed jsonl line")
		}
	})
	t.Run("malformed json object", func(t *testing.T) {
		path := writeTemp(t, "bad2.json", `{"a":`)
		if _, err := LoadDataset(path); err == nil {
			t.Error("expected error for malformed json object")
		}
	})
}

func TestCoerceValue_Float(t *testing.T) {
	// The float64 branch is only reachable when a decoder does not use
	// json.Number; exercise it directly.
	if got := coerceValue(2.5); got != "2.5" {
		t.Errorf("coerceValue(2.5) = %q, want 2.5", got)
	}
	if got := coerceValue(nil); got != "" {
		t.Errorf("coerceValue(nil) = %q, want empty", got)
	}
}
