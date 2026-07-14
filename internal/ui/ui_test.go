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

// TestResolve exercises the pure decision function behind New: plain wins if
// any of {--plain, non-TTY, $CI set} holds. Kept pure (no real terminal
// needed) so the --plain-forces-plain-on-a-TTY case is unit-testable.
func TestResolve(t *testing.T) {
	cases := []struct {
		name             string
		plainFlag, isTTY bool
		ciEnv            string
		want             Mode
	}{
		{"tty, no flag, no CI -> styled", false, true, "", ModeStyled},
		{"tty, --plain -> plain", true, true, "", ModePlain},
		{"non-tty, no flag, no CI -> plain", false, false, "", ModePlain},
		{"tty, no flag, CI set -> plain", false, true, "1", ModePlain},
		{"tty, no flag, CI empty string -> styled", false, true, "", ModeStyled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Resolve(tc.plainFlag, tc.isTTY, tc.ciEnv); got != tc.want {
				t.Fatalf("Resolve(%v, %v, %q) = %v, want %v", tc.plainFlag, tc.isTTY, tc.ciEnv, got, tc.want)
			}
		})
	}
}

// TestPlainFlagForcesPlainOnTTY documents (via Resolve, since we cannot open
// a real TTY in a unit test) that --plain always wins even when isTTY is
// true — this is what makes `--plain` meaningful on a developer's own
// terminal, not just in CI.
func TestPlainFlagForcesPlainOnTTY(t *testing.T) {
	if got := Resolve(true, true, ""); got != ModePlain {
		t.Fatalf("Resolve(plainFlag=true, isTTY=true, ...) = %v, want ModePlain", got)
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

// TestAccessSummaryPlainNoOp pins Task 15.3c: AccessSummary must be a
// complete no-op in ModePlain, so `up`'s plain-mode final output gains zero
// bytes from this call existing.
func TestAccessSummaryPlainNoOp(t *testing.T) {
	var b bytes.Buffer
	p := New(&b, true)
	p.AccessSummary([]PackAccess{{Name: "gitea", URLs: []string{"https://gitea.example"}}}, "credentials: cube-idp get secrets")
	if b.Len() != 0 {
		t.Fatalf("plain AccessSummary must emit nothing, got %q", b.String())
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
