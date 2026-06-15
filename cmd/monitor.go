package cmd

import (
	"github.com/spf13/cobra"

	"github.com/apilens/apilens/internal/monitor"
)

var (
	monitorOutDir string
	monitorMode   string
	monitorNotify []string
)

var monitorCmd = &cobra.Command{
	Use:   "monitor",
	Short: "Generate monitoring workflows",
	Long:  "Scaffold free monitoring on top of GitHub Actions.",
}

var monitorInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Generate a GitHub Actions monitoring workflow",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		opts := monitor.Options{
			OutDir: monitorOutDir,
			Mode:   monitorMode,
			Notify: monitorNotify,
		}
		// monitor scaffolding does not need an AI client.
		return monitor.Run(cmd.Context(), cfg, nil, opts)
	},
}

func init() {
	f := monitorInitCmd.Flags()
	f.StringVar(&monitorOutDir, "out-dir", ".", "output directory for the workflow and report folders")
	f.StringVar(&monitorMode, "mode", "hourly", "monitoring mode: 15min | 30min | hourly | daily")
	f.StringSliceVar(&monitorNotify, "notify", nil, "notification channels: email,slack,discord,telegram")
	monitorCmd.AddCommand(monitorInitCmd)
	rootCmd.AddCommand(monitorCmd)
}
