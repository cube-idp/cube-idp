// Package engine defines the GitOpsEngine seam (spec §4.1, D2). Flux ships
// in Phase 1; Argo CD in Phase 2; both are compiled in — no plugins (D8).
// Engine types never leak above this interface: packs describe intent,
// engines translate.
package engine

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/rafpe/cube-idp/internal/apply"
	"github.com/rafpe/cube-idp/internal/pack"
)

// ArtifactRef identifies a pushed OCI artifact by repository and tag, e.g.
// {"packs/gitea", "0.1.0"}.
type ArtifactRef struct{ Repo, Tag string }

// ComponentHealth is the readiness of a single engine-managed component
// (e.g. a Flux Kustomization) for a cube.
type ComponentHealth struct {
	Name    string
	Ready   bool
	Message string
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
	Health(ctx context.Context, a *apply.Applier) ([]ComponentHealth, error)
	Uninstall(ctx context.Context, a *apply.Applier, timeout time.Duration) error
}
