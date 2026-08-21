package ref

import (
	"fmt"

	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

// The ref leaf owns the CUBE-REF-* code range. Codes are declared here and
// nowhere else; the cross-package tag registry lives in
// docs/ARCHITECTURE.md §5. Consumers assert identity with errors.As into
// *cubeerr.Coded plus code equality, so the codes are exported while the
// constructors stay unexported: no other package raises REF errors.
const (
	// CodeMalformedRef reports a reference that does not match the grammar.
	CodeMalformedRef cubeerr.Code = "CUBE-REF-001"
	// CodeUnsupportedScheme reports a scheme outside the grammar's table.
	CodeUnsupportedScheme cubeerr.Code = "CUBE-REF-002"
	// CodeFetchFailed reports a backend that could not deliver the bytes.
	CodeFetchFailed cubeerr.Code = "CUBE-REF-003"
	// CodeEscapesRoot reports content reachable only by leaving the
	// resolved root — path traversal or a symlink pointing outside it.
	CodeEscapesRoot cubeerr.Code = "CUBE-REF-004"
	// CodePinMismatch reports resolved content whose pin does not match
	// the pin it was checked against.
	CodePinMismatch cubeerr.Code = "CUBE-REF-005"
	// CodeModeMismatch reports a reference resolved in the wrong mode: a
	// tree where a single file was expected, or the reverse.
	CodeModeMismatch cubeerr.Code = "CUBE-REF-006"
	// CodeGitNotImplemented reports the git backend as absent from this build.
	CodeGitNotImplemented cubeerr.Code = "CUBE-REF-007"
	// CodeOCINotImplemented reports the oci backend as absent from this build.
	CodeOCINotImplemented cubeerr.Code = "CUBE-REF-008"
	// CodeS3NotImplemented reports the s3 backend as absent from this build.
	CodeS3NotImplemented cubeerr.Code = "CUBE-REF-009"
)

// grammarHint is the one place the supported forms are spelled out for a
// user; every grammar-level remediation quotes it so the list cannot drift
// between two error messages.
const grammarHint = "use ./path, ../path, file:///abs/path, https://host/path, " +
	"git+https://host/org/repo.git?ref=<rev>&path=<sub>, oci://host/repo:tag, or s3://bucket/key"

func newMalformedRefError(ref, reason string) error {
	return cubeerr.Wrap(CodeMalformedRef,
		fmt.Sprintf("malformed reference %q: %s", ref, reason),
		grammarHint, nil)
}

func newUnsupportedSchemeError(ref, scheme string) error {
	return cubeerr.Wrap(CodeUnsupportedScheme,
		fmt.Sprintf("unsupported scheme %q in reference %q", scheme, ref),
		grammarHint, nil)
}

func newFetchFailedError(ref string, cause error) error {
	return cubeerr.Wrap(CodeFetchFailed,
		fmt.Sprintf("cannot fetch reference %q", ref),
		"check the reference points at readable content and that the network path to it is up", cause)
}

func newEscapesRootError(ref, entry string) error {
	return cubeerr.Wrap(CodeEscapesRoot,
		fmt.Sprintf("reference %q escapes its root at %q", ref, entry),
		"keep the content self-contained: remove the entry or replace the symlink with one that stays inside the root", nil)
}

func newPinMismatchError(ref, want, got string) error {
	return cubeerr.Wrap(CodePinMismatch,
		fmt.Sprintf("reference %q resolved to %s, want %s", ref, got, want),
		"the content behind this reference changed; re-pin it deliberately or restore the pinned content", nil)
}

func newModeMismatchError(ref string, want, got Mode) error {
	return cubeerr.Wrap(CodeModeMismatch,
		fmt.Sprintf("reference %q resolves to a %s, want a %s", ref, got, want),
		fmt.Sprintf("point this field at a %s, or resolve the reference as a %s", want, got), nil)
}

// newNotImplementedError builds the not-implemented error for one deferred
// backend. Each backend keeps its own code so the error names which one is
// missing and where it lands.
func newNotImplementedError(code cubeerr.Code, backend, ref, lands string) error {
	return cubeerr.Wrap(code,
		fmt.Sprintf("the %s backend is not implemented in this build, so %q cannot be resolved", backend, ref),
		fmt.Sprintf("%s resolution lands in %s; use a local path or an https reference until then", backend, lands),
		nil)
}
