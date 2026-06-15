package cmd

import (
	"github.com/spf13/cobra"

	"github.com/apilens/apilens/internal/generate"
)

var (
	genResponse string
	genEndpoint string
	genStatus   int
	genOutput   string
)

var generateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate API test scripts from a sample response (AI)",
	Long: "Generate Postman/Newman-compatible test scripts for an endpoint from a\n" +
		"sample response, expected status, and endpoint metadata.",
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		client, err := newClient()
		if err != nil {
			return err
		}
		opts := generate.Options{
			ResponsePath: genResponse,
			Endpoint:     genEndpoint,
			Status:       genStatus,
			OutputPath:   genOutput,
		}
		return generate.Run(cmd.Context(), cfg, client, opts)
	},
}

func init() {
	f := generateCmd.Flags()
	f.StringVar(&genResponse, "response", "", "path to a sample response JSON file")
	f.StringVar(&genEndpoint, "endpoint", "", `endpoint metadata, e.g. "GET /users"`)
	f.IntVar(&genStatus, "status", 200, "expected HTTP status code")
	f.StringVar(&genOutput, "output", "generated-tests.js", "output path for the generated tests")
	rootCmd.AddCommand(generateCmd)
}
