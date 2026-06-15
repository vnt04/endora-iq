package parser

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/apilens/apilens/internal/core"
)

// ParsePostman parses a Postman Collection v2.1 JSON file at path into a
// normalized [core.Collection]. Folders nest arbitrarily; every request item
// (no matter how deeply nested) becomes one [core.Endpoint]. The collection's
// folder names accumulate into each endpoint's Tags. Malformed JSON is wrapped
// with context.
func ParsePostman(path string) (*core.Collection, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read postman collection %q: %w", path, err)
	}
	return parsePostmanData(data)
}

// parsePostmanData parses raw Postman v2.1 JSON bytes. Split out so the
// auto-detecting dispatcher can avoid re-reading the file.
func parsePostmanData(data []byte) (*core.Collection, error) {
	var pc pmCollection
	if err := json.Unmarshal(data, &pc); err != nil {
		return nil, fmt.Errorf("decode postman collection: %w: %w", err, core.ErrUnsupportedFormat)
	}

	col := &core.Collection{
		Name:        pc.Info.Name,
		Description: string(pc.Info.Description),
		Source:      core.SourcePostman,
		BaseURL:     postmanBaseURL(pc.Variable),
	}

	b := &postmanBuilder{collectionAuth: pc.Auth}
	b.walk(pc.Item, nil, pc.Auth)
	col.Endpoints = b.endpoints
	return col, nil
}

// postmanBuilder accumulates endpoints while recursively walking items.
type postmanBuilder struct {
	collectionAuth *pmAuth
	endpoints      []core.Endpoint
}

// walk recurses through items. tags carries the enclosing folder names and
// inheritedAuth is the nearest ancestor auth (collection or folder) that an
// inner request inherits when it declares none of its own.
func (b *postmanBuilder) walk(items []pmItem, tags []string, inheritedAuth *pmAuth) {
	for i := range items {
		it := items[i]
		if it.Request != nil {
			b.endpoints = append(b.endpoints, b.toEndpoint(it, tags, inheritedAuth))
			continue
		}
		// Treat anything without a request as a (possibly empty) folder.
		childTags := tags
		if name := strings.TrimSpace(it.Name); name != "" {
			childTags = append(append([]string{}, tags...), name)
		}
		childAuth := inheritedAuth
		if it.Auth != nil {
			childAuth = it.Auth
		}
		b.walk(it.Item, childTags, childAuth)
	}
}

// toEndpoint converts a single request item into a normalized endpoint.
func (b *postmanBuilder) toEndpoint(it pmItem, tags []string, inheritedAuth *pmAuth) core.Endpoint {
	req := it.Request
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = "GET"
	}
	path := buildPostmanPath(req.URL)

	id := method
	switch {
	case path != "":
		id = method + " " + path
	case req.URL.Raw != "":
		id = method + " " + req.URL.Raw
	default:
		id = method + " " + it.Name
	}

	// Auth resolution: request auth wins, then nearest folder/collection auth.
	effAuth := req.Auth
	if effAuth == nil {
		effAuth = inheritedAuth
	}
	hasAuth, authType := resolvePostmanAuth(effAuth)

	ep := core.Endpoint{
		ID:          id,
		Name:        it.Name,
		Method:      method,
		Path:        path,
		RawURL:      req.URL.Raw,
		Description: string(req.Description),
		Headers:     postmanHeaders(req.Header),
		QueryParams: postmanQuery(req.URL.Query),
		PathParams:  postmanPathParams(req.URL.Variable),
		HasAuth:     hasAuth,
		AuthType:    authType,
		RequestBody: summarizePostmanBody(req.Body),
		TestScripts: postmanTestScripts(it.Event),
		Responses:   postmanResponses(it.Response),
	}
	if len(tags) > 0 {
		ep.Tags = append([]string{}, tags...)
	}
	return ep
}

// --- field extraction helpers -------------------------------------------------

func postmanHeaders(hs []pmHeader) []core.Header {
	if len(hs) == 0 {
		return nil
	}
	out := make([]core.Header, 0, len(hs))
	for _, h := range hs {
		if h.Disabled {
			continue
		}
		out = append(out, core.Header{Key: h.Key, Value: h.Value})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func postmanQuery(qs []pmQuery) []core.Param {
	if len(qs) == 0 {
		return nil
	}
	out := make([]core.Param, 0, len(qs))
	for _, q := range qs {
		if q.Disabled {
			continue
		}
		out = append(out, core.Param{Name: q.Key, In: core.InQuery, Value: q.Value})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func postmanPathParams(vs []pmVariable) []core.Param {
	if len(vs) == 0 {
		return nil
	}
	out := make([]core.Param, 0, len(vs))
	for _, v := range vs {
		if strings.TrimSpace(v.Key) == "" {
			continue
		}
		out = append(out, core.Param{Name: v.Key, In: core.InPath, Value: v.Value})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func postmanResponses(rs []pmResponse) []core.ExampleResponse {
	if len(rs) == 0 {
		return nil
	}
	out := make([]core.ExampleResponse, 0, len(rs))
	for _, r := range rs {
		name := r.Name
		if name == "" {
			name = r.Status
		}
		out = append(out, core.ExampleResponse{
			Name:    name,
			Status:  r.Code,
			Body:    r.Body,
			Headers: postmanHeaders(r.Header),
		})
	}
	return out
}

func postmanTestScripts(events []pmEvent) []string {
	if len(events) == 0 {
		return nil
	}
	var out []string
	for _, ev := range events {
		if !strings.EqualFold(strings.TrimSpace(ev.Listen), "test") {
			continue
		}
		joined := strings.TrimSpace(strings.Join([]string(ev.Script.Exec), "\n"))
		if joined != "" {
			out = append(out, joined)
		}
	}
	return out
}

// resolvePostmanAuth returns whether auth is declared and its lowercase type.
// An explicit "noauth" (or empty type) means no authentication.
func resolvePostmanAuth(a *pmAuth) (bool, string) {
	if a == nil {
		return false, ""
	}
	t := strings.ToLower(strings.TrimSpace(a.Type))
	if t == "" || t == "noauth" {
		return false, ""
	}
	return true, t
}

// summarizePostmanBody renders a request body into a single string suitable for
// analysis. raw bodies pass through verbatim; urlencoded/formdata are summarized
// as key=value pairs joined by "&".
func summarizePostmanBody(b *pmBody) string {
	if b == nil {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(b.Mode)) {
	case "raw":
		return b.Raw
	case "urlencoded":
		return postmanKVString(b.URLEncoded)
	case "formdata":
		return postmanKVString(b.FormData)
	default:
		return b.Raw
	}
}

func postmanKVString(kvs []pmKV) string {
	if len(kvs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(kvs))
	for _, kv := range kvs {
		if kv.Disabled {
			continue
		}
		parts = append(parts, kv.Key+"="+kv.Value)
	}
	return strings.Join(parts, "&")
}

// buildPostmanPath derives a path template from the structured URL, preferring
// the explicit path segments and falling back to parsing the raw URL.
func buildPostmanPath(u pmURL) string {
	if len(u.Path) > 0 {
		segs := make([]string, 0, len(u.Path))
		for _, s := range u.Path {
			s = strings.Trim(strings.TrimSpace(s), "/")
			if s != "" {
				segs = append(segs, s)
			}
		}
		if len(segs) > 0 {
			return "/" + strings.Join(segs, "/")
		}
	}
	if u.Raw != "" {
		return pathFromRawURL(u.Raw)
	}
	return ""
}

// pathFromRawURL extracts just the path component of a raw URL string, tolerating
// unparseable template URLs (e.g. containing "{{baseUrl}}").
func pathFromRawURL(raw string) string {
	r := raw
	if i := strings.IndexAny(r, "?#"); i >= 0 {
		r = r[:i]
	}
	if pu, err := url.Parse(r); err == nil && pu.Path != "" {
		return pu.Path
	}
	return ""
}

// postmanURLString returns a usable URL string for a structured Postman URL.
func postmanURLString(u pmURL) string {
	if u.Raw != "" {
		return u.Raw
	}
	return buildPostmanPath(u)
}

// postmanBaseURL looks for a collection variable that names a base URL/host.
func postmanBaseURL(vars []pmVariable) string {
	for _, v := range vars {
		switch strings.ToLower(strings.TrimSpace(v.Key)) {
		case "baseurl", "base_url", "url", "host", "server":
			if strings.TrimSpace(v.Value) != "" {
				return v.Value
			}
		}
	}
	return ""
}

// --- Postman v2.1 wire types --------------------------------------------------

type pmCollection struct {
	Info     pmInfo       `json:"info"`
	Item     []pmItem     `json:"item"`
	Auth     *pmAuth      `json:"auth"`
	Variable []pmVariable `json:"variable"`
}

type pmInfo struct {
	Name        string        `json:"name"`
	Schema      string        `json:"schema"`
	Description pmDescription `json:"description"`
}

type pmItem struct {
	Name     string       `json:"name"`
	Item     []pmItem     `json:"item"`
	Request  *pmRequest   `json:"request"`
	Response []pmResponse `json:"response"`
	Event    []pmEvent    `json:"event"`
	Auth     *pmAuth      `json:"auth"`
}

type pmRequest struct {
	Method      string        `json:"method"`
	Header      []pmHeader    `json:"header"`
	URL         pmURL         `json:"url"`
	Body        *pmBody       `json:"body"`
	Auth        *pmAuth       `json:"auth"`
	Description pmDescription `json:"description"`
}

// UnmarshalJSON tolerates the shorthand where "request" is a bare URL string.
func (r *pmRequest) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		r.URL = pmURL{Raw: s}
		return nil
	}
	type alias pmRequest
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*r = pmRequest(a)
	return nil
}

type pmURL struct {
	Raw      string       `json:"raw"`
	Path     pmPath       `json:"path"`
	Query    []pmQuery    `json:"query"`
	Variable []pmVariable `json:"variable"`
}

// UnmarshalJSON tolerates "url" being either a bare string or an object.
func (u *pmURL) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		u.Raw = s
		return nil
	}
	type alias pmURL
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*u = pmURL(a)
	return nil
}

// pmPath normalizes Postman's path, which may be a string or an array whose
// elements are either strings or {"value": "..."} objects (path variables).
type pmPath []string

func (p *pmPath) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		trimmed := strings.Trim(strings.TrimSpace(s), "/")
		if trimmed == "" {
			*p = nil
			return nil
		}
		*p = strings.Split(trimmed, "/")
		return nil
	}
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	out := make([]string, 0, len(raw))
	for _, r := range raw {
		var seg string
		if err := json.Unmarshal(r, &seg); err == nil {
			out = append(out, seg)
			continue
		}
		var obj struct {
			Value string `json:"value"`
		}
		if err := json.Unmarshal(r, &obj); err == nil {
			out = append(out, obj.Value)
		}
	}
	*p = out
	return nil
}

type pmQuery struct {
	Key      string `json:"key"`
	Value    string `json:"value"`
	Disabled bool   `json:"disabled"`
}

type pmVariable struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type pmHeader struct {
	Key      string `json:"key"`
	Value    string `json:"value"`
	Disabled bool   `json:"disabled"`
}

type pmBody struct {
	Mode       string `json:"mode"`
	Raw        string `json:"raw"`
	URLEncoded []pmKV `json:"urlencoded"`
	FormData   []pmKV `json:"formdata"`
}

type pmKV struct {
	Key      string `json:"key"`
	Value    string `json:"value"`
	Disabled bool   `json:"disabled"`
}

type pmAuth struct {
	Type string `json:"type"`
}

type pmEvent struct {
	Listen string   `json:"listen"`
	Script pmScript `json:"script"`
}

type pmScript struct {
	Type string `json:"type"`
	Exec pmExec `json:"exec"`
}

// pmExec tolerates a script body given as either a string or an array of lines.
type pmExec []string

func (e *pmExec) UnmarshalJSON(data []byte) error {
	var arr []string
	if err := json.Unmarshal(data, &arr); err == nil {
		*e = arr
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*e = []string{s}
		return nil
	}
	// Tolerate other shapes (null, object) by leaving the script empty.
	return nil
}

type pmResponse struct {
	Name   string     `json:"name"`
	Code   int        `json:"code"`
	Status string     `json:"status"`
	Header []pmHeader `json:"header"`
	Body   string     `json:"body"`
}

// pmDescription tolerates Postman's description being a string or a
// {"content": "...", "type": "..."} object.
type pmDescription string

func (d *pmDescription) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*d = pmDescription(s)
		return nil
	}
	var obj struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(data, &obj); err == nil {
		*d = pmDescription(obj.Content)
		return nil
	}
	return nil
}
