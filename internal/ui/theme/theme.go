// Package theme is the single source of cube-idp's terminal look: one
// background-adaptive palette, one glyph set, and per-stage presentation
// metadata. It is a LEAF package (lipgloss + x/term + stdlib only) so both
// internal/ui and internal/ui/render can import it — dissolving the cycle
// that forced render/styled.go to duplicate ui.go's styles.
package theme

import (
	"os"

	lipgloss "charm.land/lipgloss/v2"
	"golang.org/x/term"
)

// Glyphs — the single normative set. Renderers own glyphs; event
// content never carries them (spec R2).
const (
	GlyphStep = "▸"
	GlyphOK   = "✔"
	GlyphErr  = "✗"
	GlyphWarn = "⚠"
)

// Theme is the semantic style set. Foregrounds stay inside the basic
// ANSI-16 range so user terminal palettes keep control; meaning never rides
// on color alone (glyph + word are always paired by callers).
type Theme struct {
	Badge    lipgloss.Style // [stage] badges
	Msg      lipgloss.Style // step message text
	OK       lipgloss.Style // success
	Err      lipgloss.Style // failure
	Warn     lipgloss.Style // advisories, spinner
	Dim      lipgloss.Style // durations, hints, log tails
	Section  lipgloss.Style // headings
	ErrPanel lipgloss.Style // CUBE-xxxx box border
	ErrLabel lipgloss.Style // "cause:"/"fix:" labels
}

// New builds the palette for a dark or light background. Pure — tests and
// the live program (via tea.BackgroundColorMsg) call it directly.
func New(isDark bool) Theme {
	ld := lipgloss.LightDark(isDark)
	return Theme{
		Badge:    lipgloss.NewStyle().Bold(true).Foreground(ld(lipgloss.Color("4"), lipgloss.Color("12"))),
		Msg:      lipgloss.NewStyle().Foreground(ld(lipgloss.Color("0"), lipgloss.Color("7"))),
		OK:       lipgloss.NewStyle().Bold(true).Foreground(ld(lipgloss.Color("2"), lipgloss.Color("10"))),
		Err:      lipgloss.NewStyle().Bold(true).Foreground(ld(lipgloss.Color("1"), lipgloss.Color("9"))),
		Warn:     lipgloss.NewStyle().Bold(true).Foreground(ld(lipgloss.Color("3"), lipgloss.Color("11"))),
		Dim:      lipgloss.NewStyle().Foreground(lipgloss.Color("8")),
		Section:  lipgloss.NewStyle().Bold(true),
		ErrPanel: lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(ld(lipgloss.Color("1"), lipgloss.Color("9"))).Padding(0, 1),
		ErrLabel: lipgloss.NewStyle().Foreground(lipgloss.Color("8")),
	}
}

// Detect resolves the background ONCE for static (non-Bubble-Tea) output.
// Guarded to real TTYs; any doubt = dark, so the worst case equals today's
// dark-assuming palette (OSC queries can hang under tmux — never query a
// non-terminal). Inside a live program use tea.RequestBackgroundColor
// instead; never both.
func Detect(in, out *os.File) Theme {
	if in == nil || out == nil ||
		!term.IsTerminal(int(in.Fd())) || !term.IsTerminal(int(out.Fd())) {
		return New(true)
	}
	return New(lipgloss.HasDarkBackground(in, out))
}

// StageMeta attaches presentation to the open stage-name strings documented
// in internal/ui/event/event.go. Producers never change for presentation.
type StageMeta struct {
	Group string // phase grouping for the live tree
}

// Stages covers every stage name event.go documents plus down's stages.
// An unknown stage renders fine — metadata is additive decoration.
var Stages = map[string]StageMeta{
	"config": {Group: "prepare"}, "ca": {Group: "prepare"},
	"cluster": {Group: "cluster"}, "registry": {Group: "cluster"},
	"packs-crd": {Group: "engine"}, "engine": {Group: "engine"},
	"tls": {Group: "gateway"}, "pack": {Group: "packs"},
	"packs": {Group: "packs"}, "lock": {Group: "packs"},
	"dns": {Group: "gateway"}, "health": {Group: "verify"},
	"cnoe": {Group: "packs"}, "cascade": {Group: "teardown"},
	"trust": {Group: "teardown"},
}

// BadgeWidth is the fixed "[stage]" column width: badges are sized to the
// longest known stage name so every message starts at one x-position.
func BadgeWidth() int {
	w := 0
	for name := range Stages {
		if n := len(name) + 2; n > w {
			w = n
		}
	}
	return w
}
