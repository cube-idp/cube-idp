package flux

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/cube-idp/cube-idp/internal/engine"
)

// DeliverGit is the git-sourced counterpart of Deliver: it translates a
// continuously-synced git source into a Flux GitRepository (source) + a
// Kustomization (apply), both named cube-idp-<name>, in flux-system. Same
// purity rule as Deliver — it RETURNS objects, the caller applies them.
// Empty Branch defaults to "main" and empty Path to "./". dependsOn is
// translated exactly as Deliver's Rendered.DependsOn is (p6 DEP3).
func (f *Flux) DeliverGit(ctx context.Context, name string, src engine.GitSource, dependsOn []string) ([]*unstructured.Unstructured, error) {
	dName := deliveryName(name)
	branch := src.Branch
	if branch == "" {
		branch = "main"
	}
	path := src.Path
	if path == "" {
		path = "./"
	}
	repo := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "source.toolkit.fluxcd.io/v1",
		"kind":       "GitRepository",
		"metadata":   map[string]any{"name": dName, "namespace": fluxNS},
		"spec": map[string]any{
			"interval": "30s",
			"url":      src.URL,
			"ref":      map[string]any{"branch": branch},
		},
	}}
	kust := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "kustomize.toolkit.fluxcd.io/v1",
		"kind":       "Kustomization",
		"metadata":   map[string]any{"name": dName, "namespace": fluxNS},
		"spec": map[string]any{
			"interval": "10m",
			"prune":    true,
			"wait":     true,
			"timeout":  "5m",
			"path":     path,
			"sourceRef": map[string]any{
				"kind": "GitRepository",
				"name": dName,
			},
		},
	}}
	if len(dependsOn) > 0 {
		kust.Object["spec"].(map[string]any)["dependsOn"] = dependsOnRefs(dependsOn)
	}
	return []*unstructured.Unstructured{repo, kust}, nil
}
