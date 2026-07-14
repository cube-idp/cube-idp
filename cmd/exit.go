package cmd

import (
	"errors"
	"os/exec"
)

// ExitCodeFor maps Execute's error to the process exit code and whether
// main.go should render it. A plugin's *exec.ExitError (spec §4.4 tier 2:
// "its exit code is propagated verbatim") returns the plugin's own code
// with render=false — the plugin already wrote its diagnosis to its
// inherited stderr, so cube-idp printing "Error: exit status N" on top
// would both pollute that output and collapse the code to 1. Every other
// error (diag.Error or plain) keeps the pre-plugin behavior: exit 1,
// rendered through diag.Render.
func ExitCodeFor(err error) (code int, render bool) {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), false
	}
	return 1, true
}
