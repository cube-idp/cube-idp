package render

import (
	"fmt"
	"io"

	lipgloss "charm.land/lipgloss/v2"

	"github.com/rafpe/cube-idp/internal/ui/event"
)

// The styled-static style definitions are a deliberate, small duplication of
// internal/ui's Printer styles (stepBadgeStyle/stepMsgStyle/warnStyle,
// ui.go): package render must NOT import package ui (ui imports render), so
// Styled reimplements the handful of lipgloss styles it needs rather than
// sharing Printer's. Content stays identical to Plain; only presentation
// differs (design doc §5.2's content-identical rule).
var (
	styledBadgeStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	styledMsgStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	styledWarnStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
)

// Styled returns the styled-static projection for request/response commands
// migrated onto the event stream (Phase 4 R3): the same content as Plain,
// rendered through the existing Printer styling — StepDone via Printer.Step
// (badge+dim), Note via Fprintln, Warn via Printer.Warn, Access via
// Printer.AccessSummary. Zero bytes for the same event set Plain ignores.
// It is ONLY ever constructed for a real TTY (RunPipelineStatic's switch),
// so it builds its Printer with ui-package styling enabled.
func Styled(w io.Writer) func(event.Event) {
	return func(ev event.Event) {
		switch e := ev.(type) {
		case event.StepDone:
			// Printer.Step's ModeStyled branch, reproduced: badge + dimmed
			// message, content identical to Plain's "▸ [%s] %s".
			fmt.Fprintf(w, "%s %s\n",
				styledBadgeStyle.Render(fmt.Sprintf("▸ [%s]", e.Stage)),
				styledMsgStyle.Render(e.Msg))
		case event.Note:
			fmt.Fprintln(w, e.Msg)
		case event.Warn:
			// Printer.Warn's ModeStyled branch: amber glyph prefix + styled
			// message — content (the message text) identical to Plain.
			fmt.Fprintf(w, "%s %s\n", styledWarnStyle.Render("⚠"), styledWarnStyle.Render(e.Msg))
		case event.Access:
			// Printer.AccessSummary's ModeStyled branch.
			var b []byte
			b = append(b, "\n"+styledBadgeSectionStyle.Render("Access")+"\n"...)
			for _, pk := range e.Packs {
				for _, u := range pk.URLs {
					b = append(b, fmt.Sprintf("  %s %s\n", styledBadgeStyle.Render(fmt.Sprintf("%-12s", pk.Name)), u)...)
				}
			}
			b = append(b, fmt.Sprintf("  %s\n", styledMsgStyle.Render(e.Hint))...)
			w.Write(b)
		case event.RunStarted, event.StepStarted, event.StepFailed,
			event.HealthTick, event.Diagnosis, event.RunDone:
			// Zero bytes: same silent event set as Plain.
		}
	}
}

// styledBadgeSectionStyle mirrors ui.go's sectionStyle (bold, no color) —
// used only by Access's "Access" heading.
var styledBadgeSectionStyle = lipgloss.NewStyle().Bold(true)
