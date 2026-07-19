package flux

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/cube-idp/cube-idp/internal/engine"
)

func TestDeliverGitShapesFluxObjects(t *testing.T) {
	f := New()
	objs, err := f.DeliverGit(context.Background(), "app",
		engine.GitSource{URL: "http://gitea-http.gitea.svc.cluster.local:3000/gitea_admin/app.git", Branch: "main", Path: "./"}, nil)
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
		engine.GitSource{URL: "http://gitea-http.gitea.svc.cluster.local:3000/gitea_admin/app.git"}, nil)
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

// TestDeliverGitDependsOnSetsKustomizationSpec mirrors Deliver's pin for the
// git-sourced path (p6 DEP3): dependsOn is a DeliverGit param now, not part
// of GitSource, so it flows in as a trailing argument.
func TestDeliverGitDependsOnSetsKustomizationSpec(t *testing.T) {
	f := New()
	objs, err := f.DeliverGit(context.Background(), "app",
		engine.GitSource{URL: "http://gitea-http.gitea.svc.cluster.local:3000/gitea_admin/app.git"},
		[]string{"floci", "gitea"})
	if err != nil {
		t.Fatal(err)
	}
	kust := objs[1]
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

// TestDeliverGitNilDependsOnOmitsKey is DeliverGit's half of the byte-compat
// fence: nil (or empty) dependsOn must produce NO dependsOn key.
func TestDeliverGitNilDependsOnOmitsKey(t *testing.T) {
	f := New()
	objs, err := f.DeliverGit(context.Background(), "app",
		engine.GitSource{URL: "http://gitea-http.gitea.svc.cluster.local:3000/gitea_admin/app.git"},
		nil)
	if err != nil {
		t.Fatal(err)
	}
	kust := objs[1]
	spec, _, _ := unstructured.NestedMap(kust.Object, "spec")
	if _, ok := spec["dependsOn"]; ok {
		t.Fatalf("spec.dependsOn present with nil dependsOn: %v", spec["dependsOn"])
	}
}
