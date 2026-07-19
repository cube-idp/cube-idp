package argocd

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/cube-idp/cube-idp/internal/engine"
)

func TestDeliverGitShapesApplication(t *testing.T) {
	g := New()
	objs, err := g.DeliverGit(context.Background(), "app",
		engine.GitSource{URL: "http://gitea-http.gitea.svc.cluster.local:3000/gitea_admin/app.git", Branch: "main", Path: "./"}, nil)
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
	if app.GetName() != "cube-idp-app" {
		t.Fatalf("name: %s (want cube-idp-app)", app.GetName())
	}
	if got := nestedString(app, "spec", "source", "repoURL"); got != "http://gitea-http.gitea.svc.cluster.local:3000/gitea_admin/app.git" {
		t.Fatalf("repoURL: %s", got)
	}
	if got := nestedString(app, "spec", "source", "targetRevision"); got != "main" {
		t.Fatalf("targetRevision: %s (git delivery tracks the branch)", got)
	}
	if got := nestedString(app, "spec", "source", "path"); got != "./" {
		t.Fatalf("path: %s", got)
	}
	prune, _, _ := unstructured.NestedBool(app.Object, "spec", "syncPolicy", "automated", "prune")
	if !prune {
		t.Fatal("syncPolicy.automated.prune must be true (down/upgrade rely on it)")
	}
	if got := nestedString(app, "spec", "destination", "server"); got != "https://kubernetes.default.svc" {
		t.Fatalf("destination: %s", got)
	}
}

func TestDeliverGitDefaultsBranchAndPath(t *testing.T) {
	g := New()
	objs, err := g.DeliverGit(context.Background(), "app",
		engine.GitSource{URL: "http://gitea-http.gitea.svc.cluster.local:3000/gitea_admin/app.git"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := nestedString(objs[0], "spec", "source", "targetRevision"); got != "main" {
		t.Fatalf("empty Branch must default to main, got %q", got)
	}
	if got := nestedString(objs[0], "spec", "source", "path"); got != "./" {
		t.Fatalf("empty Path must default to ./, got %q", got)
	}
}

// TestDeliverGitDependsOnSetsAnnotation mirrors Deliver's pin for the
// git-sourced path (p6 DEP3): dependsOn is a DeliverGit param now, flowing
// in as a trailing argument, translated to the same annotation.
func TestDeliverGitDependsOnSetsAnnotation(t *testing.T) {
	g := New()
	objs, err := g.DeliverGit(context.Background(), "app",
		engine.GitSource{URL: "http://gitea-http.gitea.svc.cluster.local:3000/gitea_admin/app.git"},
		[]string{"floci", "gitea"})
	if err != nil {
		t.Fatal(err)
	}
	app := objs[0]
	if got := app.GetAnnotations()["cube-idp.dev/depends-on"]; got != "floci,gitea" {
		t.Fatalf("cube-idp.dev/depends-on annotation = %q, want %q", got, "floci,gitea")
	}
}

// TestDeliverGitNilDependsOnOmitsAnnotationsKey is DeliverGit's half of the
// byte-compat fence: nil (or empty) dependsOn must produce NO
// metadata.annotations key.
func TestDeliverGitNilDependsOnOmitsAnnotationsKey(t *testing.T) {
	g := New()
	objs, err := g.DeliverGit(context.Background(), "app",
		engine.GitSource{URL: "http://gitea-http.gitea.svc.cluster.local:3000/gitea_admin/app.git"},
		nil)
	if err != nil {
		t.Fatal(err)
	}
	app := objs[0]
	meta, _, _ := unstructured.NestedMap(app.Object, "metadata")
	if _, ok := meta["annotations"]; ok {
		t.Fatalf("metadata.annotations present with nil dependsOn: %v", app.GetAnnotations())
	}
}
