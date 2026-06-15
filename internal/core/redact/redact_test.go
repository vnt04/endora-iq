package redact

import (
	"strings"
	"testing"

	"github.com/apilens/apilens/internal/core"
)

func TestIsSensitiveHeader(t *testing.T) {
	tests := []struct {
		key  string
		want bool
	}{
		{"Authorization", true},
		{"authorization", true},
		{"  X-Api-Key ", true},
		{"Cookie", true},
		{"Set-Cookie", true},
		{"Content-Type", false},
		{"Accept", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := IsSensitiveHeader(tt.key); got != tt.want {
			t.Errorf("IsSensitiveHeader(%q) = %v, want %v", tt.key, got, tt.want)
		}
	}
}

func TestIsSensitiveName(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"password", true},
		{"api_key", true},
		{"apiKey", true},
		{"access-token", true},
		{"client_secret", true},
		{"sessionId", true},
		{"username", false},
		{"limit", false},
		{"id", false},
	}
	for _, tt := range tests {
		if got := IsSensitiveName(tt.name); got != tt.want {
			t.Errorf("IsSensitiveName(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestString(t *testing.T) {
	tests := []struct {
		name      string
		in        string
		mustMask  []string // substrings that must NOT appear in output
		keepParts []string // substrings that MUST appear (context preserved)
		wantMask  bool     // expect the Mask token in output
	}{
		{
			name:      "empty unchanged",
			in:        "",
			wantMask:  false,
			keepParts: nil,
		},
		{
			name:      "no secrets unchanged",
			in:        "GET /users returned 200",
			wantMask:  false,
			keepParts: []string{"GET /users returned 200"},
		},
		{
			name:      "authorization header line",
			in:        "Authorization: Bearer abcdef123456",
			mustMask:  []string{"abcdef123456"},
			keepParts: []string{"Authorization:"},
			wantMask:  true,
		},
		{
			name:      "bearer scheme keeps word",
			in:        "token is Bearer secretToken9999",
			mustMask:  []string{"secretToken9999"},
			keepParts: []string{"Bearer"},
			wantMask:  true,
		},
		{
			name:      "basic scheme",
			in:        "Basic dXNlcjpwYXNzd29yZA==",
			mustMask:  []string{"dXNlcjpwYXNzd29yZA"},
			keepParts: []string{"Basic"},
			wantMask:  true,
		},
		{
			name:      "jwt token",
			in:        "got eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJhZG1pbiJ9.s3cr3tSig now",
			mustMask:  []string{"eyJhbGciOiJIUzI1NiJ9", "s3cr3tSig"},
			keepParts: []string{"got", "now"},
			wantMask:  true,
		},
		{
			name:      "json secret value",
			in:        `{"username":"bob","password":"hunter2"}`,
			mustMask:  []string{"hunter2"},
			keepParts: []string{`"username":"bob"`, `"password":"`},
			wantMask:  true,
		},
		{
			name:      "url query secret",
			in:        "https://api.test/cb?token=abc123&user=bob",
			mustMask:  []string{"abc123"},
			keepParts: []string{"user=bob", "token="},
			wantMask:  true,
		},
		{
			name:      "kv secret in free text",
			in:        "api_key=supersecretvalue and more text",
			mustMask:  []string{"supersecretvalue"},
			keepParts: []string{"api_key=", "and more text"},
			wantMask:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := String(tt.in)
			if tt.in == "" && got != "" {
				t.Fatalf("empty input should yield empty, got %q", got)
			}
			for _, m := range tt.mustMask {
				if strings.Contains(got, m) {
					t.Errorf("secret %q leaked in output: %q", m, got)
				}
			}
			for _, k := range tt.keepParts {
				if !strings.Contains(got, k) {
					t.Errorf("expected context %q preserved in output: %q", k, got)
				}
			}
			if tt.wantMask && !strings.Contains(got, Mask) {
				t.Errorf("expected mask token in output: %q", got)
			}
		})
	}
}

func TestMaskScheme_EmptyMatch(t *testing.T) {
	// Defensive branch: an empty match yields just the Mask.
	if got := maskScheme(""); got != Mask {
		t.Errorf("maskScheme(\"\") = %q, want %q", got, Mask)
	}
	if got := maskScheme("Bearer xyz"); got != "Bearer "+Mask {
		t.Errorf("maskScheme = %q, want %q", got, "Bearer "+Mask)
	}
}

func TestHeaders(t *testing.T) {
	if Headers(nil) != nil {
		t.Error("nil input should yield nil")
	}

	in := []core.Header{
		{Key: "Authorization", Value: "Bearer abc123"},
		{Key: "Content-Type", Value: "application/json"},
		{Key: "X-Trace", Value: "Bearer leakedToken123"},
	}
	out := Headers(in)

	if out[0].Value != Mask {
		t.Errorf("sensitive header not fully masked: %q", out[0].Value)
	}
	if out[1].Value != "application/json" {
		t.Errorf("benign header altered: %q", out[1].Value)
	}
	// Non-sensitive header still scrubbed for embedded secrets.
	if strings.Contains(out[2].Value, "leakedToken123") {
		t.Errorf("embedded secret leaked: %q", out[2].Value)
	}
	// Input must not be mutated.
	if in[0].Value != "Bearer abc123" {
		t.Errorf("input header mutated: %q", in[0].Value)
	}
}

func TestEndpoint_RedactsAndDoesNotMutate(t *testing.T) {
	in := core.Endpoint{
		ID:     "POST /login",
		Method: "POST",
		Path:   "/login",
		RawURL: "https://api.test/login?token=raw-secret-token",
		Headers: []core.Header{
			{Key: "Authorization", Value: "Bearer topsecret"},
		},
		QueryParams: []core.Param{
			{Name: "password", In: core.InQuery, Value: "p@ss"},
			{Name: "limit", In: core.InQuery, Value: "10"},
		},
		PathParams: []core.Param{
			{Name: "id", In: core.InPath, Value: "42"},
		},
		RequestBody: `{"password":"hunter2","name":"bob"}`,
		Description: "uses api_key=descsecret internally",
		TestScripts: []string{`pm.environment.set("token", "eyJhbGciOiJ.eyJ.sig")`},
		Responses: []core.ExampleResponse{
			{
				Name:    "OK",
				Status:  200,
				Body:    `{"access_token":"resp-secret"}`,
				Headers: []core.Header{{Key: "Set-Cookie", Value: "sid=abc"}},
			},
		},
	}

	out := Endpoint(in)

	if out.Headers[0].Value != Mask {
		t.Errorf("header not masked: %q", out.Headers[0].Value)
	}
	if out.QueryParams[0].Value != Mask {
		t.Errorf("sensitive query param not masked: %q", out.QueryParams[0].Value)
	}
	if out.QueryParams[1].Value != "10" {
		t.Errorf("benign query param altered: %q", out.QueryParams[1].Value)
	}
	if strings.Contains(out.RawURL, "raw-secret-token") {
		t.Errorf("raw URL secret leaked: %q", out.RawURL)
	}
	if strings.Contains(out.RequestBody, "hunter2") {
		t.Errorf("body secret leaked: %q", out.RequestBody)
	}
	if strings.Contains(out.Description, "descsecret") {
		t.Errorf("description secret leaked: %q", out.Description)
	}
	if strings.Contains(out.TestScripts[0], "eyJhbGciOiJ") {
		t.Errorf("test script JWT leaked: %q", out.TestScripts[0])
	}
	if strings.Contains(out.Responses[0].Body, "resp-secret") {
		t.Errorf("response body secret leaked: %q", out.Responses[0].Body)
	}
	if out.Responses[0].Headers[0].Value != Mask {
		t.Errorf("response header not masked: %q", out.Responses[0].Headers[0].Value)
	}

	// Original input is untouched (immutability).
	if in.Headers[0].Value != "Bearer topsecret" {
		t.Errorf("input mutated: header = %q", in.Headers[0].Value)
	}
	if in.QueryParams[0].Value != "p@ss" {
		t.Errorf("input mutated: query = %q", in.QueryParams[0].Value)
	}
}

func TestEndpoint_NilSlicesStayNil(t *testing.T) {
	out := Endpoint(core.Endpoint{Method: "GET", Path: "/x"})
	if out.Headers != nil || out.QueryParams != nil || out.PathParams != nil {
		t.Error("expected nil slices to remain nil")
	}
	if out.Responses != nil || out.TestScripts != nil {
		t.Error("expected nil Responses/TestScripts to remain nil")
	}
}

func TestCollection(t *testing.T) {
	if Collection(nil) != nil {
		t.Error("nil input should yield nil")
	}

	in := &core.Collection{
		Name:        "Demo",
		Description: "api_key=col-secret-value",
		Endpoints: []core.Endpoint{
			{Method: "GET", Path: "/a", Headers: []core.Header{{Key: "Api-Key", Value: "k"}}},
		},
	}
	out := Collection(in)

	if out == in {
		t.Error("expected a new collection, got same pointer")
	}
	if out.Name != "Demo" {
		t.Errorf("name altered: %q", out.Name)
	}
	if strings.Contains(out.Description, "col-secret-value") {
		t.Errorf("description secret leaked: %q", out.Description)
	}
	if out.Endpoints[0].Headers[0].Value != Mask {
		t.Errorf("endpoint header not masked: %q", out.Endpoints[0].Headers[0].Value)
	}
	// Input deep structure not mutated.
	if in.Endpoints[0].Headers[0].Value != "k" {
		t.Errorf("input endpoint mutated: %q", in.Endpoints[0].Headers[0].Value)
	}
	if strings.Contains(in.Description, Mask) {
		t.Errorf("input description mutated: %q", in.Description)
	}
}
