package render

import (
	"bytes"
	"regexp"
	"strings"
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
// exercises (RunStarted, StepDone, StepStarted, Epilogue, Access,
// HealthTick, RunDone) — it does NOT exercise event.Warn, which is not part
// of that fixture. Warn is a deliberate exception to the content-identical
// rule: Styled renders "⚠ msg" (glyph prefix) while Plain renders the bare
// "msg" (see styled.go's and plain.go's Warn cases) — by design, not a
// drift bug. Do not add Warn to canonicalUpRun() to "complete" this test's
// coverage. The epilogue's "✔ " is the second such exception (ratified R2):
// Styled re-adds the glyph as presentation, so it is normalized away below.
func TestStyledContentIdenticalToPlain(t *testing.T) {
	evs := canonicalUpRun()

	var plainBuf bytes.Buffer
	project(t, evs, Plain(&plainBuf))

	var styledBuf bytes.Buffer
	project(t, evs, Styled(&styledBuf))

	gotStyled := stripANSI(styledBuf.String())
	// R2: drop the epilogue's presentation glyph (exactly one occurrence)
	// before comparing — content identity holds glyph-excepted.
	gotStyled = strings.Replace(gotStyled, "✔ ", "", 1)
	wantPlain := plainBuf.String()
	if gotStyled != wantPlain {
		t.Fatalf("styled (ANSI-stripped) content drifted from plain:\ngot:  %q\nwant: %q", gotStyled, wantPlain)
	}
}

// TestStyledSilentEventsAreZeroBytes restates the zero-byte event set for
// Styled: RunStarted/StepFailed/HealthTick/Diagnosis/RunDone produce no
// output in the styled-static projection either — same as Plain.
// (StepStarted left the set when start lines were sanctioned.)
func TestStyledSilentEventsAreZeroBytes(t *testing.T) {
	for _, ev := range silentEventsFixture() {
		var b bytes.Buffer
		Styled(&b)(ev)
		if b.Len() != 0 {
			t.Fatalf("%T must project to zero styled bytes, got %q", ev, b.String())
		}
	}
}
