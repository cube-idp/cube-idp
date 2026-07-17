// Package render holds the three renderers of the cube-idp event stream
// (design doc 2026-07-14 §5): Plain (the byte-frozen phase-1 projection),
// JSON (one event per line, {"v":1,...}), and Live (Bubble Tea v2 inline).
// Renderers project events; they never invent content.
package render

import (
	"fmt"
	"io"

	"github.com/cube-idp/cube-idp/internal/ui/event"
)

// Plain returns the plain projection: a pure per-event function whose output
// is defined as the bytes internal/ui emitted before Task 14b (design doc
// §5.1, normative table). No ANSI, no goroutines, zero bytes for
// RunStarted/StepFailed/HealthTick/Diagnosis/RunDone.
//
// The one deliberate new projection is Access (§9): previously a styled-only
// block, now stable plain lines — "what URLs did I just get" is exactly what
// scripts and CI want to scrape. The epilogue's one-glyph change (ratified
// R2) and the StepStarted start line (ratified R1) are the other sanctioned
// deltas (TUI design doc §5).
func Plain(w io.Writer) func(event.Event) {
	return func(ev event.Event) {
		switch e := ev.(type) {
		case event.StepStarted:
			// R1 (ratified, TUI design doc §5): a started step prints a
			// start line so CI logs distinguish hung from slow (audit P12).
			fmt.Fprintf(w, "▸ [%s] %s...\n", e.Stage, e.Msg)
		case event.StepDone:
			// Printer.Step's ModePlain branch — the phase-1 checkpoint-0.13
			// format, byte-for-byte. Dur is NEVER printed in plain mode.
			fmt.Fprintf(w, "▸ [%s] %s\n", e.Stage, e.Msg)
		case event.Note:
			fmt.Fprintln(w, e.Msg)
		case event.Epilogue:
			// R2 (ratified, design doc §5): the epilogue is data — plain
			// projects it WITHOUT the ✔ glyph (presentation belongs to the
			// styled/live renderers). These bytes are frozen; Context and
			// Registry never print here (TE-4.4 keeps plain minimal).
			fmt.Fprintf(w, "\ncube %q is up — %s\n  %s\n", e.Cube, e.GatewayURL, e.Hint)
		case event.Warn:
			fmt.Fprintln(w, e.Msg)
		case event.Access:
			fmt.Fprint(w, "\nAccess\n")
			for _, pk := range e.Packs {
				for _, u := range pk.URLs {
					fmt.Fprintf(w, "  %-12s %s\n", pk.Name, u)
				}
			}
			fmt.Fprintf(w, "  %s\n", e.Hint)
		case event.RunStarted, event.StepFailed, event.StepLog,
			event.HealthTick, event.Diagnosis, event.RunDone:
			// Zero bytes: nothing was printed for these today. The failure
			// diagnosis stays main.go's job (stderr, ui.RenderError).
		}
	}
}
