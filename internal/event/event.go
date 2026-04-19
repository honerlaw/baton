// Package event defines the event types emitted by the orchestrator and
// the fan-out bus that carries them to subscribers (TUI, plaintext renderer,
// NDJSON logger).
package event

import (
	"encoding/json"
	"time"
)

// Event is implemented by every concrete event type. The unexported method
// keeps it sealed to this package.
type Event interface {
	isEvent()
	// Kind is a short stable string identifying the event (used by the
	// NDJSON logger and for debugging).
	Kind() string
}

type StageStarted struct {
	StageID  string    `json:"stage_id"`
	MemberID string    `json:"member_id,omitempty"`
	Persona  string    `json:"persona"`
	Model    string    `json:"model"`
	At       time.Time `json:"at"`
}

func (StageStarted) isEvent()     {}
func (StageStarted) Kind() string { return "stage_started" }

type MessageStreamed struct {
	StageID  string `json:"stage_id"`
	MemberID string `json:"member_id,omitempty"`
	Delta    string `json:"delta"`
}

func (MessageStreamed) isEvent()     {}
func (MessageStreamed) Kind() string { return "message_streamed" }

type ToolCalled struct {
	StageID  string          `json:"stage_id"`
	MemberID string          `json:"member_id,omitempty"`
	ToolName string          `json:"tool_name"`
	CallID   string          `json:"call_id"`
	Args     json.RawMessage `json:"args"`
}

func (ToolCalled) isEvent()     {}
func (ToolCalled) Kind() string { return "tool_called" }

type ToolCompleted struct {
	StageID       string `json:"stage_id"`
	MemberID      string `json:"member_id,omitempty"`
	ToolName      string `json:"tool_name"`
	CallID        string `json:"call_id"`
	ResultSummary string `json:"result_summary"`
	Bytes         int    `json:"bytes"`
	Err           string `json:"err,omitempty"`
}

func (ToolCompleted) isEvent()     {}
func (ToolCompleted) Kind() string { return "tool_completed" }

type ArtifactWritten struct {
	StageID  string `json:"stage_id"`
	MemberID string `json:"member_id,omitempty"`
	Name     string `json:"name"`
	Path     string `json:"path"`
	Bytes    int    `json:"bytes"`
}

func (ArtifactWritten) isEvent()     {}
func (ArtifactWritten) Kind() string { return "artifact_written" }

type StageCompleted struct {
	StageID      string        `json:"stage_id"`
	MemberID     string        `json:"member_id,omitempty"`
	Turns        int           `json:"turns"`
	InputTokens  int           `json:"input_tokens"`
	OutputTokens int           `json:"output_tokens"`
	Dur          time.Duration `json:"duration_ns"`
}

func (StageCompleted) isEvent()     {}
func (StageCompleted) Kind() string { return "stage_completed" }

type StageFailed struct {
	StageID  string `json:"stage_id"`
	MemberID string `json:"member_id,omitempty"`
	Err      string `json:"err"`
}

func (StageFailed) isEvent()     {}
func (StageFailed) Kind() string { return "stage_failed" }

type ReviewAggregated struct {
	StageID string   `json:"stage_id"`
	Summary string   `json:"summary"`
	Failed  []string `json:"failed,omitempty"`
}

func (ReviewAggregated) isEvent()     {}
func (ReviewAggregated) Kind() string { return "review_aggregated" }

type WorkflowCompleted struct {
	RunID   string `json:"run_id"`
	Verdict string `json:"verdict"`
}

func (WorkflowCompleted) isEvent()     {}
func (WorkflowCompleted) Kind() string { return "workflow_completed" }

type WorkflowHalted struct {
	StageID string `json:"stage_id"`
	Reason  string `json:"reason"`
}

func (WorkflowHalted) isEvent()     {}
func (WorkflowHalted) Kind() string { return "workflow_halted" }
