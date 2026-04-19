package openrouter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChatStream_ContentAndToolCalls(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(strings.Join([]string{
			`data: {"choices":[{"delta":{"role":"assistant","content":"Hello"}}]}`,
			``,
			`data: {"choices":[{"delta":{"content":", world"}}]}`,
			``,
			`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"read_file"}}]}}]}`,
			``,
			`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"path\":"}}]}}]}`,
			``,
			`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"x.md\"}"}}]}}]}`,
			``,
			`data: {"choices":[{"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":4,"total_tokens":14}}`,
			``,
			`data: [DONE]`,
			``,
		}, "\n")))
	}))
	defer ts.Close()

	c := NewClient("key", WithBaseURL(ts.URL))
	frames, wait := c.ChatStream(context.Background(), ChatRequest{
		Model: "m", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	msg := AccumulateFrames(frames)
	if err := wait(); err != nil {
		t.Fatalf("stream err: %v", err)
	}
	if msg.Content != "Hello, world" {
		t.Fatalf("content=%q", msg.Content)
	}
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("tool calls=%d", len(msg.ToolCalls))
	}
	tc := msg.ToolCalls[0]
	if tc.ID != "call_1" || tc.Function.Name != "read_file" {
		t.Fatalf("tool call mismatch: %+v", tc)
	}
	if tc.Function.Arguments != `{"path":"x.md"}` {
		t.Fatalf("args=%q", tc.Function.Arguments)
	}
	if msg.Usage.TotalTokens != 14 {
		t.Fatalf("usage=%+v", msg.Usage)
	}
	if msg.FinishReason != "tool_calls" {
		t.Fatalf("finish=%q", msg.FinishReason)
	}
}

func TestChatStream_AuthErrorNotRetried(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad key", http.StatusUnauthorized)
	}))
	defer ts.Close()
	c := NewClient("x", WithBaseURL(ts.URL), WithMaxRetries(0))
	frames, wait := c.ChatStream(context.Background(), ChatRequest{Model: "m"})
	for range frames {
	}
	err := wait()
	if err == nil {
		t.Fatal("expected error")
	}
}
