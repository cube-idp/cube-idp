package ui

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// TestPlainMatchesPhase1Format pins the plain-mode Step output to the exact
// original step() format that internal/up/up.go printed inline:
// "▸ [%s] %s\n". Several existing tests (e.g. internal/up's
// TestRunOrdersCABeforeCluster) grep this literal format, so it must never
// drift — this test is the guardrail.
func TestPlainMatchesPhase1Format(t *testing.T) {
	var b bytes.Buffer
	p := New(&b, true)
	p.Step("tls", "gateway certificate ready")
	const want = "▸ [tls] gateway certificate ready\n"
	if got := b.String(); got != want {
		t.Fatalf("plain output drifted from the frozen step() format:\ngot:  %q\nwant: %q", got, want)
	}
	if strings.Contains(b.String(), "\x1b[") {
		t.Fatal("plain mode must emit zero ANSI escapes")
	}
}

func TestNonTTYWriterForcesPlain(t *testing.T) {
	var b bytes.Buffer // a bytes.Buffer is never a TTY
	p := New(&b, false)
	p.Step("dns", "ready")
	if strings.Contains(b.String(), "\x1b[") {
		t.Fatal("non-TTY output must be plain even without --plain")
	}
	const want = "▸ [dns] ready\n"
	if got := b.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// ttyRequest is the baseline "developer's rich terminal" Request the
// resolve-ladder cases mutate: a real TTY, a capable TERM, no CI, no flags.
// The pre-14b TestResolve cases map 1:1 onto this (their implicit
// environment was exactly this baseline) and must keep their outcomes.
func ttyRequest() Request {
	return Request{IsTTY: true, Term: "xterm-256color"}
}

// TestResolve exercises the pure §6.2 resolve ladder, one case per rung
// (rungs 4–9 are live in stage A; the --progress flag rungs 1–3 are
// dormant-field tested: the field exists, no cobra flag registers it yet).
func TestResolve(t *testing.T) {
	cases := []struct {
		name string
		req  func() Request
		want Mode
	}{
		// The five pre-14b cases, outcomes identical:
		{"tty, no flag, no CI -> styled", ttyRequest, ModeStyled},
		{"tty, --plain -> plain", func() Request { r := ttyRequest(); r.PlainFlag = true; return r }, ModePlain},
		{"non-tty, no flag, no CI -> plain", func() Request { r := ttyRequest(); r.IsTTY = false; return r }, ModePlain},
		{"tty, no flag, CI set -> plain", func() Request { r := ttyRequest(); r.CIEnv = "1"; return r }, ModePlain},
		{"tty, no flag, CI empty string -> styled", ttyRequest, ModeStyled},
		// Rung 1–3 (dormant field until stage B ships the flag):
		{"--progress=json wins over everything", func() Request {
			r := ttyRequest()
			r.ProgressFlag = "json"
			r.PlainFlag = true
			return r
		}, ModeJSON},
		{"--progress=plain -> plain", func() Request { r := ttyRequest(); r.ProgressFlag = "plain"; return r }, ModePlain},
		{"--progress=live -> ModeLive even on a non-TTY", func() Request {
			r := ttyRequest()
			r.ProgressFlag = "live"
			r.IsTTY = false
			return r
		}, ModeLive},
		{"--progress=auto falls through", func() Request { r := ttyRequest(); r.ProgressFlag = "auto"; return r }, ModeStyled},
		// Rung 4: --plain beats the env policy? No — env is rung 5, flag is rung 4.
		{"--plain beats CUBE_IDP_PROGRESS", func() Request {
			r := ttyRequest()
			r.PlainFlag = true
			r.EnvProgress = "live"
			return r
		}, ModePlain},
		// Rung 5: env policy (the BUILDKIT_PROGRESS precedent):
		{"CUBE_IDP_PROGRESS=plain -> plain", func() Request { r := ttyRequest(); r.EnvProgress = "plain"; return r }, ModePlain},
		{"CUBE_IDP_PROGRESS=live -> ModeLive even on a non-TTY", func() Request {
			r := ttyRequest()
			r.EnvProgress = "live"
			r.IsTTY = false
			return r
		}, ModeLive},
		{"CUBE_IDP_PROGRESS=json -> json", func() Request { r := ttyRequest(); r.EnvProgress = "json"; return r }, ModeJSON},
		{"CUBE_IDP_PROGRESS=auto falls through", func() Request { r := ttyRequest(); r.EnvProgress = "auto"; return r }, ModeStyled},
		{"CUBE_IDP_PROGRESS unknown falls through", func() Request { r := ttyRequest(); r.EnvProgress = "fancy"; return r }, ModeStyled},
		// Rung 8: dumb/unset TERM only — NO_COLOR left the mode ladder in
		// Per no-color.org, a non-empty NO_COLOR keeps the resolved mode and
		// strips color at the writer instead (ColorEnabled/NewFor).
		{"non-empty NO_COLOR keeps ModeStyled on a TTY (strip-only)", func() Request { r := ttyRequest(); r.NoColor = true; return r }, ModeStyled},
		{"TERM=dumb -> plain", func() Request { r := ttyRequest(); r.Term = "dumb"; return r }, ModePlain},
		{"TERM unset -> plain", func() Request { r := ttyRequest(); r.Term = ""; return r }, ModePlain},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := tc.req()
			if got := Resolve(req); got != tc.want {
				t.Fatalf("Resolve(%+v) = %v, want %v", req, got, tc.want)
			}
		})
	}
}

// TestResolveAutoNeverProducesLive pins the §6.1 invariant that makes
// ModeLive meaningful: auto-detection can only ever produce ModeStyled
// (downgradeable); only an explicit live request yields ModeLive.
func TestResolveAutoNeverProducesLive(t *testing.T) {
	tty := []bool{true, false}
	ci := []string{"", "1"}
	noColor := []bool{true, false}
	terms := []string{"", "dumb", "xterm-256color"}
	for _, isTTY := range tty {
		for _, ciEnv := range ci {
			for _, nc := range noColor {
				for _, term := range terms {
					r := Request{IsTTY: isTTY, CIEnv: ciEnv, NoColor: nc, Term: term}
					if got := Resolve(r); got == ModeLive {
						t.Fatalf("Resolve(%+v) = ModeLive — only an explicit live request may produce it", r)
					}
				}
			}
		}
	}
}

// TestPlainFlagForcesPlainOnTTY documents (via Resolve, since we cannot open
// a real TTY in a unit test) that --plain always wins even when IsTTY is
// true — this is what makes `--plain` meaningful on a developer's own
// terminal, not just in CI.
func TestPlainFlagForcesPlainOnTTY(t *testing.T) {
	r := ttyRequest()
	r.PlainFlag = true
	if got := Resolve(r); got != ModePlain {
		t.Fatalf("Resolve(PlainFlag=true, IsTTY=true, ...) = %v, want ModePlain", got)
	}
}

// TestNewForDowngradeMatrix pins the per-writer downgrade rule (§6.4): a
// non-terminal writer renders plain even under a styled process mode — the
// exact behavior that keeps every bytes.Buffer test and e2e pipe
// byte-stable — and only the explicit ModeLive bypasses the check. A
// Printer has no JSON form, so ModeJSON downgrades to plain too.
func TestNewForDowngradeMatrix(t *testing.T) {
	defer SetMode(ModeStyled) // restore the zero-value default

	cases := []struct {
		mode Mode
		want Mode
	}{
		{ModeStyled, ModePlain}, // auto-styled never reaches a non-terminal
		{ModePlain, ModePlain},
		{ModeJSON, ModePlain},  // plain IS the static machine contract in stage A
		{ModeLive, ModeStyled}, // the explicit-force bypass
	}
	for _, tc := range cases {
		SetMode(tc.mode)
		var b bytes.Buffer // never a TTY
		p := NewFor(&b)
		if p.mode != tc.want {
			t.Fatalf("NewFor under SetMode(%v) = mode %v, want %v", tc.mode, p.mode, tc.want)
		}
	}
}

// TestSectionPlainExactLiteral pins Section's ModePlain output to exactly
// fmt.Fprintln(out, title) — the raw call every migrated command (diff's
// KERNEL OBJECTS/PACK CONTENT/ORPHANS, upgrade's "Kernel + delivery object
// changes:") made before switching to Section.
func TestSectionPlainExactLiteral(t *testing.T) {
	cases := []string{
		"KERNEL OBJECTS",
		"PACK CONTENT",
		"ORPHANS (in inventory, no longer desired)",
		"\nKernel + delivery object changes:",
	}
	for _, title := range cases {
		var b bytes.Buffer
		p := New(&b, true)
		p.Section(title)
		want := title + "\n"
		if got := b.String(); got != want {
			t.Fatalf("Section(%q) plain = %q, want %q", title, got, want)
		}
	}
}

// TestGlyphPlainPassesThrough pins Glyph's ModePlain output to the bare
// character unchanged — the same literal every migrated call site (status'
// "✔"/"✗", doctor's "✔"/"✗"/"⚠") printed inline before switching to Glyph.
func TestGlyphPlainPassesThrough(t *testing.T) {
	var b bytes.Buffer
	p := New(&b, true)
	for _, g := range []string{GlyphOK, GlyphErr, GlyphWarn, "?"} {
		if got := p.Glyph(g); got != g {
			t.Fatalf("Glyph(%q) plain = %q, want unchanged", g, got)
		}
	}
}

// TestWarnPlainExactLiteral pins Warn's ModePlain output to exactly
// fmt.Fprintln(out, msg) — get secrets' legacy-label deprecation note.
func TestWarnPlainExactLiteral(t *testing.T) {
	var b bytes.Buffer
	p := New(&b, true)
	p.Warn("%s", "note: gitea was found via the legacy label")
	const want = "note: gitea was found via the legacy label\n"
	if got := b.String(); got != want {
		t.Fatalf("Warn plain = %q, want %q", got, want)
	}
}

// TestAccessSummaryPlainStableLines pins the ONE owner-ratified plain
// contract change for the Access summary (see ADR 0023, replacing the earlier
// TestAccessSummaryPlainNoOp): Access is data with a stable plain
// projection — a blank line, the "Access" header, one %-12s-padded line per
// pack URL, and the hint line — and still zero ANSI escapes.
func TestAccessSummaryPlainStableLines(t *testing.T) {
	var b bytes.Buffer
	p := New(&b, true)
	p.AccessSummary([]PackAccess{{Name: "gitea", URLs: []string{"https://gitea.cube.local:8443"}}},
		"credentials: cube-idp get secrets")
	const want = "\nAccess\n" +
		"  gitea        https://gitea.cube.local:8443\n" +
		"  credentials: cube-idp get secrets\n"
	if got := b.String(); got != want {
		t.Fatalf("plain AccessSummary drifted from the §9 contract:\ngot:  %q\nwant: %q", got, want)
	}
	if strings.Contains(b.String(), "\x1b[") {
		t.Fatal("plain mode must emit zero ANSI escapes")
	}
}

// TestAccessSummaryStyledListsURLsAndHint is a deterministic styled-mode
// check (no goroutines involved — AccessSummary is synchronous) that the
// pack URL and the closing hint both appear.
func TestAccessSummaryStyledListsURLsAndHint(t *testing.T) {
	var b bytes.Buffer
	p := &Printer{out: &b, mode: ModeStyled}
	p.AccessSummary([]PackAccess{{Name: "gitea", URLs: []string{"https://gitea.example"}}}, "credentials: cube-idp get secrets")
	got := b.String()
	if !strings.Contains(got, "https://gitea.example") {
		t.Fatalf("AccessSummary missing pack URL: %q", got)
	}
	if !strings.Contains(got, "credentials: cube-idp get secrets") {
		t.Fatalf("AccessSummary missing the credentials hint: %q", got)
	}
}

// resetColorPolicy restores the zero-signal default every color test must
// leave behind: --color=auto, no env vars, no explicit plain ask.
func resetColorPolicy() { SetColorPolicy("auto", false, false, false) }

// TestEnvColorPolicyEmptyMeansUnset pins the no-color.org fix: an
// empty NO_COLOR is unset — only a non-empty value counts — and the same
// non-empty rule applies to CLICOLOR_FORCE (bixense).
func TestEnvColorPolicyEmptyMeansUnset(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("CLICOLOR_FORCE", "")
	noColor, force := EnvColorPolicy()
	if noColor {
		t.Fatal("empty NO_COLOR must count as unset (no-color.org)")
	}
	if force {
		t.Fatal("empty CLICOLOR_FORCE must count as unset")
	}
	t.Setenv("NO_COLOR", "1")
	t.Setenv("CLICOLOR_FORCE", "1")
	noColor, force = EnvColorPolicy()
	if !noColor {
		t.Fatal("non-empty NO_COLOR must be honored")
	}
	if !force {
		t.Fatal("non-empty CLICOLOR_FORCE must be honored")
	}
}

// TestNoColorStripsColorOnly pins NO_COLOR's actual spec: the styled
// surface keeps its layout and glyphs, only the escapes go. ModeLive is the
// documented escape hatch that lets a styled Printer reach a buffer.
func TestNoColorStripsColorOnly(t *testing.T) {
	defer SetMode(ModeStyled)
	defer resetColorPolicy()
	SetMode(ModeLive)
	SetColorPolicy("auto", true, false, false)
	var b bytes.Buffer
	p := NewFor(&b)
	if !p.Styled() {
		t.Fatal("NO_COLOR must not downgrade the mode — strip color only")
	}
	p.Step("tls", "gateway certificate ready")
	got := b.String()
	if strings.Contains(got, "\x1b[") {
		t.Fatalf("non-empty NO_COLOR must render zero ANSI: %q", got)
	}
	if got != "▸ [tls] gateway certificate ready\n" {
		t.Fatalf("layout and glyphs must survive the strip: %q", got)
	}
}

// TestColorNeverStripsOnStyled pins --color=never: zero ANSI on a styled
// surface, and the flag beats a force-color environment.
func TestColorNeverStripsOnStyled(t *testing.T) {
	defer SetMode(ModeStyled)
	defer resetColorPolicy()
	SetMode(ModeLive)
	SetColorPolicy("never", false, true, false) // CLICOLOR_FORCE set — the flag must win
	var b bytes.Buffer
	p := NewFor(&b)
	p.Step("tls", "ready")
	if strings.Contains(b.String(), "\x1b[") {
		t.Fatalf("--color=never must render zero ANSI even under CLICOLOR_FORCE: %q", b.String())
	}
}

// TestColorForcePipesStyledStatic pins the CI half of the bixense ladder:
// CLICOLOR_FORCE (or --color=always) upgrades an auto-resolved plain pipe to
// the colored styled-static surface — but never an explicit plain/json ask.
func TestColorForcePipesStyledStatic(t *testing.T) {
	defer SetMode(ModeStyled)
	defer resetColorPolicy()

	SetMode(ModePlain) // what CI's rung 7 resolves
	SetColorPolicy("auto", false, true, false)
	var b bytes.Buffer
	p := NewFor(&b)
	if !p.Styled() {
		t.Fatal("CLICOLOR_FORCE must upgrade an auto-plain pipe to styled-static")
	}
	p.Step("tls", "ready")
	if !strings.Contains(b.String(), "\x1b[") {
		t.Fatalf("forced color must emit ANSI on the pipe: %q", b.String())
	}

	// --plain / --progress=plain stays plain: force never overrides an ask.
	SetColorPolicy("auto", false, true, true)
	var b2 bytes.Buffer
	if p := NewFor(&b2); p.Styled() {
		t.Fatal("force-color must never override an explicit plain request")
	}

	// ModeJSON stays the machine contract: never colored.
	SetMode(ModeJSON)
	SetColorPolicy("always", false, false, false)
	var b3 bytes.Buffer
	if p := NewFor(&b3); p.Styled() {
		t.Fatal("force-color must never restyle ModeJSON output")
	}
}

// TestColorEnabledLadder pins the exported color ladder end to end:
// flag beats env, NO_COLOR beats CLICOLOR_FORCE, terminal detection last.
func TestColorEnabledLadder(t *testing.T) {
	defer resetColorPolicy()
	cases := []struct {
		name           string
		flag           string
		noColor, force bool
		want           bool
	}{
		{"--color=never", "never", false, false, false},
		{"--color=never beats CLICOLOR_FORCE", "never", false, true, false},
		{"--color=always", "always", false, false, true},
		{"--color=always beats NO_COLOR", "always", true, false, true},
		{"NO_COLOR beats CLICOLOR_FORCE", "auto", true, true, false},
		{"CLICOLOR_FORCE forces a pipe", "auto", false, true, true},
		{"auto on a non-terminal", "auto", false, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			SetColorPolicy(tc.flag, tc.noColor, tc.force, false)
			var b bytes.Buffer // never a terminal
			if got := ColorEnabled(&b); got != tc.want {
				t.Fatalf("ColorEnabled(flag=%q noColor=%v force=%v) = %v, want %v",
					tc.flag, tc.noColor, tc.force, got, tc.want)
			}
		})
	}
}

// TestRunPipelineStaticForcedColorOnPipe pins the pipeline-level reach:
// CLICOLOR_FORCE selects the styled-static projection (colored bytes, no
// animations) for an auto-plain pipe — the CI log use case.
func TestRunPipelineStaticForcedColorOnPipe(t *testing.T) {
	defer SetMode(ModeStyled)
	defer resetColorPolicy()
	SetMode(ModePlain)
	SetColorPolicy("auto", false, true, false)
	var b bytes.Buffer
	err := RunPipelineStatic(context.Background(), "test", &b,
		func(ctx context.Context, con *Console) error {
			con.Step("tls", "gateway certificate ready")
			return nil
		})
	if err != nil {
		t.Fatalf("RunPipelineStatic: %v", err)
	}
	got := b.String()
	if !strings.Contains(got, "gateway certificate ready") {
		t.Fatalf("styled-static projection lost the step content: %q", got)
	}
	if !strings.Contains(got, "\x1b[") {
		t.Fatalf("CLICOLOR_FORCE must emit colored styled-static bytes on a pipe: %q", got)
	}
}
