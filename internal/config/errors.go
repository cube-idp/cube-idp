package config

import (
	"fmt"

	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

// The config domain owns the CUBE-CFG-* code range. Codes are declared
// here and nowhere else; the cross-domain tag registry lives in
// docs/ARCHITECTURE.md.
const (
	CodeUnsupportedAPIVersion cubeerr.Code = "CUBE-CFG-001"
	CodeUnknownField          cubeerr.Code = "CUBE-CFG-002"
	CodeInvalidConfig         cubeerr.Code = "CUBE-CFG-003"
	CodeUnreadableConfig      cubeerr.Code = "CUBE-CFG-004"
	CodeNameConflict          cubeerr.Code = "CUBE-CFG-005"
	CodeAlreadyExists         cubeerr.Code = "CUBE-CFG-006"
	CodeScaffoldFailed        cubeerr.Code = "CUBE-CFG-007"
)

func errUnsupportedAPIVersion(gvk string) error {
	return cubeerr.Wrap(CodeUnsupportedAPIVersion,
		fmt.Sprintf("unsupported apiVersion/kind %q", gvk),
		"set apiVersion: cube-idp.dev/v1alpha1 and kind: Config", nil)
}

func errUnknownField(cause error) error {
	return cubeerr.Wrap(CodeUnknownField,
		"config contains unknown fields",
		"remove the unknown fields listed above, or check for typos against `cube-idp config show`", cause)
}

func errInvalidConfig(cause error) error {
	return cubeerr.Wrap(CodeInvalidConfig,
		"invalid config",
		"fix the fields above and re-run `cube-idp config validate`", cause)
}

// ErrNameConflict is exported because the CLI edge raises it: it detects
// the --name-vs-document mismatch when composing scaffold-if-absent.
// Mismatch only — a flag name equal to the document's metadata.name is a
// no-op for the caller, not an error.
func ErrNameConflict(path, documentName, flagName string) error {
	return cubeerr.Wrap(CodeNameConflict,
		fmt.Sprintf("--name %q conflicts with %s (metadata.name %q)", flagName, path, documentName),
		fmt.Sprintf("edit metadata.name in %s instead — flags never mutate an existing config", path), nil)
}

func errUnreadableConfig(cause error) error {
	return cubeerr.Wrap(CodeUnreadableConfig,
		"cannot read config file",
		"check that the path exists and is readable, or point -f/--config at the right file", cause)
}

// New-convention constructor names (New<Thing>Error / new<thing>Error,
// rules audit 2026-08-02); the older err*/Err* constructors above are
// renamed in a separate mechanical PR.
func newAlreadyExistsError(path string) error {
	return cubeerr.Wrap(CodeAlreadyExists,
		fmt.Sprintf("config already exists at %s", path),
		"cube-idp never overwrites an existing config; delete it or pass a different -f", nil)
}

func newScaffoldFailedError(path string, cause error) error {
	return cubeerr.Wrap(CodeScaffoldFailed,
		fmt.Sprintf("cannot scaffold config file %s", path),
		"check that the target directory exists and is writable", cause)
}
