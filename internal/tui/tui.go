// Package tui is a Bubble Tea event-log TUI for baton. It subscribes to
// the orchestrator's event bus and renders a three-region layout:
// header / main scroll / footer.
package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/honerlaw/baton/internal/event"
)

var (
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Background(lipgloss.Color("57")).
			Foreground(lipgloss.Color("230")).
			Padding(0, 1)
	runningStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	okStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	errStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	muted         = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	streamStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	footerStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Padding(0, 1)
	toolCallStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("111"))
)

// Run starts the Bubble Tea program. It returns when the user quits or
// when ctx is cancelled. The caller is responsible for running the
// orchestrator alongside it (usually in a goroutine).
func Run(ctx context.Context, events <-chan event.Event) error {
	m := newModel(events)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(ctx))
	_, err := p.Run()
	return err
}

type model struct {
	events <-chan event.Event

	stage     string
	memberID  string
	persona   string
	model     string
	startedAt time.Time
	tokenIn   int
	tokenOut  int
	status    string // running | ok | err | done | halted

	// Running log of events formatted as lines.
	log      strings.Builder
	stream   strings.Builder
	viewport viewport.Model
	width    int
	height   int
	quit     bool
}

func newModel(events <-chan event.Event) *model {
	vp := viewport.New(0, 0)
	return &model{events: events, viewport: vp, status: "idle"}
}

type (
	evMsg   struct{ ev event.Event }
	tickMsg time.Time
)

func (m *model) Init() tea.Cmd {
	return tea.Batch(m.nextEvent(), tea.Every(500*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) }))
}

func (m *model) nextEvent() tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-m.events
		if !ok {
			return evMsg{ev: nil}
		}
		return evMsg{ev: ev}
	}
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.viewport.Width = msg.Width
		m.viewport.Height = msg.Height - 4
		m.viewport.SetContent(m.log.String())
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quit = true
			return m, tea.Quit
		case "g":
			m.viewport.GotoTop()
		case "G":
			m.viewport.GotoBottom()
		}
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	case tickMsg:
		// Periodic refresh for the "elapsed" timer and stream flush.
		return m, tea.Every(500*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
	case evMsg:
		if msg.ev == nil {
			// Event channel closed — orchestration complete.
			return m, nil
		}
		m.applyEvent(msg.ev)
		m.viewport.SetContent(m.log.String() + streamStyle.Render(m.stream.String()))
		m.viewport.GotoBottom()
		return m, m.nextEvent()
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m *model) applyEvent(ev event.Event) {
	switch e := ev.(type) {
	case event.StageStarted:
		m.stage, m.memberID, m.persona, m.model = e.StageID, e.MemberID, e.Persona, e.Model
		m.startedAt = e.At
		m.status = "running"
		m.stream.Reset()
		fmt.Fprintf(&m.log, "\n%s %s%s  persona=%s  model=%s\n",
			runningStyle.Render("▶ stage"), e.StageID, memberSuffix(e.MemberID), e.Persona, e.Model)
	case event.MessageStreamed:
		m.stream.WriteString(e.Delta)
		// Periodically flush a chunk into the log so the viewport doesn't
		// grow unbounded with a single live bubble.
		if m.stream.Len() > 2048 {
			m.log.WriteString(streamStyle.Render(m.stream.String()))
			m.stream.Reset()
		}
	case event.ToolCalled:
		m.log.WriteString(streamStyle.Render(m.stream.String()))
		m.stream.Reset()
		fmt.Fprintf(&m.log, "  %s %s %s\n",
			toolCallStyle.Render("⚙"), e.ToolName, muted.Render(truncate(string(e.Args), 160)))
	case event.ToolCompleted:
		status := okStyle.Render("ok")
		if e.Err != "" {
			status = errStyle.Render("ERR: " + e.Err)
		}
		fmt.Fprintf(&m.log, "    %s %d bytes  %s\n", toolCallStyle.Render("✓"), e.Bytes, status)
	case event.ArtifactWritten:
		fmt.Fprintf(&m.log, "    📄 %s (%d bytes)\n", e.Name, e.Bytes)
	case event.StageCompleted:
		m.tokenIn += e.InputTokens
		m.tokenOut += e.OutputTokens
		m.log.WriteString(streamStyle.Render(m.stream.String()))
		m.stream.Reset()
		fmt.Fprintf(&m.log, "%s %s%s  turns=%d  +tokens %d/%d\n",
			okStyle.Render("✓ stage"), e.StageID, memberSuffix(e.MemberID),
			e.Turns, e.InputTokens, e.OutputTokens)
	case event.StageFailed:
		fmt.Fprintf(&m.log, "%s %s%s: %s\n",
			errStyle.Render("✗ stage"), e.StageID, memberSuffix(e.MemberID), e.Err)
		m.status = "err"
	case event.ReviewAggregated:
		fmt.Fprintf(&m.log, "  Σ %s  %s\n", e.StageID, e.Summary)
	case event.WorkflowCompleted:
		m.status = "done"
		fmt.Fprintf(&m.log, "\n%s run=%s verdict=%s\n", okStyle.Render("✓ workflow complete"), e.RunID, e.Verdict)
	case event.WorkflowHalted:
		m.status = "halted"
		fmt.Fprintf(&m.log, "\n%s at %s: %s\n", errStyle.Render("⚠ halted"), e.StageID, e.Reason)
	}
}

func (m *model) View() string {
	if m.width == 0 {
		return "initializing TUI…"
	}
	var header string
	if m.stage == "" {
		header = headerStyle.Render(fmt.Sprintf(" baton  %-20s", "idle"))
	} else {
		elapsed := time.Since(m.startedAt).Round(time.Second)
		header = headerStyle.Render(fmt.Sprintf(
			" baton  stage=%s%s  persona=%s  model=%s  elapsed=%s  tokens=%d/%d  %s",
			m.stage, memberSuffix(m.memberID), m.persona, m.model, elapsed,
			m.tokenIn, m.tokenOut, statusBadge(m.status)))
	}
	footer := footerStyle.Render("q: quit  g/G: top/bottom")
	return lipgloss.JoinVertical(lipgloss.Left, header, m.viewport.View(), footer)
}

func statusBadge(s string) string {
	switch s {
	case "running":
		return runningStyle.Render("● running")
	case "done":
		return okStyle.Render("● done")
	case "err", "halted":
		return errStyle.Render("● " + s)
	}
	return muted.Render("● " + s)
}

func memberSuffix(m string) string {
	if m == "" {
		return ""
	}
	return "/" + m
}

func truncate(s string, limit int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "…"
}
