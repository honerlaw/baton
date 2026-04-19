package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// WriteFile writes content to a file. Path must be inside the working
// directory or the run root.
type WriteFile struct {
	WorkDir string
	RunRoot string
}

func (*WriteFile) Name() string { return "write_file" }
func (*WriteFile) Description() string {
	return "Write content to a file. The path must be inside the working directory or the current run's directory. Overwrites without prompting."
}

func (*WriteFile) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "path":    {"type": "string"},
    "content": {"type": "string"}
  },
  "required": ["path", "content"]
}`)
}

func (t *WriteFile) Execute(ctx context.Context, raw json.RawMessage) (string, error) {
	var args struct {
		Path    string `json:"path"`
		Content string `json:"content"`
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
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(abs, []byte(args.Content), 0o644); err != nil {
		return "", err
	}
	return fmt.Sprintf("wrote %d bytes to %s", len(args.Content), abs), nil
}
