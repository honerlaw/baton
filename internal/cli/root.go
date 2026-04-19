// Package cli wires cobra subcommands.
package cli

import "github.com/spf13/cobra"

// NewRootCmd builds the root cobra command tree.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "baton",
		Short:         "Run a quality-focused multi-stage agentic workflow.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(
		newRunCmd(),
		newValidateCmd(),
		newInitCmd(),
		newVersionCmd(),
		newPersonasCmd(),
	)
	return root
}
