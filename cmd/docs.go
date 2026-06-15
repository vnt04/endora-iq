package cmd

import (
	"github.com/spf13/cobra"

	"github.com/apilens/apilens/internal/docs"
)

var (
	docsOutDir string
	docsTitle  string
	docsLogo   string
	docsTheme  string
	docsDeploy bool
)

var docsCmd = &cobra.Command{
	Use:   "docs <path>",
	Short: "Generate branded API documentation",
	Long: "Convert a Postman collection or OpenAPI spec into branded HTML\n" +
		"documentation, optionally deploying it to GitHub Pages.",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		opts := docs.Options{
			InputPath: args[0],
			OutDir:    docsOutDir,
			Title:     docsTitle,
			Logo:      docsLogo,
			Theme:     docsTheme,
			Deploy:    docsDeploy,
		}
		// docs does not need an AI client.
		return docs.Run(cmd.Context(), cfg, nil, opts)
	},
}

func init() {
	f := docsCmd.Flags()
	f.StringVar(&docsOutDir, "out-dir", "docs", "output directory for documentation")
	f.StringVar(&docsTitle, "title", "", "documentation title")
	f.StringVar(&docsLogo, "logo", "", "path or URL to a logo image")
	f.StringVar(&docsTheme, "theme", "", "documentation theme")
	f.BoolVar(&docsDeploy, "deploy", false, "deploy the generated docs to GitHub Pages")
	rootCmd.AddCommand(docsCmd)
}
