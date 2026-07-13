package argocd

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/rafpe/cube-idp/internal/engine"
	"github.com/rafpe/cube-idp/internal/pack"
	"github.com/rafpe/cube-idp/internal/registry"
)

// Deliver translates a rendered pack + already-pushed OCI artifact into a
// single Argo CD Application whose source is the in-cluster zot registry's
// OCI repository (see manifests/repo-secret.yaml for how that repository is
// registered). The caller applies the returned object via the Applier —
// Deliver itself never touches the cluster.
func (g *ArgoCD) Deliver(ctx context.Context, r *pack.Rendered, src engine.ArtifactRef) ([]*unstructured.Unstructured, error) {
	name := "cube-idp-" + r.Name
	app := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "argoproj.io/v1alpha1",
		"kind":       "Application",
		"metadata": map[string]any{
			"name":       name,
			"namespace":  Namespace,
			"finalizers": []any{"resources-finalizer.argocd.argoproj.io"},
		},
		"spec": map[string]any{
			"project": "default",
			"source": map[string]any{
				"repoURL":        fmt.Sprintf("oci://%s/%s", registry.InClusterURL, src.Repo),
				"targetRevision": src.Tag,
				"path":           ".",
			},
			"destination": map[string]any{"server": "https://kubernetes.default.svc"},
			"syncPolicy": map[string]any{
				"automated":   map[string]any{"prune": true, "selfHeal": true},
				"syncOptions": []any{"CreateNamespace=true", "ServerSideApply=true"},
			},
		},
	}}
	return []*unstructured.Unstructured{app}, nil
}
