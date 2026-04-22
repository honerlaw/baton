package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestListFiles_RejectsEscape ensures a crafted cwd cannot list files outside
// the working directory.
func TestListFiles_RejectsEscape(t *testing.T) {
	outer := t.TempDir()
	workDir := filepath.Join(outer, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A file that lives outside workDir; must not be reachable.
	if err := os.WriteFile(filepath.Join(outer, "secret.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	lf := &ListFiles{WorkDir: workDir}
	args, _ := json.Marshal(map[string]string{"pattern": "*.txt", "cwd": ".."})
	_, err := lf.Execute(context.Background(), args)
	if !errors.Is(err, ErrPathOutsideSafe) {
		t.Fatalf("expected ErrPathOutsideSafe, got %v", err)
	}
}

// TestListFiles_AllowsWithinWorkDir sanity-checks the happy path still works
// for a legitimate subdirectory.
func TestListFiles_AllowsWithinWorkDir(t *testing.T) {
	workDir := t.TempDir()
	sub := filepath.Join(workDir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "ok.txt"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}

	lf := &ListFiles{WorkDir: workDir}
	args, _ := json.Marshal(map[string]string{"pattern": "*.txt", "cwd": "sub"})
	out, err := lf.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "ok.txt" {
		t.Fatalf("expected ok.txt, got %q", out)
	}
}

// TestSearch_RejectsEscape ensures a crafted path cannot search files outside
// the working directory.
func TestSearch_RejectsEscape(t *testing.T) {
	outer := t.TempDir()
	workDir := filepath.Join(outer, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outer, "secret.txt"), []byte("needle"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := &Search{WorkDir: workDir}
	args, _ := json.Marshal(map[string]string{"pattern": "needle", "path": ".."})
	_, err := s.Execute(context.Background(), args)
	if !errors.Is(err, ErrPathOutsideSafe) {
		t.Fatalf("expected ErrPathOutsideSafe, got %v", err)
	}
}
