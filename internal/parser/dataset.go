package parser

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/apilens/apilens/internal/core"
)

// DetectFormat returns the dataset format ("csv", "json", "jsonl") inferred from
// the file extension, or the lower-cased extension without its leading dot when
// it is not one of the known formats. An empty extension yields "".
func DetectFormat(path string) string {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	switch ext {
	case "csv":
		return "csv"
	case "json":
		return "json"
	case "jsonl", "ndjson":
		return "jsonl"
	default:
		return ext
	}
}

// LoadDataset loads a data-driven testing dataset from path. Supported formats
// are CSV (header row defines keys), JSON (an array of objects), and JSONL (one
// JSON object per line). All values are coerced to their string representation.
func LoadDataset(path string) ([]map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read dataset %q: %w", path, err)
	}

	switch DetectFormat(path) {
	case "csv":
		return parseCSV(data)
	case "json":
		return parseJSONArray(data)
	case "jsonl":
		return parseJSONL(data)
	default:
		return nil, fmt.Errorf("dataset %q: %w", path, core.ErrUnsupportedFormat)
	}
}

// parseCSV reads a CSV with a header row. Rows shorter or longer than the header
// are tolerated; missing trailing cells become empty strings.
func parseCSV(data []byte) ([]map[string]string, error) {
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1 // tolerate ragged rows
	r.TrimLeadingSpace = true

	header, err := r.Read()
	if err == io.EOF {
		return []map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read csv header: %w", err)
	}
	for i := range header {
		header[i] = strings.TrimSpace(header[i])
	}

	var out []map[string]string
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read csv row: %w", err)
		}
		row := make(map[string]string, len(header))
		for i, key := range header {
			if key == "" {
				continue
			}
			if i < len(rec) {
				row[key] = rec[i]
			} else {
				row[key] = ""
			}
		}
		out = append(out, row)
	}
	return out, nil
}

// parseJSONArray decodes a JSON array of objects. A single top-level object is
// tolerated and treated as a one-row dataset.
func parseJSONArray(data []byte) ([]map[string]string, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return []map[string]string{}, nil
	}

	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()

	if trimmed[0] == '{' {
		var obj map[string]any
		if err := dec.Decode(&obj); err != nil {
			return nil, fmt.Errorf("decode json object: %w", err)
		}
		return []map[string]string{coerceRow(obj)}, nil
	}

	var rows []map[string]any
	if err := dec.Decode(&rows); err != nil {
		return nil, fmt.Errorf("decode json array: %w", err)
	}
	out := make([]map[string]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, coerceRow(row))
	}
	return out, nil
}

// parseJSONL decodes one JSON object per non-empty line.
func parseJSONL(data []byte) ([]map[string]string, error) {
	var out []map[string]string
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	line := 0
	for scanner.Scan() {
		line++
		raw := bytes.TrimSpace(scanner.Bytes())
		if len(raw) == 0 {
			continue
		}
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		var obj map[string]any
		if err := dec.Decode(&obj); err != nil {
			return nil, fmt.Errorf("decode jsonl line %d: %w", line, err)
		}
		out = append(out, coerceRow(obj))
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan jsonl: %w", err)
	}
	return out, nil
}

// coerceRow converts every value in a decoded object to its string form.
func coerceRow(obj map[string]any) map[string]string {
	row := make(map[string]string, len(obj))
	for k, v := range obj {
		row[k] = coerceValue(v)
	}
	return row
}

// coerceValue renders a decoded JSON value as a string. Scalars become their
// natural text; composite values (objects, arrays) are re-marshaled to JSON.
func coerceValue(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case json.Number:
		return x.String()
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	default:
		b, err := json.Marshal(x)
		if err != nil {
			return fmt.Sprintf("%v", x)
		}
		return string(b)
	}
}
