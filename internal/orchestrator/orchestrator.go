// Package orchestrator runs a Workflow end to end: sequential stages,
// parallel groups, verdict-driven re-entry.
//
// It has no knowledge of specific stage types (design, review, etc.).
// It reads a *workflow.Workflow as data and drives it.
package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/honerlaw/baton/internal/agent"
	"github.com/honerlaw/baton/internal/artifact"
	"github.com/honerlaw/baton/internal/event"
	"github.com/honerlaw/baton/internal/persona"
	"github.com/honerlaw/baton/internal/tools"
	"github.com/honerlaw/baton/internal/workflow"
)

// Orchestrator executes workflows.
type Orchestrator struct {
	Workflow *workflow.Workflow
	Vars     map[string]string
	UserNote string
	Run      *artifact.Run
	Personas persona.Loader
	Tools    *tools.Registry
	Agent    *agent.Agent
	Bus      *event.Bus

	DefaultModel string // falls back when persona/stage/workflow leave model blank
}

// Result summarizes a completed workflow.
type Result struct {
	Verdict string // "completed", "halted:<reason>", or a verdict decision
}

// ErrHalted is returned when the workflow halts for user attention.
type ErrHalted struct {
	StageID string
	Reason  string
}

func (e *ErrHalted) Error() string { return fmt.Sprintf("halted at %s: %s", e.StageID, e.Reason) }

// Execute runs the workflow. Returns a Result on success or an error.
func (o *Orchestrator) Execute(ctx context.Context) (*Result, error) {
	if err := o.writeRunManifest(); err != nil {
		return nil, err
	}
	stages := o.Workflow.Stages
	i := 0
	for i < len(stages) {
		s := stages[i]
		next, err := o.runStage(ctx, &s)
		if err != nil {
			var h *ErrHalted
			if errors.As(err, &h) {
				o.Bus.Publish(event.WorkflowHalted{StageID: h.StageID, Reason: h.Reason})
				return nil, err
			}
			return nil, err
		}
		if next == "" {
			i++
			continue
		}
		// Verdict route.
		if next == routeComplete {
			o.Bus.Publish(event.WorkflowCompleted{RunID: o.Run.ID, Verdict: "accept"})
			return &Result{Verdict: "accept"}, nil
		}
		// Re-enter stage with ID == next.
		targetIdx := indexOfStage(stages, next)
		if targetIdx < 0 {
			return nil, fmt.Errorf("verdict route to unknown stage %q", next)
		}
		if err := o.archiveForReentry(stages, targetIdx); err != nil {
			return nil, err
		}
		i = targetIdx
	}
	o.Bus.Publish(event.WorkflowCompleted{RunID: o.Run.ID, Verdict: "completed"})
	return &Result{Verdict: "completed"}, nil
}

// routeComplete is the sentinel returned by runStage when a verdict routes
// to the empty stage id (workflow success).
const routeComplete = "\x00complete"

// runStage dispatches a stage (sequential or parallel) and interprets its
// verdict if any. Returns the next stage ID to execute, or "" for the
// natural next stage.
func (o *Orchestrator) runStage(ctx context.Context, s *workflow.Stage) (next string, err error) {
	if s.Parallel {
		if err := o.runParallel(ctx, s); err != nil {
			return o.applyOnFail(s, err)
		}
	} else {
		if err := o.runSequential(ctx, s); err != nil {
			return o.applyOnFail(s, err)
		}
	}
	if s.Verdict == nil {
		return "", nil
	}
	return o.routeVerdict(s)
}

func (o *Orchestrator) applyOnFail(s *workflow.Stage, stageErr error) (string, error) {
	switch s.OnFail {
	case workflow.FailContinue:
		// Write a sentinel artifact so downstream can detect the gap.
		content := fmt.Sprintf("Stage %s failed: %v\n", s.ID, stageErr)
		_, _ = o.Run.WriteArtifact(fmt.Sprintf("stage-%s-FAILED.md", s.ID), []byte(content))
		return "", nil
	case workflow.FailRetry:
		// One automatic retry: re-run in place, then halt if it fails again.
		ctx := context.Background()
		var err error
		if s.Parallel {
			err = o.runParallel(ctx, s)
		} else {
			err = o.runSequential(ctx, s)
		}
		if err == nil {
			if s.Verdict == nil {
				return "", nil
			}
			return o.routeVerdict(s)
		}
		return "", &ErrHalted{StageID: s.ID, Reason: err.Error()}
	default:
		return "", &ErrHalted{StageID: s.ID, Reason: stageErr.Error()}
	}
}

// runSequential runs a non-parallel stage.
func (o *Orchestrator) runSequential(ctx context.Context, s *workflow.Stage) error {
	rs, err := o.buildStage(s, nil)
	if err != nil {
		return err
	}
	_, err = o.Agent.Run(ctx, rs)
	return err
}

// runParallel runs a parallel stage's members, retrying each once on error
// and proceeding with survivors if at least one succeeds.
func (o *Orchestrator) runParallel(ctx context.Context, s *workflow.Stage) error {
	type memRes struct {
		m   workflow.StageMember
		err error
	}
	results := make([]memRes, len(s.Members))
	var wg sync.WaitGroup
	for i, m := range s.Members {
		wg.Add(1)
		go func(i int, m workflow.StageMember) {
			defer wg.Done()
			memberCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			if err := o.runMemberOnce(memberCtx, s, m); err != nil {
				// One automatic retry.
				if err2 := o.runMemberOnce(memberCtx, s, m); err2 != nil {
					results[i] = memRes{m: m, err: err2}
					return
				}
			}
			results[i] = memRes{m: m, err: nil}
		}(i, m)
	}
	wg.Wait()

	var failed []string
	var okIDs []string
	for _, r := range results {
		if r.err != nil {
			failed = append(failed, r.m.ID+": "+r.err.Error())
		} else {
			okIDs = append(okIDs, r.m.ID)
		}
	}
	if len(okIDs) == 0 {
		return fmt.Errorf("all parallel members failed: %s", strings.Join(failed, "; "))
	}
	if len(failed) > 0 {
		payload, _ := json.MarshalIndent(map[string]any{
			"stage":   s.ID,
			"failed":  failed,
			"success": okIDs,
		}, "", "  ")
		_, _ = o.Run.WriteArtifact("parallel-failures.json", payload)
	}
	o.Bus.Publish(event.ReviewAggregated{
		StageID: s.ID,
		Summary: fmt.Sprintf("%d ok / %d failed", len(okIDs), len(failed)),
		Failed:  failed,
	})
	return nil
}

// runMemberOnce runs a single parallel-member agent loop.
func (o *Orchestrator) runMemberOnce(ctx context.Context, s *workflow.Stage, m workflow.StageMember) error {
	rs, err := o.buildStage(s, &m)
	if err != nil {
		return err
	}
	_, err = o.Agent.Run(ctx, rs)
	return err
}

// buildStage assembles a ResolvedStage from a Stage (or parallel member).
// If m is nil, it's a sequential stage.
func (o *Orchestrator) buildStage(s *workflow.Stage, m *workflow.StageMember) (agent.ResolvedStage, error) {
	var (
		stageID      = s.ID
		memberID     = ""
		personaName  string
		model        string
		task         string
		inputs       []string
		artifactName string
	)
	if m != nil {
		memberID = m.ID
		personaName = m.Persona
		model = m.Model
		task = m.Task
		inputs = m.Inputs
		artifactName = m.Artifact
	} else {
		personaName = s.Persona
		model = s.Model
		task = s.Task
		inputs = s.Inputs
		artifactName = s.Artifact
	}

	p, err := o.Personas.Load(personaName)
	if err != nil {
		return agent.ResolvedStage{}, fmt.Errorf("load persona %q: %w", personaName, err)
	}

	// Model precedence: persona > stage > workflow default > orchestrator default.
	chosen := firstNonEmpty(p.Model, model, o.Workflow.DefaultModel, o.DefaultModel)

	// Preload input artifacts (current + prior-run).
	artsCurrent := map[string]string{}
	for _, name := range inputs {
		content, err := o.Run.ReadArtifact(name)
		if err != nil {
			return agent.ResolvedStage{}, fmt.Errorf("read input %q: %w", name, err)
		}
		artsCurrent[name] = content
	}
	prev := o.loadPrevArtifacts()

	// Resolve task template.
	resolver := &workflow.Resolver{Vars: o.Vars, UserNote: o.UserNote}
	resolved, err := resolver.Resolve(task, artsCurrent, prev)
	if err != nil {
		return agent.ResolvedStage{}, fmt.Errorf("resolve task: %w", err)
	}

	inputArtifacts := make([]agent.InputArtifact, 0, len(inputs))
	for _, name := range inputs {
		inputArtifacts = append(inputArtifacts, agent.InputArtifact{Name: name, Content: artsCurrent[name]})
	}

	return agent.ResolvedStage{
		StageID:      stageID,
		MemberID:     memberID,
		PersonaName:  p.Name,
		PersonaBody:  p.Body,
		PersonaTools: p.Tools,
		Model:        chosen,
		Task:         resolved,
		Inputs:       inputArtifacts,
		ArtifactName: artifactName,
		ArtifactPath: o.Run.ArtifactPath(artifactName),
	}, nil
}

// loadPrevArtifacts returns the most recent artifacts.prev-N contents so
// re-entered stages can compare.
func (o *Orchestrator) loadPrevArtifacts() map[string]string {
	entries, err := os.ReadDir(o.Run.Root)
	if err != nil {
		return nil
	}
	// Pick highest-numbered prev dir.
	best := ""
	bestN := 0
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "artifacts.prev-") {
			continue
		}
		var n int
		_, _ = fmt.Sscanf(e.Name(), "artifacts.prev-%d", &n)
		if n > bestN {
			bestN = n
			best = e.Name()
		}
	}
	if best == "" {
		return nil
	}
	dir := filepath.Join(o.Run.Root, best)
	files, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	out := map[string]string{}
	for _, f := range files {
		if f.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, f.Name()))
		if err != nil {
			continue
		}
		key := strings.TrimSuffix(f.Name(), filepath.Ext(f.Name()))
		out[key] = string(b)
	}
	return out
}

// routeVerdict evaluates a stage's verdict against its output artifact
// and returns the target stage id or the complete sentinel.
func (o *Orchestrator) routeVerdict(s *workflow.Stage) (string, error) {
	content, err := o.Run.ReadArtifact(s.Artifact)
	if err != nil {
		return "", fmt.Errorf("read verdict artifact %s: %w", s.Artifact, err)
	}
	value, err := extractVerdictValue(s.Verdict, content)
	if err != nil {
		return "", &ErrHalted{StageID: s.ID, Reason: fmt.Sprintf("unable to parse verdict: %v", err)}
	}
	target, ok := s.Verdict.Routes[value]
	if !ok {
		return "", &ErrHalted{StageID: s.ID, Reason: fmt.Sprintf("verdict value %q has no matching route", value)}
	}
	if target == "" {
		return routeComplete, nil
	}
	// Check re-entry budget.
	budget := o.Workflow.EffectiveMaxReentries(target)
	if o.Run.ReentryCount(target) >= budget {
		return "", &ErrHalted{StageID: s.ID, Reason: fmt.Sprintf("max_reentries exceeded for stage %q", target)}
	}
	return target, nil
}

// archiveForReentry moves artifacts produced by stages [targetIdx..end]
// into artifacts.prev-N/ before the target stage re-runs.
func (o *Orchestrator) archiveForReentry(stages []workflow.Stage, targetIdx int) error {
	var names []string
	for _, s := range stages[targetIdx:] {
		if s.Parallel {
			for _, m := range s.Members {
				if m.Artifact != "" {
					names = append(names, m.Artifact)
				}
			}
			continue
		}
		if s.Artifact != "" {
			names = append(names, s.Artifact)
		}
	}
	_, err := o.Run.ArchivePriorArtifacts(stages[targetIdx].ID, names)
	return err
}

// writeRunManifest writes the workflow copy and variables.json.
func (o *Orchestrator) writeRunManifest() error {
	if err := o.Run.WriteWorkflowCopy(o.Workflow.SourceBytes); err != nil {
		return err
	}
	return o.Run.WriteVariables(o.Vars)
}

func indexOfStage(stages []workflow.Stage, id string) int {
	for i, s := range stages {
		if s.ID == id {
			return i
		}
	}
	return -1
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
