package orchestrator

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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

// verdictLLM emits a write_file tool call when the expected artifact does
// not yet exist on disk; otherwise it emits a terminal "done". The critic
// artifact alternates between "revise" on the first write and "accept" on
// the second, exercising re-entry.
type verdictLLM struct {
	mu         sync.Mutex
	runRoot    string
	criticCall int
}

func (f *verdictLLM) ChatStream(ctx context.Context, req openrouter.ChatRequest) (<-chan openrouter.StreamFrame, func() error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	key := extractStageFromMessages(req.Messages)
	var artName string
	switch key {
	case "p-design":
		artName = "design.md"
	case "p-critic":
		artName = "critique.md"
	case "p-impl":
		artName = "impl.md"
	}
	path := filepath.Join(f.runRoot, "artifacts", artName)

	ch := make(chan openrouter.StreamFrame, 16)
	go func() {
		defer close(ch)
		if _, err := os.Stat(path); err == nil {
			ch <- openrouter.StreamFrame{ContentDelta: "done"}
			ch <- openrouter.StreamFrame{FinishReason: "stop"}
			ch <- openrouter.StreamFrame{Done: true}
			return
		}
		var content string
		switch key {
		case "p-critic":
			f.criticCall++
			decision := "revise"
			if f.criticCall >= 2 {
				decision = "accept"
			}
			content = "```json\n{\"decision\":\"" + decision + "\"}\n```\n"
		case "p-design":
			content = "design contents"
		case "p-impl":
			content = "impl"
		}
		args, _ := json.Marshal(map[string]string{"path": path, "content": content})
		ch <- openrouter.StreamFrame{ToolCallDelta: &openrouter.ToolCallDelta{
			Index: 0, ID: "c1", FunctionName: "write_file",
		}}
		ch <- openrouter.StreamFrame{ToolCallDelta: &openrouter.ToolCallDelta{
			Index: 0, ArgsDelta: string(args),
		}}
		ch <- openrouter.StreamFrame{FinishReason: "tool_calls"}
		ch <- openrouter.StreamFrame{Done: true}
	}()
	return ch, func() error { return nil }
}

func TestOrchestrator_VerdictReentry(t *testing.T) {
	wd := t.TempDir()
	run, err := artifact.NewRun(filepath.Join(wd, ".baton/runs"))
	if err != nil {
		t.Fatal(err)
	}
	reg := tools.NewRegistry()
	if err := tools.RegisterBuiltins(reg, wd, run.Root); err != nil {
		t.Fatal(err)
	}

	personasFS := fstest.MapFS{}
	for _, p := range []string{"p-design", "p-critic", "p-impl"} {
		personasFS[p+".md"] = &fstest.MapFile{Data: []byte(
			"---\nname: " + p + "\ndescription: test\ntools: write_file\n---\n\nSTAGE:" + p + "\nbody\n")}
	}

	wfSrc := []byte(`
name: iter
version: 1.0.0
default_model: fake
max_reentries: 2
stages:
  - id: design
    persona: p-design
    artifact: design.md
    task: "go"
  - id: critic
    persona: p-critic
    inputs: [design.md]
    artifact: critique.md
    task: "go"
    verdict:
      parser: json_block
      field: .decision
      routes:
        accept: impl
        revise: design
  - id: impl
    persona: p-impl
    inputs: [design.md]
    artifact: impl.md
    task: "go"
`)
	w, err := workflow.Load(wfSrc)
	if err != nil {
		t.Fatal(err)
	}

	llm := &verdictLLM{runRoot: run.Root}
	_ = sync.Mutex{} // placeholder to keep "sync" import used

	bus := event.NewBus()
	go func() {
		for range bus.Subscribe(128, false) {
		}
	}()
	ag := &agent.Agent{LLM: llm, Tools: reg, Bus: bus}
	orch := &Orchestrator{
		Workflow: w, Vars: map[string]string{}, Run: run,
		Personas: &persona.FSLoader{FS: personasFS},
		Tools:    reg, Agent: ag, Bus: bus,
	}

	_, err = orch.Execute(context.Background())
	bus.Close()
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if run.ReentryCount("design") != 1 {
		t.Fatalf("expected 1 re-entry on design, got %d", run.ReentryCount("design"))
	}
	// design was re-run → artifacts.prev-1 directory should exist with
	// the prior design.md and critique.md.
	prev := filepath.Join(run.Root, "artifacts.prev-1")
	for _, want := range []string{"design.md", "critique.md"} {
		if _, err := os.Stat(filepath.Join(prev, want)); err != nil {
			t.Fatalf("expected %s in %s: %v", want, prev, err)
		}
	}
	// impl.md exists (workflow ran the accept branch and reached impl).
	if !run.ArtifactExists("impl.md") {
		t.Fatalf("expected impl.md after re-entry")
	}
}
