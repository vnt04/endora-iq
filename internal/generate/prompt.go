package generate

import "fmt"

// buildSystemPrompt returns the system prompt. It constrains the model to emit
// only runnable JavaScript for the Postman Tests tab (also Newman-compatible),
// with no markdown and no prose.
func buildSystemPrompt() string {
	return "You are an expert API test engineer. " +
		"Output ONLY JavaScript for the Postman Tests tab (also Newman-compatible). " +
		"Use pm.test, pm.expect, and pm.response. " +
		"Do not wrap the code in markdown code fences. No markdown, no prose, no explanations."
}

// buildUserPrompt embeds the endpoint, expected status, and the already-redacted
// example response, and asks for the full battery of assertions required by SRS
// §9 plus a trailing block of edge-case suggestions written as comments.
func buildUserPrompt(method, path string, status int, redactedResponse string) string {
	return fmt.Sprintf(`Generate Postman/Newman test scripts for the following endpoint.

Endpoint: %s %s
Expected HTTP status: %d

Example response body (secrets already redacted — DO NOT invent or expect any redacted value):
%s

Produce pm.test blocks that cover, where applicable to the response above:
- Status code validation (expect HTTP %d).
- Response time validation (e.g. assert pm.response.responseTime is below a sane threshold).
- Required-fields validation (every top-level field present in the example).
- Schema validation (overall shape of the JSON body).
- Type validation (each field has the expected JS type).
- Format validation (email, uuid, and date/ISO-8601 string formats where the value looks like one).
- Null validation (fields that must not be null).
- Nested-object validation (assert nested object fields).
- Array validation (length and per-element checks for any arrays).

After the runnable tests, append a clearly separated block of edge-case test
suggestions written ONLY as // comments (do not implement them), for example:
empty body, missing required fields, invalid auth, boundary values, large
payloads, and unexpected/extra fields.

Return ONLY JavaScript. Do not include markdown fences or prose.`,
		method, path, status, redactedResponse, status)
}
