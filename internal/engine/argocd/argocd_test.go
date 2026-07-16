package argocd

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/cube-idp/cube-idp/internal/engine"
	"github.com/cube-idp/cube-idp/internal/pack"
)

func nestedString(o *unstructured.Unstructured, fields ...string) string {
	s, _, _ := unstructured.NestedString(o.Object, fields...)
	return s
}

func TestDeliverShapesApplication(t *testing.T) {
	g := New()
	r := &pack.Rendered{Name: "gitea", Version: "0.1.0"}
	objs, err := g.Deliver(context.Background(), r, engine.ArtifactRef{Repo: "packs/gitea", Tag: "0.1.0"})
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 1 {
		t.Fatalf("want exactly one Application, got %d objects", len(objs))
	}
	app := objs[0]
	if app.GetKind() != "Application" || app.GetNamespace() != Namespace {
		t.Fatalf("got %s in ns %s", app.GetKind(), app.GetNamespace())
	}
	if got := nestedString(app, "spec", "source", "repoURL"); got != "oci://zot.cube-idp-system.svc.cluster.local:5000/packs/gitea" {
		t.Fatalf("repoURL: %s", got)
	}
	if got := nestedString(app, "spec", "source", "targetRevision"); got != "0.1.0" {
		t.Fatalf("targetRevision: %s", got)
	}
	prune, _, _ := unstructured.NestedBool(app.Object, "spec", "syncPolicy", "automated", "prune")
	if !prune {
		t.Fatal("syncPolicy.automated.prune must be true (down/upgrade rely on it)")
	}
	if got := nestedString(app, "spec", "destination", "server"); got != "https://kubernetes.default.svc" {
		t.Fatalf("destination: %s", got)
	}
}

func TestInstallManifestsIncludeRepoSecret(t *testing.T) {
	objs, err := New().InstallManifests()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, o := range objs {
		if o.GetKind() == "Secret" && o.GetName() == "cube-idp-zot-repo" {
			found = true
		}
	}
	if !found {
		t.Fatal("install manifests must register the zot OCI repository (engine-specific requirement)")
	}
}
