package docs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"regexp"
	"strings"

	"github.com/apilens/apilens/internal/core"
)

// reSafeThemeColor restricts --theme to a conservative set of CSS color tokens
// (hex, rgb()/hsl(), named colors) so it can be embedded safely.
var reSafeThemeColor = regexp.MustCompile(`^[#A-Za-z0-9(),.%\- ]{1,40}$`)

// redocHTMLTemplate is a self-contained Redoc page that loads the standalone
// bundle from the CDN and points at the sibling ./openapi.yaml. All dynamic
// values are injected through html/template's context-aware escaping; the Redoc
// options object is pre-marshaled JSON (which escapes <, >, & by default),
// preventing script-context injection from flag values.
const redocHTMLTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8"/>
  <meta name="viewport" content="width=device-width, initial-scale=1"/>
  <title>{{.Title}}</title>
  <style>
    body { margin: 0; padding: 0; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif; }
    header.apilens-banner { display: flex; align-items: center; gap: 12px; padding: 12px 20px; color: #fff; background: {{.HeaderColor}}; }
    header.apilens-banner img { height: 32px; width: auto; }
    header.apilens-banner h1 { font-size: 18px; margin: 0; font-weight: 600; }
  </style>
</head>
<body>
  <header class="apilens-banner">
    {{if .Logo}}<img src="{{.Logo}}" alt="logo"/>{{end}}
    <h1>{{.Title}}</h1>
  </header>
  <div id="redoc"></div>
  <script src="{{.ScriptURL}}"></script>
  <script>
    Redoc.init({{.SpecURL}}, {{.Options}}, document.getElementById("redoc"));
  </script>
</body>
</html>
`

var redocTmpl = template.Must(template.New("redoc").Parse(redocHTMLTemplate))

// redocData is the template payload.
type redocData struct {
	Title       string
	Logo        string
	HeaderColor template.CSS
	ScriptURL   string
	SpecURL     string
	Options     template.JS
}

// writeRedocHTML renders the self-contained Redoc fallback page to htmlPath,
// applying the title (page <title> + banner heading), optional logo, and theme
// color (Redoc primary color + banner background).
func writeRedocHTML(col *core.Collection, opts Options, htmlPath string) error {
	color := sanitizeThemeColor(opts.Theme)

	redocOpts := map[string]any{
		"theme": map[string]any{
			"colors": map[string]any{
				"primary": map[string]any{"main": color},
			},
		},
	}
	optJSON, err := json.Marshal(redocOpts)
	if err != nil {
		return fmt.Errorf("marshal redoc options: %w", err)
	}

	data := redocData{
		Title: resolveTitle(opts, col),
		Logo:  strings.TrimSpace(opts.Logo),
		// Safe to bypass html/template escaping: color is gated by
		// sanitizeThemeColor's allow-list regex (reSafeThemeColor).
		HeaderColor: template.CSS(color),
		ScriptURL:   redocCDN,
		SpecURL:     "./" + defaultOpenAPIFile,
		// Safe to bypass escaping: optJSON is json.Marshal output, which already
		// escapes <, >, and & for HTML embedding. Do not cast user-supplied
		// strings this way without equivalent validation.
		Options: template.JS(optJSON),
	}

	var buf bytes.Buffer
	if err := redocTmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("render redoc template: %w", err)
	}
	if err := os.WriteFile(htmlPath, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write %q: %w", htmlPath, err)
	}
	return nil
}

// sanitizeThemeColor returns a safe CSS color token from the --theme flag, or
// the default when it is empty or not a recognized-safe token.
func sanitizeThemeColor(theme string) string {
	t := strings.TrimSpace(theme)
	if t == "" || !reSafeThemeColor.MatchString(t) {
		return defaultThemeColor
	}
	return t
}
