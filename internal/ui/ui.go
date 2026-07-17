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
//
// Printer is the STATIC surface — Step, Section, Glyph, Warn, AccessSummary:
// whole lines only, no cursor movement, no goroutines. Every animated or
// multi-step surface goes through RunPipeline/RunPipelineStatic; the
// bubbles/spinner inside the live renderer (internal/ui/render) is the only
// animation system.
package ui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync/atomic"

	"golang.org/x/term"

	"github.com/cube-idp/cube-idp/internal/ui/event"
	"github.com/cube-idp/cube-idp/internal/ui/theme"
)

// th is the process-wide adaptive palette (internal/ui/theme): detected once
// from the real terminal, dark-defaulting on any doubt. Styled rendering only
// ever engages on a real TTY (per-writer downgrade), so non-TTY runs never
// depend on it.
var th = theme.Detect(os.Stdin, os.Stdout)

// Mode is the process-wide output mode, resolved exactly once by
// cmd/root.go's PersistentPreRunE via Resolve (the §6 ladder) and stored
// with SetMode. Existing constants keep their order and numeric values.
type Mode int

const (
	// ModeStyled renders a lipgloss-styled stage badge and dimmed message —
	// rich (auto-resolved): styled static output; the LiveRenderer on
	// event-stream commands; per-writer downgradeable (NewFor).
	ModeStyled Mode = iota
	// ModePlain reproduces the phase-1 step() format verbatim — the
	// byte-stable projection.
	ModePlain
	// ModeJSON is the machine mode: a JSON-lines event stream on
	// event-stream commands (up/down); the plain projection elsewhere.
	// Never styled. EXPERIMENTAL until the D5 v1 config freeze.
	ModeJSON
	// ModeLive is explicitly forced live (CUBE_IDP_PROGRESS=live; the
	// --progress=live flag ships in stage B): the LiveRenderer even on a
	// non-TTY — the ONLY mode that bypasses the per-writer downgrade.
	// Auto-detection can only ever produce ModeStyled, so NewFor and
	// RunPipeline can distinguish "the user demanded live" from "live
	// because a TTY was detected".
	ModeLive
)

// currentMode holds the resolved Mode. Zero value ModeStyled matches
// today's default: every non-terminal writer still downgrades to plain in
// NewFor/RunPipeline, so tests that never call SetMode stay byte-stable.
var currentMode atomic.Int32

// SetMode stores the process-wide resolved Mode. cmd/root.go calls it once,
// in PersistentPreRunE, before any command's RunE executes — the successor
// of the deleted ui.PlainFlag package var.
func SetMode(m Mode) { currentMode.Store(int32(m)) }

// CurrentMode returns the Mode stored by SetMode (ModeStyled when nothing
// resolved one — always per-writer downgraded before any styling engages).
func CurrentMode() Mode { return Mode(currentMode.Load()) }

// Request carries every input the resolve ladder consults. cmd/root.go
// fills it exactly once, in PersistentPreRunE.
type Request struct {
	ProgressFlag string // --progress value; "" or "auto" = not forced (flag ships in stage B, field exists from stage A)
	PlainFlag    bool   // --plain, the permanent alias
	EnvProgress  string // $CUBE_IDP_PROGRESS
	IsTTY        bool   // ui.IsTerminal(os.Stdout)
	CIEnv        string // $CI
	NoColor      bool   // $NO_COLOR present (os.LookupEnv ok-bool; presence suffices, no-color.org semantics)
	Term         string // $TERM
}

// Resolve is the pure, side-effect-free resolve ladder (design doc §6.2) —
// single resolve, highest rung wins; codifies gh/buildx/terraform practice,
// clig.dev, and no-color.org:
//
//  1. --progress=json  → ModeJSON
//  2. --progress=plain → ModePlain
//  3. --progress=live  → ModeLive (explicit force, works on a non-TTY)
//  4. --plain          → ModePlain (permanent alias, never deprecated)
//  5. CUBE_IDP_PROGRESS ∈ {plain,live,json} (CI images set policy once —
//     the BUILDKIT_PROGRESS precedent; auto/empty/unknown falls through)
//  6. stdout not a TTY → ModePlain
//  7. $CI set (non-empty) → ModePlain
//  8. $NO_COLOR present (even empty) or TERM dumb/unset → ModePlain (the
//     strictest reading: plain, not merely uncolored)
//  9. → ModeStyled (the rich-by-default decision)
//
// --progress beats --plain (more specific); documented, never an error.
func Resolve(r Request) Mode {
	switch r.ProgressFlag {
	case "json":
		return ModeJSON
	case "plain":
		return ModePlain
	case "live":
		return ModeLive
	}
	if r.PlainFlag {
		return ModePlain
	}
	switch r.EnvProgress {
	case "plain":
		return ModePlain
	case "live":
		return ModeLive
	case "json":
		return ModeJSON
	}
	if !r.IsTTY || r.CIEnv != "" {
		return ModePlain
	}
	if r.NoColor || r.Term == "dumb" || r.Term == "" {
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
// $CI is set — the pre-14b precedence, kept for tests (its plain=true form
// is used throughout ui_test.go). Production call sites use NewFor.
func New(out io.Writer, plain bool) *Printer {
	if plain || !IsTerminal(out) || os.Getenv("CI") != "" {
		return &Printer{out: out, mode: ModePlain}
	}
	return &Printer{out: out, mode: ModeStyled}
}

// NewFor builds a Printer for out from the process-wide resolved mode,
// downgraded per-writer: auto-resolved styled output only ever reaches a
// real terminal; only an explicit ModeLive skips that check.
//
// The per-writer downgrade rule is load-bearing: even when the resolved
// mode is ModeStyled, a writer that is not a real terminal renders plain.
// This is exactly the old New(out, plain)+IsTerminal behavior and is what
// keeps every unit test (bytes.Buffer), every e2e pipe, and every CI log
// byte-stable with zero plumbing. The sole exception is ModeLive
// (producible only by an explicit live request, never by auto-detection) —
// a documented escape hatch, the GH_FORCE_TTY analog.
func NewFor(out io.Writer) *Printer {
	m := CurrentMode()
	switch {
	case m == ModeJSON:
		m = ModePlain // a Printer has no JSON form: plain IS the machine contract for static commands in stage A
	case m == ModeLive:
		m = ModeStyled // explicit force: the ONLY path that skips the TTY downgrade
	case m == ModeStyled && !IsTerminal(out):
		m = ModePlain // per-writer downgrade: auto-styled never reaches a non-terminal
	}
	return &Printer{out: out, mode: m}
}

// Styled reports whether this Printer renders the rich (lipgloss) surface.
// Request/response commands (status, doctor) consult it to choose between
// their byte-frozen plain projection and the stage-B rich static render —
// NewFor has already applied the per-writer downgrade, so a non-terminal
// writer (every test buffer, every pipe) reports false and stays plain.
func (p *Printer) Styled() bool { return p.mode == ModeStyled }

// Out returns the writer this Printer was built for, so a command that needs
// to interleave its own rich lipgloss layout with Printer calls can target the
// same destination without threading the writer separately.
func (p *Printer) Out() io.Writer { return p.out }

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
		th.Badge.Render(fmt.Sprintf("▸ [%s]", name)),
		th.Msg.Render(msg))
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
	fmt.Fprintln(p.out, th.Section.Render(title))
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
		return th.OK.Render(g)
	case GlyphErr:
		return th.Err.Render(g)
	case GlyphWarn:
		return th.Warn.Render(g)
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
	fmt.Fprintf(p.out, "%s %s\n", p.Glyph(GlyphWarn), th.Warn.Render(msg))
}

// PackAccess is one delivered pack's access info for up.Run's access
// summary: the pack's name and its ${GATEWAY_HOST}-substituted expose URLs
// (internal/pack.ExposeURLs — the same substitution PackObject uses, not
// duplicated here). An alias of event.PackAccess (Task 14b) so
// internal/up's construction sites and the event stream share one type.
type PackAccess = event.PackAccess

// AccessSummary prints a short "here's what you just got" block after `up`
// finishes: one line per pack URL plus a closing hint (typically
// "credentials: cube-idp get secrets"). As of Task 14b (Owner Decision #15,
// design doc §9) this is DATA with a stable plain projection — the one
// owner-ratified plain-output change: scripts and CI want to scrape "what
// URLs did I just get". The plain bytes mirror the styled layout minus ANSI
// and are pinned by TestAccessSummaryPlainStableLines (and reproduced by
// the plain event renderer, internal/ui/render).
func (p *Printer) AccessSummary(packs []PackAccess, hint string) {
	if p.mode != ModeStyled {
		fmt.Fprint(p.out, "\nAccess\n")
		for _, pk := range packs {
			for _, u := range pk.URLs {
				fmt.Fprintf(p.out, "  %-12s %s\n", pk.Name, u)
			}
		}
		fmt.Fprintf(p.out, "  %s\n", hint)
		return
	}
	var b strings.Builder
	b.WriteString(th.Section.Render("Access") + "\n")
	for _, pk := range packs {
		for _, u := range pk.URLs {
			fmt.Fprintf(&b, "  %s %s\n", th.Badge.Render(fmt.Sprintf("%-12s", pk.Name)), u)
		}
	}
	fmt.Fprintf(&b, "  %s\n", th.Msg.Render(hint))
	fmt.Fprint(p.out, "\n"+b.String())
}
