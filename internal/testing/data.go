package datatest

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/apilens/apilens/internal/core"
)

// newmanDataFormat maps a source dataset format (as reported by
// parser.DetectFormat) to the on-disk format used when writing iteration-data
// files for newman. newman reads iteration data as CSV or JSON (inferred from
// the file extension): CSV is preserved, while JSON and JSONL are written as a
// JSON array.
func newmanDataFormat(srcFormat string) string {
	if strings.EqualFold(strings.TrimSpace(srcFormat), "csv") {
		return "csv"
	}
	return "json"
}

// shardRows splits rows into at most n contiguous, near-equal shards. It is pure
// and never mutates the input.
//
//   - n <= 1 yields a single shard containing all rows.
//   - n >= len(rows) yields one shard per row (no empty shards).
//   - otherwise the first len(rows)%n shards each receive one extra row.
func shardRows(rows []map[string]string, n int) [][]map[string]string {
	if n < 1 {
		n = 1
	}
	if n > len(rows) {
		n = len(rows)
	}
	if n <= 1 {
		return [][]map[string]string{rows}
	}

	shards := make([][]map[string]string, 0, n)
	base := len(rows) / n
	rem := len(rows) % n
	start := 0
	for i := 0; i < n; i++ {
		size := base
		if i < rem {
			size++
		}
		shards = append(shards, rows[start:start+size])
		start += size
	}
	return shards
}

// writeDataFile serializes rows into a new, uniquely named iteration-data file
// inside dir and returns its path. format selects the encoding ("json" or
// "csv"); "jsonl" is treated as "json" (newman has no JSONL input format, so a
// JSONL dataset is normalized to a JSON array here). The file extension always
// matches the chosen format so newman can infer it.
func writeDataFile(dir string, rows []map[string]string, format string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create data dir %q: %w", dir, err)
	}
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "csv":
		return writeCSVData(dir, rows)
	case "json", "jsonl", "ndjson", "":
		return writeJSONData(dir, rows)
	default:
		return "", fmt.Errorf("write data file: format %q: %w", format, core.ErrUnsupportedFormat)
	}
}

// writeJSONData writes rows as a pretty-printed JSON array of objects.
func writeJSONData(dir string, rows []map[string]string) (string, error) {
	f, err := os.CreateTemp(dir, "apilens-data-*.json")
	if err != nil {
		return "", fmt.Errorf("create json data file: %w", err)
	}
	defer f.Close()

	if rows == nil {
		rows = []map[string]string{}
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(rows); err != nil {
		return "", fmt.Errorf("encode json data %q: %w", f.Name(), err)
	}
	return f.Name(), nil
}

// writeCSVData writes rows as CSV with a header row. Columns are the sorted union
// of all keys present across rows, so the output is deterministic; a row missing
// a column emits an empty cell.
func writeCSVData(dir string, rows []map[string]string) (string, error) {
	f, err := os.CreateTemp(dir, "apilens-data-*.csv")
	if err != nil {
		return "", fmt.Errorf("create csv data file: %w", err)
	}
	defer f.Close()

	cols := unionKeys(rows)
	w := csv.NewWriter(f)
	if err := w.Write(cols); err != nil {
		return "", fmt.Errorf("write csv header %q: %w", f.Name(), err)
	}
	for _, row := range rows {
		rec := make([]string, len(cols))
		for i, c := range cols {
			rec[i] = row[c]
		}
		if err := w.Write(rec); err != nil {
			return "", fmt.Errorf("write csv row %q: %w", f.Name(), err)
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return "", fmt.Errorf("flush csv %q: %w", f.Name(), err)
	}
	return f.Name(), nil
}

// unionKeys returns the sorted set of all keys appearing in any row.
func unionKeys(rows []map[string]string) []string {
	set := make(map[string]struct{})
	for _, r := range rows {
		for k := range r {
			set[k] = struct{}{}
		}
	}
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
