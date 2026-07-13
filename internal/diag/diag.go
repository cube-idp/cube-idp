// Package diag defines cube-idp's typed error model. Every user-facing
// failure carries a CUBE-xxxx code and a copy-pasteable remediation
// (spec §4.5). Code ranges: 0xxx preflight/config, 1xxx cluster,
// 2xxx apply, 3xxx engine, 4xxx pack, 5xxx registry.
package diag

import (
	"errors"
	"fmt"
	"strings"
)

type Code string

type Error struct {
	Code        Code
	Summary     string
	Cause       error
	Remediation string
}

func New(code Code, summary, remediation string) *Error {
	return &Error{Code: code, Summary: summary, Remediation: remediation}
}

func Wrap(cause error, code Code, summary, remediation string) *Error {
	return &Error{Code: code, Summary: summary, Cause: cause, Remediation: remediation}
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Summary, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Summary)
}

func (e *Error) Unwrap() error { return e.Cause }

// Render produces the terminal-facing block for any error. *Error values
// get the structured layout; anything else passes through unchanged.
func Render(err error) string {
	var de *Error
	if !errors.As(err, &de) {
		return "Error: " + err.Error()
	}
	var b strings.Builder
	fmt.Fprintf(&b, "✗ %s  %s\n", de.Code, de.Summary)
	if de.Cause != nil {
		fmt.Fprintf(&b, "  cause: %v\n", de.Cause)
	}
	if de.Remediation != "" {
		fmt.Fprintf(&b, "  fix:   %s", de.Remediation)
	}
	return strings.TrimRight(b.String(), "\n")
}

type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

// Finding is a non-fatal diagnostic surfaced by providers/engines and the
// doctor command (Phase 2 wires the command; the type ships now because
// ClusterProvider.Diagnose returns it).
type Finding struct {
	Code        Code
	Severity    Severity
	Message     string
	Remediation string
}
