// Package redact implements the ApiLens security layer (SRS §20): masking of
// secrets before any data is placed into an AI prompt.
//
// Nothing in this package mutates its input. Every function returns a new value
// (or a deep copy) with secret material replaced by [Mask]. The surrounding key
// or scheme is preserved where possible so that the redacted output still
// carries enough context to be useful to an LLM ("Authorization: ***REDACTED***"
// rather than just "***REDACTED***").
//
// This is security-critical code: it favors over-redaction over leaking. When
// in doubt a value is masked.
package redact

import (
	"regexp"
	"strings"

	"github.com/apilens/apilens/internal/core"
)

// Mask is the replacement placeholder for any redacted secret value.
const Mask = "***REDACTED***"

var (
	// HTTP header lines whose value should be fully masked. Matches at the
	// start of a line so it catches headers embedded in raw request text.
	reHeaderLine = regexp.MustCompile(`(?im)^(\s*(?:authorization|proxy-authorization|www-authenticate|x-api-key|api-key|apikey|x-auth-token|x-access-token|x-session-token|x-csrf-token|x-amz-security-token|cookie|set-cookie)\s*[:=]\s*).+$`)

	// Auth scheme + credential, e.g. "Bearer eyJ...", "Basic dXNlcjpwYXNz".
	reScheme = regexp.MustCompile(`(?i)\b(bearer|basic)\s+[A-Za-z0-9._~+/\-=]{3,}`)

	// JSON Web Tokens (header.payload[.signature]).
	reJWT = regexp.MustCompile(`eyJ[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+(?:\.[A-Za-z0-9_\-]+)?`)

	// Secret-bearing JSON string values: {"password": "...", "api_key": "..."}.
	reJSONSecret = regexp.MustCompile(`(?i)("[a-z0-9_\-]*(?:password|passwd|pwd|secret|token|api[_-]?key|apikey|auth|credential|access[_-]?token|refresh[_-]?token|client[_-]?secret|private[_-]?key)[a-z0-9_\-]*"\s*:\s*")[^"]*(")`)

	// Secret-bearing URL query parameters: ?token=...&api_key=...
	reURLSecret = regexp.MustCompile(`(?i)([?&][a-z0-9_\-]*(?:password|passwd|pwd|secret|token|api[_-]?key|apikey|auth|access[_-]?token|refresh[_-]?token|client[_-]?secret|signature|sig|key)[a-z0-9_\-]*=)[^&#\s"']+`)

	// Secret-bearing key/value pairs in free text: password=..., api_key: ...
	reKVSecret = regexp.MustCompile(`(?i)\b((?:password|passwd|pwd|secret|api[_-]?key|apikey|access[_-]?token|refresh[_-]?token|client[_-]?secret|auth[_-]?token)\s*[:=]\s*)[^\s,;&"']+`)

	// Parameter/field names considered sensitive on their own.
	reSensitiveName = regexp.MustCompile(`(?i)(password|passwd|pwd|secret|token|api[_-]?key|apikey|auth|access[_-]?token|refresh[_-]?token|client[_-]?secret|signature|sig|apikey|sessionid|session[_-]?id)`)
)

// sensitiveHeaders is the set of header names whose value is always masked
// (lower-cased for case-insensitive lookup).
var sensitiveHeaders = map[string]struct{}{
	"authorization":        {},
	"proxy-authorization":  {},
	"www-authenticate":     {},
	"x-api-key":            {},
	"api-key":              {},
	"apikey":               {},
	"x-auth-token":         {},
	"x-access-token":       {},
	"x-session-token":      {},
	"x-csrf-token":         {},
	"x-xsrf-token":         {},
	"x-amz-security-token": {},
	"cookie":               {},
	"set-cookie":           {},
	"token":                {},
}

// IsSensitiveHeader reports whether a header name should have its value masked.
func IsSensitiveHeader(key string) bool {
	_, ok := sensitiveHeaders[strings.ToLower(strings.TrimSpace(key))]
	return ok
}

// IsSensitiveName reports whether a parameter/field name looks like it carries
// a secret.
func IsSensitiveName(name string) bool {
	return reSensitiveName.MatchString(name)
}

// String masks secrets found anywhere in a free-form string: auth header lines,
// "Bearer"/"Basic" credentials, JWTs, secret-bearing JSON values, URL query
// secrets, and key=value secrets. The surrounding key/scheme is preserved.
func String(s string) string {
	if s == "" {
		return s
	}
	s = reHeaderLine.ReplaceAllString(s, "${1}"+Mask)
	s = reScheme.ReplaceAllStringFunc(s, maskScheme)
	s = reJWT.ReplaceAllString(s, Mask)
	s = reJSONSecret.ReplaceAllString(s, "${1}"+Mask+"${2}")
	s = reURLSecret.ReplaceAllString(s, "${1}"+Mask)
	s = reKVSecret.ReplaceAllString(s, "${1}"+Mask)
	return s
}

// maskScheme preserves the auth scheme word ("Bearer"/"Basic") and masks the
// credential that follows it.
func maskScheme(match string) string {
	fields := strings.Fields(match)
	if len(fields) == 0 {
		return Mask
	}
	return fields[0] + " " + Mask
}

// Headers returns a new slice in which sensitive header values are fully masked
// and all other values are passed through [String] (to catch embedded secrets).
// A nil input yields a nil output.
func Headers(h []core.Header) []core.Header {
	if h == nil {
		return nil
	}
	out := make([]core.Header, len(h))
	for i, hd := range h {
		out[i] = hd
		if IsSensitiveHeader(hd.Key) {
			out[i].Value = Mask
		} else {
			out[i].Value = String(hd.Value)
		}
	}
	return out
}

// params returns a new slice with secret values masked.
func params(ps []core.Param) []core.Param {
	if ps == nil {
		return nil
	}
	out := make([]core.Param, len(ps))
	for i, p := range ps {
		out[i] = p
		if IsSensitiveName(p.Name) {
			out[i].Value = Mask
		} else {
			out[i].Value = String(p.Value)
		}
	}
	return out
}

// Endpoint returns a new Endpoint with headers, params, request body, raw URL,
// description, example responses, and test scripts redacted. The input is not
// modified.
func Endpoint(e core.Endpoint) core.Endpoint {
	out := e
	out.Headers = Headers(e.Headers)
	out.QueryParams = params(e.QueryParams)
	out.PathParams = params(e.PathParams)
	out.RequestBody = String(e.RequestBody)
	out.RawURL = String(e.RawURL)
	out.Description = String(e.Description)

	if e.Responses != nil {
		rs := make([]core.ExampleResponse, len(e.Responses))
		for i, r := range e.Responses {
			rs[i] = r
			rs[i].Headers = Headers(r.Headers)
			rs[i].Body = String(r.Body)
		}
		out.Responses = rs
	}

	if e.TestScripts != nil {
		ts := make([]string, len(e.TestScripts))
		for i, t := range e.TestScripts {
			ts[i] = String(t)
		}
		out.TestScripts = ts
	}

	return out
}

// Collection returns a new, deeply redacted copy of c. The input collection and
// its endpoints are never mutated. A nil input yields nil.
func Collection(c *core.Collection) *core.Collection {
	if c == nil {
		return nil
	}
	out := *c
	out.Description = String(c.Description)
	// BaseURL can embed credentials (e.g. https://host/api?api_key=...); redact it
	// so callers that emit it — such as the docs OpenAPI server URL — never leak.
	out.BaseURL = String(c.BaseURL)
	if c.Endpoints != nil {
		eps := make([]core.Endpoint, len(c.Endpoints))
		for i, e := range c.Endpoints {
			eps[i] = Endpoint(e)
		}
		out.Endpoints = eps
	}
	return &out
}
