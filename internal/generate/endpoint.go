package generate

import (
	"fmt"
	"strings"
)

// defaultMethod is assumed when the endpoint specification carries only a path.
const defaultMethod = "GET"

// httpMethods is the set of recognized HTTP verbs, used to validate the method
// token when both a method and a path are supplied.
var httpMethods = map[string]struct{}{
	"GET":     {},
	"HEAD":    {},
	"POST":    {},
	"PUT":     {},
	"PATCH":   {},
	"DELETE":  {},
	"OPTIONS": {},
	"TRACE":   {},
	"CONNECT": {},
}

// parseEndpoint splits an endpoint specification into an upper-cased HTTP method
// and a leading-slash path.
//
// Rules:
//   - "GET /users"  -> ("GET", "/users")
//   - "POST /a/b"   -> ("POST", "/a/b")
//   - case-insensitive method: "post /a" -> ("POST", "/a")
//   - surrounding whitespace is ignored.
//   - a single token is treated as the PATH, with the method defaulting to GET:
//     "/users" -> ("GET", "/users"); "users" -> ("GET", "/users").
//   - the path is normalized to start with "/".
//   - an empty spec, or a two-token spec whose first token is not a known HTTP
//     verb, is an error.
func parseEndpoint(spec string) (method, path string, err error) {
	fields := strings.Fields(spec)
	switch len(fields) {
	case 0:
		return "", "", fmt.Errorf("empty endpoint specification (expected e.g. %q)", "GET /users")
	case 1:
		return defaultMethod, normalizePath(fields[0]), nil
	default:
		m := strings.ToUpper(fields[0])
		if _, ok := httpMethods[m]; !ok {
			return "", "", fmt.Errorf("invalid HTTP method %q in endpoint %q", fields[0], spec)
		}
		return m, normalizePath(fields[1]), nil
	}
}

// normalizePath ensures the path begins with a single leading slash.
func normalizePath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return "/"
	}
	if !strings.HasPrefix(p, "/") {
		return "/" + p
	}
	return p
}
