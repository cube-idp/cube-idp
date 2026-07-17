package ui

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/cube-idp/cube-idp/internal/diag"
)

// RenderError produces the final-error block main.go prints to stderr —
// the process's single final-error print point (design doc §5.2). In
// ModePlain and ModeJSON it returns diag.Render(err) verbatim
// (byte-identical to the pre-14b behavior; stderr stays a human-readable
// belt even when stdout is a machine pipe). In ModeStyled/ModeLive it
// returns a lipgloss panel: bordered block with the CUBE-xxxx code badge,
// summary, cause, and the fix: remediation in copy-paste-safe plain text
// (no styling inside the remediation string itself).
//
// Diagnosis-last is structural: RunPipeline has fully released the terminal
// (the live program exited) before main.go calls this — the diagnosis can
// never be overwritten by the live region or trapped in a dead screen.
func RenderError(err error) string {
	return renderErrorForMode(CurrentMode(), err)
}

// RenderErrorTo renders err for a SPECIFIC writer, applying the same
// per-writer downgrade NewFor gives stdout: the styled panel only ever
// reaches a real terminal; a redirected stderr gets diag.Render verbatim
// (audit P11 — no more ANSI borders inside `2>file`). Under an explicit
// color suppression (--color=never / non-empty NO_COLOR) the panel keeps
// its border and layout but every escape is stripped — no-color.org's
// strip-color-only rule (W2.T13).
func RenderErrorTo(w io.Writer, err error) string {
	if !IsTerminal(w) {
		return diag.Render(err)
	}
	s := renderErrorForMode(CurrentMode(), err)
	if colorPolicyNow().colorOff() {
		s = ansi.Strip(s)
	}
	return s
}

// RenderError is RenderError's per-instance counterpart: it keys off this
// Printer's own resolved mode rather than the process-wide CurrentMode().
// It exists for callers like syncer.Watch that render a loud, non-fatal
// error mid-stream (not main.go's single final-error print point, which
// RenderError above remains) and need that block to match the styling of
// everything else they've already printed through this Printer.
func (p *Printer) RenderError(err error) string {
	return renderErrorForMode(p.mode, err)
}

func renderErrorForMode(mode Mode, err error) string {
	switch mode {
	case ModeStyled, ModeLive:
	default:
		return diag.Render(err)
	}
	var de *diag.Error
	if !errors.As(err, &de) {
		return diag.Render(err) // untyped errors keep the plain "Error: ..." form
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s %s  %s\n",
		th.Err.Render("✗"),
		th.Err.Render(string(de.Code)),
		de.Summary)
	if de.Cause != nil {
		fmt.Fprintf(&b, "%s %v\n", th.ErrLabel.Render("cause:"), de.Cause)
	}
	if de.Remediation != "" {
		// Remediation stays unstyled: copy-paste safe.
		fmt.Fprintf(&b, "%s %s\n", th.ErrLabel.Render("fix:  "), de.Remediation)
	}
	// TE-2.3 footer: every code the box shows is resolvable offline via
	// `cube-idp explain` (same wave as the command itself — the box never
	// advertises a command that doesn't run). Rendered as a dim line inside
	// the panel; the spec's border-embedded footer is approximated here.
	fmt.Fprintf(&b, "%s\n", th.ErrLabel.Render("more:  cube-idp explain "+string(de.Code)))
	return th.ErrPanel.Render(strings.TrimRight(b.String(), "\n"))
}
