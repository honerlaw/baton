package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"

	"github.com/bmatcuk/doublestar/v4"
)

// Search does a content search across files. Prefers ripgrep when
// available, falls back to a pure-Go regex walk.
type Search struct {
	WorkDir string
}

func (*Search) Name() string { return "search" }
func (*Search) Description() string {
	return "Search file contents for a regex. Uses ripgrep when available, else a pure-Go walk. Returns matching lines prefixed with their path."
}

func (*Search) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "pattern": {"type": "string", "description": "Regex pattern."},
    "path":    {"type": "string", "description": "Subdirectory of the working dir to search (optional)."},
    "glob":    {"type": "string", "description": "Optional doublestar glob filter (e.g. '**/*.go')."}
  },
  "required": ["pattern"]
}`)
}

func (t *Search) Execute(ctx context.Context, raw json.RawMessage) (string, error) {
	var args struct {
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
		Glob    string `json:"glob"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if args.Pattern == "" {
		return "", fmt.Errorf("pattern is required")
	}
	root := t.WorkDir
	if args.Path != "" {
		root = filepath.Join(t.WorkDir, args.Path)
	}
	if rgPath, err := exec.LookPath("rg"); err == nil {
		return runRipgrep(ctx, rgPath, root, args.Pattern, args.Glob)
	}
	return walkSearch(root, args.Pattern, args.Glob)
}

func runRipgrep(ctx context.Context, rg, root, pattern, glob string) (string, error) {
	rgArgs := []string{"--line-number", "--no-heading", "--color=never", pattern, root}
	if glob != "" {
		rgArgs = append([]string{"--glob", glob}, rgArgs...)
	}
	cmd := exec.CommandContext(ctx, rg, rgArgs...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		// rg returns non-zero with no matches. Only error if combined output is empty.
		if buf.Len() == 0 {
			return "(no matches)", nil
		}
	}
	return buf.String(), nil
}

func walkSearch(root, pattern, glob string) (string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("bad regex: %w", err)
	}
	var out bytes.Buffer
	walker := func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "node_modules" {
				return fs.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if glob != "" {
			ok, _ := doublestar.Match(glob, rel)
			if !ok {
				return nil
			}
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		for i, line := range bytes.Split(b, []byte("\n")) {
			if re.Match(line) {
				fmt.Fprintf(&out, "%s:%d:%s\n", rel, i+1, line)
			}
		}
		return nil
	}
	if err := filepath.WalkDir(root, walker); err != nil {
		return "", err
	}
	if out.Len() == 0 {
		return "(no matches)", nil
	}
	return out.String(), nil
}
