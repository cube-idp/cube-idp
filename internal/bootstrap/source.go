package bootstrap

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
)

const (
	// sourceName is the shared name of the engine's source and Kustomization CRs
	// (Flux's own convention for the bootstrap sync).
	sourceName = "flux-system"

	sourceAPIVersion    = "source.toolkit.fluxcd.io/v1"
	kustomizeAPIVersion = "kustomize.toolkit.fluxcd.io/v1"
)

// sourceObjects builds the Flux source CR (GitRepository or OCIRepository) and
// the Kustomization that applies it, from the configured engine source.
func sourceObjects(src *v1alpha1.EngineSource) ([]*unstructured.Unstructured, error) {
	source, err := sourceCR(src)
	if err != nil {
		return nil, err
	}
	return []*unstructured.Unstructured{source, kustomizationCR(src)}, nil
}

func sourceCR(src *v1alpha1.EngineSource) (*unstructured.Unstructured, error) {
	o := &unstructured.Unstructured{}
	o.SetAPIVersion(sourceAPIVersion)
	o.SetNamespace(InventoryNamespace)
	o.SetName(sourceName)
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
		return nil, newSourceKindError(string(src.Kind))
	}
	_ = unstructured.SetNestedField(o.Object, spec, "spec")
	return o, nil
}

func kustomizationCR(src *v1alpha1.EngineSource) *unstructured.Unstructured {
	o := &unstructured.Unstructured{}
	o.SetAPIVersion(kustomizeAPIVersion)
	o.SetKind("Kustomization")
	o.SetNamespace(InventoryNamespace)
	o.SetName(sourceName)
	_ = unstructured.SetNestedField(o.Object, map[string]any{
		"interval": src.Interval,
		"path":     src.Path,
		"prune":    true,
		"sourceRef": map[string]any{
			"kind": sourceCRKind(src.Kind),
			"name": sourceName,
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
