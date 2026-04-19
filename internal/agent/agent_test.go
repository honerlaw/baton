package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/honerlaw/baton/internal/event"
	"github.com/honerlaw/baton/internal/openrouter"
	"github.com/honerlaw/baton/internal/tools"
)

// fakeLLM produces scripted stream frames on each call to ChatStream.
type fakeLLM struct {
	calls   int
	scripts [][]openrouter.StreamFrame
}

func (f *fakeLLM) ChatStream(ctx context.Context, req openrouter.ChatRequest) (<-chan openrouter.StreamFrame, func() error) {
	i := f.calls
	f.calls++
	ch := make(chan openrouter.StreamFrame, 16)
	go func() {
		defer close(ch)
		if i < len(f.scripts) {
			for _, fr := range f.scripts[i] {
				ch <- fr
			}
		}
	}()
	return ch, func() error { return nil }
}

func TestAgent_WritesArtifactAndCompletes(t *testing.T) {
	dir := t.TempDir()
	reg := tools.NewRegistry()
	if err := tools.RegisterBuiltins(reg, dir, dir); err != nil {
		t.Fatal(err)
	}
	artifactPath := filepath.Join(dir, "out.md")
	args, _ := json.Marshal(map[string]string{"path": artifactPath, "content": "hello"})
	llm := &fakeLLM{scripts: [][]openrouter.StreamFrame{
		{
			// Turn 1: tool call to write_file
			{ToolCallDelta: &openrouter.ToolCallDelta{Index: 0, ID: "c1", FunctionName: "write_file"}},
			{ToolCallDelta: &openrouter.ToolCallDelta{Index: 0, ArgsDelta: string(args)}},
			{FinishReason: "tool_calls"},
			{Usage: &openrouter.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}},
			{Done: true},
		},
		{
			// Turn 2: final text, no tool call
			{ContentDelta: "done"},
			{FinishReason: "stop"},
			{Usage: &openrouter.Usage{PromptTokens: 3, CompletionTokens: 1, TotalTokens: 4}},
			{Done: true},
		},
	}}
	bus := event.NewBus()
	defer bus.Close()
	sub := bus.Subscribe(32, false)

	a := &Agent{LLM: llm, Tools: reg, Bus: bus}
	res, err := a.Run(context.Background(), ResolvedStage{
		StageID: "s1", PersonaName: "p", Model: "m",
		PersonaBody: "sys", Task: "do it",
		ArtifactName: "out.md", ArtifactPath: artifactPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Turns != 2 {
		t.Fatalf("turns=%d", res.Turns)
	}
	if _, err := os.Stat(artifactPath); err != nil {
		t.Fatalf("artifact missing: %v", err)
	}

	// Drain events and assert we saw the key lifecycle.
	var kinds []string
	done := make(chan struct{})
	go func() {
		for ev := range sub {
			kinds = append(kinds, ev.Kind())
		}
		close(done)
	}()
	bus.Close()
	<-done
	must := []string{"stage_started", "tool_called", "tool_completed", "artifact_written", "stage_completed"}
	for _, m := range must {
		if !has(kinds, m) {
			t.Fatalf("expected %s in events, got %v", m, kinds)
		}
	}
}

func has(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
