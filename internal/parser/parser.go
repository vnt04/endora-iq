// Package parser turns external API artifacts into ApiLens's normalized domain
// model. It understands Postman v2.1 collections and OpenAPI 3.x / Swagger 2.0
// specifications (both producing a [core.Collection]), Newman JSON run reports
// (producing a [core.NewmanReport]), and data-driven testing datasets in CSV,
// JSON, and JSONL form.
//
// All exported parse functions are defensive about malformed input and wrap the
// underlying error with context; unrecognized inputs surface
// [core.ErrUnsupportedFormat].
package parser

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/apilens/apilens/internal/core"
)

// reOpenAPIKey matches a top-level YAML/JSON `openapi:` or `swagger:` key, used
// as a fallback when content is not parseable as a JSON object (i.e. YAML).
var reOpenAPIKey = regexp.MustCompile(`(?im)^\s*["']?(openapi|swagger)["']?\s*:`)

// ParseAny reads the file at path, detects whether it is a Postman collection or
// an OpenAPI/Swagger specification, and dispatches to the matching parser. It
// returns [core.ErrUnsupportedFormat] (wrapped) when the format is unrecognized.
func ParseAny(path string) (*core.Collection, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read input %q: %w", path, err)
	}

	switch detectCollectionFormat(data) {
	case core.SourcePostman:
		return parsePostmanData(data)
	case core.SourceOpenAPI:
		// Re-load via the OpenAPI loader so relative $refs resolve against the
		// spec's own directory.
		return ParseOpenAPI(path)
	default:
		return nil, fmt.Errorf("detect format %q: %w", path, core.ErrUnsupportedFormat)
	}
}

// Parse is the dispatch entry point named in the frozen core contract; it is an
// alias for [ParseAny] so feature packages can depend on a stable name.
func Parse(path string) (*core.Collection, error) {
	return ParseAny(path)
}

// ParseNewman is the contract-named alias for [ParseNewmanReport].
func ParseNewman(path string) (*core.NewmanReport, error) {
	return ParseNewmanReport(path)
}

// detectCollectionFormat inspects raw bytes and classifies them as a Postman
// collection, an OpenAPI/Swagger spec, or unknown (the empty SourceType).
//
// Heuristic:
//   - If the bytes parse as a JSON object: an "openapi"/"swagger" key means
//     OpenAPI; otherwise an info.schema containing "collection", or a top-level
//     "item" array, means Postman.
//   - Otherwise (typically YAML): a top-level "openapi:"/"swagger:" key means
//     OpenAPI. Postman collections are always JSON, so YAML implies OpenAPI.
func detectCollectionFormat(data []byte) core.SourceType {
	var probe struct {
		OpenAPI string `json:"openapi"`
		Swagger string `json:"swagger"`
		Info    struct {
			Schema string `json:"schema"`
		} `json:"info"`
		Item json.RawMessage `json:"item"`
	}
	if err := json.Unmarshal(data, &probe); err == nil {
		switch {
		case probe.OpenAPI != "" || probe.Swagger != "":
			return core.SourceOpenAPI
		case isPostmanSchema(probe.Info.Schema) || hasItems(probe.Item):
			return core.SourcePostman
		}
	}

	if reOpenAPIKey.Match(data) {
		return core.SourceOpenAPI
	}
	return ""
}

func isPostmanSchema(schema string) bool {
	return strings.Contains(strings.ToLower(schema), "collection")
}

// hasItems reports whether the JSON "item" field is a non-empty array.
func hasItems(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return false
	}
	return len(items) > 0
}
