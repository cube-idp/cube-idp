package cnoe

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/rafpe/cube-idp/internal/diag"
)

func TestLoadFindsAppsAndExpandsAppSets(t *testing.T) {
	apps, err := Load("testdata")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, a := range apps {
		names = append(names, a.Name)
	}
	sort.Strings(names)
	want := []string{"my-app", "web-dev", "web-stage"}
	if len(names) != 3 || names[0] != want[0] || names[1] != want[1] || names[2] != want[2] {
		t.Fatalf("apps: %v, want %v", names, want)
	}
}

func TestCnoePathResolvesRelativeToFile(t *testing.T) {
	apps, err := Load("testdata")
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range apps {
		if a.Name != "my-app" {
			continue
		}
		abs, _ := filepath.Abs(filepath.Join("testdata", "manifests"))
		if a.CnoeDir != abs {
			t.Fatalf("CnoeDir: %s, want %s", a.CnoeDir, abs)
		}
	}
}

func TestRenderSetsDestinationNamespaceAndHashTag(t *testing.T) {
	apps, _ := Load("testdata")
	var app *App
	for i := range apps {
		if apps[i].Name == "my-app" {
			app = &apps[i]
		}
	}
	r, err := app.Render(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// Namespace object prepended + Deployment
	if len(r.Objects) != 2 || r.Objects[0].GetKind() != "Namespace" || r.Objects[0].GetName() != "my-app" {
		t.Fatalf("objects: %d, first %s/%s", len(r.Objects), r.Objects[0].GetKind(), r.Objects[0].GetName())
	}
	if r.Objects[1].GetNamespace() != "my-app" {
		t.Fatalf("destination.namespace not applied: %q", r.Objects[1].GetNamespace())
	}
	if r.Name != "cnoe-my-app" || len(r.Version) != 12 {
		t.Fatalf("rendered identity: %s@%s (tag must be a 12-char content hash)", r.Name, r.Version)
	}
}

func TestUnpinnedRemoteGitIsRejected(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "app.yaml"), []byte(`
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata: {name: remote}
spec:
  destination: {namespace: remote, server: https://kubernetes.default.svc}
  source:
    repoURL: https://github.com/org/repo
    targetRevision: HEAD
    path: apps/remote
`), 0o644)
	_, err := Load(dir)
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-4009" {
		t.Fatalf("want CUBE-4009 (pin targetRevision), got %v", err)
	}
}

func TestMissingCnoeDirIsTyped(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "app.yaml"), []byte(`
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata: {name: broken}
spec:
  destination: {namespace: b, server: https://kubernetes.default.svc}
  source: {repoURL: "cnoe://nope", targetRevision: HEAD, path: "."}
`), 0o644)
	_, err := Load(dir)
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-4010" {
		t.Fatalf("want CUBE-4010, got %v", err)
	}
}
