package orchestrator

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/honerlaw/baton/internal/agent"
	"github.com/honerlaw/baton/internal/artifact"
	"github.com/honerlaw/baton/internal/event"
	"github.com/honerlaw/baton/internal/openrouter"
	"github.com/honerlaw/baton/internal/persona"
	"github.com/honerlaw/baton/internal/tools"
	"github.com/honerlaw/baton/internal/workflow"
)

// fakeLLM returns a stream that writes a predetermined artifact via
// write_file, then emits a final text on the next turn.
type fakeLLM struct {
	mu      sync.Mutex
	byStage map[string]stageBehavior
	calls   map[string]int
	workDir string
	runRoot string
}

type stageBehavior struct {
	writePath string
	content   string
}

func (f *fakeLLM) ChatStream(ctx context.Context, req openrouter.ChatRequest) (<-chan openrouter.StreamFrame, func() error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Extract stage from the system prompt (first message): we encoded
	// the stage id into it for the test.
	key := extractStageFromMessages(req.Messages)
	f.calls[key]++
	call := f.calls[key]

	ch := make(chan openrouter.StreamFrame, 16)
	go func() {
		defer close(ch)
		if call == 1 {
			// First turn: tool call to write the artifact.
			b := f.byStage[key]
			args, _ := json.Marshal(map[string]string{"path": b.writePath, "content": b.content})
			ch <- openrouter.StreamFrame{ToolCallDelta: &openrouter.ToolCallDelta{
				Index: 0, ID: "c1", FunctionName: "write_file",
			}}
			ch <- openrouter.StreamFrame{ToolCallDelta: &openrouter.ToolCallDelta{
				Index: 0, ArgsDelta: string(args),
			}}
			ch <- openrouter.StreamFrame{FinishReason: "tool_calls"}
			ch <- openrouter.StreamFrame{Usage: &openrouter.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}}
			ch <- openrouter.StreamFrame{Done: true}
			return
		}
		// Second turn: terminal text.
		ch <- openrouter.StreamFrame{ContentDelta: "done"}
		ch <- openrouter.StreamFrame{FinishReason: "stop"}
		ch <- openrouter.StreamFrame{Usage: &openrouter.Usage{PromptTokens: 2, CompletionTokens: 1, TotalTokens: 3}}
		ch <- openrouter.StreamFrame{Done: true}
	}()
	return ch, func() error { return nil }
}

// extractStageFromMessages parses the test marker "STAGE:<id>" out of
// the system prompt.
func extractStageFromMessages(msgs []openrouter.Message) string {
	for _, m := range msgs {
		if m.Role != "system" {
			continue
		}
		i := strings.Index(m.Content, "STAGE:")
		if i < 0 {
			return ""
		}
		rest := m.Content[i+len("STAGE:"):]
		// Read until whitespace/newline.
		j := 0
		for j < len(rest) && rest[j] != '\n' && rest[j] != ' ' {
			j++
		}
		return rest[:j]
	}
	return ""
}

func TestOrchestrator_EndToEnd_SequentialAndParallel(t *testing.T) {
	wd := t.TempDir()
	run, err := artifact.NewRun(filepath.Join(wd, ".baton/runs"))
	if err != nil {
		t.Fatal(err)
	}

	// Persona file system: every persona is a minimal stub whose body
	// includes "STAGE:<name>" so the fake LLM can route.
	personasFS := fstest.MapFS{}
	for _, name := range []string{
		"p-context", "p-design", "p-reviewer-a", "p-reviewer-b",
		"p-reviewer-c", "p-synth", "p-impl", "p-final",
	} {
		body := "You are " + name + ".\nSTAGE:" + name + "\nWrite the artifact via write_file."
		personasFS[name+".md"] = &fstest.MapFile{Data: []byte(
			"---\nname: " + name + "\ndescription: test\ntools: write_file\n---\n\n" + body + "\n")}
	}

	reg := tools.NewRegistry()
	if err := tools.RegisterBuiltins(reg, wd, run.Root); err != nil {
		t.Fatal(err)
	}

	wfSrc := []byte(`
name: test
version: 1.0.0
default_model: fake
variables:
  - name: feature
stages:
  - id: context
    persona: p-context
    artifact: context.md
    task: "{{ .vars.feature }}"
  - id: design
    persona: p-design
    inputs: [context.md]
    artifact: design.md
    task: "go"
  - id: review
    parallel: true
    members:
      - id: a
        persona: p-reviewer-a
        inputs: [design.md]
        artifact: review-a.md
        task: "go"
      - id: b
        persona: p-reviewer-b
        inputs: [design.md]
        artifact: review-b.md
        task: "go"
      - id: c
        persona: p-reviewer-c
        inputs: [design.md]
        artifact: review-c.md
        task: "go"
  - id: synth
    persona: p-synth
    inputs: [design.md, review-a.md, review-b.md, review-c.md]
    artifact: design-final.md
    task: "go"
  - id: impl
    persona: p-impl
    inputs: [design-final.md]
    artifact: impl.md
    task: "go"
  - id: final
    persona: p-final
    inputs: [impl.md]
    artifact: verdict.md
    task: "go"
`)
	w, err := workflow.Load(wfSrc)
	if err != nil {
		t.Fatal(err)
	}

	personasByStage := map[string]string{
		"context":  "context.md",
		"design":   "design.md",
		"review-a": "review-a.md",
		"review-b": "review-b.md",
		"review-c": "review-c.md",
		"synth":    "design-final.md",
		"impl":     "impl.md",
		"final":    "verdict.md",
	}
	llm := &fakeLLM{
		byStage: map[string]stageBehavior{},
		calls:   map[string]int{},
		workDir: wd, runRoot: run.Root,
	}
	for personaName, artName := range map[string]string{
		"p-context":    "context.md",
		"p-design":     "design.md",
		"p-reviewer-a": "review-a.md",
		"p-reviewer-b": "review-b.md",
		"p-reviewer-c": "review-c.md",
		"p-synth":      "design-final.md",
		"p-impl":       "impl.md",
		"p-final":      "verdict.md",
	} {
		_ = personasByStage // unused linting avoid
		llm.byStage[personaName] = stageBehavior{
			writePath: run.ArtifactPath(artName),
			content:   "content for " + artName,
		}
	}

	bus := event.NewBus()
	var completed bool
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ev := range bus.Subscribe(64, false) {
			if _, ok := ev.(event.WorkflowCompleted); ok {
				completed = true
			}
		}
	}()

	ag := &agent.Agent{LLM: llm, Tools: reg, Bus: bus}
	orch := &Orchestrator{
		Workflow: w,
		Vars:     map[string]string{"feature": "test feature"},
		Run:      run,
		Personas: &persona.FSLoader{FS: personasFS},
		Tools:    reg,
		Agent:    ag,
		Bus:      bus,
	}

	res, err := orch.Execute(context.Background())
	bus.Close()
	<-done
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if res.Verdict != "completed" {
		t.Fatalf("verdict=%q", res.Verdict)
	}
	if !completed {
		t.Fatal("missing WorkflowCompleted event")
	}
	// All artifacts present.
	for _, n := range []string{
		"context.md", "design.md",
		"review-a.md", "review-b.md", "review-c.md",
		"design-final.md", "impl.md", "verdict.md",
	} {
		if !run.ArtifactExists(n) {
			t.Fatalf("missing artifact %s", n)
		}
	}
	// workflow.yaml was copied.
	if _, err := os.Stat(filepath.Join(run.Root, "workflow.yaml")); err != nil {
		t.Fatalf("workflow.yaml not copied: %v", err)
	}
	// variables.json was written.
	if _, err := os.Stat(filepath.Join(run.Root, "variables.json")); err != nil {
		t.Fatalf("variables.json missing: %v", err)
	}
}
