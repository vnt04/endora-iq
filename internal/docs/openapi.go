package docs

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	yaml "go.yaml.in/yaml/v3"

	"github.com/apilens/apilens/internal/core"
)

var (
	// rePathToken matches OpenAPI path template tokens like {id}.
	rePathToken = regexp.MustCompile(`\{([^}/]+)\}`)
	// reColonParam matches Express/Postman-style :id path segments.
	reColonParam = regexp.MustCompile(`:([A-Za-z0-9_]+)`)
	// reDigits matches an all-digit string.
	reDigits = regexp.MustCompile(`^[0-9]+$`)
)

// --- minimal OpenAPI 3.0 document model (struct order drives YAML order) ------

type oasDoc struct {
	OpenAPI string                 `yaml:"openapi"`
	Info    oasInfo                `yaml:"info"`
	Servers []oasServer            `yaml:"servers,omitempty"`
	Paths   map[string]oasPathItem `yaml:"paths"`
}

type oasInfo struct {
	Title       string `yaml:"title"`
	Description string `yaml:"description,omitempty"`
	Version     string `yaml:"version"`
}

type oasServer struct {
	URL string `yaml:"url"`
}

// oasPathItem maps lower-cased HTTP methods to operations.
type oasPathItem map[string]oasOperation

type oasOperation struct {
	Summary     string                 `yaml:"summary,omitempty"`
	Description string                 `yaml:"description,omitempty"`
	Tags        []string               `yaml:"tags,omitempty"`
	Parameters  []oasParam             `yaml:"parameters,omitempty"`
	Responses   map[string]oasResponse `yaml:"responses"`
}

type oasParam struct {
	Name     string    `yaml:"name"`
	In       string    `yaml:"in"`
	Required bool      `yaml:"required"`
	Schema   oasSchema `yaml:"schema"`
}

type oasSchema struct {
	Type string `yaml:"type"`
}

type oasResponse struct {
	Description string `yaml:"description"`
}

// emitMinimalOpenAPI walks a (already-redacted) collection and renders a valid,
// minimal OpenAPI 3.0 document as YAML. It deliberately emits only structural,
// non-secret material (title, description, server URL, paths, methods,
// summaries, tags, parameter names, response codes) — never header values,
// request bodies, or credentials.
func emitMinimalOpenAPI(col *core.Collection, opts Options) ([]byte, error) {
	doc := oasDoc{
		OpenAPI: openAPIVersion,
		Info: oasInfo{
			Title:   resolveTitle(opts, col),
			Version: defaultAPIVersion,
		},
		Paths: map[string]oasPathItem{},
	}

	if col != nil {
		doc.Info.Description = strings.TrimSpace(col.Description)
		if u := strings.TrimSpace(col.BaseURL); u != "" {
			doc.Servers = []oasServer{{URL: u}}
		}
		for _, ep := range col.Endpoints {
			addEndpoint(doc.Paths, ep)
		}
	}

	out, err := yaml.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("marshal openapi yaml: %w", err)
	}
	return out, nil
}

// addEndpoint merges one endpoint into the paths map under its normalized path
// and lower-cased method.
func addEndpoint(paths map[string]oasPathItem, ep core.Endpoint) {
	p := normalizePath(ep.Path)
	method := strings.ToLower(strings.TrimSpace(ep.Method))
	if method == "" {
		method = "get"
	}
	item := paths[p]
	if item == nil {
		item = oasPathItem{}
		paths[p] = item
	}
	item[method] = buildOperation(p, ep)
}

func buildOperation(path string, ep core.Endpoint) oasOperation {
	op := oasOperation{
		Summary:     strings.TrimSpace(ep.Name),
		Description: strings.TrimSpace(ep.Description),
		Responses:   buildResponses(ep.Responses),
	}
	if len(ep.Tags) > 0 {
		op.Tags = append([]string(nil), ep.Tags...)
	}
	op.Parameters = buildParameters(path, ep)
	return op
}

// buildParameters collects query and path parameters (header params are skipped
// to avoid documenting auth headers) and guarantees every {token} in the path
// template has a declared, required path parameter.
func buildParameters(path string, ep core.Endpoint) []oasParam {
	seen := map[string]bool{}
	var params []oasParam

	add := func(name, in string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		key := in + ":" + name
		if seen[key] {
			return
		}
		seen[key] = true
		params = append(params, oasParam{
			Name:     name,
			In:       in,
			Required: in == core.InPath,
			Schema:   oasSchema{Type: "string"},
		})
	}

	all := append(append([]core.Param(nil), ep.PathParams...), ep.QueryParams...)
	for _, prm := range all {
		switch strings.ToLower(strings.TrimSpace(prm.In)) {
		case core.InQuery:
			add(prm.Name, core.InQuery)
		case core.InPath:
			add(prm.Name, core.InPath)
		}
	}
	for _, m := range rePathToken.FindAllStringSubmatch(path, -1) {
		add(m[1], core.InPath)
	}

	// Deterministic order: path parameters first, then query, each by name.
	sort.SliceStable(params, func(i, j int) bool {
		if params[i].In != params[j].In {
			return params[i].In == core.InPath
		}
		return params[i].Name < params[j].Name
	})
	return params
}

// buildResponses maps example responses to OpenAPI response objects, always
// yielding at least one response (200) so the operation is valid.
func buildResponses(rs []core.ExampleResponse) map[string]oasResponse {
	out := map[string]oasResponse{}
	for _, r := range rs {
		code := responseKey(r)
		if code == "" {
			continue
		}
		desc := strings.TrimSpace(r.Name)
		if desc == "" || reDigits.MatchString(desc) {
			desc = statusText(r.Status)
		}
		out[code] = oasResponse{Description: desc}
	}
	if len(out) == 0 {
		out["200"] = oasResponse{Description: statusText(200)}
	}
	return out
}

// responseKey derives the response map key from a status code, falling back to
// "default" when no usable numeric code is present.
func responseKey(r core.ExampleResponse) string {
	if r.Status > 0 {
		return strconv.Itoa(r.Status)
	}
	n := strings.TrimSpace(r.Name)
	if reDigits.MatchString(n) {
		return n
	}
	return "default"
}

// normalizePath ensures a leading slash and converts :id segments to {id}.
func normalizePath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return "/"
	}
	p = reColonParam.ReplaceAllString(p, "{$1}")
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return p
}

// statusText returns a human description for common HTTP status codes.
func statusText(code int) string {
	switch code {
	case 200:
		return "OK"
	case 201:
		return "Created"
	case 202:
		return "Accepted"
	case 204:
		return "No Content"
	case 301:
		return "Moved Permanently"
	case 302:
		return "Found"
	case 304:
		return "Not Modified"
	case 400:
		return "Bad Request"
	case 401:
		return "Unauthorized"
	case 403:
		return "Forbidden"
	case 404:
		return "Not Found"
	case 405:
		return "Method Not Allowed"
	case 409:
		return "Conflict"
	case 422:
		return "Unprocessable Entity"
	case 429:
		return "Too Many Requests"
	case 500:
		return "Internal Server Error"
	case 502:
		return "Bad Gateway"
	case 503:
		return "Service Unavailable"
	default:
		return "Response"
	}
}
