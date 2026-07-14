package argocd

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/rafpe/cube-idp/internal/engine"
	"github.com/rafpe/cube-idp/internal/pack"
	"github.com/rafpe/cube-idp/internal/registry"
)

// deliveryName is the single source of truth for the name of the Application
// a pack owns — shared by Deliver, DeliverGit, Health's label selector, and
// Poke so all agree on what "the delivery for pack X" is.
func deliveryName(pack string) string { return "cube-idp-" + pack }

// Deliver translates a rendered pack + already-pushed OCI artifact into a
// single Argo CD Application whose source is the in-cluster zot registry's
// OCI repository (see manifests/repo-secret.yaml for how that repository is
// registered). The caller applies the returned object via the Applier —
// Deliver itself never touches the cluster.
func (g *ArgoCD) Deliver(ctx context.Context, r *pack.Rendered, src engine.ArtifactRef) ([]*unstructured.Unstructured, error) {
	return []*unstructured.Unstructured{application(deliveryName(r.Name), map[string]any{
		"repoURL":        fmt.Sprintf("oci://%s/%s", registry.InClusterURL, src.Repo),
		"targetRevision": src.Tag,
		"path":           ".",
	})}, nil
}

// application builds the cube-idp Application shape — the delivery scaffolding
// shared by Deliver (OCI source) and DeliverGit (git source): everything is
// identical bar spec.source, so both pass just the source block. Keeping this
// in one place is what makes DeliverGit "copy Deliver, change only the source"
// literally true (spec §4.1, D2).
func application(name string, source map[string]any) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "argoproj.io/v1alpha1",
		"kind":       "Application",
		"metadata": map[string]any{
			"name":       name,
			"namespace":  Namespace,
			"finalizers": []any{"resources-finalizer.argocd.argoproj.io"},
		},
		"spec": map[string]any{
			"project":     "default",
			"source":      source,
			"destination": map[string]any{"server": "https://kubernetes.default.svc"},
			"syncPolicy": map[string]any{
				"automated":   map[string]any{"prune": true, "selfHeal": true},
				"syncOptions": []any{"CreateNamespace=true", "ServerSideApply=true"},
			},
		},
	}}
}
