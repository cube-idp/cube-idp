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

func errUnreadableConfig(cause error) error {
	return cubeerr.Wrap(CodeUnreadableConfig,
		"cannot read config file",
		"check that the path exists and is readable, or point -f/--config at the right file", cause)
}
