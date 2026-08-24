package engine

import (
	"fmt"

	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

// The engine domain owns the CUBE-ENG-* code range. Codes are declared
// here and nowhere else; the cross-domain tag registry lives in
// docs/ARCHITECTURE.md. Constructors are exported because driver
// subpackages and the CLI edge raise these errors. Codes superseding the
// moving CUBE-BST-* checks land with the content that raises them (the
// substrate home and the flux driver).
const (
	CodeUnsupportedProvider cubeerr.Code = "CUBE-ENG-001"
	CodeUnrecognizedObject  cubeerr.Code = "CUBE-ENG-002"
)

// NewUnsupportedProviderError reports a spec.engine.provider no
// registered driver implements.
func NewUnsupportedProviderError(provider string) error {
	return cubeerr.Wrap(CodeUnsupportedProvider,
		fmt.Sprintf("no driver for engine provider %q", provider),
		"use a supported spec.engine.provider (flux)", nil)
}

// NewUnrecognizedObjectError reports an object handed to Reconciled that
// the driver does not cover: Reconciled judges only the driver's own
// declared objects — sync wiring and engine bundle.
func NewUnrecognizedObjectError(apiVersion, kind string) error {
	return cubeerr.Wrap(CodeUnrecognizedObject,
		fmt.Sprintf("engine driver does not recognize object %s %s", apiVersion, kind),
		"hand Reconciled only objects the driver declared via SourceObjects or EngineObjects", nil)
}
