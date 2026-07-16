package render

import (
	"fmt"
	"io"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/cube-idp/cube-idp/internal/ui/event"
	"github.com/cube-idp/cube-idp/internal/ui/theme"
)

// Live runs the LiveRenderer: a transient Bubble Tea v2 program in INLINE
// mode — never the alt screen (design doc §5.2; spec §4.1 as amended).
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

	done := make(chan struct{})
	go func() {
		defer close(done)
		// One goroutine, strict order: scrollback lines go through
		// p.Println (queued in call order), region state through p.Send.
		// After the program quits, both are no-ops — the loop then just
		// drains the channel so the producer can never block on a dead
		// renderer.
		for ev := range ch {
			if line := scrollbackLine(ev); line != "" {
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

// evMsg wraps one stream event for the model. eofMsg reports the channel
// closed — the single quit trigger (it follows every event, including the
// terminal RunDone/Diagnosis, in strict order).
type evMsg struct{ ev event.Event }
type eofMsg struct{}

// scrollbackLine is the pure projection of one event into a permanent
// scrollback line ("" = nothing printed; the event only affects the live
// region). Content-identical rule: styled presentation may add color and a
// duration suffix, never different words (design doc §5.2).
func scrollbackLine(ev event.Event) string {
	switch e := ev.(type) {
	case event.StepDone:
		line := fmt.Sprintf("%s %s %s",
			th.OK.Render("✔"),
			th.Badge.Render(fmt.Sprintf("[%s]", e.Stage)),
			e.Msg)
		if e.Dur > 0 {
			line += " " + th.Dim.Render(fmt.Sprintf("(%s)", e.Dur.Round(time.Second)))
		}
		return line
	case event.StepFailed:
		return fmt.Sprintf("%s %s",
			th.Err.Render("✗"),
			th.Badge.Render(fmt.Sprintf("[%s]", e.Stage)))
	case event.Note:
		return e.Msg // verbatim
	case event.Warn:
		return fmt.Sprintf("%s %s", th.Warn.Render("⚠"), th.Warn.Render(e.Msg))
	case event.Access:
		var b strings.Builder
		b.WriteString("\n" + th.Section.Render("Access"))
		for _, pk := range e.Packs {
			for _, u := range pk.URLs {
				b.WriteString(fmt.Sprintf("\n  %s %s", th.Badge.Render(fmt.Sprintf("%-12s", pk.Name)), u))
			}
		}
		b.WriteString("\n  " + th.Msg.Render(e.Hint))
		return b.String()
	default:
		// RunStarted/StepStarted/HealthTick/RunDone/Diagnosis: live-region
		// state only. The diagnosis renders AFTER the program exits, via
		// main.go's ui.RenderError — never here (diagnosis-last, §5.2).
		return ""
	}
}

// inFlight is one open step shown as a spinner line in the live region.
type inFlight struct {
	stage, msg string
	start      time.Time
}

// liveModel is the inline Bubble Tea model. The View is ONLY the in-flight
// region: header, one spinner line per open step, and the health component
// table while the "health" stage is open. It collapses to zero lines on
// RunDone and quits when the stream ends.
type liveModel struct {
	cancel     func()
	th         theme.Theme // fixed dark palette for now; T05 adapts it via tea.BackgroundColorMsg
	spin       spinner.Model
	header     string
	steps      []inFlight
	components []event.ComponentState
	collapsed  bool
	now        func() time.Time // injectable clock for elapsed rendering in tests
}

func newLiveModel(cancel func()) liveModel {
	th := theme.New(true)
	return liveModel{
		cancel: cancel,
		th:     th,
		spin:   spinner.New(spinner.WithStyle(th.Warn)),
		now:    time.Now,
	}
}

func (m liveModel) Init() tea.Cmd { return m.spin.Tick }

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
	case event.StepDone:
		m.steps = removeStep(m.steps, e.Stage)
		if e.Stage == "health" {
			m.components = nil // the table collapses with its stage
		}
	case event.StepFailed:
		m.steps = removeStep(m.steps, e.Stage)
		if e.Stage == "health" {
			m.components = nil
		}
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

// View renders ONLY the managed bottom region. AltScreen is never set —
// inline mode is the whole point (Dagger/gh pattern; Tilt's full-screen HUD
// is the rejected anti-pattern).
func (m liveModel) View() tea.View {
	if m.collapsed {
		return tea.NewView("")
	}
	var lines []string
	if m.header != "" {
		lines = append(lines, m.header)
	}
	for _, s := range m.steps {
		elapsed := m.now().Sub(s.start).Round(time.Second)
		lines = append(lines, fmt.Sprintf("%s %s %s… %s",
			m.spin.View(),
			m.th.Badge.Render(fmt.Sprintf("[%s]", s.stage)),
			m.th.Msg.Render(s.msg),
			m.th.Dim.Render(fmt.Sprintf("(%s)", elapsed))))
	}
	if len(m.components) > 0 && hasStage(m.steps, "health") {
		for _, c := range m.components {
			glyph := m.th.Err.Render("✗")
			if c.Ready {
				glyph = m.th.OK.Render("✔")
			}
			lines = append(lines, fmt.Sprintf("  %s %-24s %s",
				glyph, c.Name, m.th.Msg.Render(c.Message)))
		}
	}
	return tea.NewView(strings.Join(lines, "\n"))
}

func hasStage(steps []inFlight, stage string) bool {
	for _, s := range steps {
		if s.stage == stage {
			return true
		}
	}
	return false
}
