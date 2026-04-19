// Package assets embeds the default personas and workflows that ship
// with the binary. `baton init` scaffolds these to disk.
package assets

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

//go:embed agents/*.md workflows/*.yaml
var FS embed.FS

// PersonasFS returns an fs.FS rooted at the embedded agents directory.
// Personas are at the root of the returned FS (no "agents/" prefix).
func PersonasFS() fs.FS {
	sub, err := fs.Sub(FS, "agents")
	if err != nil {
		// embed guarantees directory exists; panic is a bug.
		panic(err)
	}
	return sub
}

// WorkflowsFS returns an fs.FS rooted at the embedded workflows directory.
func WorkflowsFS() fs.FS {
	sub, err := fs.Sub(FS, "workflows")
	if err != nil {
		panic(err)
	}
	return sub
}

// Scaffold writes every embedded persona and workflow to the right
// directory under dstRoot. It only creates files; existing files are
// left untouched unless overwrite is true.
//
// Layout written:
//
//	<dstRoot>/.claude/agents/*.md
//	<dstRoot>/.baton/workflows/*.yaml
func Scaffold(dstRoot string, overwrite bool) (wrote []string, err error) {
	pairs := []struct {
		embedDir string
		destSub  string
	}{
		{"agents", filepath.Join(".claude", "agents")},
		{"workflows", filepath.Join(".baton", "workflows")},
	}
	for _, p := range pairs {
		destDir := filepath.Join(dstRoot, p.destSub)
		if err := os.MkdirAll(destDir, 0o755); err != nil {
			return wrote, err
		}
		entries, err := fs.ReadDir(FS, p.embedDir)
		if err != nil {
			return wrote, err
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			src := p.embedDir + "/" + e.Name()
			dst := filepath.Join(destDir, e.Name())
			if !overwrite {
				if _, err := os.Stat(dst); err == nil {
					continue
				}
			}
			b, err := fs.ReadFile(FS, src)
			if err != nil {
				return wrote, err
			}
			if err := os.WriteFile(dst, b, 0o644); err != nil {
				return wrote, err
			}
			wrote = append(wrote, dst)
		}
	}
	return wrote, nil
}

// LoadEmbeddedWorkflow parses an embedded workflow by filename (with
// or without .yaml extension) and returns the raw bytes.
func LoadEmbeddedWorkflow(name string) ([]byte, error) {
	if !strings.HasSuffix(name, ".yaml") {
		name += ".yaml"
	}
	b, err := fs.ReadFile(FS, "workflows/"+name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("embedded workflow %q not found", name)
		}
		return nil, err
	}
	return b, nil
}
