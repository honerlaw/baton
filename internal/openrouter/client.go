// Package openrouter is a streaming HTTP client for OpenRouter's
// OpenAI-compatible chat completions endpoint. No SDK dependency; stdlib
// net/http + encoding/json only.
package openrouter

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"time"
)

// Client is the minimal chat-completions client.
type Client struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
	maxRetries int
	referer    string
	title      string
}

// Option configures the Client.
type Option func(*Client)

// WithBaseURL overrides the OpenRouter base URL.
func WithBaseURL(u string) Option { return func(c *Client) { c.baseURL = u } }

// WithHTTPClient sets a custom http.Client.
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.httpClient = h } }

// WithMaxRetries sets the transport-layer retry count for 5xx/EOF.
func WithMaxRetries(n int) Option { return func(c *Client) { c.maxRetries = n } }

// NewClient constructs a streaming client.
func NewClient(apiKey string, opts ...Option) *Client {
	c := &Client{
		apiKey:     apiKey,
		baseURL:    "https://openrouter.ai/api/v1",
		httpClient: &http.Client{Timeout: 0},
		maxRetries: 2,
		referer:    "https://github.com/honerlaw/baton",
		title:      "baton",
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// ChatStream streams a chat completion. Frames are delivered via the returned
// channel; the channel is closed when the stream ends or an error occurs.
// The returned err func blocks until the stream finishes and returns any
// terminal error.
func (c *Client) ChatStream(ctx context.Context, req ChatRequest) (<-chan StreamFrame, func() error) {
	out := make(chan StreamFrame, 32)
	errCh := make(chan error, 1)
	go func() {
		defer close(out)
		err := c.runWithRetries(ctx, req, out)
		errCh <- err
	}()
	return out, func() error { return <-errCh }
}

func (c *Client) runWithRetries(ctx context.Context, req ChatRequest, out chan<- StreamFrame) error {
	req.Stream = true
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			d := backoff(attempt)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(d):
			}
		}
		err := c.streamOnce(ctx, body, out)
		if err == nil {
			return nil
		}
		if !isRetryable(err) {
			return err
		}
		lastErr = err
	}
	return fmt.Errorf("retries exhausted: %w", lastErr)
}

func (c *Client) streamOnce(ctx context.Context, body []byte, out chan<- StreamFrame) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("HTTP-Referer", c.referer)
	httpReq.Header.Set("X-Title", c.title)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return &retryable{err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return classifyStatus(resp.StatusCode, resp.Header, b)
	}

	return parseSSE(resp.Body, out)
}

// parseSSE reads an SSE stream from r and writes normalized frames to out.
func parseSSE(r io.Reader, out chan<- StreamFrame) error {
	br := bufio.NewReaderSize(r, 1<<20)
	pending := map[int]*partialToolCall{}
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				if line == "" {
					return finishPartials(pending, out)
				}
			} else {
				return &retryable{err: err}
			}
		}
		line = trimEOL(line)
		if line == "" {
			continue
		}
		if !bytes.HasPrefix([]byte(line), []byte("data:")) {
			continue
		}
		payload := trimLeadingSpace(line[len("data:"):])
		if payload == "[DONE]" {
			if err := finishPartials(pending, out); err != nil {
				return err
			}
			out <- StreamFrame{Done: true}
			return nil
		}
		var chunk streamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			// Ignore malformed chunks rather than crashing the stream.
			continue
		}
		if chunk.Usage != nil {
			out <- StreamFrame{Usage: chunk.Usage}
		}
		for _, ch := range chunk.Choices {
			if ch.Delta.Content != "" {
				out <- StreamFrame{ContentDelta: ch.Delta.Content}
			}
			for _, tc := range ch.Delta.ToolCalls {
				p, ok := pending[tc.Index]
				if !ok {
					p = &partialToolCall{Index: tc.Index}
					pending[tc.Index] = p
				}
				if tc.ID != "" {
					p.ID = tc.ID
				}
				if tc.Function.Name != "" {
					p.Name = tc.Function.Name
				}
				if tc.Function.Arguments != "" {
					p.Args.WriteString(tc.Function.Arguments)
				}
				out <- StreamFrame{ToolCallDelta: &ToolCallDelta{
					Index:        tc.Index,
					ID:           tc.ID,
					FunctionName: tc.Function.Name,
					ArgsDelta:    tc.Function.Arguments,
				}}
			}
			if ch.FinishReason != "" {
				out <- StreamFrame{FinishReason: ch.FinishReason}
			}
		}
	}
}

// AccumulateFrames turns a stream into a FinalMessage. Consumers who don't
// need to render deltas live can use this and ignore the per-frame API.
func AccumulateFrames(frames <-chan StreamFrame) FinalMessage {
	var m FinalMessage
	pending := map[int]*partialToolCall{}
	for f := range frames {
		switch {
		case f.ContentDelta != "":
			m.Content += f.ContentDelta
		case f.ToolCallDelta != nil:
			d := f.ToolCallDelta
			p, ok := pending[d.Index]
			if !ok {
				p = &partialToolCall{Index: d.Index}
				pending[d.Index] = p
			}
			if d.ID != "" {
				p.ID = d.ID
			}
			if d.FunctionName != "" {
				p.Name = d.FunctionName
			}
			p.Args.WriteString(d.ArgsDelta)
		case f.Usage != nil:
			m.Usage = *f.Usage
		case f.FinishReason != "":
			m.FinishReason = f.FinishReason
		}
	}
	m.ToolCalls = flattenPartials(pending)
	return m
}

// finishPartials emits nothing new (tool calls are consumed incrementally
// via ToolCallDelta frames); it exists only to signal to downstream that
// pending state is complete at end of stream.
func finishPartials(_ map[int]*partialToolCall, _ chan<- StreamFrame) error {
	return nil
}

func flattenPartials(pending map[int]*partialToolCall) []ToolCall {
	if len(pending) == 0 {
		return nil
	}
	keys := make([]int, 0, len(pending))
	for k := range pending {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	out := make([]ToolCall, 0, len(keys))
	for _, k := range keys {
		p := pending[k]
		args := p.Args.String()
		if args == "" {
			args = "{}"
		}
		out = append(out, ToolCall{
			ID:       p.ID,
			Type:     "function",
			Function: ToolFunc{Name: p.Name, Arguments: args},
		})
	}
	return out
}

type partialToolCall struct {
	Index int
	ID    string
	Name  string
	Args  bytes.Buffer
}

type streamChunk struct {
	Choices []struct {
		Delta struct {
			Role      string `json:"role,omitempty"`
			Content   string `json:"content,omitempty"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id,omitempty"`
				Type     string `json:"type,omitempty"`
				Function struct {
					Name      string `json:"name,omitempty"`
					Arguments string `json:"arguments,omitempty"`
				} `json:"function,omitempty"`
			} `json:"tool_calls,omitempty"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason,omitempty"`
	} `json:"choices"`
	Usage *Usage `json:"usage,omitempty"`
}

func trimEOL(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

func trimLeadingSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	return s
}

// Error classification.

var (
	ErrAuth      = errors.New("openrouter: auth error")
	ErrClient    = errors.New("openrouter: client error")
	ErrRateLimit = errors.New("openrouter: rate limited")
	ErrServer    = errors.New("openrouter: server error")
)

type retryable struct {
	err        error
	retryAfter time.Duration
}

func (e *retryable) Error() string { return e.err.Error() }
func (e *retryable) Unwrap() error { return e.err }

func isRetryable(err error) bool {
	var r *retryable
	return errors.As(err, &r)
}

func classifyStatus(status int, h http.Header, body []byte) error {
	msg := fmt.Sprintf("HTTP %d: %s", status, string(body))
	switch {
	case status == http.StatusUnauthorized, status == http.StatusForbidden:
		return fmt.Errorf("%w: %s", ErrAuth, msg)
	case status == http.StatusTooManyRequests:
		r := &retryable{err: fmt.Errorf("%w: %s", ErrRateLimit, msg)}
		if ra := h.Get("Retry-After"); ra != "" {
			if secs, err := strconv.Atoi(ra); err == nil {
				r.retryAfter = time.Duration(secs) * time.Second
			}
		}
		return r
	case status >= 500:
		return &retryable{err: fmt.Errorf("%w: %s", ErrServer, msg)}
	default:
		return fmt.Errorf("%w: %s", ErrClient, msg)
	}
}

func backoff(attempt int) time.Duration {
	base := 500 * time.Millisecond
	for i := 1; i < attempt; i++ {
		base *= 3
	}
	if base > 5*time.Second {
		base = 5 * time.Second
	}
	return base
}
