package render

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"charm.land/bubbles/v2/progress"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/cube-idp/cube-idp/internal/ui/event"
	"github.com/cube-idp/cube-idp/internal/ui/theme"
)

// Live runs the LiveRenderer: a transient Bubble Tea v2 program in INLINE
// mode — never the alt screen. Persistent full-screen dashboards were
// evaluated and rejected; see ADR-0026.
// Completed steps stream into native scrollback via the program's Println;
// the managed bottom region holds only in-flight state (spinner lines and
// the health table); the program exits leaving clean scrollback.
//
// input is the interactive reader (os.Stdin when it is a real terminal, nil
// otherwise — nil disables input entirely, which keeps ModeLive-forced runs
// on pipes and tests deterministic). cancel is the run context's cancel
// func: ctrl+c maps to it, the producer unwinds through its normal error
// paths, and the program quits through the single ordinary path (the
// channel closing). Live never calls os.Exit and returns only after the
// program has exited AND the event channel is fully drained — no goroutine
// survives it.
func Live(out io.Writer, input io.Reader, cancel func(), ch <-chan event.Event) {
	m := newLiveModel(cancel)
	p := tea.NewProgram(m, tea.WithOutput(out), tea.WithInput(input))

	sb := newScrollback(theme.Detect(os.Stdin, outFile(out)))
	done := make(chan struct{})
	go func() {
		defer close(done)
		// One goroutine, strict order: scrollback lines go through
		// p.Println (queued in call order), region state through p.Send.
		// After the program quits, both are no-ops — the loop then just
		// drains the channel so the producer can never block on a dead
		// renderer.
		for ev := range ch {
			for _, line := range sb.lines(ev) {
				p.Println(line)
			}
			p.Send(evMsg{ev})
		}
		p.Send(eofMsg{})
	}()

	// The program runs on the calling goroutine (input handling wants the
	// foreground). If it fails to start (exotic terminal), the forwarder
	// above still drains every event — the run completes, only the live
	// view is lost.
	_, _ = p.Run()
	p.Quit() // idempotent; covers early Run errors so the forwarder's Sends stay no-ops
	<-done
}

// outFile unwraps the *os.File behind out (nil for pipes and buffers) so
// theme.Detect's TTY guard sees the real terminal when there is one.
func outFile(out io.Writer) *os.File {
	f, _ := out.(*os.File)
	return f
}

// evMsg wraps one stream event for the model. eofMsg reports the channel
// closed — the single quit trigger (it follows every event, including the
// terminal RunDone/Diagnosis, in strict order).
type evMsg struct{ ev event.Event }
type eofMsg struct{}

// scrollback projects events into permanent scrollback lines. Stateful:
// it buffers StepLog lines per open stage so a failure can dump the full
// captured tail — the whole buffer, not the 5-line live window — ahead of
// the diagnosis box, so logs are never lost behind progress UI.
// Content-identical rule: styled presentation may add color, alignment,
// and a duration suffix, never different words.
type scrollback struct {
	th    theme.Theme
	tails map[string][]string
	// started/cmd come from RunStarted and gate the RunDone summary line —
	// a stream with no RunStarted (config.Load failed) ends silently.
	started bool
	cmd     string
}

func newScrollback(th theme.Theme) *scrollback {
	return &scrollback{th: th, tails: map[string][]string{}}
}

// lines returns zero or more finished scrollback lines for ev.
func (s *scrollback) lines(ev event.Event) []string {
	switch e := ev.(type) {
	case event.RunStarted:
		s.started, s.cmd = true, e.Cmd
		return nil
	case event.StepLog:
		s.tails[e.Stage] = append(s.tails[e.Stage], e.Line)
		return nil
	case event.StepDone:
		delete(s.tails, e.Stage) // BuildKit collapse: success discards the tail
		return []string{s.stepDoneLine(e)}
	case event.StepFailed:
		out := []string{s.stepFailedLine(e)}
		for _, l := range s.tails[e.Stage] { // full dump, most important info last
			out = append(out, "  "+s.th.Dim.Render("│ "+l))
		}
		delete(s.tails, e.Stage)
		return out
	case event.Note:
		return []string{e.Msg} // verbatim
	case event.Epilogue:
		return s.epilogueLines(e)
	case event.Warn:
		return []string{fmt.Sprintf("%s %s", s.th.Warn.Render(theme.GlyphWarn), s.th.Warn.Render(e.Msg))}
	case event.Access:
		var b strings.Builder
		b.WriteString("\n" + s.th.Section.Render("Access"))
		for _, pk := range e.Packs {
			for _, u := range pk.URLs {
				b.WriteString(fmt.Sprintf("\n  %s %s", s.th.Badge.Render(fmt.Sprintf("%-12s", pk.Name)), u))
			}
		}
		b.WriteString("\n  " + s.th.Msg.Render(e.Hint))
		return []string{b.String()}
	case event.RunDone:
		if !s.started {
			return nil
		}
		d := e.Dur.Round(time.Second)
		if e.OK {
			return []string{fmt.Sprintf("%s %s finished in %s", s.th.OK.Render(theme.GlyphOK), s.cmd, d)}
		}
		return []string{fmt.Sprintf("%s %s failed after %s", s.th.Err.Render(theme.GlyphErr), s.cmd, d)}
	default:
		// StepStarted/HealthTick: live-region state only. The diagnosis
		// renders AFTER the program exits, via main.go's ui.RenderError —
		// never here (diagnosis-last, §5.2).
		return nil
	}
}

// Scrollback line layout: fixed badge column, message field, right-aligned dim
// duration at durCol (golden-pinned at 80 cols; wider terminals keep the
// same columns — scrollback lines are permanent and must not depend on
// resize).
const durCol = 62

// regionIndent hangs progress-bar and log-tail lines under their step
// line.
const regionIndent = "             "

func (s *scrollback) stepDoneLine(e event.StepDone) string {
	return s.stepLine(s.th.OK.Render(theme.GlyphOK), e.Stage, e.Msg, e.Dur)
}

func (s *scrollback) stepFailedLine(e event.StepFailed) string {
	msg := e.Msg
	if msg == "" {
		msg = "failed" // never a naked ✗ with no message
	}
	return s.stepLine(s.th.Err.Render(theme.GlyphErr), e.Stage, msg, e.Dur)
}

func (s *scrollback) stepLine(glyph, stage, msg string, dur time.Duration) string {
	badge := s.th.Badge.Render(fmt.Sprintf("%-*s", theme.BadgeWidth(), "["+stage+"]"))
	line := fmt.Sprintf("%s %s %s", glyph, badge, msg)
	if dur > 0 {
		d := s.th.Dim.Render(fmt.Sprintf("(%s)", dur.Round(time.Second)))
		if pad := durCol - lipgloss.Width(line); pad > 1 {
			return line + strings.Repeat(" ", pad) + d
		}
		return line + " " + d
	}
	return line
}

// epilogueLines renders the success epilogue: blank separator, headline, one
// key-value row per non-empty field, next-hint. The headline carries no
// duration — RunDone arrives after Epilogue and prints the run summary
// line with the total (the run duration is rendered there, not here).
func (s *scrollback) epilogueLines(e event.Epilogue) []string {
	out := []string{"", fmt.Sprintf("%s cube %q is up", s.th.OK.Render(theme.GlyphOK), e.Cube)}
	row := func(key, val string) string {
		return fmt.Sprintf("  %s %s", s.th.Dim.Render(fmt.Sprintf("%-11s", key)), val)
	}
	if e.GatewayURL != "" {
		out = append(out, row("gateway", s.th.Badge.Render(e.GatewayURL)))
	}
	if e.Context != "" {
		out = append(out, row("context", e.Context))
	}
	if e.Registry != "" {
		out = append(out, row("registry", e.Registry))
	}
	return append(out, "  "+s.th.Dim.Render("next: cube-idp status · "+e.Hint))
}

// inFlight is one open step shown as a spinner line in the live region.
type inFlight struct {
	stage, msg string
	start      time.Time
}

// liveModel is the inline Bubble Tea model. The View is ONLY the in-flight
// region: header, one spinner line per open step (with n/m counter,
// progress bar, and bounded log tail where applicable), and the health
// component table from the latest HealthTick until RunDone. It collapses
// to zero lines on RunDone and quits when the stream ends.
type liveModel struct {
	cancel     func()
	th         theme.Theme
	spin       spinner.Model
	prog       progress.Model // bubbles/v2 progress bar for the enumerated step
	header     string
	width      int // from tea.WindowSizeMsg; 0 = unknown, clamp only when known
	steps      []inFlight
	packStage  string // stage of the open enumerated step ("" = none)
	packIdx    int
	packTotal  int
	tails      map[string][]string // last ≤5 StepLog lines per open stage
	components []event.ComponentState
	collapsed  bool
	now        func() time.Time // injectable clock for elapsed rendering in tests
}

func newLiveModel(cancel func()) liveModel {
	th := theme.New(true)
	return liveModel{
		cancel: cancel,
		th:     th,
		spin:   spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(th.Warn)),
		// █/░ fill: these glyphs are literal, not decorative — the
		// bubbles v2 default is a half-block.
		prog:  progress.New(progress.WithWidth(30), progress.WithFillCharacters('█', '░')),
		tails: map[string][]string{},
		now:   time.Now,
	}
}

func (m liveModel) Init() tea.Cmd {
	return tea.Batch(m.spin.Tick, tea.RequestBackgroundColor)
}

func (m liveModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case evMsg:
		return m.applyEvent(msg.ev), nil
	case eofMsg:
		// The stream is over: every scrollback line is already queued in
		// order ahead of this message, so quitting now loses nothing.
		m.collapsed = true
		return m, tea.Quit
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	case tea.BackgroundColorMsg:
		m.th = theme.New(msg.IsDark())
		return m, nil
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			// Map to the run context's cancel: the producer unwinds through
			// its normal error paths and the terminal events flow — the
			// interrupt is never swallowed and quit happens on eofMsg, the
			// single ordinary path.
			m.cancel()
		}
		return m, nil
	case tea.InterruptMsg:
		m.cancel()
		return m, nil
	}
	return m, nil
}

func (m liveModel) applyEvent(ev event.Event) liveModel {
	switch e := ev.(type) {
	case event.RunStarted:
		m.header = m.th.Dim.Render(fmt.Sprintf("cube-idp %s — cube %q", e.Cmd, e.Cube))
	case event.StepStarted:
		m.steps = append(m.steps, inFlight{stage: e.Stage, msg: e.Msg, start: m.now()})
		if e.Total > 0 {
			m.packStage, m.packIdx, m.packTotal = e.Stage, e.Index, e.Total
		}
	case event.StepDone:
		m.steps = removeStep(m.steps, e.Stage)
		delete(m.tails, e.Stage)
		if e.Stage == m.packStage {
			m.packStage, m.packIdx, m.packTotal = "", 0, 0
		}
		// The health snapshot persists until RunDone (spec WP3) — closing
		// the health stage no longer drops the table.
	case event.StepFailed:
		m.steps = removeStep(m.steps, e.Stage)
		delete(m.tails, e.Stage)
		if e.Stage == m.packStage {
			m.packStage, m.packIdx, m.packTotal = "", 0, 0
		}
	case event.StepLog:
		t := append(m.tails[e.Stage], e.Line)
		if len(t) > 5 {
			t = t[len(t)-5:] // bounded tail window (TE-1.4)
		}
		m.tails[e.Stage] = t
	case event.HealthTick:
		m.components = e.Components
	case event.RunDone:
		// The live region collapses; scrollback (already printed) is all
		// that remains. Quit follows on eofMsg.
		m.collapsed = true
	case event.Diagnosis:
		// Rendered by main.go after exit — nothing to show here.
	}
	return m
}

func removeStep(steps []inFlight, stage string) []inFlight {
	kept := steps[:0]
	for _, s := range steps {
		if s.stage != stage {
			kept = append(kept, s)
		}
	}
	return kept
}

// View renders ONLY the managed bottom region. The alt screen is never
// set — inline mode is the whole point (Dagger/gh pattern; Tilt's
// full-screen HUD is the rejected anti-pattern).
func (m liveModel) View() tea.View {
	if m.collapsed {
		return tea.NewView("")
	}
	var lines []string
	add := func(s string) { lines = append(lines, clamp(s, m.width)) }
	if m.header != "" {
		add(m.header)
	}
	for _, s := range m.steps {
		badge := m.th.Badge.Render(fmt.Sprintf("%-*s", theme.BadgeWidth(), "["+s.stage+"]"))
		line := fmt.Sprintf("%s %s %s", m.spin.View(), badge, m.th.Msg.Render(s.msg))
		if s.stage == m.packStage && m.packTotal > 0 {
			// TE-1.3: right-aligned n/m counter + progress bar underneath.
			counter := m.th.Dim.Render(fmt.Sprintf("%d/%d", m.packIdx, m.packTotal))
			if pad := durCol - lipgloss.Width(line); pad > 1 {
				line += strings.Repeat(" ", pad) + counter
			} else {
				line += " " + counter
			}
			add(line)
			add(regionIndent + m.prog.ViewAs(float64(m.packIdx)/float64(m.packTotal)))
		} else {
			elapsed := m.now().Sub(s.start).Round(time.Second)
			add(fmt.Sprintf("%s… %s", line, m.th.Dim.Render(fmt.Sprintf("(%s)", elapsed))))
		}
		for _, l := range m.tails[s.stage] {
			add(regionIndent + m.th.Dim.Render("│ "+l))
		}
	}
	if len(m.components) > 0 {
		for _, c := range m.components {
			glyph := m.th.Err.Render(theme.GlyphErr)
			if c.Ready {
				glyph = m.th.OK.Render(theme.GlyphOK)
			}
			add(fmt.Sprintf("  %s %-24s %s", glyph, c.Name, m.th.Msg.Render(c.Message)))
		}
	}
	return tea.NewView(strings.Join(lines, "\n"))
}

// clamp truncates a rendered line to w cells with a trailing … — the region
// must never wrap. w == 0 means the width is unknown: no clamping.
func clamp(s string, w int) string {
	if w <= 0 || lipgloss.Width(s) <= w {
		return s
	}
	return ansi.Truncate(s, w-1, "…")
}
