package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// ReadFile reads a file from disk within safe directories.
type ReadFile struct {
	WorkDir string
	RunRoot string
	MaxSize int64 // default 1MB; callers can override for tests
}

func (*ReadFile) Name() string { return "read_file" }
func (*ReadFile) Description() string {
	return "Read the contents of a file from the working directory or the current run directory."
}

func (*ReadFile) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "path":   {"type": "string", "description": "File path (absolute or relative to the working directory)."},
    "offset": {"type": "integer", "description": "1-indexed starting line (optional)."},
    "limit":  {"type": "integer", "description": "Max number of lines to read (optional)."}
  },
  "required": ["path"]
}`)
}

func (t *ReadFile) Execute(ctx context.Context, raw json.RawMessage) (string, error) {
	var args struct {
		Path   string `json:"path"`
		Offset int    `json:"offset"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if args.Path == "" {
		return "", fmt.Errorf("path is required")
	}
	abs, err := resolveSafe(args.Path, t.WorkDir, t.RunRoot)
	if err != nil {
		return "", err
	}
	f, err := os.Open(abs)
	if err != nil {
		return "", err
	}
	defer f.Close()

	maxBytes := t.MaxSize
	if maxBytes <= 0 {
		maxBytes = 1 << 20
	}
	if args.Offset == 0 && args.Limit == 0 {
		info, err := f.Stat()
		if err != nil {
			return "", err
		}
		if info.Size() > maxBytes {
			return "", fmt.Errorf("file %s is %d bytes (>%d); use offset/limit", args.Path, info.Size(), maxBytes)
		}
		b, err := io.ReadAll(f)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	b, err := io.ReadAll(io.LimitReader(f, maxBytes*8))
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(b), "\n")
	start := args.Offset - 1
	if start < 0 {
		start = 0
	}
	if start >= len(lines) {
		return "", nil
	}
	end := len(lines)
	if args.Limit > 0 && start+args.Limit < end {
		end = start + args.Limit
	}
	return strings.Join(lines[start:end], "\n"), nil
}
