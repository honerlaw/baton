package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// maxBashOutputBytes caps combined stdout+stderr captured from a bash run.
// Unbounded capture would let a runaway command exhaust memory; at this
// size the tail is truncated and a marker is appended.
const maxBashOutputBytes = 1 << 20 // 1 MiB

// bashEnvAllowlist is the exact set of environment variable names passed
// through to bash. Anything outside this list — including API keys
// (OPENROUTER_API_KEY), auth tokens, and baton's own BATON_* vars — is
// withheld so a compromised persona cannot exfiltrate them via `echo
// $FOO | curl ...`. Prefixes are handled separately in buildBashEnv.
var bashEnvAllowlist = map[string]bool{
	"HOME":           true,
	"PATH":           true,
	"USER":           true,
	"LOGNAME":        true,
	"SHELL":          true,
	"TERM":           true,
	"LANG":           true,
	"TZ":             true,
	"TMPDIR":         true,
	"PWD":            true,
	"GOPATH":         true,
	"GOCACHE":        true,
	"GOMODCACHE":     true,
	"GOTOOLCHAIN":    true,
	"GOFLAGS":        true,
	"CARGO_HOME":     true,
	"RUSTUP_HOME":    true,
	"NODE_PATH":      true,
	"PYTHONPATH":     true,
	"VIRTUAL_ENV":    true,
	"NO_COLOR":       true,
	"CLICOLOR":       true,
	"FORCE_COLOR":    true,
	"COLUMNS":        true,
	"LINES":          true,
	"EDITOR":         true,
	"VISUAL":         true,
	"PAGER":          true,
	"LESS":           true,
	"PS1":            true,
	"HISTFILE":       true,
	"XDG_CACHE_HOME": true,
	"XDG_DATA_HOME":  true,
	"XDG_STATE_HOME": true,
}

// bashEnvAllowlistPrefixes matches locale vars (LC_ALL, LC_CTYPE, …) in
// bulk, since the real LC_* set varies by platform.
var bashEnvAllowlistPrefixes = []string{"LC_"}

// Bash executes a shell command with a timeout and a scrubbed environment.
type Bash struct {
	WorkDir string
}

func (*Bash) Name() string { return "bash" }
func (*Bash) Description() string {
	return "Execute a shell command via /bin/sh -c. Captures combined stdout+stderr. Default timeout 60s, max 600s. The child process runs with a scrubbed environment (API keys and other secrets are not inherited) and captured output is capped at 1 MiB."
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
	cmd.Env = buildBashEnv(os.Environ())
	buf := &cappedBuffer{max: maxBashOutputBytes}
	cmd.Stdout = buf
	cmd.Stderr = buf
	err := cmd.Run()
	out := buf.String()
	if err != nil {
		return out, fmt.Errorf("exit: %w", err)
	}
	return out, nil
}

// buildBashEnv filters a process environ slice (KEY=VALUE) to just the
// names on bashEnvAllowlist (plus allowlisted prefixes).
func buildBashEnv(parent []string) []string {
	out := make([]string, 0, len(parent))
	for _, kv := range parent {
		eq := strings.IndexByte(kv, '=')
		if eq <= 0 {
			continue
		}
		name := kv[:eq]
		if bashEnvAllowlist[name] {
			out = append(out, kv)
			continue
		}
		for _, p := range bashEnvAllowlistPrefixes {
			if strings.HasPrefix(name, p) {
				out = append(out, kv)
				break
			}
		}
	}
	return out
}

// cappedBuffer is an io.Writer that appends to an in-memory buffer up to
// max bytes, then silently discards the rest and appends a truncation
// marker when String() is called. It never errors — a bash child should
// not be killed for producing too much output; the capture is just
// truncated.
type cappedBuffer struct {
	buf       bytes.Buffer
	max       int
	truncated bool
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	remaining := c.max - c.buf.Len()
	if remaining <= 0 {
		c.truncated = true
		return len(p), nil
	}
	if len(p) > remaining {
		_, _ = c.buf.Write(p[:remaining])
		c.truncated = true
		return len(p), nil
	}
	return c.buf.Write(p)
}

func (c *cappedBuffer) String() string {
	if c.truncated {
		return c.buf.String() + "\n[output truncated: exceeded 1 MiB cap]"
	}
	return c.buf.String()
}

// Compile-time assertion that cappedBuffer satisfies io.Writer.
var _ io.Writer = (*cappedBuffer)(nil)
