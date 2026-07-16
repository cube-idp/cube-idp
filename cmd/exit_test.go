package cmd

import (
	"errors"
	"os/exec"
	"runtime"
	"testing"

	"github.com/cube-idp/cube-idp/internal/diag"
)

// realExitError3 produces a genuine *exec.ExitError with exit code 3 — not
// a hand-built one — so the test exercises exactly what plugin.Exec returns
// after a plugin exits non-zero. The shell command is a fixed literal (no
// variable data reaches the shell).
func realExitError3(t *testing.T) error {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("sh-based exit-code fixture is unix-only")
	}
	err := exec.Command("sh", "-c", "exit 3").Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 3 {
		t.Fatalf("fixture: want ExitError 3, got %v", err)
	}
	return err
}

// TestExitCodeForPluginExitError covers the CRITICAL review finding: a
// plugin's non-zero exit must reach the operator's shell verbatim, with NO
// diag rendering — the plugin's own output is its diagnosis. main.go's
// blanket diag.Render + os.Exit(1) collapsed every plugin exit code to 1
// and polluted the plugin's output with "Error: exit status N".
func TestExitCodeForPluginExitError(t *testing.T) {
	code, render := ExitCodeFor(realExitError3(t))
	if code != 3 || render {
		t.Fatalf("want (3, false) for a plugin ExitError, got (%d, %v)", code, render)
	}
}

func TestExitCodeForDiagError(t *testing.T) {
	err := diag.New(diag.CodePluginUntrusted, "plugin refused", "trust it")
	code, render := ExitCodeFor(err)
	if code != 1 || !render {
		t.Fatalf("want (1, true) for a diag.Error, got (%d, %v)", code, render)
	}
}

func TestExitCodeForPlainError(t *testing.T) {
	code, render := ExitCodeFor(errors.New("boom"))
	if code != 1 || !render {
		t.Fatalf("want (1, true) for a plain error, got (%d, %v)", code, render)
	}
}
