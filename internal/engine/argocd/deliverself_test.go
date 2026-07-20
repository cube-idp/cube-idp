package argocd

import (
	"context"
	"fmt"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/cube-idp/cube-idp/internal/engine"
	"github.com/cube-idp/cube-idp/internal/registry"
)

// TestDeliverSelfShapes pins the argocd engine self-source used when
// spec.engine.selfManage is on (ADR 0020): exactly one
// Application named cube-engine in ns argocd, destination its own
// namespace, automated sync with prune: false — and, unlike the pack
// application() shape, NO resources-finalizer: cascading deletion of the
// self Application would tear the engine itself down mid-`down`, when the
// inventory-driven DeleteAll owns engine removal. The refresh annotation
// makes each `up` apply double as a reconcile-now poke.
func TestDeliverSelfShapes(t *testing.T) {
	src := engine.ArtifactRef{Repo: "packs/cube-engine", Tag: "latest", Digest: "sha256:abc"}
	objs, err := New().DeliverSelf(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 1 {
		t.Fatalf("want exactly one Application, got %d objects", len(objs))
	}

	app := objs[0]
	if app.GetKind() != "Application" || app.GetName() != "cube-engine" || app.GetNamespace() != Namespace {
		t.Fatalf("self source must be Application cube-engine in %s, got %s %s/%s",
			Namespace, app.GetKind(), app.GetNamespace(), app.GetName())
	}
	if fins := app.GetFinalizers(); len(fins) != 0 {
		t.Fatalf("self Application must carry NO finalizers (cascade would delete the engine on down), got %v", fins)
	}
	if app.GetAnnotations()[pokeAnnotation] != "normal" {
		t.Fatalf("self Application must carry the %s=normal refresh stamp (the poke), annotations: %v",
			pokeAnnotation, app.GetAnnotations())
	}

	wantURL := fmt.Sprintf("oci://%s/%s", registry.InClusterURL, src.Repo)
	if url, _, _ := unstructured.NestedString(app.Object, "spec", "source", "repoURL"); url != wantURL {
		t.Fatalf("Application repoURL = %q, want %q", url, wantURL)
	}
	if rev, _, _ := unstructured.NestedString(app.Object, "spec", "source", "targetRevision"); rev != src.Tag {
		t.Fatalf("Application targetRevision = %q, want %q", rev, src.Tag)
	}
	if ns, _, _ := unstructured.NestedString(app.Object, "spec", "destination", "namespace"); ns != Namespace {
		t.Fatalf("Application destination.namespace = %q, want its own namespace %q", ns, Namespace)
	}

	prune, found, _ := unstructured.NestedBool(app.Object, "spec", "syncPolicy", "automated", "prune")
	if !found || prune {
		t.Fatalf("automated sync prune must be exactly false (found=%v prune=%v)", found, prune)
	}
	// Drift between `up`s must be corrected by the engine, not the CLI: argocd
	// only re-syncs live drift with selfHeal on.
	if heal, _, _ := unstructured.NestedBool(app.Object, "spec", "syncPolicy", "automated", "selfHeal"); !heal {
		t.Fatal("automated sync selfHeal must be true — the engine corrects drift between `up`s")
	}
}
