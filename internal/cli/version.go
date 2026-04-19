package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version is overridden at link time; defaults for `go run`.
var Version = "dev"

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the baton version.",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintln(cmd.OutOrStdout(), Version)
		},
	}
}
