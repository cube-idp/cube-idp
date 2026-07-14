package flux

import (
	"context"
	"testing"

	"github.com/rafpe/cube-idp/internal/engine"
)

func TestDeliverGitShapesFluxObjects(t *testing.T) {
	f := New()
	objs, err := f.DeliverGit(context.Background(), "app",
		engine.GitSource{URL: "http://gitea-http.gitea.svc.cluster.local:3000/gitea_admin/app.git", Branch: "main", Path: "./"})
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 2 {
		t.Fatalf("want GitRepository + Kustomization, got %d objects", len(objs))
	}
	repo, kust := objs[0], objs[1]
	if repo.GetKind() != "GitRepository" || kust.GetKind() != "Kustomization" {
		t.Fatalf("kinds: %s, %s", repo.GetKind(), kust.GetKind())
	}
	if repo.GetName() != "cube-idp-app" || kust.GetName() != "cube-idp-app" {
		t.Fatalf("names: %s, %s (want cube-idp-app for both)", repo.GetName(), kust.GetName())
	}
	if repo.GetNamespace() != fluxNS || kust.GetNamespace() != fluxNS {
		t.Fatalf("namespaces: %s, %s (want %s)", repo.GetNamespace(), kust.GetNamespace(), fluxNS)
	}
	url, _, _ := unstructuredNestedString(repo, "spec", "url")
	if url != "http://gitea-http.gitea.svc.cluster.local:3000/gitea_admin/app.git" {
		t.Fatalf("url: %s", url)
	}
	branch, _, _ := unstructuredNestedString(repo, "spec", "ref", "branch")
	if branch != "main" {
		t.Fatalf("ref.branch: %s", branch)
	}
	src, _, _ := unstructuredNestedString(kust, "spec", "sourceRef", "kind")
	if src != "GitRepository" {
		t.Fatalf("sourceRef.kind: %s (git delivery must reference a GitRepository, not an OCIRepository)", src)
	}
	prune, _, _ := unstructuredNestedBool(kust, "spec", "prune")
	if !prune {
		t.Fatal("Kustomization.spec.prune must be true")
	}
	path, _, _ := unstructuredNestedString(kust, "spec", "path")
	if path != "./" {
		t.Fatalf("path: %s", path)
	}
}

func TestDeliverGitDefaultsBranchAndPath(t *testing.T) {
	f := New()
	objs, err := f.DeliverGit(context.Background(), "app",
		engine.GitSource{URL: "http://gitea-http.gitea.svc.cluster.local:3000/gitea_admin/app.git"})
	if err != nil {
		t.Fatal(err)
	}
	branch, _, _ := unstructuredNestedString(objs[0], "spec", "ref", "branch")
	if branch != "main" {
		t.Fatalf("empty Branch must default to main, got %q", branch)
	}
	path, _, _ := unstructuredNestedString(objs[1], "spec", "path")
	if path != "./" {
		t.Fatalf("empty Path must default to ./, got %q", path)
	}
}
