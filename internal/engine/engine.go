// Package engine is the gitops-engine domain: the tier-2 Provider driver
// seam and its conformance suite. The seam covers the engine only — the
// tier-1 Flux substrate is invariant platform, never driver-selected,
// and not behind it. Implementations live in subpackages; driver
// selection happens at the CLI edge, never here.
package engine

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
)

// Provider is the tier-2 gitops-engine driver seam. It covers the
// ENGINE only — the tier-1 substrate is invariant platform and is not
// behind this seam. It is PURE: every method returns data or judges
// data; no method performs I/O. Applying objects and polling status
// belong to the caller (bootstrap machinery, composed at the edge).
// Implementations must satisfy RunEngineConformance.
type Provider interface {
	// SourceObjects returns the engine's sync wiring derived from
	// spec.engine.source — how the cube's declared source becomes this
	// engine's coordination loop — or nil when no source is configured.
	// flux (degenerate): the GitRepository|OCIRepository +
	// Kustomization pair; the substrate doubles as the engine, so the
	// wiring is substrate vocabulary.
	SourceObjects(ctx context.Context, spec v1alpha1.EngineSpec) ([]*unstructured.Unstructured, error)

	// EngineObjects returns the engine's own install bundle —
	// pack-shaped content delivered THROUGH tier 1 (written into the
	// source; never applied by bootstrap's SSA). flux (degenerate):
	// empty — the substrate already is the engine and no second
	// install occurs.
	EngineObjects(ctx context.Context, spec v1alpha1.EngineSpec) ([]*unstructured.Unstructured, error)

	// Reconciled judges one declared object's status: reconciled, not
	// yet (with a human-readable reason — it feeds the
	// reconciliation-wait timeout diagnostics, CUBE-BST-009), or a
	// coded error for an object the driver does not recognize. It
	// covers the driver's declared objects — sync wiring and engine
	// bundle. Pure — it reads the unstructured status it is handed, it
	// never fetches.
	Reconciled(obj *unstructured.Unstructured) (bool, string, error)

	// EngineNamespace names the namespace tier-2 engine content lives
	// in. flux (degenerate): the substrate namespace. It is distinct
	// from the substrate namespace fact, which is invariant and owns
	// inventory placement.
	EngineNamespace() string
}

// SpecValidator is implemented by drivers that can validate the
// engine spec without any cluster. Pure: no I/O, no side effects.
type SpecValidator interface {
	ValidateSpec(spec v1alpha1.EngineSpec) error
}
