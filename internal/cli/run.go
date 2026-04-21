package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/honerlaw/baton/internal/agent"
	"github.com/honerlaw/baton/internal/artifact"
	"github.com/honerlaw/baton/internal/assets"
	"github.com/honerlaw/baton/internal/config"
	"github.com/honerlaw/baton/internal/event"
	"github.com/honerlaw/baton/internal/openrouter"
	"github.com/honerlaw/baton/internal/orchestrator"
	"github.com/honerlaw/baton/internal/persona"
	"github.com/honerlaw/baton/internal/render"
	"github.com/honerlaw/baton/internal/tools"
	"github.com/honerlaw/baton/internal/tui"
	"github.com/honerlaw/baton/internal/workflow"
)

type runFlags struct {
	workflowFile  string
	workflowName  string // embedded workflow name (e.g. "default")
	featureText   string
	variables     []string
	artifactsRoot string
	noTUI         bool
	model         string
}

func newRunCmd() *cobra.Command {
	var f runFlags
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Execute a workflow against a feature request.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflow(cmd.Context(), f)
		},
	}
	cmd.Flags().StringVarP(&f.workflowFile, "file", "f", "", "workflow YAML file (takes precedence over --workflow)")
	cmd.Flags().StringVarP(&f.workflowName, "workflow", "w", "default", "embedded workflow name (default|minimal|iter-design)")
	cmd.Flags().StringVar(&f.featureText, "feature", "", "feature-request variable value")
	cmd.Flags().StringSliceVar(&f.variables, "var", nil, "additional variable, name=value (repeatable)")
	cmd.Flags().StringVar(&f.artifactsRoot, "artifacts-dir", "", "root directory for run artifacts (default .baton/runs)")
	cmd.Flags().BoolVar(&f.noTUI, "no-tui", false, "disable the TUI; render events as plaintext")
	cmd.Flags().StringVar(&f.model, "model", "", "override the default model (otherwise config / env / workflow default)")
	return cmd
}

func runWorkflow(ctx context.Context, f runFlags) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.APIKey == "" {
		return fmt.Errorf("%s not set", cfg.APIKeyEnvVar)
	}

	// Load workflow from file or embedded.
	var (
		w     *workflow.Workflow
		bytes []byte
	)
	if f.workflowFile != "" {
		w, err = workflow.LoadFile(f.workflowFile)
		if err != nil {
			return err
		}
	} else {
		bytes, err = assets.LoadEmbeddedWorkflow(f.workflowName)
		if err != nil {
			return err
		}
		w, err = workflow.Load(bytes)
		if err != nil {
			return err
		}
		w.SourcePath = "<embedded:" + f.workflowName + ".yaml>"
	}
	if f.model != "" {
		w.DefaultModel = f.model
	}

	// Validate.
	reg := tools.NewRegistry()
	runRoot := "" // set below
	if err := tools.RegisterBuiltins(reg, ".", runRoot); err != nil {
		return err
	}
	personas := &persona.ChainLoader{Loaders: []persona.Loader{
		persona.DirLoader(".baton/personas", "project"),
		&persona.FSLoader{FS: assets.PersonasFS(), Name: "embedded"},
	}}
	if res := (&workflow.Validator{Personas: personas, Tools: reg}).Validate(w); !res.OK() {
		return errors.New(res.Error())
	}

	// Resolve variables.
	vars := map[string]string{}
	if f.featureText != "" {
		vars["feature_request"] = f.featureText
	}
	for _, kv := range f.variables {
		i := findEq(kv)
		if i < 0 {
			return fmt.Errorf("invalid --var %q (expect name=value)", kv)
		}
		vars[kv[:i]] = kv[i+1:]
	}
	for _, v := range w.Variables {
		if v.Required && vars[v.Name] == "" {
			if v.Default != "" {
				vars[v.Name] = v.Default
				continue
			}
			return fmt.Errorf("required variable %q not provided (use --var %s=...)", v.Name, v.Name)
		}
	}

	// Prepare run directory. Re-register file tools with runRoot now known.
	artifactsRoot := f.artifactsRoot
	if artifactsRoot == "" {
		artifactsRoot = cfg.ArtifactsRoot
	}
	run, err := artifact.NewRun(artifactsRoot)
	if err != nil {
		return err
	}
	reg = tools.NewRegistry()
	if err := tools.RegisterBuiltins(reg, ".", run.Root); err != nil {
		return err
	}

	// Event bus + subscribers.
	bus := event.NewBus()
	defer bus.Close()

	// Always: NDJSON logger.
	logger, err := render.NewNDJSONLogger(run.EventLogPath())
	if err != nil {
		return err
	}
	defer func() { _ = logger.Close() }()
	go func() { _ = logger.Run(bus.Subscribe(256, false)) }()

	// LLM client + orchestrator.
	client := openrouter.NewClient(cfg.APIKey)
	ag := &agent.Agent{LLM: client, Tools: reg, Bus: bus}
	orch := &orchestrator.Orchestrator{
		Workflow:     w,
		Vars:         vars,
		Run:          run,
		Personas:     personas,
		Tools:        reg,
		Agent:        ag,
		Bus:          bus,
		DefaultModel: cfg.DefaultModel,
	}

	useTUI := !f.noTUI && isTTY()

	if useTUI {
		// TUI mode: run orchestrator in background, TUI on main goroutine.
		tuiCh := bus.Subscribe(512, true)
		errCh := make(chan error, 1)
		go func() {
			_, err := orch.Execute(ctx)
			bus.Close()
			errCh <- err
		}()
		if err := tui.Run(ctx, tuiCh); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "\nrun id: %s\nartifacts: %s\n", run.ID, filepath.Clean(run.Root))
		return <-errCh
	}

	// Plaintext mode.
	go (&render.Plaintext{W: os.Stdout}).Run(bus.Subscribe(256, false))
	fmt.Fprintf(os.Stderr, "run id: %s\n", run.ID)
	fmt.Fprintf(os.Stderr, "artifacts: %s\n", filepath.Clean(run.Root))
	_, err = orch.Execute(ctx)
	return err
}

func findEq(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == '=' {
			return i
		}
	}
	return -1
}

func isTTY() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
