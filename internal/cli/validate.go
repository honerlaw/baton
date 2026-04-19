package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/honerlaw/baton/internal/assets"
	"github.com/honerlaw/baton/internal/persona"
	"github.com/honerlaw/baton/internal/tools"
	"github.com/honerlaw/baton/internal/workflow"
)

func newValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate <workflow.yaml>",
		Short: "Validate a workflow file.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w, err := workflow.LoadFile(args[0])
			if err != nil {
				return err
			}
			reg := tools.NewRegistry()
			if err := tools.RegisterBuiltins(reg, ".", ""); err != nil {
				return err
			}
			loader := &persona.ChainLoader{Loaders: []persona.Loader{
				persona.DirLoader(".claude/agents", "project"),
				&persona.FSLoader{FS: assets.PersonasFS(), Name: "embedded"},
			}}
			v := &workflow.Validator{Personas: loader, Tools: reg}
			res := v.Validate(w)
			if !res.OK() {
				fmt.Fprintln(os.Stderr, res.Error())
				os.Exit(2)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "ok")
			return nil
		},
	}
}
