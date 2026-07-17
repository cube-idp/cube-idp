package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TestHelpOnPipeHasZeroANSI pins the fang help contract for machine
// surfaces: help rendered into anything that is not a real terminal (every
// pipe, every CI log, every test buffer) must carry zero ANSI escapes —
// fang's colorprofile writer downgrades to a full strip off-TTY, and this
// test is the fence that keeps `cube-idp --help | grep` scriptable.
func TestHelpOnPipeHasZeroANSI(t *testing.T) {
	// A developer's force-color environment must not leak color into this
	// pipe assertion (colorprofile honors CLICOLOR_FORCE even off-TTY).
	t.Setenv("CLICOLOR_FORCE", "")

	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--help"})

	if err := executeFang(context.Background(), root); err != nil {
		t.Fatalf("--help via fang returned error: %v", err)
	}
	got := out.String()
	if got == "" {
		t.Fatal("--help produced no output")
	}
	if stripped := ansi.Strip(got); stripped != got {
		t.Fatalf("help on a pipe must carry zero ANSI escapes:\n%q", got)
	}
	if !strings.Contains(got, "cube-idp") {
		t.Fatalf("help output missing the program name:\n%q", got)
	}
}
