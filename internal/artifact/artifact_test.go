package artifact

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRun_WriteAndReadArtifact(t *testing.T) {
	r := mustNewRun(t)
	p, err := r.WriteArtifact("context.md", []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(p) != "context.md" {
		t.Fatalf("unexpected path %s", p)
	}
	got, err := r.ReadArtifact("context.md")
	if err != nil {
		t.Fatal(err)
	}
	if got != "hello" {
		t.Fatalf("got %q", got)
	}
}

func TestRun_ArchivePriorArtifacts(t *testing.T) {
	r := mustNewRun(t)
	for _, n := range []string{"a.md", "b.md"} {
		if _, err := r.WriteArtifact(n, []byte(n)); err != nil {
			t.Fatal(err)
		}
	}
	dir, err := r.ArchivePriorArtifacts("stage1", []string{"a.md", "b.md"})
	if err != nil {
		t.Fatal(err)
	}
	if r.ReentryCount("stage1") != 1 {
		t.Fatalf("expected reentry=1, got %d", r.ReentryCount("stage1"))
	}
	for _, n := range []string{"a.md", "b.md"} {
		if _, err := os.Stat(filepath.Join(dir, n)); err != nil {
			t.Fatalf("missing archived %s: %v", n, err)
		}
		if _, err := os.Stat(r.ArtifactPath(n)); !os.IsNotExist(err) {
			t.Fatalf("original %s should have been moved", n)
		}
	}
}

func TestRun_WithinRun(t *testing.T) {
	r := mustNewRun(t)
	if !r.WithinRun(r.ArtifactPath("x.md")) {
		t.Fatal("path inside artifacts should be within run")
	}
	if r.WithinRun("/etc/passwd") {
		t.Fatal("/etc/passwd should not be within run")
	}
}

func mustNewRun(t *testing.T) *Run {
	t.Helper()
	r, err := NewRun(t.TempDir())
	if err != nil {
		t.Fatalf("NewRun: %v", err)
	}
	return r
}
