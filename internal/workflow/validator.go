package workflow

import (
	"fmt"
	"sort"
	"strings"
	"text/template"

	"github.com/honerlaw/baton/internal/persona"
	"github.com/honerlaw/baton/internal/tools"
)

// Validator runs all static checks on a workflow.
type Validator struct {
	Personas persona.Loader
	Tools    *tools.Registry
}

// ValidationError is one finding, with a source-line pointer.
type ValidationError struct {
	Path    string // source path (may be empty)
	Line    int
	Message string
}

func (e ValidationError) Error() string {
	if e.Path != "" {
		return fmt.Sprintf("%s:%d: %s", e.Path, e.Line, e.Message)
	}
	return fmt.Sprintf("line %d: %s", e.Line, e.Message)
}

// Result collects all errors from a validation pass.
type Result struct {
	Errors []ValidationError
}

// OK reports whether no errors were found.
func (r Result) OK() bool { return len(r.Errors) == 0 }

// Error implements error for convenience; multi-error join.
func (r Result) Error() string {
	if r.OK() {
		return ""
	}
	var b strings.Builder
	for i, e := range r.Errors {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(e.Error())
	}
	b.WriteString(fmt.Sprintf("\n\n%d errors. workflow is not executable.", len(r.Errors)))
	return b.String()
}

// Validate runs all checks on w.
func (v *Validator) Validate(w *Workflow) Result {
	var res Result
	add := func(line int, format string, a ...any) {
		res.Errors = append(res.Errors, ValidationError{
			Path:    w.SourcePath,
			Line:    line,
			Message: fmt.Sprintf(format, a...),
		})
	}

	// 1. Required top-level fields.
	if w.Name == "" {
		add(1, "name is required")
	}
	if w.Version == "" {
		add(1, "version is required")
	}
	if w.DefaultModel == "" {
		add(1, "default_model is required")
	}
	if len(w.Stages) == 0 {
		add(1, "at least one stage is required")
	}

	// 2. Stage IDs unique.
	seenStage := map[string]bool{}
	for _, s := range w.Stages {
		if s.ID == "" {
			add(s.Line, "stage.id is required")
			continue
		}
		if seenStage[s.ID] {
			add(s.Line, "duplicate stage id %q", s.ID)
		}
		seenStage[s.ID] = true
	}

	// 5. Artifact names unique across workflow.
	seenArt := map[string]bool{}
	artStage := map[string]string{}

	// 3, 4, 5, 6, 9 together — per-stage walk.
	validStageIDs := map[string]bool{}
	for _, s := range w.Stages {
		validStageIDs[s.ID] = true
	}

	// availableArtifacts tracks artifact names produced by prior stages
	// in iteration order (used for checking inputs).
	availableArtifacts := map[string]bool{}

	for _, s := range w.Stages {
		// Persona + task/artifact shape differs for parallel vs sequential.
		if s.Parallel {
			if len(s.Members) < 2 {
				add(s.Line, "parallel stage %q needs at least 2 members", s.ID)
			}
			memberIDs := map[string]bool{}
			for _, m := range s.Members {
				if m.ID == "" {
					add(m.Line, "parallel member.id is required in stage %q", s.ID)
					continue
				}
				if memberIDs[m.ID] {
					add(m.Line, "duplicate member id %q in stage %q", m.ID, s.ID)
				}
				memberIDs[m.ID] = true
				validatePersonaAndTools(v, &res, w.SourcePath, m.Line, m.Persona)
				validateTaskTemplate(&res, w.SourcePath, m.Line, m.Task, s.ID+"/"+m.ID)
				if m.Artifact == "" {
					add(m.Line, "member %q in stage %q: artifact is required", m.ID, s.ID)
				} else {
					if seenArt[m.Artifact] {
						add(m.Line, "duplicate artifact %q (also produced by %q)", m.Artifact, artStage[m.Artifact])
					}
					seenArt[m.Artifact] = true
					artStage[m.Artifact] = s.ID + "/" + m.ID
				}
				validateInputs(&res, w.SourcePath, m.Line, m.Inputs, availableArtifacts)
			}
			// Members inside a parallel group can NOT see each other's artifacts.
			// Add their artifacts to the shared set only AFTER the whole group has been validated.
			for _, m := range s.Members {
				if m.Artifact != "" {
					availableArtifacts[m.Artifact] = true
				}
			}
		} else {
			if s.Persona == "" {
				add(s.Line, "stage %q: persona is required", s.ID)
			} else {
				validatePersonaAndTools(v, &res, w.SourcePath, s.Line, s.Persona)
			}
			if s.Task == "" {
				add(s.Line, "stage %q: task is required", s.ID)
			}
			validateTaskTemplate(&res, w.SourcePath, s.Line, s.Task, s.ID)
			if s.Artifact == "" {
				add(s.Line, "stage %q: artifact is required", s.ID)
			} else {
				if seenArt[s.Artifact] {
					add(s.Line, "duplicate artifact %q (also produced by %q)", s.Artifact, artStage[s.Artifact])
				}
				seenArt[s.Artifact] = true
				artStage[s.Artifact] = s.ID
			}
			validateInputs(&res, w.SourcePath, s.Line, s.Inputs, availableArtifacts)
			if s.Artifact != "" {
				availableArtifacts[s.Artifact] = true
			}
		}

		// 11. FailPolicy valid.
		switch s.OnFail {
		case "", FailHalt, FailRetry, FailContinue:
		default:
			add(s.Line, "stage %q: on_fail must be one of halt|retry|continue, got %q", s.ID, s.OnFail)
		}

		// 7. Verdict routes target valid stage.
		if s.Verdict != nil {
			v := s.Verdict
			switch v.Parser {
			case VerdictJSONBlock, VerdictStructured:
			default:
				add(s.Line, "stage %q: verdict.parser must be json_block or structured_field", s.ID)
			}
			if v.Field == "" {
				add(s.Line, "stage %q: verdict.field is required", s.ID)
			}
			for value, target := range v.Routes {
				if target == "" {
					continue // end-of-workflow
				}
				if !validStageIDs[target] {
					add(s.Line, "stage %q: verdict.routes.%s targets unknown stage %q", s.ID, value, target)
					continue
				}
				// Re-entry requires nonzero max_reentries.
				budget := s.MaxReentries
				if budget == 0 {
					budget = w.MaxReentries
				}
				if budget < 1 {
					add(s.Line, "stage %q: verdict.routes.%s targets %q but max_reentries=0; raise max_reentries to enable re-entry",
						s.ID, value, target)
				}
			}
		}
	}

	// 10. Variables declared: every {{ .vars.X }} reference in any task
	// binds to a declared variable. Cheap check via substring.
	declaredVars := map[string]bool{}
	for _, vd := range w.Variables {
		declaredVars[vd.Name] = true
	}
	for _, s := range w.Stages {
		refs := extractVarRefs(s.Task)
		for _, r := range refs {
			if !declaredVars[r] {
				add(s.Line, "stage %q: task references undeclared variable .vars.%s", s.ID, r)
			}
		}
		for _, m := range s.Members {
			for _, r := range extractVarRefs(m.Task) {
				if !declaredVars[r] {
					add(m.Line, "member %q/%q: task references undeclared variable .vars.%s", s.ID, m.ID, r)
				}
			}
		}
	}

	// Deterministic error order (by line, then message).
	sort.SliceStable(res.Errors, func(i, j int) bool {
		if res.Errors[i].Line != res.Errors[j].Line {
			return res.Errors[i].Line < res.Errors[j].Line
		}
		return res.Errors[i].Message < res.Errors[j].Message
	})
	return res
}

func validatePersonaAndTools(v *Validator, res *Result, path string, line int, name string) {
	if name == "" {
		return // already flagged elsewhere
	}
	if v == nil || v.Personas == nil {
		return
	}
	p, err := v.Personas.Load(name)
	if err != nil {
		res.Errors = append(res.Errors, ValidationError{
			Path: path, Line: line,
			Message: fmt.Sprintf("persona %q could not be loaded: %v", name, err),
		})
		return
	}
	if v.Tools != nil {
		for _, t := range p.Tools {
			if !v.Tools.Has(t) {
				res.Errors = append(res.Errors, ValidationError{
					Path: path, Line: line,
					Message: fmt.Sprintf("persona %q references unknown tool %q", name, t),
				})
			}
		}
	}
}

func validateTaskTemplate(res *Result, path string, line int, task, id string) {
	if task == "" {
		return
	}
	if _, err := template.New(id).Parse(task); err != nil {
		res.Errors = append(res.Errors, ValidationError{
			Path: path, Line: line,
			Message: fmt.Sprintf("stage %q: task template parse error: %v", id, err),
		})
	}
}

func validateInputs(res *Result, path string, line int, inputs []string, available map[string]bool) {
	for i, name := range inputs {
		if !available[name] {
			var candidates []string
			for k := range available {
				candidates = append(candidates, k)
			}
			sort.Strings(candidates)
			res.Errors = append(res.Errors, ValidationError{
				Path: path, Line: line,
				Message: fmt.Sprintf("inputs[%d] references unknown artifact %q\n    prior artifacts at this point: %v", i, name, candidates),
			})
		}
	}
}

// extractVarRefs returns the variable names referenced as {{ .vars.X }}.
// Best-effort substring scan; false positives are fine (template parse
// will catch truly bad templates).
func extractVarRefs(s string) []string {
	var out []string
	pat := ".vars."
	for {
		i := strings.Index(s, pat)
		if i < 0 {
			break
		}
		j := i + len(pat)
		k := j
		for k < len(s) && (isIdentChar(s[k])) {
			k++
		}
		if k > j {
			out = append(out, s[j:k])
		}
		s = s[k:]
	}
	return out
}

func isIdentChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_'
}
