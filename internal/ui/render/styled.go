package render

import (
	"fmt"
	"io"
	"os"

	"github.com/cube-idp/cube-idp/internal/ui/event"
	"github.com/cube-idp/cube-idp/internal/ui/theme"
)

// th is this package's adaptive palette (internal/ui/theme) — the leaf
// package both internal/ui and render import, so the styles are shared, not
// duplicated. Styled/Live output only ever reaches a real TTY, where Detect
// resolves the background once (dark on any doubt).
var th = theme.Detect(os.Stdin, os.Stdout)

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
				th.Badge.Render(fmt.Sprintf("▸ [%s]", e.Stage)),
				th.Msg.Render(e.Msg))
		case event.Note:
			fmt.Fprintln(w, e.Msg)
		case event.Epilogue:
			// R2: the ✔ is presentation — Styled re-adds it here; the words
			// stay identical to Plain's projection (content-identical rule,
			// glyph excepted like Warn's ⚠). Full TE-4 rows are live/T05.
			fmt.Fprintf(w, "\n%s cube %q is up — %s\n  %s\n",
				th.OK.Render(theme.GlyphOK), e.Cube, e.GatewayURL, th.Dim.Render(e.Hint))
		case event.Warn:
			// Printer.Warn's ModeStyled branch: amber glyph prefix + styled
			// message — content (the message text) identical to Plain.
			fmt.Fprintf(w, "%s %s\n", th.Warn.Render("⚠"), th.Warn.Render(e.Msg))
		case event.Access:
			// Printer.AccessSummary's ModeStyled branch.
			var b []byte
			b = append(b, "\n"+th.Section.Render("Access")+"\n"...)
			for _, pk := range e.Packs {
				for _, u := range pk.URLs {
					b = append(b, fmt.Sprintf("  %s %s\n", th.Badge.Render(fmt.Sprintf("%-12s", pk.Name)), u)...)
				}
			}
			b = append(b, fmt.Sprintf("  %s\n", th.Msg.Render(e.Hint))...)
			w.Write(b)
		case event.RunStarted, event.StepStarted, event.StepFailed,
			event.StepLog, event.HealthTick, event.Diagnosis, event.RunDone:
			// Zero bytes: same silent event set as Plain.
		}
	}
}
