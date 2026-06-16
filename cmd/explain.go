package cmd

import (
	"github.com/spf13/cobra"

	"github.com/apilens/apilens/internal/explain"
)

var (
	explainCollection string
	explainOut        string
)

var explainCmd = &cobra.Command{
	Use:   "explain <newman report path>",
	Short: "Explain test failures and predict root causes (AI)",
	Long: "Analyze a Newman JSON/HTML report: group failures, predict root causes\n" +
		"with confidence, and recommend fixes. Optionally correlate with a collection.",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := optionalClient()
		if err != nil {
			return err
		}
		opts := explain.Options{
			ReportPath:     args[0],
			CollectionPath: explainCollection,
			OutPath:        explainOut,
		}
		return explain.Run(cmd.Context(), cfg, client, opts)
	},
}

func init() {
	f := explainCmd.Flags()
	f.StringVar(&explainCollection, "collection", "", "optional collection file for correlation")
	f.StringVar(&explainOut, "out", "failure-analysis.md", "output path for the failure analysis")
	rootCmd.AddCommand(explainCmd)
}
