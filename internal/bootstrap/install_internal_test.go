package bootstrap

import (
	"errors"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// TestInstallOrder pins the recoverability contract: every object is applied,
// then the inventory is recorded, then readiness is polled — apply-before-
// inventory-before-wait, never interleaved out of order.
func TestInstallOrder(t *testing.T) {
	objs := []*unstructured.Unstructured{
		newNamespace("flux-system", "Active"),
		newDeployment("source-controller", "flux-system", 1, 1, 1),
	}
	f := newFakeCluster()
	a := testApplier(f)
	if err := a.Install(t.Context(), objs); err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	inventoryApply := "apply:ConfigMap/" + testInvNS + "/" + InventoryName
	phase := func(c string) int {
		switch {
		case strings.HasPrefix(c, "get:"):
			return 2 // wait
		case c == inventoryApply:
			return 1 // record inventory
		default:
			return 0 // object apply
		}
	}

	seen := map[int]bool{}
	prev := -1
	for _, c := range f.calls {
		p := phase(c)
		if p < prev {
			t.Fatalf("call %q (phase %d) out of order after phase %d; calls=%v", c, p, prev, f.calls)
		}
		prev = p
		seen[p] = true
	}
	for p, label := range map[int]string{0: "object apply", 1: "inventory record", 2: "readiness wait"} {
		if !seen[p] {
			t.Errorf("Install never performed the %s phase; calls=%v", label, f.calls)
		}
	}
}

// TestInstallStopsOnApplyFailure never records inventory or waits when the
// object apply fails.
func TestInstallStopsOnApplyFailure(t *testing.T) {
	f := newFakeCluster()
	f.applyErr = errors.New("apply refused") // any error; Apply returns the first failure
	a := testApplier(f)
	err := a.Install(t.Context(), []*unstructured.Unstructured{newNamespace("flux-system", "Active")})
	if err == nil {
		t.Fatal("Install() error = nil, want the apply failure")
	}
	for _, c := range f.calls {
		if strings.HasPrefix(c, "get:") {
			t.Errorf("Install waited after an apply failure; calls=%v", f.calls)
		}
	}
}

// TestInstallEngineWithWiring: the substrate installs and becomes ready,
// THEN the injected sync wiring is applied (after the kind-set wait) and the
// inventory includes it.
func TestInstallEngineWithWiring(t *testing.T) {
	f := &fakeCluster{store: map[string]*unstructured.Unstructured{}, readyApply: true}
	a := testApplier(f)
	if err := a.InstallEngine(t.Context(), testSubstrateObjs(), testWiringObjs(), EngineWait{}); err != nil {
		t.Fatalf("InstallEngine() error = %v", err)
	}

	for _, key := range []string{
		"GitRepository/flux-system/flux-system",
		"Kustomization/flux-system/flux-system",
	} {
		if _, ok := f.store[key]; !ok {
			t.Errorf("wiring object %s not applied", key)
		}
	}

	inv := f.store["ConfigMap/"+testInvNS+"/"+InventoryName]
	if inv == nil {
		t.Fatal("no inventory recorded")
	}
	data := nested(t, inv, "data", inventoryKey)
	for _, want := range []string{"GitRepository", "Kustomization"} {
		if !strings.Contains(data, want) {
			t.Errorf("inventory missing %s:\n%s", want, data)
		}
	}

	// Wiring is applied only AFTER the kind-set wait (a get precedes the
	// GitRepository apply) — the CRDs-established prerequisite.
	gitApply := callIndex(f.calls, "apply:GitRepository/flux-system/flux-system")
	firstGet := firstCallWithPrefix(f.calls, "get:")
	if firstGet < 0 || gitApply < firstGet {
		t.Errorf("wiring applied before the kind-set wait; calls=%v", f.calls)
	}
}

// TestInstallEngineNoWiring installs only the substrate when the driver
// emitted no sync wiring (the no-source case, decided at the edge).
func TestInstallEngineNoWiring(t *testing.T) {
	f := &fakeCluster{store: map[string]*unstructured.Unstructured{}, readyApply: true}
	a := testApplier(f)
	if err := a.InstallEngine(t.Context(), testSubstrateObjs(), nil, EngineWait{}); err != nil {
		t.Fatalf("InstallEngine() error = %v", err)
	}
	if _, ok := f.store["GitRepository/flux-system/flux-system"]; ok {
		t.Errorf("wiring applied without any injected; calls=%v", f.calls)
	}
}

// TestInstallEnginePartialWiringApplyRecordsIntent: the inventory records the
// full owned set BEFORE the wiring apply, so a half-applied wiring (here the
// Kustomization fails after the GitRepository applied) is still in the
// inventory for `down` to clean, and the apply error propagates.
func TestInstallEnginePartialWiringApplyRecordsIntent(t *testing.T) {
	f := &fakeCluster{
		store:         map[string]*unstructured.Unstructured{},
		readyApply:    true,
		failApplyKind: "Kustomization",
	}
	a := testApplier(f)

	if err := a.InstallEngine(t.Context(), testSubstrateObjs(), testWiringObjs(), EngineWait{}); err == nil {
		t.Fatal("InstallEngine() = nil, want the wiring apply failure")
	}

	inv := f.store["ConfigMap/"+testInvNS+"/"+InventoryName]
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
