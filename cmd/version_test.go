package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersionCommand(t *testing.T) {
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"version"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out.String(), "cube-idp version dev") {
		t.Fatalf("unexpected output: %q", out.String())
	}
}

// TestVersionPrintsCommitAndDate pins the ldflags-stamped version surface
// (ADR-0017): the un-stamped defaults render exactly as below, and the
// leading "cube-idp version dev" prefix survives.
func TestVersionPrintsCommitAndDate(t *testing.T) {
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"version"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "cube-idp version dev (commit none, built unknown)\n" {
		t.Fatalf("version output: %q", got)
	}
}
