// Package core defines the normalized domain model shared across ApiLens.
//
// Both Postman collections and OpenAPI specifications are parsed into a single
// [Collection] of [Endpoint] values, so every feature package (audit, review,
// generate, explain, coverage, scoring) operates on one stable representation
// regardless of the input format. Newman execution reports are modeled
// separately ([NewmanReport]) because they describe a test run rather than an
// API surface and are consumed by the failure-analysis (explain) feature.
//
// All types here are plain data with sensible zero values. They carry no
// behavior beyond small convenience helpers and are safe to copy.
package core

// SourceType identifies the origin format a [Collection] was parsed from.
type SourceType string

// Supported collection source formats.
const (
	SourcePostman SourceType = "postman"
	SourceOpenAPI SourceType = "openapi"
	SourceNewman  SourceType = "newman"
)

// Parameter locations for [Param.In].
const (
	InQuery  = "query"
	InPath   = "path"
	InHeader = "header"
)

// Header is a single HTTP header key/value pair.
type Header struct {
	Key   string
	Value string
}

// Param is a request parameter. In is one of [InQuery], [InPath] or [InHeader].
type Param struct {
	Name  string
	In    string
	Value string
}

// ExampleResponse is a saved or example response attached to an [Endpoint].
// Status is the HTTP status code; Body is the raw (possibly JSON) body.
type ExampleResponse struct {
	Name    string
	Status  int
	Body    string
	Headers []Header
}

// Endpoint is one normalized API operation. It is the unit of analysis for the
// coverage and scoring engines and the unit of input for the AI features.
type Endpoint struct {
	// ID is a stable identifier within the collection (request id or
	// "METHOD path"). Used to correlate Newman executions back to endpoints.
	ID string
	// Name is a human-readable label (request name or OpenAPI summary).
	Name string
	// Method is the upper-case HTTP verb (GET, POST, ...).
	Method string
	// Path is the URL path template, e.g. "/users/{id}".
	Path string
	// RawURL is the full original URL as written in the source, if any.
	RawURL string
	// Description is free-form documentation for the operation.
	Description string

	Headers     []Header
	QueryParams []Param
	PathParams  []Param

	// HasAuth reports whether the operation declares any authentication.
	HasAuth bool
	// AuthType is the declared auth scheme (e.g. "bearer", "apikey", "basic").
	AuthType string

	// RequestBody is the raw request body (typically JSON).
	RequestBody string

	// TestScripts holds the source of any test/assertion scripts attached to
	// the request (Postman "test" event scripts). The coverage engine inspects
	// these statically to decide which checks are present.
	TestScripts []string

	// Responses holds example/saved responses for the operation.
	Responses []ExampleResponse

	// Tags groups the endpoint (OpenAPI tags or Postman folder names).
	Tags []string
}

// Collection is a normalized set of endpoints parsed from a single source.
type Collection struct {
	Name        string
	Description string
	Source      SourceType
	// BaseURL is the collection-level base URL/host, if one was declared.
	BaseURL   string
	Endpoints []Endpoint
}

// EndpointCount returns the number of endpoints in the collection. It is safe
// to call on a nil receiver.
func (c *Collection) EndpointCount() int {
	if c == nil {
		return 0
	}
	return len(c.Endpoints)
}

// NewmanReport is the parsed result of a Newman (Postman CLI) run. It is the
// input to the failure-analysis feature.
type NewmanReport struct {
	CollectionName string
	Total          int
	Failed         int
	Passed         int
	Executions     []Execution
}

// Execution is a single request execution within a [NewmanReport].
type Execution struct {
	Name           string
	Method         string
	URL            string
	Status         int
	ResponseTimeMs int
	Assertions     []Assertion
}

// Assertion is a single test assertion result within an [Execution].
type Assertion struct {
	Name         string
	Failed       bool
	ErrorName    string
	ErrorMessage string
}

// HasFailures reports whether the report contains any failed executions or
// failed assertions.
func (r *NewmanReport) HasFailures() bool {
	if r == nil {
		return false
	}
	if r.Failed > 0 {
		return true
	}
	for _, e := range r.Executions {
		for _, a := range e.Assertions {
			if a.Failed {
				return true
			}
		}
	}
	return false
}
