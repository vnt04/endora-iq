package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version is the ApiLens version. Overridable at build time via
// -ldflags "-X github.com/apilens/apilens/cmd.Version=x.y.z".
var Version = "0.1.0-dev"

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the ApiLens version",
	Args:  cobra.NoArgs,
	RunE: func(_ *cobra.Command, _ []string) error {
		fmt.Printf("apilens %s\n", Version)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
