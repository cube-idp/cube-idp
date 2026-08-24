package bootstrap

import (
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

	inventoryApply := "apply:ConfigMap/" + InventoryNamespace + "/" + InventoryName
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
	f.applyErr = newManifestParseError(nil) // any error; Apply returns the first failure
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
