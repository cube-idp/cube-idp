// Package flux is the flux engine driver — the degenerate tier-2 case,
// where the invariant substrate doubles as the engine. It contributes
// sync wiring and readiness judgment only: SourceObjects emits the
// GitRepository|OCIRepository + Kustomization pair (moved verbatim from
// bootstrap at M10-C3), EngineObjects is empty (no second install
// occurs), Reconciled rejects stale success in Flux's own freshness
// vocabulary, and EngineNamespace is the substrate namespace. It runs
// the shared conformance suite from its own test package. Contract:
// docs/domains/engine.md.
package flux

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
	"github.com/cube-idp/cube-idp/internal/engine"
	"github.com/cube-idp/cube-idp/internal/engine/substrate"
)

// Compile-time seam assertion: Flux is a Provider. No SpecValidator
// assertion — flux deliberately does not implement the capability (the
// engine spec has no driver-private surface; api/config validation is
// the whole contract).
var _ engine.Provider = (*Flux)(nil)

const (
	// wiringName is the shared name of the engine's source and
	// Kustomization CRs (Flux's own convention for the bootstrap sync).
	wiringName = "flux-system"

	sourceAPIVersion    = "source.toolkit.fluxcd.io/v1"
	kustomizeAPIVersion = "kustomize.toolkit.fluxcd.io/v1"
)

// Flux is the flux driver. The zero value is ready to use; New exists
// so the CLI edge constructs drivers uniformly.
type Flux struct{}

// New returns a flux driver.
func New() *Flux { return &Flux{} }

// SourceObjects returns the engine's sync wiring derived from
// spec.engine.source — the GitRepository|OCIRepository + Kustomization
// pair the substrate's controllers reconcile — or nil when no source is
// configured (a nil source, or the defensive empty-URL case config
// validation already rejects).
func (f *Flux) SourceObjects(_ context.Context, spec v1alpha1.EngineSpec) ([]*unstructured.Unstructured, error) {
	src := spec.Source
	if src == nil || src.URL == "" {
		return nil, nil
	}
	source, err := sourceCR(src)
	if err != nil {
		return nil, err
	}
	return []*unstructured.Unstructured{source, kustomizationCR(src)}, nil
}

// EngineObjects returns the engine's own install bundle: empty for flux
// — the substrate already is the engine and no second install occurs.
func (f *Flux) EngineObjects(_ context.Context, _ v1alpha1.EngineSpec) ([]*unstructured.Unstructured, error) {
	return nil, nil
}

// EngineNamespace names where tier-2 engine content lives: for flux,
// the substrate namespace — degenerate, like everything else about this
// driver.
func (f *Flux) EngineNamespace() string { return substrate.Namespace }

// sourceCR builds the Flux source CR (GitRepository or OCIRepository)
// from the configured engine source.
func sourceCR(src *v1alpha1.EngineSource) (*unstructured.Unstructured, error) {
	o := &unstructured.Unstructured{}
	o.SetAPIVersion(sourceAPIVersion)
	o.SetNamespace(substrate.Namespace)
	o.SetName(wiringName)
	spec := map[string]any{"interval": src.Interval, "url": src.URL}
	switch src.Kind {
	case v1alpha1.EngineSourceGit:
		o.SetKind("GitRepository")
		spec["ref"] = map[string]any{"branch": src.Ref}
	case v1alpha1.EngineSourceOCI:
		o.SetKind("OCIRepository")
		spec["ref"] = map[string]any{"tag": src.Ref}
		spec["provider"] = "generic"
	default:
		return nil, engine.NewUnsupportedSourceKindError(string(src.Kind))
	}
	_ = unstructured.SetNestedField(o.Object, spec, "spec")
	return o, nil
}

// kustomizationCR builds the Kustomization that applies the source's
// Path on Interval, pruning what disappears from the source.
func kustomizationCR(src *v1alpha1.EngineSource) *unstructured.Unstructured {
	o := &unstructured.Unstructured{}
	o.SetAPIVersion(kustomizeAPIVersion)
	o.SetKind("Kustomization")
	o.SetNamespace(substrate.Namespace)
	o.SetName(wiringName)
	_ = unstructured.SetNestedField(o.Object, map[string]any{
		"interval": src.Interval,
		"path":     src.Path,
		"prune":    true,
		"sourceRef": map[string]any{
			"kind": sourceCRKind(src.Kind),
			"name": wiringName,
		},
	}, "spec")
	return o
}

// sourceCRKind maps an engine source kind to its Flux source CR kind.
func sourceCRKind(k v1alpha1.EngineSourceKind) string {
	if k == v1alpha1.EngineSourceOCI {
		return "OCIRepository"
	}
	return "GitRepository"
}
