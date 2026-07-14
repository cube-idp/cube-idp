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
