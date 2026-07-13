package apply

import (
	"context"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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
