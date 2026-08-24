package bootstrap

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
)

func nested(t *testing.T, o *unstructured.Unstructured, fields ...string) string {
	t.Helper()
	s, _, _ := unstructured.NestedString(o.Object, fields...)
	return s
}

// testSubstrateObjs is the injected install content the InstallEngine tests
// run over: bootstrap applies whatever substrate set the edge hands it, so a
// minimal kind-set-covered pair stands in for the real payload.
func testSubstrateObjs() []*unstructured.Unstructured {
	return []*unstructured.Unstructured{
		newNamespace("flux-system", "Active"),
		newDeployment("source-controller", "flux-system", 1, 1, 1),
	}
}

// TestSourceObjectsGit builds a GitRepository + Kustomization pointing at it.
func TestSourceObjectsGit(t *testing.T) {
	objs, err := sourceObjects(&v1alpha1.EngineSource{
		Kind: v1alpha1.EngineSourceGit, URL: "https://github.com/org/fleet",
		Ref: "main", Path: "./", Interval: "10m",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 2 {
		t.Fatalf("got %d objects, want 2", len(objs))
	}
	src, kz := objs[0], objs[1]

	if src.GetAPIVersion() != "source.toolkit.fluxcd.io/v1" || src.GetKind() != "GitRepository" {
		t.Errorf("source = %s/%s, want source.toolkit.fluxcd.io/v1/GitRepository", src.GetAPIVersion(), src.GetKind())
	}
	if got := nested(t, src, "spec", "url"); got != "https://github.com/org/fleet" {
		t.Errorf("spec.url = %q", got)
	}
	if got := nested(t, src, "spec", "ref", "branch"); got != "main" {
		t.Errorf("spec.ref.branch = %q, want main", got)
	}
	if src.GetNamespace() != InventoryNamespace || src.GetName() != sourceName {
		t.Errorf("source name/ns = %s/%s, want %s/%s", src.GetNamespace(), src.GetName(), InventoryNamespace, sourceName)
	}
	if src.GetKind() == "" || kz.GetAPIVersion() != "kustomize.toolkit.fluxcd.io/v1" || kz.GetKind() != "Kustomization" {
		t.Errorf("kustomization = %s/%s", kz.GetAPIVersion(), kz.GetKind())
	}
	if got := nested(t, kz, "spec", "sourceRef", "kind"); got != "GitRepository" {
		t.Errorf("kustomization sourceRef.kind = %q, want GitRepository", got)
	}
	if got := nested(t, kz, "spec", "path"); got != "./" {
		t.Errorf("kustomization spec.path = %q, want ./", got)
	}
}

// TestSourceObjectsOCI builds an OCIRepository (provider generic) + Kustomization.
func TestSourceObjectsOCI(t *testing.T) {
	objs, err := sourceObjects(&v1alpha1.EngineSource{
		Kind: v1alpha1.EngineSourceOCI, URL: "oci://ghcr.io/org/fleet",
		Ref: "latest", Path: "./", Interval: "10m",
	})
	if err != nil {
		t.Fatal(err)
	}
	src, kz := objs[0], objs[1]
	if src.GetKind() != "OCIRepository" {
		t.Errorf("source kind = %s, want OCIRepository", src.GetKind())
	}
	if got := nested(t, src, "spec", "ref", "tag"); got != "latest" {
		t.Errorf("spec.ref.tag = %q, want latest", got)
	}
	if got := nested(t, src, "spec", "provider"); got != "generic" {
		t.Errorf("spec.provider = %q, want generic", got)
	}
	if got := nested(t, kz, "spec", "sourceRef", "kind"); got != "OCIRepository" {
		t.Errorf("kustomization sourceRef.kind = %q, want OCIRepository", got)
	}
}

// TestSourceObjectsUnknownKind is the defensive guard (config validation is the
// primary gate): an unmappable kind is CUBE-BST-007.
func TestSourceObjectsUnknownKind(t *testing.T) {
	_, err := sourceObjects(&v1alpha1.EngineSource{Kind: "svn", URL: "https://x"})
	assertCode(t, err, CodeSourceKind)
}

// TestInstallEngineWithSource: Flux installs and becomes ready, THEN the source
// CRs are applied (after the readiness wait) and the inventory includes them.
func TestInstallEngineWithSource(t *testing.T) {
	f := &fakeCluster{store: map[string]*unstructured.Unstructured{}, readyApply: true}
	a := testApplier(f)
	engine := &v1alpha1.EngineSpec{
		Provider: v1alpha1.EngineProviderFlux,
		Source: &v1alpha1.EngineSource{
			Kind: v1alpha1.EngineSourceGit, URL: "https://github.com/org/fleet",
			Ref: "main", Path: "./", Interval: "10m",
		},
	}
	if err := a.InstallEngine(t.Context(), engine, testSubstrateObjs(), EngineWait{}); err != nil {
		t.Fatalf("InstallEngine() error = %v", err)
	}

	for _, key := range []string{
		"GitRepository/flux-system/flux-system",
		"Kustomization/flux-system/flux-system",
	} {
		if _, ok := f.store[key]; !ok {
			t.Errorf("source CR %s not applied", key)
		}
	}

	inv := f.store["ConfigMap/"+InventoryNamespace+"/"+InventoryName]
	if inv == nil {
		t.Fatal("no inventory recorded")
	}
	data := nested(t, inv, "data", inventoryKey)
	for _, want := range []string{"GitRepository", "Kustomization"} {
		if !strings.Contains(data, want) {
			t.Errorf("inventory missing %s:\n%s", want, data)
		}
	}

	// Source CRs are applied only AFTER the Flux readiness wait (a get precedes
	// the GitRepository apply) — the CRDs-established prerequisite.
	gitApply := callIndex(f.calls, "apply:GitRepository/flux-system/flux-system")
	firstGet := firstCallWithPrefix(f.calls, "get:")
	if firstGet < 0 || gitApply < firstGet {
		t.Errorf("source CRs applied before the Flux readiness wait; calls=%v", f.calls)
	}
}

// TestInstallEngineNoSource installs Flux only when no source is configured
// (nil engine and empty source both).
func TestInstallEngineNoSource(t *testing.T) {
	for _, engine := range []*v1alpha1.EngineSpec{nil, {Provider: v1alpha1.EngineProviderFlux}} {
		f := &fakeCluster{store: map[string]*unstructured.Unstructured{}, readyApply: true}
		a := testApplier(f)
		if err := a.InstallEngine(t.Context(), engine, testSubstrateObjs(), EngineWait{}); err != nil {
			t.Fatalf("InstallEngine(%v) error = %v", engine, err)
		}
		if _, ok := f.store["GitRepository/flux-system/flux-system"]; ok {
			t.Errorf("source CR applied without a configured source (engine=%v)", engine)
		}
	}
}

// TestInstallEnginePartialSourceApplyRecordsIntent: the inventory records the
// full owned set BEFORE the source apply, so a half-applied source (here the
// Kustomization fails after the GitRepository applied) is still in the
// inventory for `down` to clean, and the apply error propagates.
func TestInstallEnginePartialSourceApplyRecordsIntent(t *testing.T) {
	f := &fakeCluster{
		store:         map[string]*unstructured.Unstructured{},
		readyApply:    true,
		failApplyKind: "Kustomization",
	}
	a := testApplier(f)
	engine := &v1alpha1.EngineSpec{Source: &v1alpha1.EngineSource{
		Kind: v1alpha1.EngineSourceGit, URL: "https://github.com/org/fleet",
		Ref: "main", Path: "./", Interval: "10m",
	}}

	if err := a.InstallEngine(t.Context(), engine, testSubstrateObjs(), EngineWait{}); err == nil {
		t.Fatal("InstallEngine() = nil, want the source apply failure")
	}

	inv := f.store["ConfigMap/"+InventoryNamespace+"/"+InventoryName]
	if inv == nil {
		t.Fatal("no inventory recorded")
	}
	data := nested(t, inv, "data", inventoryKey)
	for _, want := range []string{"GitRepository", "Kustomization"} {
		if !strings.Contains(data, want) {
			t.Errorf("inventory (recorded as intent) missing %s — down could not clean a partial apply:\n%s", want, data)
		}
	}
}
