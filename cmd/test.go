package cmd

import (
	"github.com/spf13/cobra"

	datatest "github.com/apilens/apilens/internal/testing"
)

var (
	testCollection string
	testData       string
	testOutDir     string
	testParallel   int
	testBail       bool
	testDelay      int
)

var testCmd = &cobra.Command{
	Use:   "test",
	Short: "Run a collection against datasets (data-driven testing)",
	Long: "Run a Postman collection against a CSV/JSON/JSONL dataset, with optional\n" +
		"parallelism, bail-on-failure, and inter-request delay.",
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		opts := datatest.Options{
			CollectionPath: testCollection,
			DataPath:       testData,
			OutDir:         testOutDir,
			Parallel:       testParallel,
			Bail:           testBail,
			DelayMs:        testDelay,
		}
		// data-driven testing does not need an AI client.
		return datatest.Run(cmd.Context(), cfg, nil, opts)
	},
}

func init() {
	f := testCmd.Flags()
	f.StringVar(&testCollection, "collection", "", "path to the collection file")
	f.StringVar(&testData, "data", "", "path to the dataset (CSV | JSON | JSONL)")
	f.StringVar(&testOutDir, "out-dir", "reports", "directory for test reports")
	f.IntVar(&testParallel, "parallel", 1, "number of parallel executions")
	f.BoolVar(&testBail, "bail", false, "stop on the first failure")
	f.IntVar(&testDelay, "delay", 0, "delay between requests in milliseconds")
	rootCmd.AddCommand(testCmd)
}
