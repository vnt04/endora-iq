package parser

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/apilens/apilens/internal/core"
)

// ParseOpenAPI parses an OpenAPI 3.x (or Swagger 2.0) document at path — in
// either YAML or JSON — into a normalized [core.Collection]. Each path+method
// pair becomes one [core.Endpoint]. The document is loaded but not strictly
// validated, so partially-specified specs still yield useful output.
func ParseOpenAPI(path string) (*core.Collection, error) {
	loader := openapi3.NewLoader()
	// Allow external $refs so a spec split across files still loads instead of
	// hard-failing; resolution is best-effort.
	loader.IsExternalRefsAllowed = true

	doc, err := loader.LoadFromFile(path)
	if err != nil {
		return nil, fmt.Errorf("load openapi spec %q: %w", path, err)
	}
	if doc == nil {
		return nil, fmt.Errorf("load openapi spec %q: empty document: %w", path, core.ErrUnsupportedFormat)
	}

	col := &core.Collection{Source: core.SourceOpenAPI}
	if doc.Info != nil {
		col.Name = doc.Info.Title
		col.Description = doc.Info.Description
	}
	if len(doc.Servers) > 0 && doc.Servers[0] != nil {
		col.BaseURL = doc.Servers[0].URL
	}

	col.Endpoints = openapiEndpoints(doc)
	return col, nil
}

// openapiEndpoints flattens the document's paths and operations into endpoints,
// emitted in a deterministic order (path, then method) for stable output.
func openapiEndpoints(doc *openapi3.T) []core.Endpoint {
	if doc.Paths == nil {
		return nil
	}
	pathMap := doc.Paths.Map()
	paths := make([]string, 0, len(pathMap))
	for p := range pathMap {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	var endpoints []core.Endpoint
	for _, p := range paths {
		item := pathMap[p]
		if item == nil {
			continue
		}
		ops := item.Operations()
		methods := make([]string, 0, len(ops))
		for m := range ops {
			methods = append(methods, m)
		}
		sort.Strings(methods)

		for _, method := range methods {
			op := ops[method]
			if op == nil {
				continue
			}
			endpoints = append(endpoints, openapiEndpoint(doc, item, p, method, op))
		}
	}
	return endpoints
}

// openapiEndpoint builds a single endpoint from one operation.
func openapiEndpoint(doc *openapi3.T, item *openapi3.PathItem, path, method string, op *openapi3.Operation) core.Endpoint {
	method = strings.ToUpper(method)

	name := strings.TrimSpace(op.OperationID)
	if name == "" {
		name = method + " " + path
	}
	desc := op.Description
	if desc == "" {
		desc = op.Summary
	}

	// Path-level parameters apply to every operation; merge them in first.
	params := openapiParams(item.Parameters)
	params = append(params, openapiParams(op.Parameters)...)

	hasAuth := openapiHasAuth(doc, op)
	authType := ""
	if hasAuth {
		authType = openapiAuthType(doc, op)
	}

	return core.Endpoint{
		ID:          method + " " + path,
		Name:        name,
		Method:      method,
		Path:        path,
		Description: desc,
		QueryParams: params,
		HasAuth:     hasAuth,
		AuthType:    authType,
		Responses:   openapiResponses(op.Responses),
		Tags:        append([]string(nil), op.Tags...),
	}
}

// openapiParams collects declared parameters (query/path/header). Per the parser
// contract all declared parameters are captured in QueryParams with their In
// field preserved so callers can still distinguish them.
func openapiParams(ps openapi3.Parameters) []core.Param {
	if len(ps) == 0 {
		return nil
	}
	var out []core.Param
	for _, ref := range ps {
		if ref == nil || ref.Value == nil {
			continue
		}
		p := ref.Value
		in := strings.ToLower(strings.TrimSpace(p.In))
		switch in {
		case core.InQuery, core.InPath, core.InHeader:
			out = append(out, core.Param{Name: p.Name, In: in, Value: ""})
		}
	}
	return out
}

// openapiResponses emits one ExampleResponse per declared response code, sorted
// by code for deterministic output. Non-numeric codes ("default", "2XX") get a
// zero Status and keep their key as the Name.
func openapiResponses(rs *openapi3.Responses) []core.ExampleResponse {
	if rs == nil {
		return nil
	}
	m := rs.Map()
	codes := make([]string, 0, len(m))
	for code := range m {
		codes = append(codes, code)
	}
	sort.Strings(codes)

	var out []core.ExampleResponse
	for _, code := range codes {
		ref := m[code]
		status, _ := strconv.Atoi(code)
		er := core.ExampleResponse{Name: code, Status: status}
		if ref != nil && ref.Value != nil && ref.Value.Description != nil {
			er.Body = *ref.Value.Description
		}
		out = append(out, er)
	}
	return out
}

// openapiHasAuth reports whether the operation requires authentication, either
// through an operation-level security requirement or a document-wide default.
func openapiHasAuth(doc *openapi3.T, op *openapi3.Operation) bool {
	if op.Security != nil {
		// A present (possibly empty) operation security overrides the global
		// default; non-empty means auth is required.
		return len(*op.Security) > 0
	}
	return len(doc.Security) > 0
}

// openapiAuthType derives a coarse auth scheme name ("bearer", "apikey",
// "basic", "oauth2", ...) from the first applicable security requirement.
func openapiAuthType(doc *openapi3.T, op *openapi3.Operation) string {
	if doc.Components == nil || len(doc.Components.SecuritySchemes) == 0 {
		return ""
	}
	var reqs openapi3.SecurityRequirements
	if op.Security != nil {
		reqs = *op.Security
	} else {
		reqs = doc.Security
	}
	for _, req := range reqs {
		names := make([]string, 0, len(req))
		for name := range req {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			ref := doc.Components.SecuritySchemes[name]
			if ref == nil || ref.Value == nil {
				continue
			}
			if t := normalizeSchemeType(ref.Value); t != "" {
				return t
			}
		}
	}
	return ""
}

func normalizeSchemeType(s *openapi3.SecurityScheme) string {
	switch strings.ToLower(strings.TrimSpace(s.Type)) {
	case "http":
		scheme := strings.ToLower(strings.TrimSpace(s.Scheme))
		if scheme == "bearer" || scheme == "basic" {
			return scheme
		}
		return "http"
	case "apikey":
		return "apikey"
	case "oauth2":
		return "oauth2"
	case "openidconnect":
		return "oidc"
	default:
		return strings.ToLower(strings.TrimSpace(s.Type))
	}
}
