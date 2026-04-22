package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestBash_EnvScrubbed ensures sensitive env vars are not inherited by
// the child shell. If a persona's bash tool could see OPENROUTER_API_KEY
// it could exfiltrate it via `echo $OPENROUTER_API_KEY | curl ...`.
func TestBash_EnvScrubbed(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "sekret")
	t.Setenv("AWS_ACCESS_KEY_ID", "leaky")
	t.Setenv("BATON_MODEL", "some/model")

	b := &Bash{WorkDir: t.TempDir()}
	args, _ := json.Marshal(map[string]string{
		"command": "env",
	})
	out, err := b.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v\noutput: %s", err, out)
	}
	for _, forbidden := range []string{
		"OPENROUTER_API_KEY", "AWS_ACCESS_KEY_ID", "BATON_MODEL",
	} {
		if strings.Contains(out, forbidden) {
			t.Errorf("env leaked %q in output:\n%s", forbidden, out)
		}
	}
	// Sanity: PATH should pass through so basic commands work.
	if !strings.Contains(out, "PATH=") {
		t.Errorf("PATH not propagated; bash tool would be effectively broken")
	}
}

// TestBash_OutputCapped verifies combined stdout+stderr are truncated at
// the configured cap, preventing a runaway command from exhausting memory.
func TestBash_OutputCapped(t *testing.T) {
	b := &Bash{WorkDir: t.TempDir()}
	// Produce well over maxBashOutputBytes of output.
	args, _ := json.Marshal(map[string]string{
		"command": "yes x | head -c 2000000",
	})
	out, err := b.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) > maxBashOutputBytes+200 { // 200-byte slack for the truncation marker
		t.Errorf("output not capped: got %d bytes, cap %d", len(out), maxBashOutputBytes)
	}
	if !strings.Contains(out, "truncated") {
		t.Errorf("expected truncation marker in output, got tail: %q", out[max(0, len(out)-200):])
	}
}

// TestBuildBashEnv_AllowlistSemantics verifies the filter keeps locale vars
// (LC_*) via the prefix rule and drops anything not explicitly allowed.
func TestBuildBashEnv_AllowlistSemantics(t *testing.T) {
	input := []string{
		"PATH=/usr/bin",
		"OPENROUTER_API_KEY=sekret",
		"LC_ALL=en_US.UTF-8",
		"GITHUB_TOKEN=ghp_xxx",
		"HOME=/home/u",
		"RANDOM_VAR=nope",
		"malformed-no-equals",
		"=empty-name",
	}
	got := buildBashEnv(input)
	set := map[string]string{}
	for _, kv := range got {
		i := strings.IndexByte(kv, '=')
		set[kv[:i]] = kv[i+1:]
	}
	for _, want := range []string{"PATH", "LC_ALL", "HOME"} {
		if _, ok := set[want]; !ok {
			t.Errorf("expected %q to survive allowlist, missing from %v", want, got)
		}
	}
	for _, forbidden := range []string{"OPENROUTER_API_KEY", "GITHUB_TOKEN", "RANDOM_VAR"} {
		if _, ok := set[forbidden]; ok {
			t.Errorf("expected %q to be dropped, survived", forbidden)
		}
	}
	if len(got) != 3 {
		t.Errorf("expected exactly 3 entries, got %d: %v", len(got), got)
	}
}
