package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

// Bash executes a shell command with a timeout.
type Bash struct {
	WorkDir string
}

func (*Bash) Name() string { return "bash" }
func (*Bash) Description() string {
	return "Execute a shell command via /bin/sh -c. Captures combined stdout+stderr. Default timeout 60s, max 600s."
}

func (*Bash) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "command":   {"type": "string"},
    "timeout_s": {"type": "integer", "description": "Timeout in seconds (default 60, max 600)."}
  },
  "required": ["command"]
}`)
}

func (t *Bash) Execute(ctx context.Context, raw json.RawMessage) (string, error) {
	var args struct {
		Command  string `json:"command"`
		TimeoutS int    `json:"timeout_s"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if args.Command == "" {
		return "", fmt.Errorf("command is required")
	}
	timeout := 60 * time.Second
	if args.TimeoutS > 0 {
		timeout = time.Duration(args.TimeoutS) * time.Second
	}
	if timeout > 600*time.Second {
		timeout = 600 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cctx, "/bin/sh", "-c", args.Command)
	cmd.Dir = t.WorkDir
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	out := buf.String()
	if err != nil {
		return out, fmt.Errorf("exit: %w", err)
	}
	return out, nil
}
