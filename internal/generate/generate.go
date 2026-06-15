// Package generate implements SRS Feature A: AI test generation. It turns a
// sample response, expected status, and endpoint metadata into executable,
// Newman-compatible Postman test scripts.
//
// Security (SRS §20): the example response body is treated as untrusted and may
// contain tokens or cookies. It is normalized and passed through
// [redact.String] before it is ever placed into an AI prompt.
package generate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/apilens/apilens/internal/core"
	"github.com/apilens/apilens/internal/core/ai"
	"github.com/apilens/apilens/internal/core/config"
	"github.com/apilens/apilens/internal/core/redact"
	"github.com/apilens/apilens/pkg/logging"
	"github.com/apilens/apilens/pkg/tui"
)

const (
	// defaultOutputPath is used when Options.OutputPath is empty.
	defaultOutputPath = "generated-tests.js"
	// defaultStatus is assumed when Options.Status is 0.
	defaultStatus = 200
	// spinnerLabel is shown while the AI call is in flight.
	spinnerLabel = "Generating tests with AI"
	// outputDirPerm / outputFilePerm govern created artifacts.
	outputDirPerm  = 0o755
	outputFilePerm = 0o644
)

// Options configures test generation; it mirrors the `apilens generate` flags.
type Options struct {
	ResponsePath string // --response: path to a sample response JSON file
	Endpoint     string // --endpoint: e.g. "GET /users"
	Status       int    // --status: expected HTTP status code
	OutputPath   string // --output: destination file (default generated-tests.js)
}

// Run is the feature entry point. It reads the example response, builds a
// redacted prompt, asks the AI to emit Postman/Newman test JavaScript, strips
// any markdown fences, prepends a header comment, and writes the result.
func Run(ctx context.Context, cfg config.Config, client *ai.Client, opts Options) error {
	// Defensive: the cmd layer guarantees a client for `generate`, but never
	// reach for an LLM without one.
	if client == nil {
		return fmt.Errorf("generate: AI client is nil; configure a provider and API key: %w", core.ErrMissingAPIKey)
	}

	method, path, err := parseEndpoint(opts.Endpoint)
	if err != nil {
		return fmt.Errorf("generate: %w", err)
	}

	status := opts.Status
	if status == 0 {
		status = defaultStatus
	}

	exampleResponse, err := readExampleResponse(opts.ResponsePath)
	if err != nil {
		return fmt.Errorf("generate: %w", err)
	}

	// SECURITY (SRS §20): redact the example body before it enters any prompt.
	redactedResponse := redact.String(exampleResponse)

	system := buildSystemPrompt()
	user := buildUserPrompt(method, path, status, redactedResponse)

	var generated string
	runErr := tui.RunWithSpinner(ctx, spinnerLabel, func(ctx context.Context) error {
		out, err := client.Generate(ctx, system, user)
		if err != nil {
			return err
		}
		generated = out
		return nil
	})
	if runErr != nil {
		return fmt.Errorf("generate: ai call: %w", runErr)
	}

	body := stripFences(generated)
	if body == "" {
		return fmt.Errorf("generate: AI returned an empty test script")
	}
	content := withHeader(body, method, path, status)

	outputPath := opts.OutputPath
	if strings.TrimSpace(outputPath) == "" {
		outputPath = defaultOutputPath
	}
	if err := writeOutput(outputPath, content); err != nil {
		return fmt.Errorf("generate: %w", err)
	}

	logging.L().Info().
		Str("output", outputPath).
		Str("endpoint", method+" "+path).
		Int("status", status).
		Msg("generated tests written")
	return nil
}

// readExampleResponse validates and reads the example response file, returning
// its normalized (pretty-printed when valid JSON) contents.
func readExampleResponse(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("response path is required (use --response)")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read response file %q: %w", path, err)
	}
	return normalizeJSON(raw), nil
}

// writeOutput creates the parent directory (if any) and writes content.
func writeOutput(outputPath, content string) error {
	if dir := filepath.Dir(outputPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, outputDirPerm); err != nil {
			return fmt.Errorf("create output dir %q: %w", dir, err)
		}
	}
	if err := os.WriteFile(outputPath, []byte(content), outputFilePerm); err != nil {
		return fmt.Errorf("write %q: %w", outputPath, err)
	}
	return nil
}
