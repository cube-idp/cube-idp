package bootstrap

import (
	"errors"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

// The bootstrap domain owns the CUBE-BST-* code range. Codes are declared
// here and nowhere else; the cross-domain tag registry lives in
// docs/ARCHITECTURE.md. Constructors stay unexported — no other package
// raises BST errors. The catalog grows with the domain: later M7 tasks
// (source/sync, inventory) add their codes beside the constructors that
// raise them.
const (
	// CodeAssetIntegrity reports embedded Flux manifests whose content no
	// longer matches the recorded sha256 provenance (a build-time drift).
	CodeAssetIntegrity cubeerr.Code = "CUBE-BST-001"
	// CodeManifestParse reports embedded Flux manifests that fail to parse
	// into Kubernetes objects.
	CodeManifestParse cubeerr.Code = "CUBE-BST-002"
	// CodeRESTMapping reports an object whose kind the cluster does not map
	// to a resource (e.g. a CR applied before its CRD is established).
	CodeRESTMapping cubeerr.Code = "CUBE-BST-003"
	// CodeApplyFailed reports a server-side apply (or readiness read) that
	// the API server rejected.
	CodeApplyFailed cubeerr.Code = "CUBE-BST-004"
	// CodeWaitTimeout reports bootstrap resources that did not reach the
	// ready kind-set before the context was done.
	CodeWaitTimeout cubeerr.Code = "CUBE-BST-005"
	// CodeInventory reports a failure to encode the bootstrap inventory (the
	// seed of down) before recording it.
	CodeInventory cubeerr.Code = "CUBE-BST-006"
	// CodeSourceKind reports an engine source kind the bootstrapper cannot turn
	// into a Flux source CR (guarded upstream by config validation).
	CodeSourceKind cubeerr.Code = "CUBE-BST-007"
	// CodeVersionMismatch reports a requested engine version that differs from
	// this binary's embedded Flux distribution.
	CodeVersionMismatch cubeerr.Code = "CUBE-BST-008"
)

func newAssetIntegrityError(got string) error {
	return cubeerr.Wrap(CodeAssetIntegrity,
		fmt.Sprintf("embedded Flux manifests failed provenance (sha256 %s does not match the pin)", got),
		"rebuild from a clean checkout; if you regenerated the asset, run `make flux-manifests` and update fluxManifestsSHA256",
		nil)
}

func newManifestParseError(cause error) error {
	return cubeerr.Wrap(CodeManifestParse,
		"cannot parse the embedded Flux install manifests",
		"this is a build-integrity problem; rebuild from a clean checkout",
		cause)
}

func newMappingError(obj *unstructured.Unstructured, cause error) error {
	return cubeerr.Wrap(CodeRESTMapping,
		fmt.Sprintf("no REST mapping for %s", describe(obj)),
		"ensure the cluster serves this API — Flux CRDs are applied and established before their custom resources",
		cause)
}

func newApplyError(obj *unstructured.Unstructured, cause error) error {
	return cubeerr.Wrap(CodeApplyFailed,
		fmt.Sprintf("server-side apply failed for %s", describe(obj)),
		"check cluster connectivity and RBAC for the cube-idp field manager",
		cause)
}

func newVersionMismatchError(requested string) error {
	return cubeerr.Wrap(CodeVersionMismatch,
		fmt.Sprintf("engine version %q does not match this binary's embedded Flux %s", requested, FluxVersion),
		"leave spec.engine.version empty to use the embedded version, or run a cube-idp build whose embedded Flux matches",
		nil)
}

func newSourceKindError(kind string) error {
	return cubeerr.Wrap(CodeSourceKind,
		fmt.Sprintf("unsupported engine source kind %q", kind),
		"set spec.engine.source.kind to git or oci",
		nil)
}

func newInventoryError(cause error) error {
	return cubeerr.Wrap(CodeInventory,
		"cannot encode the bootstrap inventory",
		"this is an internal error; please report it",
		cause)
}

// newWaitError wraps a readiness-wait failure in CUBE-BST-005 — unless the
// cause already carries a cubeerr code (e.g. a CUBE-BST-003 mapping miss
// surfacing mid-wait), which passes through unchanged: one failure, one code,
// never retagged.
func newWaitError(pending []*unstructured.Unstructured, cause error) error {
	var coded *cubeerr.Coded
	if errors.As(cause, &coded) {
		return cause
	}
	return cubeerr.Wrap(CodeWaitTimeout,
		fmt.Sprintf("bootstrap resources did not become ready: %s", describeAll(pending)),
		"inspect the Flux controllers (`kubectl -n flux-system get pods`) and their events",
		cause)
}

// describe renders an object as "Kind namespace/name" (or "Kind name" for
// cluster-scoped objects) for error messages.
func describe(o *unstructured.Unstructured) string {
	if ns := o.GetNamespace(); ns != "" {
		return fmt.Sprintf("%s %s/%s", o.GetKind(), ns, o.GetName())
	}
	return fmt.Sprintf("%s %s", o.GetKind(), o.GetName())
}

// describeAll renders a list of objects as a comma-separated description.
func describeAll(objs []*unstructured.Unstructured) string {
	parts := make([]string, 0, len(objs))
	for _, o := range objs {
		parts = append(parts, describe(o))
	}
	return strings.Join(parts, ", ")
}
