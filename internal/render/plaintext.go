package render

import (
	"fmt"
	"io"
	"strings"

	"github.com/honerlaw/baton/internal/event"
)

// Plaintext renders events to an io.Writer as human-readable lines.
// Used by --no-tui mode and when stdout is not a TTY.
type Plaintext struct {
	W io.Writer

	// Verbose enables per-token stream output. Off by default — a long
	// stream can overwhelm log files.
	Verbose bool
}

// Run consumes events from ch until the channel closes.
func (p *Plaintext) Run(ch <-chan event.Event) {
	for ev := range ch {
		p.render(ev)
	}
}

func (p *Plaintext) render(ev event.Event) {
	switch e := ev.(type) {
	case event.StageStarted:
		fmt.Fprintf(p.W, "▶ stage %s%s  persona=%s  model=%s\n",
			e.StageID, memberSuffix(e.MemberID), e.Persona, e.Model)
	case event.MessageStreamed:
		if p.Verbose {
			fmt.Fprint(p.W, e.Delta)
		}
	case event.ToolCalled:
		fmt.Fprintf(p.W, "  ⚙ %s  args=%s\n", e.ToolName, truncate(string(e.Args), 120))
	case event.ToolCompleted:
		status := "ok"
		if e.Err != "" {
			status = "ERR: " + e.Err
		}
		fmt.Fprintf(p.W, "  ✓ %s  %d bytes  %s\n", e.ToolName, e.Bytes, status)
	case event.ArtifactWritten:
		fmt.Fprintf(p.W, "  📄 artifact %s  (%d bytes)  %s\n", e.Name, e.Bytes, e.Path)
	case event.StageCompleted:
		fmt.Fprintf(p.W, "✓ stage %s%s  turns=%d  tokens=%d/%d  elapsed=%s\n",
			e.StageID, memberSuffix(e.MemberID),
			e.Turns, e.InputTokens, e.OutputTokens, e.Dur.Round(0))
	case event.StageFailed:
		fmt.Fprintf(p.W, "✗ stage %s%s failed: %s\n",
			e.StageID, memberSuffix(e.MemberID), e.Err)
	case event.ReviewAggregated:
		fmt.Fprintf(p.W, "Σ parallel stage %s  %s\n", e.StageID, e.Summary)
		if len(e.Failed) > 0 {
			for _, f := range e.Failed {
				fmt.Fprintf(p.W, "  · failed: %s\n", f)
			}
		}
	case event.WorkflowCompleted:
		fmt.Fprintf(p.W, "\n✓ workflow %s completed: %s\n", e.RunID, e.Verdict)
	case event.WorkflowHalted:
		fmt.Fprintf(p.W, "\n⚠ workflow halted at %s: %s\n", e.StageID, e.Reason)
	}
}

func memberSuffix(m string) string {
	if m == "" {
		return ""
	}
	return "/" + m
}

func truncate(s string, limit int) string {
	if len(s) <= limit {
		return strings.ReplaceAll(s, "\n", " ")
	}
	return strings.ReplaceAll(s[:limit], "\n", " ") + "…"
}
