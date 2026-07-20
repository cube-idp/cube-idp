package argocd

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/cube-idp/cube-idp/internal/engine"
	"github.com/cube-idp/cube-idp/internal/registry"
)

// DeliverSelf translates the pushed cube-engine artifact — the opt-in
// engine self-management path, spec.engine.selfManage (ADR 0020) — into
// the single Application through which Argo CD manages ITSELF: name
// cube-engine, ns argocd, destination its own namespace. The source shape
// mirrors Deliver exactly (same zot repoURL derivation, targetRevision,
// path — the repo-creds secret from manifests/repo-secret.yaml
// prefix-matches this URL too, and the artifact is pushed by the same
// oci.PushRendered, so the media-type allow-list already covers it).
// Differences from the pack application() shape, each load-bearing:
//
//   - automated sync with prune: false — the engine must never prune its
//     own controllers; selfHeal stays true so live drift between `up`s is
//     corrected — self-management requires drift between `up`s to be
//     corrected by the engine, not left to the next CLI run.
//   - NO resources-finalizer: on `down` the inventory-driven DeleteAll
//     removes this Application and then the engine itself — a cascading
//     finalizer would tear the engine down from inside instead.
//   - the argocd.argoproj.io/refresh: normal annotation, so each `up`
//     apply doubles as a reconcile-now poke: push → apply → the controller
//     re-resolves the tag now instead of on its refresh interval (argocd
//     removes the annotation once processed; the next `up` re-adds it).
//
// Same purity rule as Deliver: RETURNS objects, never touches the cluster.
func (g *ArgoCD) DeliverSelf(ctx context.Context, src engine.ArtifactRef) ([]*unstructured.Unstructured, error) {
	app := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "argoproj.io/v1alpha1",
		"kind":       "Application",
		"metadata": map[string]any{
			"name":        engine.SelfArtifactName,
			"namespace":   Namespace,
			"annotations": map[string]any{pokeAnnotation: "normal"},
		},
		"spec": map[string]any{
			"project": "default",
			"source": map[string]any{
				"repoURL":        fmt.Sprintf("oci://%s/%s", registry.InClusterURL, src.Repo),
				"targetRevision": src.Tag,
				"path":           ".",
			},
			"destination": map[string]any{"server": "https://kubernetes.default.svc", "namespace": Namespace},
			"syncPolicy": map[string]any{
				"automated":   map[string]any{"prune": false, "selfHeal": true},
				"syncOptions": []any{"CreateNamespace=true", "ServerSideApply=true"},
			},
		},
	}}
	return []*unstructured.Unstructured{app}, nil
}
