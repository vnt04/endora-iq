package docs

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/apilens/apilens/pkg/logging"
)

// deployWorkflowTemplate is a GitHub Actions workflow that publishes the
// generated docs directory to GitHub Pages. The single %q is the publish path.
const deployWorkflowTemplate = `name: Deploy ApiLens Docs to GitHub Pages

on:
  push:
    branches: [main]
  workflow_dispatch:

permissions:
  contents: read
  pages: write
  id-token: write

concurrency:
  group: pages
  cancel-in-progress: true

jobs:
  deploy:
    environment:
      name: github-pages
      url: ${{ steps.deployment.outputs.page_url }}
    runs-on: ubuntu-latest
    steps:
      - name: Checkout
        uses: actions/checkout@v4
      - name: Configure Pages
        uses: actions/configure-pages@v5
      - name: Upload docs artifact
        uses: actions/upload-pages-artifact@v3
        with:
          path: %q
      - name: Deploy to GitHub Pages
        id: deployment
        uses: actions/deploy-pages@v4
`

// writeDeployWorkflow writes a GitHub Pages deployment workflow to
// <outDir>/../.github/workflows/apilens-docs.yml. The publish path is the
// relative outDir (or its base name when outDir is absolute). This scaffolding
// is best-effort; the caller logs and continues on error.
func writeDeployWorkflow(outDir string) error {
	log := logging.L()

	wfDir := filepath.Join(filepath.Dir(outDir), ".github", "workflows")
	if err := os.MkdirAll(wfDir, 0o755); err != nil {
		return fmt.Errorf("create workflow dir %q: %w", wfDir, err)
	}

	pagesPath := filepath.ToSlash(outDir)
	if filepath.IsAbs(outDir) {
		pagesPath = filepath.Base(outDir)
	}

	wfPath := filepath.Join(wfDir, "apilens-docs.yml")
	content := fmt.Sprintf(deployWorkflowTemplate, pagesPath)
	if err := os.WriteFile(wfPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write workflow %q: %w", wfPath, err)
	}

	log.Info().
		Str("workflow", wfPath).
		Str("publish_path", pagesPath).
		Msg("docs: wrote GitHub Pages workflow (review the publish path before committing)")
	return nil
}
