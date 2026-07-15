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
