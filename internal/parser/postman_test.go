package parser

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/apilens/apilens/internal/core"
)

const postmanCollection = `{
  "info": {
    "name": "Demo API",
    "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json",
    "description": {"content": "A demo collection", "type": "text/plain"}
  },
  "variable": [{"key": "baseUrl", "value": "https://api.example.com"}],
  "auth": {"type": "bearer"},
  "item": [
    {
      "name": "Users",
      "item": [
        {
          "name": "Get user",
          "request": {
            "method": "get",
            "header": [
              {"key": "X-Api-Key", "value": "secret"},
              {"key": "X-Off", "value": "x", "disabled": true}
            ],
            "url": {
              "raw": "{{baseUrl}}/users/:id?active=true",
              "path": ["users", ":id"],
              "query": [
                {"key": "active", "value": "true"},
                {"key": "off", "value": "1", "disabled": true}
              ],
              "variable": [{"key": "id", "value": "1"}]
            },
            "body": {"mode": "raw", "raw": "{\"name\":\"x\"}"}
          },
          "event": [
            {"listen": "prerequest", "script": {"exec": ["console.log('pre')"]}},
            {"listen": "test", "script": {"exec": ["pm.test('ok', function(){ pm.response.to.have.status(200); });"]}}
          ],
          "response": [
            {"name": "OK", "code": 200, "body": "{}", "header": [{"key": "Content-Type", "value": "application/json"}]}
          ]
        }
      ]
    },
    {
      "name": "Create user",
      "request": {
        "method": "POST",
        "url": {"raw": "https://api.example.com/users", "path": "users"},
        "body": {"mode": "urlencoded", "urlencoded": [{"key": "a", "value": "1"}, {"key": "b", "value": "2", "disabled": true}]},
        "auth": {"type": "noauth"}
      }
    },
    {
      "name": "Health (string request)",
      "request": "https://api.example.com/health"
    }
  ]
}`

func TestParsePostman(t *testing.T) {
	path := writeTemp(t, "collection.json", postmanCollection)
	col, err := ParsePostman(path)
	if err != nil {
		t.Fatalf("ParsePostman: %v", err)
	}

	if col.Name != "Demo API" {
		t.Errorf("name = %q", col.Name)
	}
	if col.Source != core.SourcePostman {
		t.Errorf("source = %q", col.Source)
	}
	if col.BaseURL != "https://api.example.com" {
		t.Errorf("baseURL = %q", col.BaseURL)
	}
	if col.Description != "A demo collection" {
		t.Errorf("description = %q (object form should resolve to content)", col.Description)
	}
	if len(col.Endpoints) != 3 {
		t.Fatalf("expected 3 endpoints, got %d", len(col.Endpoints))
	}

	get := col.Endpoints[0]
	if get.Method != "GET" {
		t.Errorf("method = %q, want GET (normalized)", get.Method)
	}
	if get.Path != "/users/:id" {
		t.Errorf("path = %q, want /users/:id", get.Path)
	}
	if get.ID != "GET /users/:id" {
		t.Errorf("id = %q", get.ID)
	}
	if !get.HasAuth || get.AuthType != "bearer" {
		t.Errorf("expected inherited bearer auth, got hasAuth=%v type=%q", get.HasAuth, get.AuthType)
	}
	if len(get.Tags) != 1 || get.Tags[0] != "Users" {
		t.Errorf("tags = %v, want [Users]", get.Tags)
	}
	if len(get.Headers) != 1 || get.Headers[0].Key != "X-Api-Key" {
		t.Errorf("headers = %v (disabled header should be dropped)", get.Headers)
	}
	if len(get.QueryParams) != 1 || get.QueryParams[0].Name != "active" {
		t.Errorf("query = %v (disabled query should be dropped)", get.QueryParams)
	}
	if len(get.PathParams) != 1 || get.PathParams[0].Name != "id" {
		t.Errorf("pathParams = %v", get.PathParams)
	}
	if get.RequestBody != `{"name":"x"}` {
		t.Errorf("body = %q", get.RequestBody)
	}
	if len(get.TestScripts) != 1 {
		t.Errorf("expected 1 test script (only 'test' listener), got %d", len(get.TestScripts))
	}
	if len(get.Responses) != 1 || get.Responses[0].Status != 200 {
		t.Errorf("responses = %v", get.Responses)
	}

	create := col.Endpoints[1]
	if create.Method != "POST" || create.Path != "/users" {
		t.Errorf("create = %s %s", create.Method, create.Path)
	}
	if create.HasAuth {
		t.Error("noauth should mean HasAuth=false")
	}
	if create.RequestBody != "a=1" {
		t.Errorf("urlencoded body = %q, want a=1 (disabled dropped)", create.RequestBody)
	}

	health := col.Endpoints[2]
	if health.Method != "GET" {
		t.Errorf("string-request method = %q, want GET default", health.Method)
	}
	if health.Path != "/health" {
		t.Errorf("string-request path = %q, want /health", health.Path)
	}
}

func TestParsePostman_FormData(t *testing.T) {
	const c = `{
	  "info": {"name": "F", "schema": "collection"},
	  "item": [{"name": "Upload", "request": {"method": "POST", "url": {"raw": "/upload"}, "body": {"mode": "formdata", "formdata": [{"key": "file", "value": "x"}]}}}]
	}`
	path := writeTemp(t, "f.json", c)
	col, err := ParsePostman(path)
	if err != nil {
		t.Fatalf("ParsePostman: %v", err)
	}
	if col.Endpoints[0].RequestBody != "file=x" {
		t.Errorf("formdata body = %q", col.Endpoints[0].RequestBody)
	}
}

func TestParsePostman_StringExecScript(t *testing.T) {
	const c = `{
	  "info": {"name": "S", "schema": "collection"},
	  "item": [{"name": "T", "request": {"method": "GET", "url": "/t"}, "event": [{"listen": "test", "script": {"exec": "pm.test('x');"}}]}]
	}`
	path := writeTemp(t, "s.json", c)
	col, err := ParsePostman(path)
	if err != nil {
		t.Fatalf("ParsePostman: %v", err)
	}
	if len(col.Endpoints[0].TestScripts) != 1 {
		t.Errorf("expected 1 script from string exec, got %d", len(col.Endpoints[0].TestScripts))
	}
}

func TestParsePostman_Errors(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		if _, err := ParsePostman(filepath.Join(t.TempDir(), "nope.json")); err == nil {
			t.Error("expected error for missing file")
		}
	})
	t.Run("malformed json", func(t *testing.T) {
		path := writeTemp(t, "bad.json", `{"info":`)
		_, err := ParsePostman(path)
		if !errors.Is(err, core.ErrUnsupportedFormat) {
			t.Errorf("expected ErrUnsupportedFormat, got %v", err)
		}
	})
}
