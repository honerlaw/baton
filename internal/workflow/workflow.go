// Package workflow defines the YAML schema for workflows, the loader,
// and the validator invoked by `baton validate` and before any run.
package workflow

// Workflow is a parsed YAML workflow definition.
type Workflow struct {
	Name         string
	Version      string
	Description  string
	DefaultModel string
	MaxReentries int
	Variables    []VarDecl
	Stages       []Stage
	SourcePath   string
	SourceBytes  []byte // verbatim; preserved for run-dir copying
}

// VarDecl declares a user-supplied input.
type VarDecl struct {
	Name        string
	Description string
	Required    bool
	Default     string
}

// Stage is one step in a workflow. If Parallel is true, Members contains
// the concurrent members and stage-level Persona/Task/etc. are ignored.
type Stage struct {
	ID           string
	Name         string
	Parallel     bool
	Members      []StageMember
	Persona      string
	Model        string
	Task         string
	Inputs       []string
	Artifact     string
	MaxReentries int
	OnFail       FailPolicy
	Verdict      *VerdictRule

	// Source position for diagnostics.
	Line int
}

// StageMember is one member of a parallel stage.
type StageMember struct {
	ID       string
	Persona  string
	Model    string
	Task     string
	Inputs   []string
	Artifact string

	Line int
}

// FailPolicy governs what happens when a stage fails.
type FailPolicy string

const (
	FailHalt     FailPolicy = "halt"
	FailRetry    FailPolicy = "retry"
	FailContinue FailPolicy = "continue"
)

// VerdictRule extracts a routing value from an artifact and maps it to a
// stage ID to re-enter (empty string => workflow completes).
type VerdictRule struct {
	Parser VerdictParser
	Field  string
	Routes map[string]string

	Line int
}

// VerdictParser names how to extract the routing value from the artifact.
type VerdictParser string

const (
	VerdictJSONBlock  VerdictParser = "json_block"
	VerdictStructured VerdictParser = "structured_field"
)

// AllStageIDs returns stage IDs in order (non-parallel stages + parallel
// groups). Parallel member IDs are not included; they are internal to
// the group.
func (w *Workflow) AllStageIDs() []string {
	out := make([]string, 0, len(w.Stages))
	for _, s := range w.Stages {
		out = append(out, s.ID)
	}
	return out
}

// StageByID returns a pointer into Stages, or nil if not found.
func (w *Workflow) StageByID(id string) *Stage {
	for i := range w.Stages {
		if w.Stages[i].ID == id {
			return &w.Stages[i]
		}
	}
	return nil
}

// EffectiveMaxReentries returns the stage's max_reentries, falling back to
// the workflow default.
func (w *Workflow) EffectiveMaxReentries(stageID string) int {
	s := w.StageByID(stageID)
	if s == nil {
		return w.MaxReentries
	}
	if s.MaxReentries > 0 {
		return s.MaxReentries
	}
	return w.MaxReentries
}
