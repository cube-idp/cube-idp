package flux

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/rafpe/cube-idp/internal/engine"
	"github.com/rafpe/cube-idp/internal/pack"
	"github.com/rafpe/cube-idp/internal/registry"
)

const fluxNS = "flux-system"

// Deliver translates a rendered pack + already-pushed OCI artifact into the
// Flux objects that pull and apply it: one OCIRepository (source) and one
// Kustomization (apply), both named cube-idp-<pack>. The caller applies the
// returned objects via the Applier — Deliver itself never touches the
// cluster.
func (f *Flux) Deliver(ctx context.Context, r *pack.Rendered, src engine.ArtifactRef) ([]*unstructured.Unstructured, error) {
	name := "cube-idp-" + r.Name
	repo := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "source.toolkit.fluxcd.io/v1",
		"kind":       "OCIRepository",
		"metadata":   map[string]any{"name": name, "namespace": fluxNS},
		"spec": map[string]any{
			"interval": "1m",
			"url":      fmt.Sprintf("oci://%s/%s", registry.InClusterURL, src.Repo),
			"ref":      map[string]any{"tag": src.Tag},
			"insecure": true, // zot is plain HTTP inside the cluster
		},
	}}
	kust := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "kustomize.toolkit.fluxcd.io/v1",
		"kind":       "Kustomization",
		"metadata":   map[string]any{"name": name, "namespace": fluxNS},
		"spec": map[string]any{
			"interval": "10m",
			"prune":    true,
			"wait":     true,
			"timeout":  "5m",
			"path":     "./",
			"sourceRef": map[string]any{
				"kind": "OCIRepository",
				"name": name,
			},
		},
	}}
	return []*unstructured.Unstructured{repo, kust}, nil
}
