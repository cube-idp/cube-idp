package argocd

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/cube-idp/cube-idp/internal/engine"
	"github.com/cube-idp/cube-idp/internal/pack"
)

// TestDeliverDependsOnSetsAnnotation pins the p6 DEP3 translation: argocd
// cannot order cross-Application deliveries natively, so a resolved
// DependsOn becomes the cube-idp.dev/depends-on annotation (comma-joined,
// in ResolveOrder's sorted order) — up's wave gate is the enforcement side.
func TestDeliverDependsOnSetsAnnotation(t *testing.T) {
	g := New()
	r := &pack.Rendered{Name: "app", Version: "0.1.0", DependsOn: []string{"floci", "gitea"}}
	objs, err := g.Deliver(context.Background(), r, engine.ArtifactRef{Repo: "packs/app", Tag: "0.1.0"})
	if err != nil {
		t.Fatal(err)
	}
	app := objs[0]
	if got := app.GetAnnotations()["cube-idp.dev/depends-on"]; got != "floci,gitea" {
		t.Fatalf("cube-idp.dev/depends-on annotation = %q, want %q", got, "floci,gitea")
	}
}

// TestDeliverNilDependsOnOmitsAnnotationsKey is the byte-compat fence
// (dep-free cubes stay byte-identical to pre-p6 Applications): nil
// DependsOn must produce NO metadata.annotations key at all.
func TestDeliverNilDependsOnOmitsAnnotationsKey(t *testing.T) {
	g := New()
	r := &pack.Rendered{Name: "app", Version: "0.1.0"}
	objs, err := g.Deliver(context.Background(), r, engine.ArtifactRef{Repo: "packs/app", Tag: "0.1.0"})
	if err != nil {
		t.Fatal(err)
	}
	app := objs[0]
	meta, _, _ := unstructured.NestedMap(app.Object, "metadata")
	if _, ok := meta["annotations"]; ok {
		t.Fatalf("metadata.annotations present with nil DependsOn: %v", app.GetAnnotations())
	}
}
