package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
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

// TestListFiles_RejectsSymlinkEscape verifies that a symlink inside the
// working directory pointing outside it does not appear in list_files
// output, even though os.DirFS would otherwise expose it.
func TestListFiles_RejectsSymlinkEscape(t *testing.T) {
	outer := t.TempDir()
	workDir := filepath.Join(outer, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A target file outside workDir.
	target := filepath.Join(outer, "secret.txt")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Symlink inside workDir that points at it.
	if err := os.Symlink(target, filepath.Join(workDir, "leak.txt")); err != nil {
		t.Skipf("symlink not supported on this platform: %v", err)
	}
	// A benign file that should still be listed.
	if err := os.WriteFile(filepath.Join(workDir, "ok.txt"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}

	lf := &ListFiles{WorkDir: workDir}
	args, _ := json.Marshal(map[string]string{"pattern": "*.txt"})
	out, err := lf.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(out, "leak.txt") {
		t.Fatalf("symlink-escape exposed leak.txt: %q", out)
	}
	if !strings.Contains(out, "ok.txt") {
		t.Fatalf("expected ok.txt in output: %q", out)
	}
}

// TestWalkSearch_RejectsSymlinkEscape verifies that the pure-Go walker
// does not read through a symlink that points outside workDir.
// Bypasses ripgrep detection by calling walkSearch directly.
func TestWalkSearch_RejectsSymlinkEscape(t *testing.T) {
	outer := t.TempDir()
	workDir := filepath.Join(outer, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(outer, "secret.txt")
	if err := os.WriteFile(target, []byte("forbidden-needle"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(workDir, "leak.txt")); err != nil {
		t.Skipf("symlink not supported on this platform: %v", err)
	}

	out, err := walkSearch(workDir, workDir, "forbidden-needle", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(out, "forbidden-needle") {
		t.Fatalf("symlink-escape leaked secret content: %q", out)
	}
}
