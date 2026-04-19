// Package artifact manages the on-disk layout for a workflow run.
//
// Each run is rooted at .baton/runs/<ulid>/ with this layout:
//
//	workflow.yaml      verbatim copy of the executed workflow
//	variables.json     resolved variable values
//	events.ndjson      append-only event log (canonical record)
//	artifacts/         logical artifacts read by later stages
//	artifacts.prev-N/  prior values archived on re-entry
//	stages/<id>/       per-stage message transcripts and usage
package artifact

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
)

// Run represents a live run's on-disk directory layout.
type Run struct {
	ID        string
	Root      string // .baton/runs/<ulid>
	StartedAt time.Time
	reentries map[string]int
}

// NewRun creates a fresh run directory under rootDir. rootDir is usually
// ".baton/runs". The returned Run has ID and Root populated; directories
// are created.
func NewRun(rootDir string) (*Run, error) {
	t := time.Now()
	id := ulid.MustNew(ulid.Timestamp(t), cryptoReader{}).String()
	root := filepath.Join(rootDir, id)
	for _, sub := range []string{"", "artifacts", "stages"} {
		if err := os.MkdirAll(filepath.Join(root, sub), 0o755); err != nil {
			return nil, fmt.Errorf("mkdir %s: %w", sub, err)
		}
	}
	return &Run{
		ID:        id,
		Root:      root,
		StartedAt: t,
		reentries: map[string]int{},
	}, nil
}

// ArtifactsDir is the logical artifacts directory (artifacts/).
func (r *Run) ArtifactsDir() string { return filepath.Join(r.Root, "artifacts") }

// StagesDir is the per-stage data directory (stages/).
func (r *Run) StagesDir() string { return filepath.Join(r.Root, "stages") }

// ArtifactPath resolves a logical artifact name to its absolute path.
// Relative names (no leading /) are rooted under ArtifactsDir.
func (r *Run) ArtifactPath(name string) string {
	clean := filepath.Clean(name)
	if filepath.IsAbs(clean) {
		return clean
	}
	return filepath.Join(r.ArtifactsDir(), clean)
}

// WriteArtifact writes content to the artifact with the given logical name.
func (r *Run) WriteArtifact(name string, content []byte) (string, error) {
	p := r.ArtifactPath(name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(p, content, 0o644); err != nil {
		return "", err
	}
	return p, nil
}

// ReadArtifact reads an artifact by logical name.
func (r *Run) ReadArtifact(name string) (string, error) {
	p := r.ArtifactPath(name)
	b, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ArtifactExists reports whether an artifact file exists.
func (r *Run) ArtifactExists(name string) bool {
	_, err := os.Stat(r.ArtifactPath(name))
	return err == nil
}

// StageDir returns the path for a stage's transcript directory, creating
// it on first access. If memberID is empty, it points at stages/<stageID>/;
// otherwise stages/<stageID>/<memberID>/.
func (r *Run) StageDir(stageID, memberID string) (string, error) {
	parts := []string{r.StagesDir(), stageID}
	if memberID != "" {
		parts = append(parts, memberID)
	}
	p := filepath.Join(parts...)
	if err := os.MkdirAll(p, 0o755); err != nil {
		return "", err
	}
	return p, nil
}

// WriteWorkflowCopy writes a verbatim copy of the workflow YAML source.
func (r *Run) WriteWorkflowCopy(src []byte) error {
	return os.WriteFile(filepath.Join(r.Root, "workflow.yaml"), src, 0o644)
}

// WriteVariables writes the resolved variables as JSON.
func (r *Run) WriteVariables(vars map[string]string) error {
	b, err := json.MarshalIndent(vars, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(r.Root, "variables.json"), b, 0o644)
}

// EventLogPath returns the path of the NDJSON event log.
func (r *Run) EventLogPath() string {
	return filepath.Join(r.Root, "events.ndjson")
}

// ArchivePriorArtifacts moves artifacts listed in names to artifacts.prev-<n>/
// where n is the re-entry count for stageID (starting at 1). It is used when
// re-entering a stage to preserve the prior run's outputs for audit.
func (r *Run) ArchivePriorArtifacts(stageID string, names []string) (archivedDir string, err error) {
	r.reentries[stageID]++
	n := r.reentries[stageID]
	dir := filepath.Join(r.Root, fmt.Sprintf("artifacts.prev-%d", n))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	for _, name := range names {
		src := r.ArtifactPath(name)
		if _, err := os.Stat(src); errors.Is(err, os.ErrNotExist) {
			continue
		}
		dst := filepath.Join(dir, filepath.Base(name))
		if err := moveFile(src, dst); err != nil {
			return "", fmt.Errorf("archive %s: %w", name, err)
		}
	}
	return dir, nil
}

// ReentryCount returns the number of times stageID has been re-entered.
func (r *Run) ReentryCount(stageID string) int { return r.reentries[stageID] }

// WithinRun returns true iff absPath is inside the run's root directory.
func (r *Run) WithinRun(absPath string) bool {
	abs, err := filepath.Abs(absPath)
	if err != nil {
		return false
	}
	root, err := filepath.Abs(r.Root)
	if err != nil {
		return false
	}
	return strings.HasPrefix(abs, root+string(filepath.Separator))
}

func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	// Cross-device fallback: copy+remove.
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Remove(src)
}

// cryptoReader satisfies io.Reader using crypto/rand for ULID entropy.
type cryptoReader struct{}

func (cryptoReader) Read(p []byte) (int, error) { return rand.Read(p) }
