package parser

import (
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

func TestNormalizeSchemeType(t *testing.T) {
	tests := []struct {
		name   string
		typ    string
		scheme string
		want   string
	}{
		{"http bearer", "http", "bearer", "bearer"},
		{"http basic", "http", "basic", "basic"},
		{"http digest", "http", "digest", "http"},
		{"apikey", "apiKey", "", "apikey"},
		{"oauth2", "oauth2", "", "oauth2"},
		{"openidconnect", "openIdConnect", "", "oidc"},
		{"unknown", "mutualTLS", "", "mutualtls"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeSchemeType(&openapi3.SecurityScheme{Type: tt.typ, Scheme: tt.scheme})
			if got != tt.want {
				t.Errorf("normalizeSchemeType(%q/%q) = %q, want %q", tt.typ, tt.scheme, got, tt.want)
			}
		})
	}
}

func TestParsePostman_PathArrayOfObjects(t *testing.T) {
	// url.path as an array mixing plain strings and {"value": "..."} entries,
	// and no raw URL (postmanURLString falls back to the built path).
	const c = `{
	  "info": {"name": "P", "schema": "collection"},
	  "item": [{"name": "Get", "request": {"method": "GET", "url": {"path": ["users", {"value": ":id"}]}}}]
	}`
	path := writeTemp(t, "p.json", c)
	col, err := ParsePostman(path)
	if err != nil {
		t.Fatalf("ParsePostman: %v", err)
	}
	if got := col.Endpoints[0].Path; got != "/users/:id" {
		t.Errorf("path = %q, want /users/:id", got)
	}
}

func TestPathFromRawURL_NoPath(t *testing.T) {
	// A URL that is only a fragment/query has no path component -> "".
	if got := pathFromRawURL("#section"); got != "" {
		t.Errorf("pathFromRawURL(\"#section\") = %q, want empty", got)
	}
	if got := pathFromRawURL("?a=1"); got != "" {
		t.Errorf("pathFromRawURL(\"?a=1\") = %q, want empty", got)
	}
}
