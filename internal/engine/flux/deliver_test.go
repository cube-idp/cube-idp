package flux

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/cube-idp/cube-idp/internal/engine"
	"github.com/cube-idp/cube-idp/internal/pack"
)

// TestDeliverDependsOnSetsKustomizationSpec pins the p6 DEP3 translation:
// a Rendered with a resolved DependsOn must produce a Kustomization whose
// spec.dependsOn names the delivery objects (cube-idp-<dep>) of each
// dependency, in the given order (ResolveOrder already sorted it).
func TestDeliverDependsOnSetsKustomizationSpec(t *testing.T) {
	f := New()
	r := &pack.Rendered{Name: "app", Version: "0.1.0", DependsOn: []string{"floci", "gitea"}}
	objs, err := f.Deliver(context.Background(), r, engine.ArtifactRef{Repo: "packs/app", Tag: "0.1.0"})
	if err != nil {
		t.Fatal(err)
	}
	kust := objs[1]
	if kust.GetKind() != "Kustomization" {
		t.Fatalf("objs[1] kind = %s, want Kustomization", kust.GetKind())
	}
	got, found, err := unstructured.NestedSlice(kust.Object, "spec", "dependsOn")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("spec.dependsOn missing")
	}
	want := []any{
		map[string]any{"name": "cube-idp-floci"},
		map[string]any{"name": "cube-idp-gitea"},
	}
	if len(got) != len(want) {
		t.Fatalf("spec.dependsOn = %v, want %v", got, want)
	}
	for i := range want {
		if got[i].(map[string]any)["name"] != want[i].(map[string]any)["name"] {
			t.Fatalf("spec.dependsOn[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

// TestDeliverNilDependsOnOmitsKey is the byte-compat fence (dep-free cubes
// stay byte-identical to pre-p6 Kustomizations): nil DependsOn must produce
// NO dependsOn key at all, not an empty list.
func TestDeliverNilDependsOnOmitsKey(t *testing.T) {
	f := New()
	r := &pack.Rendered{Name: "app", Version: "0.1.0"}
	objs, err := f.Deliver(context.Background(), r, engine.ArtifactRef{Repo: "packs/app", Tag: "0.1.0"})
	if err != nil {
		t.Fatal(err)
	}
	kust := objs[1]
	spec, _, _ := unstructured.NestedMap(kust.Object, "spec")
	if _, ok := spec["dependsOn"]; ok {
		t.Fatalf("spec.dependsOn present with nil DependsOn: %v", spec["dependsOn"])
	}
}
