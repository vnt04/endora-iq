# templates/ (reference only)

The files in this directory are **reference copies** of the templates that ApiLens
renders at runtime. They are here so you can read, review, and customize the output
shape without digging through the Go source.

> **Source of truth:** the templates are defined as string constants in Go and
> rendered with the standard library `text/template` / `html/template` engines.
> Editing the files here does **not** change ApiLens behavior — change the Go
> constants instead. These copies are kept in sync for documentation purposes.

| Reference file | Rendered by | Go source of truth | Engine / delimiters |
|----------------|-------------|--------------------|---------------------|
| [`monitor-workflow.yml.tmpl`](monitor-workflow.yml.tmpl) | `apilens monitor init` | `internal/monitor/templates.go` (`workflowTemplate`) | `text/template`, delimiters `[[` `]]` |
| [`redoc-index.html.tmpl`](redoc-index.html.tmpl) | `apilens docs` (CDN fallback) | `internal/docs/html.go` (`redocHTMLTemplate`) | `html/template`, default `{{` `}}` |

## Why the monitor template uses `[[` `]]`

The GitHub Actions workflow is full of `${{ secrets.* }}` and `${{ github.* }}`
expressions that GitHub itself must interpret. To avoid `text/template` consuming
those `{{ ... }}` tokens, ApiLens switches its own template delimiters to `[[` and
`]]`. Only `[[.Cron]]`, `[[.Mode]]`, and the `[[range .Notify]]` blocks are ApiLens
substitutions; everything in `${{ ... }}` passes through to the generated YAML
verbatim.

## Notifications

The workflow emits one notification step per channel passed to `--notify`
(`slack`, `discord`, `telegram`, `email`), each guarded by `if: failure()`. The
required repository secrets per channel are documented in the generated
`alerts/config.yaml` (see `internal/monitor/templates.go` `alertsConfigTemplate`).
