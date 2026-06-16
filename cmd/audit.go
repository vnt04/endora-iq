package cmd

import (
	"github.com/spf13/cobra"

	"github.com/apilens/apilens/internal/audit"
)

var (
	auditOutDir string
	auditFormat string
)

var auditCmd = &cobra.Command{
	Use:   "audit <collection|spec path>",
	Short: "Audit API testing quality and compute coverage/quality scores",
	Long: "Audit a Postman collection or OpenAPI spec: deterministic coverage and\n" +
		"quality scores plus an AI narrative of gaps and risk.",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := optionalClient()
		if err != nil {
			return err
		}
		opts := audit.Options{
			InputPath: args[0],
			OutDir:    auditOutDir,
			Format:    auditFormat,
		}
		return audit.Run(cmd.Context(), cfg, client, opts)
	},
}

func init() {
	f := auditCmd.Flags()
	f.StringVar(&auditOutDir, "out-dir", "reports", "directory for audit reports")
	f.StringVar(&auditFormat, "format", "both", "report format: md | json | both")
	rootCmd.AddCommand(auditCmd)
}
