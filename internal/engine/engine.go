// Package engine defines the GitOpsEngine seam (spec §4.1, D2). Flux ships
// in Phase 1; Argo CD in Phase 2; both are compiled in — no plugins (D8).
// Engine types never leak above this interface: packs describe intent,
// engines translate.
package engine

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/cube-idp/cube-idp/internal/apply"
	"github.com/cube-idp/cube-idp/internal/pack"
)

// ArtifactRef identifies a pushed OCI artifact by repository and tag, e.g.
// {"packs/gitea", "0.1.0"}. Digest is the manifest digest PushRendered
// resolved for that push (Task 10) — both engines' Deliver ignore it today;
// Task 11's change-skip logic is the first consumer.
type ArtifactRef struct{ Repo, Tag, Digest string }

// SelfArtifactName is the reserved name of the engine's own rendered
// install pushed to zot when spec.engine.selfManage is on (GT16, P8): `up`
// pushes it via oci.PushRendered (so the artifact lands at
// packs/cube-engine) and DeliverSelf's objects carry this name verbatim —
// deliberately NOT the cube-idp-<pack> delivery convention, so no pack can
// ever collide with the self-source (every pack's delivery objects are
// prefixed; a pack literally named "cube-engine" would still deliver as
// cube-idp-cube-engine).
const SelfArtifactName = "cube-engine"

// ComponentHealth is the readiness of a single engine-managed component
// (e.g. a Flux Kustomization) for a cube.
type ComponentHealth struct {
	Name    string
	Ready   bool
	Message string
}

// GitSource describes a continuously-synced git delivery source. It is the
// git-flavoured counterpart of ArtifactRef: DeliverGit turns it into
// engine-native objects (flux GitRepository + Kustomization; argocd
// Application with a git source). Branch and Path have engine-applied
// defaults ("main" and "./") when left empty.
type GitSource struct {
	URL    string // in-cluster clone URL, e.g. http://gitea-http.gitea.svc.cluster.local:3000/<owner>/<repo>.git
	Branch string // default "main"
	Path   string // default "./"
}

// Engine is the seam between packs (intent) and a concrete GitOps
// controller (delivery). Implementations install their own controllers,
// translate a rendered pack + pushed artifact into engine-native objects,
// report component health, and clean up on uninstall.
type Engine interface {
	Install(ctx context.Context, a *apply.Applier, timeout time.Duration) error
	// InstallManifests returns the objects Install applies, so the caller
	// (the `up` orchestrator) can record them in the inventory without
	// importing an engine implementation package directly — `down` needs to
	// remove the engine's own controllers too.
	InstallManifests() ([]*unstructured.Unstructured, error)
	// Deliver RETURNS engine-native objects; the caller applies them via the
	// Applier (keeps Deliver pure/testable and one apply path).
	Deliver(ctx context.Context, r *pack.Rendered, src ArtifactRef) ([]*unstructured.Unstructured, error)
	// DeliverGit registers a continuously-synced git source with the engine
	// (flux: GitRepository + Kustomization; argocd: Application with a git
	// source). Same purity rule as Deliver: it RETURNS objects, the caller
	// applies them — DeliverGit never touches the cluster. name matches the
	// name Deliver/Poke use; delivered objects are named cube-idp-<name>.
	// dependsOn is the pack's resolved dependency list (p6 DEP3), translated
	// the same way Deliver's Rendered.DependsOn is — see OrdersDeliveries.
	DeliverGit(ctx context.Context, name string, src GitSource, dependsOn []string) ([]*unstructured.Unstructured, error)
	// DeliverSelf returns the engine-native self-source objects watching
	// the cube-engine artifact (GT16, P8) in the ENGINE's own namespace
	// with pruning disabled: flux → OCIRepository + Kustomization
	// (prune: false) in flux-system; argocd → one Application over ns
	// argocd (automated sync, prune: false, no resources-finalizer). Same
	// purity rule as Deliver: it RETURNS objects, the caller applies them.
	// The returned SOURCE object carries a fresh reconcile-now annotation
	// (flux requestedAt / argocd refresh), so every apply doubles as the
	// GT16 "poke" — Poke(name) addresses cube-idp-<pack> names and cannot
	// reach the plain cube-engine self-source (see SelfArtifactName).
	DeliverSelf(ctx context.Context, src ArtifactRef) ([]*unstructured.Unstructured, error)
	// Poke asks the engine to reconcile the delivered pack now instead of on
	// its poll interval. packName matches the name Deliver/DeliverGit was
	// called with. Implementations must be idempotent and cheap (an
	// annotation patch — no apply, no wait). Poke works for BOTH delivery
	// shapes: flux pokes the OCIRepository or GitRepository named
	// cube-idp-<pack> (whichever exists), argocd always pokes the Application.
	// A pack with no delivery source to poke is CUBE-3007.
	Poke(ctx context.Context, a *apply.Applier, packName string) error
	Health(ctx context.Context, a *apply.Applier) ([]ComponentHealth, error)
	Uninstall(ctx context.Context, a *apply.Applier, timeout time.Duration) error
	// OrdersDeliveries reports whether this engine natively orders delivery
	// reconciliation by a pack's DependsOn (flux: true — Kustomization
	// spec.dependsOn). false (argocd: no cross-Application ordering) tells
	// `up` it must run the wave gate (waitDepsHealthy) itself before
	// delivering a dependent pack, instead of trusting the engine to
	// sequence reconciliation in-cluster. Every implementation must answer
	// this consciously — see internal/engine/contract's per-impl assertion.
	OrdersDeliveries() bool
}
