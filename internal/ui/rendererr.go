package ui

import (
	"errors"
	"fmt"
	"strings"

	lipgloss "charm.land/lipgloss/v2"

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
		errPanelGlyphStyle.Render("✗"),
		errPanelCodeStyle.Render(string(de.Code)),
		de.Summary)
	if de.Cause != nil {
		fmt.Fprintf(&b, "%s %v\n", errPanelLabelStyle.Render("cause:"), de.Cause)
	}
	if de.Remediation != "" {
		// Remediation stays unstyled: copy-paste safe.
		fmt.Fprintf(&b, "%s %s\n", errPanelLabelStyle.Render("fix:  "), de.Remediation)
	}
	return errPanelStyle.Render(strings.TrimRight(b.String(), "\n"))
}

var (
	errPanelStyle      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("196")).Padding(0, 1)
	errPanelGlyphStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196"))
	errPanelCodeStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196"))
	errPanelLabelStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
)
