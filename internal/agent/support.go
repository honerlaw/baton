package agent

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/honerlaw/baton/internal/openrouter"
)

// artifactExists reports whether the artifact file has been written.
func artifactExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// artifactSize returns the file size in bytes, or 0 on error.
func artifactSize(path string) int {
	fi, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return int(fi.Size())
}

// wroteArtifact reports whether the write_file call's path argument
// (resolved against the same rules as the tool) matches the expected
// artifact path. The agent can't reach into tools; it just compares the
// final absolute path.
func wroteArtifact(tc openrouter.ToolCall, expected string) bool {
	var a struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &a); err != nil {
		return false
	}
	if filepath.IsAbs(a.Path) {
		return a.Path == expected
	}
	abs, err := filepath.Abs(a.Path)
	if err != nil {
		return false
	}
	return abs == expected || filepath.Base(a.Path) == filepath.Base(expected)
}

// summarize truncates content for event payloads.
func summarize(s string) string {
	const limit = 512
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "…"
}
