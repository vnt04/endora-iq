package datatest

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/apilens/apilens/internal/core/config"
	"github.com/apilens/apilens/internal/parser"
)

// --- buildNewmanArgs ---------------------------------------------------------

// indexOf returns the position of want in args, or -1.
func indexOf(args []string, want string) int {
	for i, a := range args {
		if a == want {
			return i
		}
	}
	return -1
}

// flagValue returns the token immediately following flag, or "" if the flag is
// absent or has no following token.
func flagValue(args []string, flag string) string {
	i := indexOf(args, flag)
	if i < 0 || i+1 >= len(args) {
		return ""
	}
	return args[i+1]
}

func TestBuildNewmanArgs(t *testing.T) {
	const (
		coll = "collection.json"
		data = "data.json"
		jrep = "reports/report.json"
		hrep = "reports/report.html"
	)

	tests := []struct {
		name     string
		opts     Options
		dataFile string
		// presence assertions
		wantIterationData bool
		wantBail          bool
		wantDelay         bool
		wantDelayValue    string
	}{
		{
			name:              "minimal: reporters only, no optional flags",
			opts:              Options{CollectionPath: coll},
			dataFile:          "",
			wantIterationData: false,
			wantBail:          false,
			wantDelay:         false,
		},
		{
			name:              "with iteration data",
			opts:              Options{CollectionPath: coll},
			dataFile:          data,
			wantIterationData: true,
		},
		{
			name:              "bail enabled",
			opts:              Options{CollectionPath: coll, Bail: true},
			dataFile:          data,
			wantIterationData: true,
			wantBail:          true,
		},
		{
			name:              "delay set",
			opts:              Options{CollectionPath: coll, DelayMs: 250},
			dataFile:          data,
			wantIterationData: true,
			wantDelay:         true,
			wantDelayValue:    "250",
		},
		{
			name:              "all optional flags",
			opts:              Options{CollectionPath: coll, Bail: true, DelayMs: 100},
			dataFile:          data,
			wantIterationData: true,
			wantBail:          true,
			wantDelay:         true,
			wantDelayValue:    "100",
		},
		{
			name:              "zero delay omits the flag",
			opts:              Options{CollectionPath: coll, DelayMs: 0},
			dataFile:          data,
			wantIterationData: true,
			wantDelay:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := buildNewmanArgs(tt.opts, tt.dataFile, jrep, hrep)

			// Base flags are always present in order.
			if len(args) < 2 || args[0] != "run" || args[1] != tt.opts.CollectionPath {
				t.Fatalf("args must begin with `run <collection>`, got %v", args)
			}
			if got := flagValue(args, "--reporters"); got != "cli,json,html" {
				t.Fatalf("--reporters = %q; want cli,json,html (args=%v)", got, args)
			}
			if got := flagValue(args, "--reporter-json-export"); got != jrep {
				t.Fatalf("--reporter-json-export = %q; want %q", got, jrep)
			}
			if got := flagValue(args, "--reporter-html-export"); got != hrep {
				t.Fatalf("--reporter-html-export = %q; want %q", got, hrep)
			}

			// --iteration-data
			if got := indexOf(args, "--iteration-data") >= 0; got != tt.wantIterationData {
				t.Fatalf("--iteration-data present=%v; want %v (args=%v)", got, tt.wantIterationData, args)
			}
			if tt.wantIterationData {
				if got := flagValue(args, "--iteration-data"); got != tt.dataFile {
					t.Fatalf("--iteration-data = %q; want %q", got, tt.dataFile)
				}
			}

			// --bail
			if got := indexOf(args, "--bail") >= 0; got != tt.wantBail {
				t.Fatalf("--bail present=%v; want %v (args=%v)", got, tt.wantBail, args)
			}

			// --delay-request
			if got := indexOf(args, "--delay-request") >= 0; got != tt.wantDelay {
				t.Fatalf("--delay-request present=%v; want %v (args=%v)", got, tt.wantDelay, args)
			}
			if tt.wantDelay {
				if got := flagValue(args, "--delay-request"); got != tt.wantDelayValue {
					t.Fatalf("--delay-request = %q; want %q", got, tt.wantDelayValue)
				}
			}
		})
	}
}

// --- shardRows ---------------------------------------------------------------

func makeRows(n int) []map[string]string {
	rows := make([]map[string]string, n)
	for i := range rows {
		rows[i] = map[string]string{"id": itoa(i)}
	}
	return rows
}

// itoa is a tiny local int->string to keep the test free of strconv noise.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

func TestShardRows(t *testing.T) {
	tests := []struct {
		name      string
		rowCount  int
		n         int
		wantSizes []int
	}{
		{name: "even split", rowCount: 4, n: 2, wantSizes: []int{2, 2}},
		{name: "uneven split front-loaded", rowCount: 5, n: 2, wantSizes: []int{3, 2}},
		{name: "uneven split three shards", rowCount: 7, n: 3, wantSizes: []int{3, 2, 2}},
		{name: "n equals len", rowCount: 3, n: 3, wantSizes: []int{1, 1, 1}},
		{name: "n greater than len caps at len", rowCount: 2, n: 5, wantSizes: []int{1, 1}},
		{name: "n=1 single shard", rowCount: 4, n: 1, wantSizes: []int{4}},
		{name: "n<1 treated as single shard", rowCount: 3, n: 0, wantSizes: []int{3}},
		{name: "empty dataset single empty shard", rowCount: 0, n: 3, wantSizes: []int{0}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := makeRows(tt.rowCount)
			shards := shardRows(rows, tt.n)

			if len(shards) != len(tt.wantSizes) {
				t.Fatalf("shard count = %d; want %d (shards=%v)", len(shards), len(tt.wantSizes), shards)
			}
			total := 0
			for i, s := range shards {
				if len(s) != tt.wantSizes[i] {
					t.Fatalf("shard %d size = %d; want %d", i, len(s), tt.wantSizes[i])
				}
				total += len(s)
			}
			if total != tt.rowCount {
				t.Fatalf("total rows across shards = %d; want %d", total, tt.rowCount)
			}

			// Order is preserved and every row is covered exactly once.
			if tt.rowCount > 0 {
				next := 0
				for _, s := range shards {
					for _, r := range s {
						if r["id"] != itoa(next) {
							t.Fatalf("row out of order: got id=%q want id=%q", r["id"], itoa(next))
						}
						next++
					}
				}
			}
		})
	}
}

// --- newmanDataFormat / reportPaths ------------------------------------------

func TestNewmanDataFormat(t *testing.T) {
	tests := map[string]string{
		"csv":   "csv",
		"CSV":   "csv",
		"json":  "json",
		"jsonl": "json",
		"":      "json",
	}
	for in, want := range tests {
		if got := newmanDataFormat(in); got != want {
			t.Fatalf("newmanDataFormat(%q) = %q; want %q", in, got, want)
		}
	}
}

func TestReportPaths(t *testing.T) {
	jOne, hOne := reportPaths("reports", 0)
	if jOne != filepath.Join("reports", "report.json") || hOne != filepath.Join("reports", "report.html") {
		t.Fatalf("single-run report paths = (%q, %q)", jOne, hOne)
	}
	jShard, hShard := reportPaths("reports", 2)
	if jShard != filepath.Join("reports", "report-2.json") || hShard != filepath.Join("reports", "report-2.html") {
		t.Fatalf("sharded report paths = (%q, %q)", jShard, hShard)
	}
}

// --- writeDataFile + round-trips --------------------------------------------

func sameRows(a, b []map[string]string) bool {
	return reflect.DeepEqual(a, b)
}

func TestWriteDataFileJSONRoundTrip(t *testing.T) {
	dir := t.TempDir()
	rows := []map[string]string{
		{"id": "1", "email": "a@b.com"},
		{"id": "2", "email": "c@d.com"},
	}

	path, err := writeDataFile(dir, rows, "json")
	if err != nil {
		t.Fatalf("writeDataFile json: %v", err)
	}
	if filepath.Ext(path) != ".json" {
		t.Fatalf("expected .json extension, got %q", path)
	}
	if filepath.Dir(path) != filepath.Clean(dir) {
		t.Fatalf("file written outside requested dir: %q (dir=%q)", path, dir)
	}

	got, err := parser.LoadDataset(path)
	if err != nil {
		t.Fatalf("reload json data: %v", err)
	}
	if !sameRows(got, rows) {
		t.Fatalf("json round-trip mismatch:\n got=%v\nwant=%v", got, rows)
	}
}

func TestWriteDataFileCSVRoundTrip(t *testing.T) {
	dir := t.TempDir()
	// Uniform keys so the CSV round-trip is lossless.
	rows := []map[string]string{
		{"id": "1", "name": "alice"},
		{"id": "2", "name": "bob"},
	}

	path, err := writeDataFile(dir, rows, "csv")
	if err != nil {
		t.Fatalf("writeDataFile csv: %v", err)
	}
	if filepath.Ext(path) != ".csv" {
		t.Fatalf("expected .csv extension, got %q", path)
	}

	got, err := parser.LoadDataset(path)
	if err != nil {
		t.Fatalf("reload csv data: %v", err)
	}
	if !sameRows(got, rows) {
		t.Fatalf("csv round-trip mismatch:\n got=%v\nwant=%v", got, rows)
	}
}

func TestWriteDataFileUniquePaths(t *testing.T) {
	dir := t.TempDir()
	rows := []map[string]string{{"id": "1"}}
	p1, err := writeDataFile(dir, rows, "json")
	if err != nil {
		t.Fatalf("first write: %v", err)
	}
	p2, err := writeDataFile(dir, rows, "json")
	if err != nil {
		t.Fatalf("second write: %v", err)
	}
	if p1 == p2 {
		t.Fatalf("expected unique data files, both = %q", p1)
	}
}

func TestWriteDataFileUnsupportedFormat(t *testing.T) {
	if _, err := writeDataFile(t.TempDir(), nil, "xml"); err == nil {
		t.Fatal("expected error for unsupported format")
	}
}

// TestJSONLNormalization mirrors the runSingle JSONL path: load a JSONL dataset,
// then re-emit it as a JSON array file that newman can consume, and verify the
// rows survive the normalization.
func TestJSONLNormalization(t *testing.T) {
	dir := t.TempDir()
	jsonlPath := filepath.Join(dir, "data.jsonl")
	const content = `{"id":"1","active":"true"}
{"id":"2","active":"false"}
`
	if err := os.WriteFile(jsonlPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write jsonl fixture: %v", err)
	}

	if got := parser.DetectFormat(jsonlPath); got != "jsonl" {
		t.Fatalf("DetectFormat(%q) = %q; want jsonl", jsonlPath, got)
	}

	rows, err := parser.LoadDataset(jsonlPath)
	if err != nil {
		t.Fatalf("load jsonl: %v", err)
	}

	normalized, err := writeDataFile(dir, rows, "json")
	if err != nil {
		t.Fatalf("normalize jsonl -> json: %v", err)
	}
	if filepath.Ext(normalized) != ".json" {
		t.Fatalf("normalized file must be .json, got %q", normalized)
	}

	got, err := parser.LoadDataset(normalized)
	if err != nil {
		t.Fatalf("reload normalized json: %v", err)
	}
	want := []map[string]string{
		{"id": "1", "active": "true"},
		{"id": "2", "active": "false"},
	}
	if !sameRows(got, want) {
		t.Fatalf("jsonl normalization mismatch:\n got=%v\nwant=%v", got, want)
	}
}

// --- resolveOutDir -----------------------------------------------------------

func TestResolveOutDir(t *testing.T) {
	if got := resolveOutDir("custom", "cfg"); got != "custom" {
		t.Fatalf("explicit OutDir wins: got %q", got)
	}
	if got := resolveOutDir("  ", "cfg"); got != "cfg" {
		t.Fatalf("blank OutDir falls back to cfg: got %q", got)
	}
	if got := resolveOutDir("", ""); got != defaultOutDir {
		t.Fatalf("both empty falls back to default: got %q", got)
	}
}

// --- Run validation (no newman required; fails before runner resolution) -----

func TestRunValidationErrors(t *testing.T) {
	dir := t.TempDir()
	collection := filepath.Join(dir, "collection.json")
	if err := os.WriteFile(collection, []byte(`{"item":[]}`), 0o644); err != nil {
		t.Fatalf("write collection fixture: %v", err)
	}
	dataset := filepath.Join(dir, "data.csv")
	if err := os.WriteFile(dataset, []byte("id\n1\n"), 0o644); err != nil {
		t.Fatalf("write dataset fixture: %v", err)
	}

	tests := []struct {
		name    string
		opts    Options
		wantSub string
	}{
		{
			name:    "missing collection path",
			opts:    Options{DataPath: dataset},
			wantSub: "collection path is required",
		},
		{
			name:    "collection file does not exist",
			opts:    Options{CollectionPath: filepath.Join(dir, "nope.json"), DataPath: dataset},
			wantSub: "collection",
		},
		{
			name:    "missing data path",
			opts:    Options{CollectionPath: collection},
			wantSub: "data path is required",
		},
		{
			name:    "data file does not exist",
			opts:    Options{CollectionPath: collection, DataPath: filepath.Join(dir, "nope.csv")},
			wantSub: "load dataset",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Run(context.Background(), config.Default(), nil, tt.opts)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantSub) {
				t.Fatalf("error %q does not contain %q", err.Error(), tt.wantSub)
			}
		})
	}
}
