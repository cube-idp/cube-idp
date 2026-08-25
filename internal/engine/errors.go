package engine

import (
	"fmt"

	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

// The engine domain owns the CUBE-ENG-* code range. Codes are declared
// here and nowhere else; the cross-domain tag registry lives in
// docs/ARCHITECTURE.md. Constructors are exported because the substrate
// and driver subpackages and the CLI edge raise these errors. Codes
// superseding the moving CUBE-BST-* checks land with the content that
// raises them: 003–005 arrived with the substrate; the successor of
// CUBE-BST-007 lands with the flux driver.
const (
	CodeUnsupportedProvider cubeerr.Code = "CUBE-ENG-001"
	CodeUnrecognizedObject  cubeerr.Code = "CUBE-ENG-002"
	// CodeSubstrateProvenance supersedes CUBE-BST-001 (M10): the embedded
	// substrate payload no longer matches its recorded sha256 provenance.
	CodeSubstrateProvenance cubeerr.Code = "CUBE-ENG-003"
	// CodeSubstrateParse supersedes CUBE-BST-002 (M10): the embedded
	// substrate payload fails to parse into Kubernetes objects.
	CodeSubstrateParse cubeerr.Code = "CUBE-ENG-004"
	// CodeVersionMismatch supersedes CUBE-BST-008 (M10): a requested
	// engine version differs from the pinned substrate.
	CodeVersionMismatch cubeerr.Code = "CUBE-ENG-005"
	// CodeUnsupportedSourceKind supersedes CUBE-BST-007 (M10): an engine
	// source kind the driver cannot turn into sync wiring.
	CodeUnsupportedSourceKind cubeerr.Code = "CUBE-ENG-006"
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

// NewSubstrateProvenanceError reports an embedded substrate payload that
// fails its recorded provenance: content that does not match the sha256
// pin, or a payload the build did not carry.
func NewSubstrateProvenanceError(detail string, cause error) error {
	return cubeerr.Wrap(CodeSubstrateProvenance,
		fmt.Sprintf("embedded substrate manifests failed provenance: %s", detail),
		"rebuild from a clean checkout; if you regenerated the payload, run `make flux-manifests` and update manifestsSHA256",
		cause)
}

// NewSubstrateParseError reports an embedded substrate payload that
// fails to parse into Kubernetes objects.
func NewSubstrateParseError(cause error) error {
	return cubeerr.Wrap(CodeSubstrateParse,
		"cannot parse the embedded substrate manifests",
		"this is a build-integrity problem; rebuild from a clean checkout",
		cause)
}

// NewUnsupportedSourceKindError reports a spec.engine.source.kind the
// driver cannot turn into sync wiring (guarded upstream by config
// validation; this is the driver's defensive check).
func NewUnsupportedSourceKindError(kind string) error {
	return cubeerr.Wrap(CodeUnsupportedSourceKind,
		fmt.Sprintf("unsupported engine source kind %q", kind),
		"set spec.engine.source.kind to git or oci", nil)
}

// NewVersionMismatchError reports a requested engine version that
// differs from the pinned substrate version.
func NewVersionMismatchError(requested, pinned string) error {
	return cubeerr.Wrap(CodeVersionMismatch,
		fmt.Sprintf("engine version %q does not match this binary's embedded substrate %s", requested, pinned),
		fmt.Sprintf("leave spec.engine.version empty or set it to %q (clean SemVer, no v prefix), or run a cube-idp build whose embedded substrate matches", pinned),
		nil)
}
