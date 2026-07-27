// Package cubeerr is the error machinery for user-facing cube-idp errors.
// It defines the Coded error shape and exit-code mapping ONLY. Error code
// catalogs live in the domain packages that own them (e.g. internal/config
// owns CUBE-CFG-*) — this package must never grow a code table.
package cubeerr

import (
	"errors"
	"fmt"
	"strings"
)

// Code is a diagnostic code of the form CUBE-<TAG>-NNN, where <TAG>
// names the owning component (CFG, CLI, CLU, ...).
type Code string

// Process exit codes.
const (
	ExitSuccess = 0
	ExitError   = 1
	ExitConfig  = 2
)

// Coded is the only error shape that reaches a user: a diagnostic code,
// a one-line summary, and a remediation hint, wrapping the technical cause.
type Coded struct {
	Code        Code
	Summary     string
	Remediation string
	err         error
}

func (e *Coded) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Summary) }

func (e *Coded) Unwrap() error { return e.err }

// Wrap builds a Coded error around an optional cause.
func Wrap(code Code, summary, remediation string, cause error) *Coded {
	return &Coded{Code: code, Summary: summary, Remediation: remediation, err: cause}
}

// ExitCode maps an error chain to the process exit code.
func ExitCode(err error) int {
	var c *Coded
	switch {
	case err == nil:
		return ExitSuccess
	case errors.As(err, &c):
		if tag(c.Code) == "CFG" {
			return ExitConfig
		}
		return ExitError
	default:
		return ExitError
	}
}

// tag extracts the component tag from CUBE-<TAG>-NNN; empty if malformed.
func tag(c Code) string {
	parts := strings.Split(string(c), "-")
	if len(parts) != 3 || parts[0] != "CUBE" {
		return ""
	}
	return parts[1]
}
