package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/honerlaw/baton/internal/assets"
)

func newInitCmd() *cobra.Command {
	var (
		dir       string
		overwrite bool
	)
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Scaffold the default personas and workflows into the current project.",
		Long: `Write the embedded personas (.baton/personas/*.md) and workflows
(.baton/workflows/*.yaml) to disk. Existing files are preserved unless
--force is passed.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			wrote, err := assets.Scaffold(dir, overwrite)
			if err != nil {
				return err
			}
			for _, p := range wrote {
				fmt.Fprintln(cmd.OutOrStdout(), "wrote:", p)
			}
			if len(wrote) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "nothing to write (use --force to overwrite existing files)")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", ".", "project root to scaffold into")
	cmd.Flags().BoolVar(&overwrite, "force", false, "overwrite existing files")
	return cmd
}
