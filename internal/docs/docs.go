// Package docs implements SRS Feature E: the documentation generator. It
// converts a Postman collection or OpenAPI spec into branded HTML docs via the
// pipeline Collection -> OpenAPI -> Redoc. External Node tooling
// (postman-to-openapi, @redocly/cli / redoc-cli) is used when present; when it
// is missing or fails, robust built-in fallbacks guarantee a valid openapi.yaml
// and a self-contained, CDN-backed index.html. This feature does NOT use AI;
// callers pass a nil ai.Client.
package docs

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/apilens/apilens/internal/core"
	"github.com/apilens/apilens/internal/core/ai"
	"github.com/apilens/apilens/internal/core/config"
	"github.com/apilens/apilens/internal/core/redact"
	"github.com/apilens/apilens/internal/parser"
	"github.com/apilens/apilens/pkg/logging"
)

// Output and rendering defaults.
const (
	defaultOpenAPIFile = "openapi.yaml"
	defaultIndexFile   = "index.html"

	// redocCDN is the standalone Redoc bundle used by the HTML fallback.
	redocCDN = "https://cdn.redocly.com/redoc/latest/bundles/redoc.standalone.js"

	// openAPIVersion / defaultAPIVersion seed the built-in minimal emitter.
	openAPIVersion    = "3.0.3"
	defaultAPIVersion = "1.0.0"

	// defaultThemeColor is the Redoc primary color used when --theme is empty
	// or not a safe CSS color token.
	defaultThemeColor = "#1976d2"

	defaultTitle = "API Documentation"
)

// Options configures documentation generation; it mirrors the `apilens docs`
// flags.
type Options struct {
	InputPath string // positional arg: collection or spec path
	OutDir    string // --out-dir: output directory (default docs)
	Title     string // --title
	Logo      string // --logo
	Theme     string // --theme (Redoc primary color)
	Deploy    bool   // --deploy: publish to GitHub Pages
}

// Run is the feature entry point. It detects the input format, produces
// <OutDir>/openapi.yaml (via postman-to-openapi or the built-in converter, or by
// normalizing an existing spec) and <OutDir>/index.html (via Redoc tooling or
// the CDN fallback), and optionally writes a GitHub Pages workflow. The ai
// client is unused.
func Run(ctx context.Context, cfg config.Config, _ *ai.Client, opts Options) error {
	log := logging.L()

	if strings.TrimSpace(opts.InputPath) == "" {
		return fmt.Errorf("docs: input path is required")
	}

	outDir := strings.TrimSpace(opts.OutDir)
	if outDir == "" {
		outDir = strings.TrimSpace(cfg.DocsDir)
	}
	if outDir == "" {
		outDir = "docs"
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("docs: create output dir %q: %w", outDir, err)
	}

	col, err := parser.Parse(opts.InputPath)
	if err != nil {
		return fmt.Errorf("docs: parse input %q: %w", opts.InputPath, err)
	}

	specPath := filepath.Join(outDir, defaultOpenAPIFile)
	switch col.Source {
	case core.SourceOpenAPI:
		log.Info().Str("input", opts.InputPath).Msg("docs: input is OpenAPI; normalizing to openapi.yaml")
		if err := normalizeOpenAPI(opts.InputPath, specPath); err != nil {
			return fmt.Errorf("docs: normalize openapi: %w", err)
		}
	default:
		// Postman (or any other collection source) -> OpenAPI.
		if err := buildOpenAPI(ctx, col, opts, specPath); err != nil {
			return fmt.Errorf("docs: build openapi: %w", err)
		}
	}

	htmlPath := filepath.Join(outDir, defaultIndexFile)
	if err := renderHTML(ctx, col, opts, specPath, htmlPath); err != nil {
		return fmt.Errorf("docs: render html: %w", err)
	}

	if opts.Deploy {
		// Deployment scaffolding is best-effort: never fail the run over it.
		if err := writeDeployWorkflow(outDir); err != nil {
			log.Warn().Err(err).Msg("docs: could not write GitHub Pages workflow (docs still generated)")
		}
	}

	log.Info().Str("spec", specPath).Str("html", htmlPath).Msg("docs: documentation generated")
	return nil
}

// buildOpenAPI converts a non-OpenAPI collection into <specPath>. It first tries
// the postman-to-openapi Node CLI via npx; on any failure (or absent tooling) it
// falls back to the built-in minimal emitter, which is fed a redacted copy of
// the collection so no secrets leak into the generated spec.
func buildOpenAPI(ctx context.Context, col *core.Collection, opts Options, specPath string) error {
	log := logging.L()

	if hasCommand("npx") {
		args := []string{"--yes", "postman-to-openapi", opts.InputPath, "-f", specPath}
		switch err := runTool(ctx, "npx", args...); {
		case err == nil && fileNonEmpty(specPath):
			log.Info().Msg("docs: converted collection with postman-to-openapi")
			return nil
		case err != nil:
			log.Warn().Err(err).Msg("docs: postman-to-openapi failed; using built-in converter")
		default:
			log.Warn().Msg("docs: postman-to-openapi produced no output; using built-in converter")
		}
	} else {
		log.Info().Msg("docs: npx not found; using built-in OpenAPI converter")
	}

	// Fallback: redact BEFORE emitting (defense in depth; published docs may be
	// public) and write a minimal, valid OpenAPI 3.0 document.
	safe := redact.Collection(col)
	data, err := emitMinimalOpenAPI(safe, opts)
	if err != nil {
		return fmt.Errorf("emit minimal openapi: %w", err)
	}
	if err := os.WriteFile(specPath, data, 0o644); err != nil {
		return fmt.Errorf("write %q: %w", specPath, err)
	}
	log.Info().Str("spec", specPath).Msg("docs: wrote OpenAPI via built-in converter")
	return nil
}

// normalizeOpenAPI copies an already-OpenAPI input to <specPath>. JSON specs are
// valid YAML, so a verbatim copy is a faithful normalization. Copying a file
// onto itself is a no-op.
func normalizeOpenAPI(input, specPath string) error {
	if absEqual(input, specPath) {
		return nil
	}
	data, err := os.ReadFile(input)
	if err != nil {
		return fmt.Errorf("read %q: %w", input, err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return fmt.Errorf("input spec %q is empty", input)
	}
	if err := os.WriteFile(specPath, data, 0o644); err != nil {
		return fmt.Errorf("write %q: %w", specPath, err)
	}
	return nil
}

// renderHTML produces <htmlPath> from <specPath>. It tries @redocly/cli then
// redoc-cli via npx; failing that it writes a self-contained CDN-backed page.
func renderHTML(ctx context.Context, col *core.Collection, opts Options, specPath, htmlPath string) error {
	log := logging.L()

	if hasCommand("npx") {
		if err := runTool(ctx, "npx", "--yes", "@redocly/cli", "build-docs", specPath, "-o", htmlPath); err == nil && fileNonEmpty(htmlPath) {
			log.Info().Msg("docs: rendered HTML with @redocly/cli build-docs")
			return nil
		} else if err != nil {
			log.Warn().Err(err).Msg("docs: @redocly/cli build-docs failed; trying redoc-cli")
		}
		if err := runTool(ctx, "npx", "--yes", "redoc-cli", "build", specPath, "-o", htmlPath); err == nil && fileNonEmpty(htmlPath) {
			log.Info().Msg("docs: rendered HTML with redoc-cli")
			return nil
		} else if err != nil {
			log.Warn().Err(err).Msg("docs: redoc-cli failed; using CDN Redoc fallback")
		}
	} else {
		log.Info().Msg("docs: npx not found; using CDN Redoc fallback")
	}

	if err := writeRedocHTML(col, opts, htmlPath); err != nil {
		return err
	}
	log.Info().Str("html", htmlPath).Msg("docs: wrote self-contained Redoc HTML (CDN)")
	return nil
}

// resolveTitle picks the documentation title: explicit --title, else the
// collection name, else a generic default.
func resolveTitle(opts Options, col *core.Collection) string {
	if t := strings.TrimSpace(opts.Title); t != "" {
		return t
	}
	if col != nil {
		if n := strings.TrimSpace(col.Name); n != "" {
			return n
		}
	}
	return defaultTitle
}

// fileNonEmpty reports whether path exists and has a non-zero size.
func fileNonEmpty(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Size() > 0
}

// absEqual reports whether two paths resolve to the same cleaned absolute path.
func absEqual(a, b string) bool {
	aa, err1 := filepath.Abs(a)
	bb, err2 := filepath.Abs(b)
	if err1 != nil || err2 != nil {
		return false
	}
	return filepath.Clean(aa) == filepath.Clean(bb)
}
