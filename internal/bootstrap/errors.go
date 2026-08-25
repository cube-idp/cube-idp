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
	// CodeAssetIntegrity is SUPERSEDED by CUBE-ENG-003 (M10): the embedded
	// substrate asset and its sha256 provenance check moved to
	// internal/engine/substrate. The constant stays so the number is never
	// reused.
	CodeAssetIntegrity cubeerr.Code = "CUBE-BST-001"
	// CodeManifestParse is SUPERSEDED by CUBE-ENG-004 (M10): parsing the
	// embedded substrate payload moved to internal/engine/substrate. The
	// constant stays so the number is never reused.
	CodeManifestParse cubeerr.Code = "CUBE-BST-002"
	// CodeRESTMapping reports an object whose kind the cluster does not map
	// to a resource (e.g. a CR applied before its CRD is established).
	CodeRESTMapping cubeerr.Code = "CUBE-BST-003"
	// CodeApplyFailed reports a server-side apply that the API server
	// rejected.
	CodeApplyFailed cubeerr.Code = "CUBE-BST-004"
	// CodeWaitTimeout reports bootstrap resources that did not reach the
	// ready kind-set before the context was done. It covers the kind-set
	// wait only; the reconciliation waits time out as CUBE-BST-009.
	CodeWaitTimeout cubeerr.Code = "CUBE-BST-005"
	// CodeInventory reports a failure to encode the bootstrap inventory (the
	// seed of down) before recording it.
	CodeInventory cubeerr.Code = "CUBE-BST-006"
	// CodeSourceKind reports an engine source kind the bootstrapper cannot turn
	// into a Flux source CR (guarded upstream by config validation).
	CodeSourceKind cubeerr.Code = "CUBE-BST-007"
	// CodeVersionMismatch is SUPERSEDED by CUBE-ENG-005 (M10): the version
	// pin moved to internal/engine/substrate, which asserts requested
	// versions at the edge. The constant stays so the number is never
	// reused.
	CodeVersionMismatch cubeerr.Code = "CUBE-BST-008"
	// CodeReconcileTimeout reports polled objects — applied sync wiring or
	// declared engine content — that the injected judgment did not report
	// reconciled before the context was done. It names the pending objects
	// with the judgment's reasons.
	CodeReconcileTimeout cubeerr.Code = "CUBE-BST-009"
	// CodePollFailed reports a permanent failure while polling an object for
	// reconciliation — a read the API server rejected for a reason waiting
	// cannot fix, or a judgment failure carrying no code of its own. It is
	// coded at the failure point and never retagged as a timeout.
	CodePollFailed cubeerr.Code = "CUBE-BST-010"
)

func newMappingError(obj *unstructured.Unstructured, cause error) error {
	return cubeerr.Wrap(CodeRESTMapping,
		fmt.Sprintf("no REST mapping for %s", describe(obj)),
		"ensure the cluster serves this API — CRDs must be applied and established before their custom resources",
		cause)
}

func newApplyError(obj *unstructured.Unstructured, cause error) error {
	return cubeerr.Wrap(CodeApplyFailed,
		fmt.Sprintf("server-side apply failed for %s", describe(obj)),
		"check cluster connectivity and RBAC for the cube-idp field manager",
		cause)
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

// newWaitError wraps a kind-set readiness-wait failure in CUBE-BST-005 —
// unless the cause already carries a cubeerr code (e.g. a CUBE-BST-003
// mapping miss surfacing mid-wait), which passes through unchanged: one
// failure, one code, never retagged. namespace is the injected install
// namespace, named so the remediation points somewhere real without this
// domain knowing the engine.
func newWaitError(namespace string, pending []*unstructured.Unstructured, cause error) error {
	var coded *cubeerr.Coded
	if errors.As(cause, &coded) {
		return cause
	}
	return cubeerr.Wrap(CodeWaitTimeout,
		fmt.Sprintf("bootstrap resources did not become ready: %s", describeAll(pending)),
		fmt.Sprintf("inspect the engine controllers (`kubectl -n %s get pods`) and their events", namespace),
		cause)
}

// newReconcileWaitError wraps a reconciliation-wait failure in CUBE-BST-009,
// naming the pending objects with the judgment's reasons — unless the cause
// already carries a cubeerr code (a permanent CUBE-BST-010 poll failure or
// the judgment's own coded error surfacing mid-wait), which passes through
// unchanged: one failure, one code, never retagged.
func newReconcileWaitError(pending []pendingObject, cause error) error {
	var coded *cubeerr.Coded
	if errors.As(cause, &coded) {
		return cause
	}
	return cubeerr.Wrap(CodeReconcileTimeout,
		fmt.Sprintf("engine resources did not reconcile: %s", describePending(pending)),
		"inspect the named objects and their controllers' logs; check that the configured source is reachable",
		cause)
}

// newPollError wraps a permanent reconciliation-polling failure in
// CUBE-BST-010 — coded at the failure point, never retagged as a timeout.
func newPollError(obj *unstructured.Unstructured, cause error) error {
	return cubeerr.Wrap(CodePollFailed,
		fmt.Sprintf("polling %s for reconciliation failed", describe(obj)),
		"check cluster connectivity and RBAC for reading the named object",
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

// describePending renders pending objects with their reasons, e.g.
// "GitRepository flux-system/flux-system (artifact not ready)".
func describePending(pending []pendingObject) string {
	parts := make([]string, 0, len(pending))
	for _, p := range pending {
		parts = append(parts, fmt.Sprintf("%s (%s)", describe(p.obj), p.reason))
	}
	return strings.Join(parts, ", ")
}
