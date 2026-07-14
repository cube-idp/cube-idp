package cnoe

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
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

// TestRenderDoesNotDuplicateExistingNamespace pins the dedupe rule: when the
// rendered objects already carry a Namespace matching destination.namespace
// (chart renders always do — pack.RenderChart prepends one; manifest trees
// may declare their own), applyDestinationNamespace must not prepend a
// byte-identical duplicate into the artifact.
func TestRenderDoesNotDuplicateExistingNamespace(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "manifests"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "app.yaml"), []byte(`
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata: {name: with-ns}
spec:
  destination: {namespace: my-ns, server: https://kubernetes.default.svc}
  source: {repoURL: "cnoe://manifests", targetRevision: HEAD, path: "."}
`), 0o644)
	os.WriteFile(filepath.Join(dir, "manifests", "all.yaml"), []byte(`
apiVersion: v1
kind: Namespace
metadata: {name: my-ns}
---
apiVersion: v1
kind: ConfigMap
metadata: {name: cm}
`), 0o644)
	apps, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) != 1 {
		t.Fatalf("apps: %d, want 1", len(apps))
	}
	r, err := apps[0].Render(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	nsCount := 0
	for _, o := range r.Objects {
		if o.GetKind() == "Namespace" && o.GetName() == "my-ns" {
			nsCount++
		}
	}
	if nsCount != 1 {
		t.Fatalf("Namespace my-ns appears %d times, want exactly 1", nsCount)
	}
	if len(r.Objects) != 2 || r.Objects[1].GetNamespace() != "my-ns" {
		t.Fatalf("objects: %d — destination.namespace must still apply to the ConfigMap", len(r.Objects))
	}
}

// TestUnsupportedGeneratorIsNamed pins that the rejection message tells the
// user WHICH generator tripped it, not just that one did.
func TestUnsupportedGeneratorIsNamed(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "appset.yaml"), []byte(`
apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata: {name: clustered}
spec:
  generators:
    - clusters: {}
  template:
    metadata: {name: "x"}
    spec:
      destination: {namespace: x, server: https://kubernetes.default.svc}
      source: {repoURL: "cnoe://manifests", targetRevision: HEAD, path: "."}
`), 0o644)
	_, err := Load(dir)
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-4009" {
		t.Fatalf("want CUBE-4009, got %v", err)
	}
	if !strings.Contains(de.Summary, `"clusters"`) {
		t.Fatalf("rejection must name the generator, got: %s", de.Summary)
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
