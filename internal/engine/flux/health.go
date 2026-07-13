package flux

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/rafpe/cube-idp/internal/apply"
	"github.com/rafpe/cube-idp/internal/diag"
	"github.com/rafpe/cube-idp/internal/engine"
)

var kustomizationGVK = schema.GroupVersionKind{
	Group:   "kustomize.toolkit.fluxcd.io",
	Version: "v1",
	Kind:    "KustomizationList",
}

// Health lists the Kustomizations this engine delivered for a's cube
// (labeled cube-idp.dev/cube=<cube> by Applier.Apply) and reports each
// one's Ready condition.
func (f *Flux) Health(ctx context.Context, a *apply.Applier) ([]engine.ComponentHealth, error) {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(kustomizationGVK)
	if err := a.Client().List(ctx, list,
		client.InNamespace(fluxNS),
		client.MatchingLabels{apply.CubeLabel: a.Cube()},
	); err != nil {
		return nil, diag.Wrap(err, diag.CodeEngineHealthTimeout, "cannot list flux Kustomizations",
			"check kubeconfig and cluster connectivity")
	}

	health := make([]engine.ComponentHealth, 0, len(list.Items))
	for _, item := range list.Items {
		conditions, _, _ := unstructured.NestedSlice(item.Object, "status", "conditions")
		ready, message := false, "no status reported yet"
		for _, c := range conditions {
			cond, ok := c.(map[string]any)
			if !ok || cond["type"] != "Ready" {
				continue
			}
			ready = cond["status"] == "True"
			if m, ok := cond["message"].(string); ok && m != "" {
				message = m
			}
		}
		health = append(health, engine.ComponentHealth{
			Name:    item.GetName(),
			Ready:   ready,
			Message: message,
		})
	}
	return health, nil
}
