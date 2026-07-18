package flux

import (
	"context"
	"fmt"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/cube-idp/cube-idp/internal/engine"
	"github.com/cube-idp/cube-idp/internal/registry"
)

// TestDeliverSelfShapes pins P8's flux self-source (GT16): one
// OCIRepository named cube-engine watching the pushed artifact + one
// Kustomization with spec.prune == false (the self-source must never prune
// the engine out from under itself), both in flux-system. The OCIRepository
// additionally carries a fresh reconcile-now annotation — each `up` apply
// doubles as the GT16 "poke" (Poke(name) addresses cube-idp-<pack> names
// and cannot reach the plain cube-engine self-source).
func TestDeliverSelfShapes(t *testing.T) {
	src := engine.ArtifactRef{Repo: "packs/cube-engine", Tag: "latest", Digest: "sha256:abc"}
	objs, err := New().DeliverSelf(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 2 {
		t.Fatalf("want OCIRepository + Kustomization, got %d objects", len(objs))
	}

	repo, kust := objs[0], objs[1]
	if repo.GetKind() != "OCIRepository" || repo.GetName() != "cube-engine" || repo.GetNamespace() != "flux-system" {
		t.Fatalf("self source must be OCIRepository cube-engine in flux-system, got %s %s/%s",
			repo.GetKind(), repo.GetNamespace(), repo.GetName())
	}
	wantURL := fmt.Sprintf("oci://%s/%s", registry.InClusterURL, src.Repo)
	if url, _, _ := unstructured.NestedString(repo.Object, "spec", "url"); url != wantURL {
		t.Fatalf("OCIRepository url = %q, want %q", url, wantURL)
	}
	if tag, _, _ := unstructured.NestedString(repo.Object, "spec", "ref", "tag"); tag != src.Tag {
		t.Fatalf("OCIRepository ref.tag = %q, want %q", tag, src.Tag)
	}
	if insecure, _, _ := unstructured.NestedBool(repo.Object, "spec", "insecure"); !insecure {
		t.Fatal("OCIRepository must be insecure: true — zot is plain HTTP in-cluster (mirrors Deliver)")
	}
	stamp := repo.GetAnnotations()[pokeAnnotation]
	if stamp == "" {
		t.Fatalf("OCIRepository must carry a fresh %s stamp (the poke), annotations: %v",
			pokeAnnotation, repo.GetAnnotations())
	}
	if _, err := time.Parse(time.RFC3339Nano, stamp); err != nil {
		t.Fatalf("poke stamp %q is not RFC3339Nano: %v", stamp, err)
	}

	if kust.GetKind() != "Kustomization" || kust.GetName() != "cube-engine" || kust.GetNamespace() != "flux-system" {
		t.Fatalf("self apply must be Kustomization cube-engine in flux-system, got %s %s/%s",
			kust.GetKind(), kust.GetNamespace(), kust.GetName())
	}
	prune, found, _ := unstructured.NestedBool(kust.Object, "spec", "prune")
	if !found || prune {
		t.Fatalf("self Kustomization spec.prune must be exactly false (found=%v prune=%v)", found, prune)
	}
	srcKind, _, _ := unstructured.NestedString(kust.Object, "spec", "sourceRef", "kind")
	srcName, _, _ := unstructured.NestedString(kust.Object, "spec", "sourceRef", "name")
	if srcKind != "OCIRepository" || srcName != "cube-engine" {
		t.Fatalf("self Kustomization must source OCIRepository/cube-engine, got %s/%s", srcKind, srcName)
	}
}
