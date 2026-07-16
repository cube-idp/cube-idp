package cmd

import (
	"bytes"
	"errors"
	"testing"

	"github.com/cube-idp/cube-idp/internal/diag"
	"github.com/cube-idp/cube-idp/internal/ui"
)

// TestProgressFlagRejectsUnknownValue pins the design-doc §6.4 preflight: an
// unrecognized --progress value fails with a typed CUBE-0007 error before any
// command body runs.
func TestProgressFlagRejectsUnknownValue(t *testing.T) {
	root := NewRootCmd()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"--progress", "fancy", "version"})
	err := root.Execute()
	if err == nil {
		t.Fatal("want an error for --progress=fancy, got nil")
	}
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != diag.CodeBadFlagValue {
		t.Fatalf("want CUBE-0007, got %v", err)
	}
}

// TestProgressFlagAcceptsKnownValues verifies each accepted value runs and,
// through the resolve ladder, sets the expected process-wide mode. version is
// a body-less command, so this exercises PersistentPreRunE in isolation.
func TestProgressFlagAcceptsKnownValues(t *testing.T) {
	cases := []struct {
		flag string
		want ui.Mode
	}{
		{"plain", ui.ModePlain},
		{"live", ui.ModeLive},
		{"json", ui.ModeJSON},
	}
	for _, tc := range cases {
		t.Run(tc.flag, func(t *testing.T) {
			defer ui.SetMode(ui.ModeStyled)
			root := NewRootCmd()
			root.SetOut(&bytes.Buffer{})
			root.SetArgs([]string{"--progress", tc.flag, "version"})
			if err := root.Execute(); err != nil {
				t.Fatalf("execute: %v", err)
			}
			if got := ui.CurrentMode(); got != tc.want {
				t.Fatalf("--progress=%s resolved to %v, want %v", tc.flag, got, tc.want)
			}
		})
	}
}

// TestProgressBeatsPlain documents the ladder precedence at the cmd layer:
// --progress=json wins over --plain (rung 1 beats rung 4).
func TestProgressBeatsPlain(t *testing.T) {
	defer ui.SetMode(ui.ModeStyled)
	root := NewRootCmd()
	root.SetOut(&bytes.Buffer{})
	root.SetArgs([]string{"--progress", "json", "--plain", "version"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got := ui.CurrentMode(); got != ui.ModeJSON {
		t.Fatalf("--progress=json --plain resolved to %v, want ModeJSON", got)
	}
}
