package parser

import (
	"path/filepath"
	"testing"

	"github.com/apilens/apilens/internal/core"
)

const openapiYAML = `openapi: 3.0.0
info:
  title: Petstore
  description: A sample API
servers:
  - url: https://api.petstore.com
paths:
  /pets:
    parameters:
      - name: X-Trace-Id
        in: header
        schema:
          type: string
    get:
      operationId: listPets
      summary: List pets
      parameters:
        - name: limit
          in: query
          schema:
            type: integer
      security:
        - apiKeyAuth: []
      responses:
        '200':
          description: ok
        default:
          description: error
    post:
      summary: Create a pet
      responses:
        '201':
          description: created
  /pets/{id}:
    get:
      security:
        - bearerAuth: []
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        '200':
          description: ok
  /public:
    get:
      security: []
      responses:
        '200':
          description: ok
components:
  securitySchemes:
    apiKeyAuth:
      type: apiKey
      in: header
      name: X-API-Key
    bearerAuth:
      type: http
      scheme: bearer
security:
  - apiKeyAuth: []
`

func TestParseOpenAPI(t *testing.T) {
	path := writeTemp(t, "spec.yaml", openapiYAML)
	col, err := ParseOpenAPI(path)
	if err != nil {
		t.Fatalf("ParseOpenAPI: %v", err)
	}

	if col.Name != "Petstore" || col.Description != "A sample API" {
		t.Errorf("name/desc = %q / %q", col.Name, col.Description)
	}
	if col.Source != core.SourceOpenAPI {
		t.Errorf("source = %q", col.Source)
	}
	if col.BaseURL != "https://api.petstore.com" {
		t.Errorf("baseURL = %q", col.BaseURL)
	}
	if len(col.Endpoints) != 4 {
		t.Fatalf("expected 4 endpoints, got %d: %+v", len(col.Endpoints), col.Endpoints)
	}

	// Deterministic order: sorted by path, then method.
	getPets := col.Endpoints[0]
	if getPets.ID != "GET /pets" || getPets.Name != "listPets" {
		t.Errorf("ep0 id/name = %q / %q", getPets.ID, getPets.Name)
	}
	if !getPets.HasAuth || getPets.AuthType != "apikey" {
		t.Errorf("GET /pets auth = %v / %q, want apikey", getPets.HasAuth, getPets.AuthType)
	}
	// Path-level (header) + operation-level (query) params merged.
	if !hasParam(getPets.QueryParams, "X-Trace-Id", core.InHeader) {
		t.Errorf("missing merged header param: %v", getPets.QueryParams)
	}
	if !hasParam(getPets.QueryParams, "limit", core.InQuery) {
		t.Errorf("missing query param: %v", getPets.QueryParams)
	}
	// 200 + non-numeric "default".
	if !hasResponse(getPets.Responses, 200) || !hasResponseName(getPets.Responses, "default") {
		t.Errorf("responses = %+v", getPets.Responses)
	}

	postPets := col.Endpoints[1]
	if postPets.ID != "POST /pets" || postPets.Name != "POST /pets" {
		t.Errorf("ep1 id/name = %q / %q (operationId absent -> method+path)", postPets.ID, postPets.Name)
	}
	if postPets.Description != "Create a pet" {
		t.Errorf("description should fall back to summary, got %q", postPets.Description)
	}
	// No op-level security -> inherits global security (apiKeyAuth).
	if !postPets.HasAuth || postPets.AuthType != "apikey" {
		t.Errorf("POST /pets should inherit global apikey auth, got %v / %q", postPets.HasAuth, postPets.AuthType)
	}

	getByID := col.Endpoints[2]
	if getByID.ID != "GET /pets/{id}" {
		t.Errorf("ep2 id = %q", getByID.ID)
	}
	if !getByID.HasAuth || getByID.AuthType != "bearer" {
		t.Errorf("GET /pets/{id} auth = %v / %q, want bearer", getByID.HasAuth, getByID.AuthType)
	}
	if !hasParam(getByID.QueryParams, "id", core.InPath) {
		t.Errorf("missing path param id: %v", getByID.QueryParams)
	}

	public := col.Endpoints[3]
	if public.HasAuth {
		t.Error("GET /public with empty security should have HasAuth=false")
	}
}

func TestParseOpenAPI_Errors(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		if _, err := ParseOpenAPI(filepath.Join(t.TempDir(), "nope.yaml")); err == nil {
			t.Error("expected error for missing spec")
		}
	})
}

func hasParam(ps []core.Param, name, in string) bool {
	for _, p := range ps {
		if p.Name == name && p.In == in {
			return true
		}
	}
	return false
}

func hasResponse(rs []core.ExampleResponse, status int) bool {
	for _, r := range rs {
		if r.Status == status {
			return true
		}
	}
	return false
}

func hasResponseName(rs []core.ExampleResponse, name string) bool {
	for _, r := range rs {
		if r.Name == name {
			return true
		}
	}
	return false
}
