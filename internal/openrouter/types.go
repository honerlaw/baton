package openrouter

import "encoding/json"

// ChatRequest is the body of POST /api/v1/chat/completions.
type ChatRequest struct {
	Model       string      `json:"model"`
	Messages    []Message   `json:"messages"`
	Tools       []ToolDecl  `json:"tools,omitempty"`
	ToolChoice  interface{} `json:"tool_choice,omitempty"` // typically "auto"
	Stream      bool        `json:"stream"`
	Temperature *float64    `json:"temperature,omitempty"`
}

// Message is the on-the-wire chat message shape.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	Name       string     `json:"name,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

// ToolCall represents a tool invocation in an assistant message.
type ToolCall struct {
	ID       string   `json:"id"`
	Type     string   `json:"type"`
	Function ToolFunc `json:"function"`
}

// ToolFunc is the function payload within a tool call.
type ToolFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolDecl advertises a tool to the model.
type ToolDecl struct {
	Type     string       `json:"type"`
	Function ToolFuncDecl `json:"function"`
}

// ToolFuncDecl is the function schema inside a ToolDecl.
type ToolFuncDecl struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// Usage reports token consumption for a single call.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// StreamFrame is one logical slice of a streaming response.
// Exactly one of ContentDelta, ToolCallDelta, or Usage is set (Done may
// accompany any).
type StreamFrame struct {
	ContentDelta  string
	ToolCallDelta *ToolCallDelta
	Usage         *Usage
	FinishReason  string
	Done          bool
}

// ToolCallDelta is a fragment of a streamed tool call.
type ToolCallDelta struct {
	Index        int
	ID           string
	FunctionName string
	ArgsDelta    string
}

// FinalMessage accumulates a full assistant message from streamed frames.
type FinalMessage struct {
	Content      string
	ToolCalls    []ToolCall
	Usage        Usage
	FinishReason string
}
