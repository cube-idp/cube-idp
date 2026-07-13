package apply

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/fluxcd/cli-utils/pkg/object"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func cm(name, ns string, annotations map[string]string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "ConfigMap",
		"metadata": map[string]any{"name": name, "namespace": ns},
		"data":     map[string]any{"k": "v"},
	}}
	if annotations != nil {
		u.SetAnnotations(annotations)
	}
	return u
}

func ns(name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "Namespace",
		"metadata": map[string]any{"name": name},
	}}
}

func TestApplyIsIdempotentAndLabels(t *testing.T) {
	a, err := New(testREST, "dev")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	objs := []*unstructured.Unstructured{ns("t1"), cm("a", "t1", nil)}
	for i := 0; i < 2; i++ { // second apply must be a clean no-op
		if err := a.Apply(ctx, objs, true, 30*time.Second); err != nil {
			t.Fatalf("apply #%d: %v", i+1, err)
		}
	}
	got := cm("a", "t1", nil)
	if err := a.Client().Get(ctx, client.ObjectKey{Namespace: "t1", Name: "a"}, got); err != nil {
		t.Fatal(err)
	}
	if got.GetLabels()["cube-idp.dev/cube"] != "dev" {
		t.Fatalf("cube label missing: %v", got.GetLabels())
	}
}

func TestInventoryRoundTripAndDeleteAll(t *testing.T) {
	a, _ := New(testREST, "dev2")
	ctx := context.Background()
	keep := cm("keep", "t2", map[string]string{"cube-idp.dev/prune": "disabled"})
	objs := []*unstructured.Unstructured{ns("t2"), cm("gone", "t2", nil), keep}
	if err := a.Apply(ctx, objs, true, 30*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := a.RecordInventory(ctx, objs); err != nil {
		t.Fatal(err)
	}
	inv, err := a.LoadInventory(ctx)
	if err != nil || len(inv) != 3 {
		t.Fatalf("inventory: %v %v", inv, err)
	}
	if err := a.DeleteAll(ctx, 60*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := a.Client().Get(ctx, client.ObjectKey{Namespace: "t2", Name: "gone"}, cm("gone", "t2", nil)); err == nil {
		t.Fatal("object 'gone' should have been pruned")
	}
	if err := a.Client().Get(ctx, client.ObjectKey{Namespace: "t2", Name: "keep"}, cm("keep", "t2", nil)); err != nil {
		t.Fatalf("annotated object must survive DeleteAll: %v", err)
	}
}

// TestDeleteAllSkipsVanishedKinds proves that an inventory entry whose kind
// no longer exists on the cluster (e.g. its CRD was already deleted) is
// treated as "already gone": DeleteAll still prunes the remaining real
// objects and returns nil, rather than failing or orphaning the rest.
//
// Note: the complementary case — an entry whose Get fails with a genuine
// error (RBAC denial, transient failure) making DeleteAll return CUBE-2006 —
// cannot be honestly fabricated under envtest (the test client is
// cluster-admin and the API server is local), so it is not covered here.
func TestDeleteAllSkipsVanishedKinds(t *testing.T) {
	a, err := New(testREST, "dev3")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	objs := []*unstructured.Unstructured{ns("t3"), cm("real", "t3", nil)}
	if err := a.Apply(ctx, objs, true, 30*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := a.RecordInventory(ctx, objs); err != nil {
		t.Fatal(err)
	}

	// Append an entry for a kind that does not exist on the cluster
	// directly into the inventory ConfigMap, as if its CRD had been deleted
	// after the object was inventoried.
	invKey := client.ObjectKey{Namespace: SystemNamespace, Name: "cube-idp-inventory-dev3"}
	inv := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "ConfigMap"}}
	if err := a.Client().Get(ctx, invKey, inv); err != nil {
		t.Fatal(err)
	}
	raw, _, err := unstructured.NestedString(inv.Object, "data", "inventory")
	if err != nil {
		t.Fatal(err)
	}
	var strs []string
	if err := json.Unmarshal([]byte(raw), &strs); err != nil {
		t.Fatal(err)
	}
	ghost := object.ObjMetadata{
		Namespace: "t3",
		Name:      "ghost",
		GroupKind: schema.GroupKind{Group: "nope.example.com", Kind: "Ghost"},
	}
	strs = append(strs, ghost.String())
	payload, err := json.Marshal(strs)
	if err != nil {
		t.Fatal(err)
	}
	if err := unstructured.SetNestedField(inv.Object, string(payload), "data", "inventory"); err != nil {
		t.Fatal(err)
	}
	if err := a.Client().Update(ctx, inv); err != nil {
		t.Fatal(err)
	}

	loaded, err := a.LoadInventory(ctx)
	if err != nil || len(loaded) != 3 {
		t.Fatalf("inventory should hold ns+cm+ghost: %v %v", loaded, err)
	}

	if err := a.DeleteAll(ctx, 60*time.Second); err != nil {
		t.Fatalf("DeleteAll must treat vanished kinds as already gone: %v", err)
	}
	if err := a.Client().Get(ctx, client.ObjectKey{Namespace: "t3", Name: "real"}, cm("real", "t3", nil)); err == nil {
		t.Fatal("object 'real' should have been pruned despite the ghost entry")
	}
	if err := a.Client().Get(ctx, invKey, inv); err == nil {
		t.Fatal("inventory ConfigMap should have been deleted on full success")
	}
}
