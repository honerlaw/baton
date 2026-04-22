package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// ListFiles lists files matching a glob pattern under WorkDir.
type ListFiles struct {
	WorkDir string
}

func (*ListFiles) Name() string { return "list_files" }
func (*ListFiles) Description() string {
	return "List files matching a doublestar glob pattern (e.g. 'internal/**/*.go'). Returns one path per line, relative to the working directory."
}

func (*ListFiles) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "pattern": {"type": "string"},
    "cwd":     {"type": "string", "description": "Optional subdirectory to root the pattern in."}
  },
  "required": ["pattern"]
}`)
}

func (t *ListFiles) Execute(ctx context.Context, raw json.RawMessage) (string, error) {
	var args struct {
		Pattern string `json:"pattern"`
		Cwd     string `json:"cwd"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if args.Pattern == "" {
		return "", fmt.Errorf("pattern is required")
	}
	root := t.WorkDir
	if args.Cwd != "" {
		abs, err := resolveSafe(args.Cwd, t.WorkDir, "")
		if err != nil {
			return "", err
		}
		root = abs
	}
	fsys := os.DirFS(root)
	matches, err := doublestar.Glob(fsys, args.Pattern)
	if err != nil {
		return "", err
	}
	// Filter any match whose real (symlink-resolved) path escapes WorkDir.
	// os.DirFS does not sandbox symlinks; a symlink inside root pointing
	// outside would otherwise be listable.
	kept := matches[:0]
	for _, m := range matches {
		if resolvedUnder(filepath.Join(root, m), t.WorkDir) {
			kept = append(kept, m)
		}
	}
	sort.Strings(kept)
	if len(kept) == 0 {
		return "(no matches)", nil
	}
	return strings.Join(kept, "\n"), nil
}
