package gateway

import (
	"fmt"

	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

// The gateway domain owns the CUBE-GWY-* code range. Codes are declared
// here and nowhere else; the cross-domain tag registry lives in
// docs/ARCHITECTURE.md. Constructors stay unexported: this domain has no
// subpackages, so nothing outside it raises these. 001 and 002 cover the
// embedded cube-shipped assets only — an override pack's resolution
// failures are internal/ref's CUBE-REF-*, its load, validate, and render
// failures internal/pack's CUBE-PKG-*, and codes are never re-tagged
// across domains.
const (
	// CodePackProvenance reports an embedded prerequisite payload that
	// fails its recorded sha256 provenance (the CUBE-ENG-003 analogue).
	CodePackProvenance cubeerr.Code = "CUBE-GWY-001"
	// CodePackParse reports an embedded prerequisite payload that fails
	// to parse into Kubernetes objects.
	CodePackParse cubeerr.Code = "CUBE-GWY-002"
	// CodeUnrecognizedObject reports an object handed to the readiness
	// predicate outside this domain's declared coverage (the
	// CUBE-ENG-002 analogue).
	CodeUnrecognizedObject cubeerr.Code = "CUBE-GWY-003"
	// CodeCorefileStructure reports a live Corefile that does not
	// contain the structure the splice requires — unparseable, or this
	// cube's markers corrupted.
	CodeCorefileStructure cubeerr.Code = "CUBE-GWY-004"
)

// newPackProvenanceError reports an embedded prerequisite payload that
// fails its recorded provenance: content that does not match the sha256
// pin, or a payload the build did not carry.
func newPackProvenanceError(detail string, cause error) error {
	return cubeerr.Wrap(CodePackProvenance,
		fmt.Sprintf("embedded Gateway API manifests failed provenance: %s", detail),
		"rebuild from a clean checkout; if you regenerated the payload, run `make gateway-api-manifests` and update crdsSHA256",
		cause)
}

// newPackParseError reports an embedded prerequisite payload that fails
// to parse into Kubernetes objects.
func newPackParseError(cause error) error {
	return cubeerr.Wrap(CodePackParse,
		"cannot parse the embedded Gateway API manifests",
		"this is a build-integrity problem; rebuild from a clean checkout",
		cause)
}

// newUnrecognizedObjectError reports an object handed to Reconciled that
// this domain does not cover: Reconciled judges only the CRs the
// thin-helm prerequisite unit declares.
func newUnrecognizedObjectError(apiVersion, kind string) error {
	return cubeerr.Wrap(CodeUnrecognizedObject,
		fmt.Sprintf("gateway domain does not recognize object %s %s", apiVersion, kind),
		"hand Reconciled only the objects HelmPairObjects emits (HelmRelease and OCIRepository)", nil)
}

// newCorefileStructureError reports a Corefile the splice cannot act on,
// naming the specific structural fault: the diagnostic is the whole
// value of the code here, since the edge surfaces it as a bootstrap
// failure.
func newCorefileStructureError(detail string) error {
	return cubeerr.Wrap(CodeCorefileStructure,
		fmt.Sprintf("live Corefile does not contain the structure the cube-idp splice requires: %s", detail),
		"inspect the kube-system/coredns ConfigMap: the splice needs exactly one `.:53 {` server block, and at most one intact `# cube-idp:begin`/`# cube-idp:end` pair for this cube",
		nil)
}
