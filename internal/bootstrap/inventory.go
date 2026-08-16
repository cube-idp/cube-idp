package bootstrap

import (
	"context"
	"encoding/json"
	"sort"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	// InventoryName is the ConfigMap bootstrap records its applied objects in —
	// the seed a future `down` reads to tear the bootstrap back down.
	InventoryName = "cube-idp-bootstrap-inventory"
	// InventoryNamespace is where the inventory ConfigMap lives: the Flux
	// namespace bootstrap installs into.
	InventoryNamespace = "flux-system"
	// inventoryKey is the ConfigMap data key holding the JSON object list.
	inventoryKey = "objects"
)

// ObjectRef identifies one object recorded in the bootstrap inventory.
type ObjectRef struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Namespace  string `json:"namespace,omitempty"`
	Name       string `json:"name"`
}

// RecordInventory applies a ConfigMap listing every object bootstrap installed,
// so a future `down` can find and remove them. It rides the same SSA seam
// (an apply failure is CUBE-BST-004, naming the inventory ConfigMap) and is
// idempotent — re-recording overwrites. Record after Apply so the Flux
// namespace exists, and before waiting so a half-ready install is still
// recoverable.
func (a *Applier) RecordInventory(ctx context.Context, objs []*unstructured.Unstructured) error {
	cm, err := inventoryConfigMap(objs)
	if err != nil {
		return err
	}
	return a.k.apply(ctx, cm)
}

// inventoryConfigMap builds the cube-idp-owned inventory ConfigMap from the
// applied objects, with a deterministic (sorted) object list.
func inventoryConfigMap(objs []*unstructured.Unstructured) (*unstructured.Unstructured, error) {
	refs := make([]ObjectRef, 0, len(objs))
	for _, o := range objs {
		refs = append(refs, ObjectRef{
			APIVersion: o.GetAPIVersion(),
			Kind:       o.GetKind(),
			Namespace:  o.GetNamespace(),
			Name:       o.GetName(),
		})
	}
	sort.Slice(refs, func(i, j int) bool { return refKey(refs[i]) < refKey(refs[j]) })

	data, err := json.MarshalIndent(refs, "", "  ")
	if err != nil {
		return nil, newInventoryError(err)
	}

	cm := &unstructured.Unstructured{}
	cm.SetAPIVersion("v1")
	cm.SetKind("ConfigMap")
	cm.SetName(InventoryName)
	cm.SetNamespace(InventoryNamespace)
	cm.SetLabels(map[string]string{"app.kubernetes.io/managed-by": "cube-idp"})
	if err := unstructured.SetNestedField(cm.Object, map[string]any{inventoryKey: string(data)}, "data"); err != nil {
		return nil, newInventoryError(err)
	}
	return cm, nil
}

func refKey(r ObjectRef) string {
	return r.Namespace + "/" + r.Kind + "/" + r.Name
}
