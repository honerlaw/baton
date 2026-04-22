package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/honerlaw/baton/internal/assets"
	"github.com/honerlaw/baton/internal/persona"
)

func newPersonasCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "personas",
		Short: "List personas known to baton (project overrides + embedded defaults).",
		RunE: func(cmd *cobra.Command, args []string) error {
			loader := &persona.ChainLoader{Loaders: []persona.Loader{
				persona.DirLoader(".baton/personas", "project"),
				&persona.FSLoader{FS: assets.PersonasFS(), Name: "embedded"},
			}}
			ps, err := loader.List()
			if err != nil {
				return err
			}
			for _, p := range ps {
				fmt.Fprintf(cmd.OutOrStdout(), "%-32s  %s\n", p.Name, p.Description)
			}
			return nil
		},
	}
}
