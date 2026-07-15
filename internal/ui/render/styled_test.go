package render

import (
	"bytes"
	"regexp"
	"testing"
)

// stripANSI removes CSI escape sequences (\x1b[...<letter>) so a styled
// projection's bytes can be compared against its plain counterpart for
// content identity — presentation (color) differs, words never do.
var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func stripANSI(s string) string { return ansiRE.ReplaceAllString(s, "") }

// TestStyledContentIdenticalToPlain feeds the same recorded event slice
// (the canonical `up` fixture plus its silent-event set) into both Styled
// and Plain and asserts the styled projection, with ANSI stripped, equals
// the plain projection byte-for-byte — presentation only, per the ui.go
// Printer rule ("content identical, presentation only").
//
// Scope: this identity claim only covers the event types canonicalUpRun()
// exercises (RunStarted, StepDone, StepStarted, Note, Access, HealthTick,
// RunDone) — it does NOT exercise event.Warn, which is not part of that
// fixture. Warn is a deliberate exception to the content-identical rule:
// Styled renders "⚠ msg" (glyph prefix) while Plain renders the bare "msg"
// (see styled.go's and plain.go's Warn cases) — by design, not a drift bug.
// Do not add Warn to canonicalUpRun() to "complete" this test's coverage.
func TestStyledContentIdenticalToPlain(t *testing.T) {
	evs := canonicalUpRun()

	var plainBuf bytes.Buffer
	project(t, evs, Plain(&plainBuf))

	var styledBuf bytes.Buffer
	project(t, evs, Styled(&styledBuf))

	gotStyled := stripANSI(styledBuf.String())
	wantPlain := plainBuf.String()
	if gotStyled != wantPlain {
		t.Fatalf("styled (ANSI-stripped) content drifted from plain:\ngot:  %q\nwant: %q", gotStyled, wantPlain)
	}
}

// TestStyledSilentEventsAreZeroBytes restates the zero-byte event set for
// Styled: RunStarted/StepStarted/StepFailed/HealthTick/Diagnosis/RunDone
// produce no output in the styled-static projection either — same as Plain.
func TestStyledSilentEventsAreZeroBytes(t *testing.T) {
	for _, ev := range silentEventsFixture() {
		var b bytes.Buffer
		Styled(&b)(ev)
		if b.Len() != 0 {
			t.Fatalf("%T must project to zero styled bytes, got %q", ev, b.String())
		}
	}
}
