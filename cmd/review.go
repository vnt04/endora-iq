package cmd

import (
	"github.com/spf13/cobra"

	"github.com/apilens/apilens/internal/review"
)

var reviewOut string

var reviewCmd = &cobra.Command{
	Use:   "review <path>",
	Short: "Get actionable recommendations for a collection (AI)",
	Long: "Review a Postman collection or OpenAPI spec and produce prioritized,\n" +
		"actionable recommendations: weak/untested endpoints and missing scenarios.",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := optionalClient()
		if err != nil {
			return err
		}
		opts := review.Options{
			InputPath: args[0],
			OutPath:   reviewOut,
		}
		return review.Run(cmd.Context(), cfg, client, opts)
	},
}

func init() {
	reviewCmd.Flags().StringVar(&reviewOut, "out", "review.md", "output path for the review")
	rootCmd.AddCommand(reviewCmd)
}
