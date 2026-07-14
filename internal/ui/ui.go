// Package ui is the single seam every command uses to print user-facing
// progress. It preserves the phase-1 plain output format byte-for-byte
// (checkpoint 0.13: "▸ [%s] %s\n") and adds an opt-in, TTY-only styled
// presentation on top of the exact same content — never different content.
//
// Design rule (Task 13.8): the phase-1 plain format IS the CI/e2e contract.
// Styled output only ever engages when stdout is a real terminal, --plain
// was not passed, and $CI is unset; piped output (every e2e run helper,
// every CI log, every `go test` writer that isn't an *os.File) is therefore
// always byte-identical to today's output — no e2e assertion ever needs to
// change because of this package.
package ui

import (
	"fmt"
	"io"
	"os"

	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

// PlainFlag mirrors the --plain persistent flag. cmd/root.go sets it once,
// in PersistentPreRunE, before any command's RunE executes — every call
// site that builds a Printer (internal/up's step(), cmd/cnoe.go, ...) reads
// it via New(w, ui.PlainFlag) instead of threading a bool through every
// orchestrator signature. This is the least invasive threading choice:
// orchestrators keep their existing `(ctx, cfgPath, out io.Writer)`
// signatures untouched (Task 13.8).
var PlainFlag bool

// Mode controls whether a Printer emits ANSI-styled or plain output.
type Mode int

const (
	// ModeStyled renders a lipgloss-styled stage badge and dimmed message —
	// only ever selected for an interactive terminal.
	ModeStyled Mode = iota
	// ModePlain reproduces the phase-1 step() format verbatim.
	ModePlain
)

// Resolve is the pure decision function behind New. Plain wins if the
// --plain flag was passed, if the destination is not an interactive
// terminal, or if $CI is set (the common CI convention) — in that order,
// but any one of them is sufficient. Kept side-effect-free (no real
// terminal or environment lookups) so callers can unit-test every
// precedence case, including "--plain forces plain even on a TTY".
func Resolve(plainFlag, isTTY bool, ciEnv string) Mode {
	if plainFlag || !isTTY || ciEnv != "" {
		return ModePlain
	}
	return ModeStyled
}

// IsTerminal reports whether v — typically an io.Reader or io.Writer backed
// by an *os.File such as os.Stdin/os.Stdout — is attached to an interactive
// terminal. Any value that is not an *os.File (bytes.Buffer, a pipe, a
// cobra-injected test buffer, ...) is never a terminal; this is what keeps
// every existing test's captured output plain without any extra plumbing.
func IsTerminal(v any) bool {
	f, ok := v.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

// Printer prints one line of user-facing progress per Step call, in either
// plain or styled Mode (decided once, at New time).
type Printer struct {
	out  io.Writer
	mode Mode
}

// New builds a Printer for out. plain forces ModePlain regardless of
// terminal status; ModePlain is also auto-forced when out is not a TTY or
// $CI is set (Resolve holds the exact precedence).
func New(out io.Writer, plain bool) *Printer {
	mode := Resolve(plain, IsTerminal(out), os.Getenv("CI"))
	return &Printer{out: out, mode: mode}
}

var (
	stepBadgeStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	stepMsgStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
)

// Step prints one line of user-facing progress: name is the stage tag
// (e.g. "tls", "ca", "cluster"); format/args build the message exactly like
// fmt.Sprintf. In ModePlain this reproduces the phase-1 format verbatim —
// "▸ [%s] %s\n" — byte-for-byte. In ModeStyled the same stage tag and
// message are rendered with a lipgloss badge and a dimmed message: content
// identical, presentation only.
func (p *Printer) Step(name, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if p.mode == ModePlain {
		fmt.Fprintf(p.out, "▸ [%s] %s\n", name, msg)
		return
	}
	fmt.Fprintf(p.out, "%s %s\n",
		stepBadgeStyle.Render(fmt.Sprintf("▸ [%s]", name)),
		stepMsgStyle.Render(msg))
}
