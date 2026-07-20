package cmd

import (
	"errors"
	"fmt"
	"os/exec"
)

// exitStatus carries a bare exit code through the normal error return path
// so deferred cleanup (main.go's signal stop, renderer teardown) always
// runs — the replacement for exiting the process directly inside RunE
// (main.go stays the binary's only exit point — a program killed mid-run
// must never leave the terminal in raw mode).
type exitStatus struct{ code int }

func (e exitStatus) Error() string { return fmt.Sprintf("exit status %d", e.code) }

func errExitCode(code int) error { return exitStatus{code: code} }

// ExitCodeFor maps Execute's error to the process exit code and whether
// main.go should render it. The exitStatus sentinel (a command that wants
// "exit N, print nothing" — diff/doctor/upgrade drift signals) returns its
// code unrendered: the command already wrote its own output. A plugin's
// *exec.ExitError (exec plugins are tier 2 of the extensibility model, and
// their exit code is propagated
// verbatim") returns the plugin's own code with render=false — the plugin
// already wrote its diagnosis to its inherited stderr, so cube-idp printing
// "Error: exit status N" on top would both pollute that output and collapse
// the code to 1. Every other error (diag.Error or plain) keeps the
// pre-plugin behavior: exit 1, rendered through diag.Render.
func ExitCodeFor(err error) (code int, render bool) {
	var es exitStatus
	if errors.As(err, &es) {
		return es.code, false
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), false
	}
	return 1, true
}
