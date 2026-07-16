package flux

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/cube-idp/cube-idp/internal/engine"
	"github.com/cube-idp/cube-idp/internal/pack"
)

func unstructuredNestedString(u *unstructured.Unstructured, fields ...string) (string, bool, error) {
	return unstructured.NestedString(u.Object, fields...)
}

func unstructuredNestedBool(u *unstructured.Unstructured, fields ...string) (bool, bool, error) {
	return unstructured.NestedBool(u.Object, fields...)
}

func TestDeliverShapesFluxObjects(t *testing.T) {
	f := New()
	r := &pack.Rendered{Name: "gitea", Version: "0.1.0"}
	objs, err := f.Deliver(context.Background(), r, engine.ArtifactRef{Repo: "packs/gitea", Tag: "0.1.0"})
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 2 {
		t.Fatalf("want OCIRepository + Kustomization, got %d objects", len(objs))
	}
	repo, kust := objs[0], objs[1]
	if repo.GetKind() != "OCIRepository" || kust.GetKind() != "Kustomization" {
		t.Fatalf("kinds: %s, %s", repo.GetKind(), kust.GetKind())
	}
	url, _, _ := unstructuredNestedString(repo, "spec", "url")
	if url != "oci://zot.cube-idp-system.svc.cluster.local:5000/packs/gitea" {
		t.Fatalf("url: %s", url)
	}
	insecure, _, _ := unstructuredNestedBool(repo, "spec", "insecure")
	if !insecure {
		t.Fatal("in-cluster zot is plain HTTP; spec.insecure must be true")
	}
	prune, _, _ := unstructuredNestedBool(kust, "spec", "prune")
	if !prune {
		t.Fatal("Kustomization.spec.prune must be true")
	}
	src, _, _ := unstructuredNestedString(kust, "spec", "sourceRef", "kind")
	if src != "OCIRepository" {
		t.Fatalf("sourceRef.kind: %s", src)
	}
}

func TestInstallManifestsEmbedAndParse(t *testing.T) {
	objs, err := InstallManifests()
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) < 10 {
		t.Fatalf("flux install.yaml seems empty: %d objects — run hack/gen-flux-manifests.sh", len(objs))
	}
}
