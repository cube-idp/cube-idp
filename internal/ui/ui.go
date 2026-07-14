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
	"strings"
	"time"

	lipgloss "charm.land/lipgloss/v2"
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

// Section prints a heading line (e.g. internal/diff's "KERNEL OBJECTS",
// upgrade's "Kernel + delivery object changes:"). In ModePlain this is
// EXACTLY fmt.Fprintln(out, title) — the same call every one of these
// commands made directly before Task 15.3 — so callers that migrate to
// Section keep byte-identical plain output. ModeStyled renders it bold.
func (p *Printer) Section(title string) {
	if p.mode == ModePlain {
		fmt.Fprintln(p.out, title)
		return
	}
	fmt.Fprintln(p.out, sectionStyle.Render(title))
}

// Severity glyphs shared by status, doctor, and get secrets — the "one
// visual language" unification (Task 15.3b). These are the exact literal
// characters phase-1 code already printed inline; Glyph makes the choice of
// character and its styling one decision instead of N copy-pasted literals.
const (
	GlyphOK   = "✔"
	GlyphErr  = "✗"
	GlyphWarn = "⚠"
)

var (
	sectionStyle = lipgloss.NewStyle().Bold(true)
	okStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("42"))
	errStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196"))
	warnStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
)

// Glyph returns g verbatim in ModePlain (so every existing plain-mode
// literal that embedded "✔"/"✗"/"⚠" directly is unchanged byte-for-byte once
// its call site switches to p.Glyph(ui.GlyphOK) etc.) or an ANSI-colored
// rendering of it in ModeStyled. Any string other than the three constants
// above passes through unstyled in both modes.
func (p *Printer) Glyph(g string) string {
	if p.mode == ModePlain {
		return g
	}
	switch g {
	case GlyphOK:
		return okStyle.Render(g)
	case GlyphErr:
		return errStyle.Render(g)
	case GlyphWarn:
		return warnStyle.Render(g)
	default:
		return g
	}
}

// Warn prints an advisory line (e.g. get secrets' legacy-label deprecation
// note): ModePlain reproduces exactly what every caller's raw
// fmt.Fprintln(out, msg) printed before — msg followed by a newline, no
// glyph — so the migration changes zero plain bytes. ModeStyled prefixes it
// with the amber warning glyph.
func (p *Printer) Warn(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if p.mode == ModePlain {
		fmt.Fprintln(p.out, msg)
		return
	}
	fmt.Fprintf(p.out, "%s %s\n", p.Glyph(GlyphWarn), warnStyle.Render(msg))
}

// progressTick is the spinner's animation interval.
const progressTick = 100 * time.Millisecond

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

var progressStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))

// eraseLine returns the cursor to column 0 and clears the rest of the line —
// how a running Progress line is removed before the next frame (or the final
// Step line) is written in its place.
const eraseLine = "\r\x1b[2K"

// Progress is a TTY-only animated "still working" line — a spinner, the
// stage tag, the message, and elapsed time — for `up`'s long waits (cluster
// create, engine install, health polling) that used to go silent for
// minutes. It is never a substitute for Step: every Progress is eventually
// resolved by exactly one call to Done (success) or Stop (abandoned on
// error, printing nothing — matching how the phase-1 code printed nothing
// on an error path either).
type Progress struct {
	p       *Printer
	stage   string
	message string
	start   time.Time
	frame   int
	stopCh  chan struct{}
	doneCh  chan struct{}
}

// Progress starts (in ModeStyled only) an animated "<spinner> [stage]
// message… (elapsed)" line updated on a ticker goroutine. In ModePlain it
// returns a handle that emits nothing at all — no goroutine, no bytes —
// until Done, at which point Done behaves exactly like calling Step
// directly. This is the hard invariant: a plain/CI run of `up` gains zero
// bytes from the mere existence of a Progress call.
func (p *Printer) Progress(stage, message string) *Progress {
	pr := &Progress{p: p, stage: stage, message: message, start: time.Now()}
	if p.mode != ModeStyled {
		return pr
	}
	pr.stopCh = make(chan struct{})
	pr.doneCh = make(chan struct{})
	go pr.loop()
	return pr
}

// loop renders one frame immediately (so the line appears without waiting a
// full tick) and then re-renders every progressTick until stopCh closes.
func (pr *Progress) loop() {
	defer close(pr.doneCh)
	t := time.NewTicker(progressTick)
	defer t.Stop()
	pr.render()
	for {
		select {
		case <-pr.stopCh:
			return
		case <-t.C:
			pr.render()
		}
	}
}

// render draws one spinner frame, erasing the previous one first so the
// line never trails stale characters from a longer earlier message.
func (pr *Progress) render() {
	elapsed := time.Since(pr.start).Round(time.Second)
	frame := spinnerFrames[pr.frame%len(spinnerFrames)]
	pr.frame++
	line := fmt.Sprintf("%s [%s] %s… (%s)", frame, pr.stage, pr.message, elapsed)
	fmt.Fprint(pr.p.out, eraseLine+progressStyle.Render(line))
}

// Stop erases any running spinner line without printing a step — the error
// path, matching the phase-1 behavior of printing nothing when a step
// failed. A no-op in ModePlain (nothing was ever running).
func (pr *Progress) Stop() {
	if pr.stopCh == nil {
		return
	}
	close(pr.stopCh)
	<-pr.doneCh
	fmt.Fprint(pr.p.out, eraseLine)
	pr.stopCh = nil // idempotent: a second Stop/Done call is a no-op
}

// Done stops the animation (erasing its line, if one was running) and prints
// the normal Step line for stage. In ModePlain, Progress never wrote
// anything, so this is byte-identical to calling p.Step(stage, format,
// args...) directly — the phase-1 contract.
func (pr *Progress) Done(format string, args ...any) {
	pr.Stop()
	pr.p.Step(pr.stage, format, args...)
}

// PackAccess is one delivered pack's access info for up.Run's Task 15.3c
// summary: the pack's name and its ${GATEWAY_HOST}-substituted expose URLs
// (internal/pack.ExposeURLs — the same substitution PackObject uses, not
// duplicated here).
type PackAccess struct {
	Name string
	URLs []string
}

// AccessSummary prints a short "here's what you just got" block after `up`
// finishes: one line per pack URL plus a closing hint (typically
// "credentials: cube-idp get secrets"). ModeStyled only — ModePlain is a
// complete no-op, so `up`'s plain-mode final output is exactly what it was
// before Task 15.3 added this call.
func (p *Printer) AccessSummary(packs []PackAccess, hint string) {
	if p.mode != ModeStyled {
		return
	}
	var b strings.Builder
	b.WriteString(sectionStyle.Render("Access") + "\n")
	for _, pk := range packs {
		for _, u := range pk.URLs {
			fmt.Fprintf(&b, "  %s %s\n", stepBadgeStyle.Render(fmt.Sprintf("%-12s", pk.Name)), u)
		}
	}
	fmt.Fprintf(&b, "  %s\n", stepMsgStyle.Render(hint))
	fmt.Fprint(p.out, "\n"+b.String())
}
