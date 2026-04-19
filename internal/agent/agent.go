// Package agent executes a single stage's message loop against a streaming
// OpenRouter client. It is workflow-agnostic: it consumes a ResolvedStage
// and returns the result.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/honerlaw/baton/internal/event"
	"github.com/honerlaw/baton/internal/openrouter"
	"github.com/honerlaw/baton/internal/tools"
)

// ErrMaxTurnsExceeded is returned when the agent hits its turn cap.
var ErrMaxTurnsExceeded = errors.New("max turns exceeded")

// LLM is the streaming chat interface used by the agent. Pluggable for tests.
type LLM interface {
	ChatStream(ctx context.Context, req openrouter.ChatRequest) (<-chan openrouter.StreamFrame, func() error)
}

// ResolvedStage is a stage with all templates expanded and persona loaded.
type ResolvedStage struct {
	StageID      string
	MemberID     string
	PersonaName  string
	PersonaBody  string
	PersonaTools []string
	Model        string
	Task         string
	Inputs       []InputArtifact
	ArtifactName string
	ArtifactPath string // absolute path where the artifact must land
	MaxTurns     int    // 0 => default 40
}

// InputArtifact is a preloaded prior-stage artifact.
type InputArtifact struct {
	Name    string
	Content string
}

// Result summarizes a completed stage.
type Result struct {
	Turns        int
	InputTokens  int
	OutputTokens int
	Dur          time.Duration
}

// Agent runs single-stage loops.
type Agent struct {
	LLM      LLM
	Tools    *tools.Registry
	Bus      *event.Bus
	MaxTurns int // default 40 if stage.MaxTurns == 0
}

// Run executes the stage's tool-call loop.
func (a *Agent) Run(ctx context.Context, s ResolvedStage) (*Result, error) {
	start := time.Now()
	a.pub(event.StageStarted{
		StageID: s.StageID, MemberID: s.MemberID,
		Persona: s.PersonaName, Model: s.Model, At: start,
	})

	maxTurns := s.MaxTurns
	if maxTurns == 0 {
		maxTurns = a.MaxTurns
	}
	if maxTurns == 0 {
		maxTurns = 40
	}

	msgs := []openrouter.Message{{Role: "system", Content: s.PersonaBody}}
	for _, in := range s.Inputs {
		msgs = append(msgs, openrouter.Message{
			Role:    "user",
			Content: fmt.Sprintf("Prior artifact %s:\n\n%s", in.Name, in.Content),
		})
	}
	msgs = append(msgs, openrouter.Message{Role: "user", Content: s.Task})

	var totalIn, totalOut int
	for turn := 1; turn <= maxTurns; turn++ {
		req := openrouter.ChatRequest{
			Model:      s.Model,
			Messages:   msgs,
			Tools:      a.Tools.OpenRouterDecls(s.PersonaTools),
			ToolChoice: "auto",
		}
		frames, wait := a.LLM.ChatStream(ctx, req)

		asst := openrouter.Message{Role: "assistant"}
		pendingArgs := map[int]*strings.Builder{}
		pendingMeta := map[int]*partial{}

		for f := range frames {
			if f.ContentDelta != "" {
				asst.Content += f.ContentDelta
				a.pub(event.MessageStreamed{
					StageID: s.StageID, MemberID: s.MemberID, Delta: f.ContentDelta,
				})
			}
			if f.ToolCallDelta != nil {
				d := f.ToolCallDelta
				p, ok := pendingMeta[d.Index]
				if !ok {
					p = &partial{}
					pendingMeta[d.Index] = p
					pendingArgs[d.Index] = &strings.Builder{}
				}
				if d.ID != "" {
					p.ID = d.ID
				}
				if d.FunctionName != "" {
					p.Name = d.FunctionName
				}
				pendingArgs[d.Index].WriteString(d.ArgsDelta)
			}
			if f.Usage != nil {
				totalIn += f.Usage.PromptTokens
				totalOut += f.Usage.CompletionTokens
			}
			if f.FinishReason != "" {
				asst.ToolCalls = finalizeToolCalls(pendingMeta, pendingArgs)
			}
		}
		if err := wait(); err != nil {
			a.pub(event.StageFailed{StageID: s.StageID, MemberID: s.MemberID, Err: err.Error()})
			return nil, err
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		// If nothing was assembled via finish_reason (some providers omit it),
		// try to flush any pending tool calls we accumulated.
		if len(asst.ToolCalls) == 0 && len(pendingMeta) > 0 {
			asst.ToolCalls = finalizeToolCalls(pendingMeta, pendingArgs)
		}
		msgs = append(msgs, asst)

		if len(asst.ToolCalls) == 0 {
			// End of turn. Enforce artifact invariant.
			if s.ArtifactPath != "" && !artifactExists(s.ArtifactPath) {
				msgs = append(msgs, openrouter.Message{
					Role: "user",
					Content: fmt.Sprintf(
						"You have not yet produced the required artifact %q. Call write_file with path=%q before ending your turn.",
						s.ArtifactName, s.ArtifactPath),
				})
				continue
			}
			dur := time.Since(start)
			a.pub(event.StageCompleted{
				StageID: s.StageID, MemberID: s.MemberID,
				Turns: turn, InputTokens: totalIn, OutputTokens: totalOut, Dur: dur,
			})
			return &Result{Turns: turn, InputTokens: totalIn, OutputTokens: totalOut, Dur: dur}, nil
		}

		for _, tc := range asst.ToolCalls {
			a.pub(event.ToolCalled{
				StageID: s.StageID, MemberID: s.MemberID,
				ToolName: tc.Function.Name, CallID: tc.ID,
				Args: json.RawMessage(tc.Function.Arguments),
			})
			res := a.Tools.Dispatch(ctx, tools.Call{
				ID:        tc.ID,
				Name:      tc.Function.Name,
				Arguments: json.RawMessage(tc.Function.Arguments),
			}, s.PersonaTools)
			a.pub(event.ToolCompleted{
				StageID: s.StageID, MemberID: s.MemberID,
				ToolName: tc.Function.Name, CallID: tc.ID,
				ResultSummary: summarize(res.Content), Bytes: len(res.Content), Err: res.Error,
			})
			if s.ArtifactPath != "" && tc.Function.Name == "write_file" && wroteArtifact(tc, s.ArtifactPath) {
				a.pub(event.ArtifactWritten{
					StageID: s.StageID, MemberID: s.MemberID,
					Name: s.ArtifactName, Path: s.ArtifactPath, Bytes: artifactSize(s.ArtifactPath),
				})
			}
			content := res.Content
			if res.Error != "" {
				if content != "" {
					content = "ERROR: " + res.Error + "\n" + content
				} else {
					content = "ERROR: " + res.Error
				}
			}
			msgs = append(msgs, openrouter.Message{
				Role: "tool", ToolCallID: tc.ID, Name: tc.Function.Name, Content: content,
			})
		}
	}
	a.pub(event.StageFailed{StageID: s.StageID, MemberID: s.MemberID, Err: ErrMaxTurnsExceeded.Error()})
	return nil, ErrMaxTurnsExceeded
}

func (a *Agent) pub(ev event.Event) {
	if a.Bus != nil {
		a.Bus.Publish(ev)
	}
}

type partial struct {
	ID   string
	Name string
}

func finalizeToolCalls(meta map[int]*partial, args map[int]*strings.Builder) []openrouter.ToolCall {
	if len(meta) == 0 {
		return nil
	}
	keys := make([]int, 0, len(meta))
	for k := range meta {
		keys = append(keys, k)
	}
	// Sort by index.
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j-1] > keys[j]; j-- {
			keys[j-1], keys[j] = keys[j], keys[j-1]
		}
	}
	out := make([]openrouter.ToolCall, 0, len(keys))
	for _, k := range keys {
		raw := ""
		if b, ok := args[k]; ok {
			raw = b.String()
		}
		if raw == "" {
			raw = "{}"
		}
		out = append(out, openrouter.ToolCall{
			ID:       meta[k].ID,
			Type:     "function",
			Function: openrouter.ToolFunc{Name: meta[k].Name, Arguments: raw},
		})
	}
	return out
}
