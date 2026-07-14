package ui

import (
	"bytes"
	"strings"
	"testing"
)

// TestPlainMatchesPhase1Format pins the plain-mode Step output to the exact
// phase-1 step() format (internal/up/up.go, checkpoint 0.13):
// "▸ [%s] %s\n". Several existing tests (e.g. internal/up's
// TestRunOrdersCABeforeCluster) grep this literal format, so it must never
// drift — this test is the guardrail.
func TestPlainMatchesPhase1Format(t *testing.T) {
	var b bytes.Buffer
	p := New(&b, true)
	p.Step("tls", "gateway certificate ready")
	const want = "▸ [tls] gateway certificate ready\n"
	if got := b.String(); got != want {
		t.Fatalf("plain output drifted from the phase-1 format:\ngot:  %q\nwant: %q", got, want)
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
		// Rung 8: no-color.org semantics + dumb/unset TERM:
		{"NO_COLOR present (even empty) -> plain", func() Request { r := ttyRequest(); r.NoColor = true; return r }, ModePlain},
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
		{ModeJSON, ModePlain}, // plain IS the static machine contract in stage A
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

// TestProgressPlainEmitsNothingBeforeDone pins Task 15.3a's hard invariant:
// in ModePlain (the only mode a bytes.Buffer ever resolves to — it is never
// a TTY), Progress must not write a single byte until Done, and Done's
// output must be byte-identical to calling Step directly — no drift from
// the phase-1 "▸ [%s] %s\n" format just because a Progress call now wraps
// it.
func TestProgressPlainEmitsNothingBeforeDone(t *testing.T) {
	var b bytes.Buffer
	p := New(&b, true)
	pr := p.Progress("cluster", "creating kind cluster")
	if b.Len() != 0 {
		t.Fatalf("plain Progress must emit nothing before Done, got %q", b.String())
	}
	pr.Done("%s cluster ready (context %s)", "kind", "kind-dev")
	const want = "▸ [cluster] kind cluster ready (context kind-dev)\n"
	if got := b.String(); got != want {
		t.Fatalf("plain Done drifted from Step's format:\ngot:  %q\nwant: %q", got, want)
	}
}

// TestProgressPlainStopEmitsNothing covers the error path: the phase-1 code
// printed nothing when a step failed, so Stop (called instead of Done on an
// error) must also emit nothing in ModePlain.
func TestProgressPlainStopEmitsNothing(t *testing.T) {
	var b bytes.Buffer
	p := New(&b, true)
	pr := p.Progress("engine", "installing flux")
	pr.Stop()
	if b.Len() != 0 {
		t.Fatalf("plain Stop must emit nothing, got %q", b.String())
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

// TestProgressStyledRendersSpinnerAndErases is the deterministic styled-mode
// smoke test (bytes.Buffer forced into ModeStyled via the &Printer{} literal
// seam, since a buffer is never a real TTY). It runs the real
// Progress/Stop/Done code path rather than a ticker: loop() unconditionally
// renders one frame before it ever selects on stopCh/the ticker (see
// Progress.loop), so calling Done immediately after Progress is
// deterministic — exactly one frame is guaranteed to have been written, and
// Stop's <-doneCh guarantees that write completed before Done goes on to
// erase and print the final Step line. No time.Sleep, no flakiness.
func TestProgressStyledRendersSpinnerAndErases(t *testing.T) {
	var b bytes.Buffer
	p := &Printer{out: &b, mode: ModeStyled}

	pr := p.Progress("cluster", "creating kind cluster")
	pr.Done("kind cluster ready (context kind-dev)")

	got := b.String()
	if !strings.Contains(got, "\x1b[2K") {
		t.Fatalf("styled Progress must erase its line: %q", got)
	}
	if !strings.Contains(got, "[cluster]") {
		t.Fatalf("styled Progress output missing the stage tag: %q", got)
	}
	if !strings.Contains(got, "creating kind cluster") {
		t.Fatalf("styled Progress output missing the in-flight message: %q", got)
	}
	if !strings.Contains(got, "kind cluster ready") {
		t.Fatalf("Done must still print the final styled Step line: %q", got)
	}
}

// TestAccessSummaryPlainStableLines pins the ONE owner-ratified plain
// contract change of Task 14b (design doc §9, replacing the pre-14b
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
