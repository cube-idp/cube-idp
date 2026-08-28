package bootstrap

import (
	"encoding/json"
	"slices"
	"sort"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// inventoryApplyCall is the seam call every inventory record produces — the
// same string every time, so ordered assertions address records by occurrence.
const inventoryApplyCall = "apply:ConfigMap/" + testInvNS + "/" + InventoryName

// testUnits is the ordered three-flavor prerequisite list the sequence tests
// run over: a raw unit whose Namespace the kind-set wait observes, an inert
// Secret whose apply is its own readiness, and a CR judged by the injected
// predicate. Nothing about these shapes is bootstrap's knowledge — the edge
// declares each unit's flavor.
func testUnits(judge ReconciledFunc) []Unit {
	return []Unit{
		NewRawUnit("gateway-namespace", []*unstructured.Unstructured{newNamespace("gateway-system", "")}),
		NewInertUnit("gateway-ca", []*unstructured.Unstructured{newSecret("gateway-ca", "gateway-system")}),
		NewCRUnit("gateway", []*unstructured.Unstructured{newHelmRelease("gateway", "gateway-system")}, judge),
	}
}

// testUnitInstall composes the full M11 bootstrap run: substrate, the three
// prerequisite units, and the driver's wiring under one judgment.
func testUnitInstall(judge ReconciledFunc) EngineInstall {
	return EngineInstall{
		Substrate:     testSubstrateObjs(),
		Prerequisites: testUnits(judge),
		Wiring:        testWiringObjs(),
		Wait:          EngineWait{Reconciled: judge},
	}
}

// TestInstallEngineUnitSequence pins the total order of an M11 run: the
// substrate installs and its kind-set is waited, then each prerequisite unit
// records the cumulative inventory BEFORE applying, applies, and runs its own
// declared wait before the next unit begins — then the wiring lands, and only
// last are the declared engine objects polled. The inert unit is the shape of
// the rule: recorded and applied like the others, waited by nobody.
func TestInstallEngineUnitSequence(t *testing.T) {
	// The declared engine object is present because tier 1 delivered it —
	// bootstrap polls it, never applies it.
	engObj := newDeployment("argocd-server", "argocd", 1, 1, 1)
	f := &fakeCluster{store: map[string]*unstructured.Unstructured{objKey(engObj): engObj}, readyApply: true}
	a := testApplier(f)
	in := testUnitInstall(alwaysReconciled)
	in.Wait.EngineObjects = []*unstructured.Unstructured{engObj}
	if err := a.InstallEngine(t.Context(), in); err != nil {
		t.Fatalf("InstallEngine() error = %v", err)
	}

	steps := []struct {
		label string
		index int
	}{
		{"substrate apply", callIndex(f.calls, "apply:Deployment/flux-system/source-controller")},
		{"substrate record", nthCallIndex(f.calls, inventoryApplyCall, 0)},
		{"substrate kind-set wait", firstCallWithPrefix(f.calls, "get:")},
		{"unit1 record", nthCallIndex(f.calls, inventoryApplyCall, 1)},
		{"unit1 apply", callIndex(f.calls, "apply:Namespace//gateway-system")},
		{"unit1 kind-set wait", callIndex(f.calls, "get:Namespace//gateway-system")},
		{"unit2 record", nthCallIndex(f.calls, inventoryApplyCall, 2)},
		{"unit2 apply", callIndex(f.calls, "apply:Secret/gateway-system/gateway-ca")},
		{"unit3 record", nthCallIndex(f.calls, inventoryApplyCall, 3)},
		{"unit3 apply", callIndex(f.calls, "apply:HelmRelease/gateway-system/gateway")},
		{"unit3 reconciliation wait", callIndex(f.calls, "live:HelmRelease/gateway-system/gateway")},
		{"wiring record", nthCallIndex(f.calls, inventoryApplyCall, 4)},
		{"wiring apply (GitRepository)", callIndex(f.calls, "apply:GitRepository/flux-system/flux-system")},
		{"wiring apply (Kustomization)", callIndex(f.calls, "apply:Kustomization/flux-system/flux-system")},
		{"wiring reconciliation wait (GitRepository)", callIndex(f.calls, "live:GitRepository/flux-system/flux-system")},
		{"wiring reconciliation wait (Kustomization)", callIndex(f.calls, "live:Kustomization/flux-system/flux-system")},
		{"declared engine objects wait", callIndex(f.calls, "live:Deployment/argocd/argocd-server")},
	}
	prev := -1
	for _, s := range steps {
		if s.index < 0 {
			t.Fatalf("%s never happened; calls=%v", s.label, f.calls)
		}
		if s.index < prev {
			t.Fatalf("%s ran out of order (index %d, after %d); calls=%v", s.label, s.index, prev, f.calls)
		}
		prev = s.index
	}

	// The inert unit is never polled: no status exists to observe.
	for _, call := range []string{"get:Secret/gateway-system/gateway-ca", "live:Secret/gateway-system/gateway-ca"} {
		if callIndex(f.calls, call) != -1 {
			t.Errorf("inert unit must not be waited (%s); calls=%v", call, f.calls)
		}
	}
}

// The exact object references each step of the run adds to the inventory.
var (
	refSubstrateNS   = ObjectRef{APIVersion: "v1", Kind: "Namespace", Name: "flux-system"}
	refSubstrateDep  = ObjectRef{APIVersion: "apps/v1", Kind: "Deployment", Namespace: "flux-system", Name: "source-controller"}
	refUnitNS        = ObjectRef{APIVersion: "v1", Kind: "Namespace", Name: "gateway-system"}
	refUnitSecret    = ObjectRef{APIVersion: "v1", Kind: "Secret", Namespace: "gateway-system", Name: "gateway-ca"}
	refUnitHelm      = ObjectRef{APIVersion: "helm.toolkit.fluxcd.io/v2", Kind: "HelmRelease", Namespace: "gateway-system", Name: "gateway"}
	refWiringGit     = ObjectRef{APIVersion: "source.toolkit.fluxcd.io/v1", Kind: "GitRepository", Namespace: "flux-system", Name: "flux-system"}
	refWiringKustomz = ObjectRef{APIVersion: "kustomize.toolkit.fluxcd.io/v1", Kind: "Kustomization", Namespace: "flux-system", Name: "flux-system"}
)

// TestInstallEngineCumulativeInventory pins the recoverability contract across
// the whole run by decoding every snapshot and comparing the COMPLETE set:
// each record is exactly what is owned by that point, so `down` can clean a run
// that failed anywhere. Exact sets — not substrings — are what reject a
// non-cumulative implementation, a duplicated entry, or an object recorded
// before the step that owns it.
func TestInstallEngineCumulativeInventory(t *testing.T) {
	f := &fakeCluster{store: map[string]*unstructured.Unstructured{}, readyApply: true}
	a := testApplier(f)
	if err := a.InstallEngine(t.Context(), testUnitInstall(alwaysReconciled)); err != nil {
		t.Fatalf("InstallEngine() error = %v", err)
	}

	substrate := []ObjectRef{refSubstrateNS, refSubstrateDep}
	want := [][]ObjectRef{
		substrate,
		append(slices.Clone(substrate), refUnitNS),
		append(slices.Clone(substrate), refUnitNS, refUnitSecret),
		append(slices.Clone(substrate), refUnitNS, refUnitSecret, refUnitHelm),
		append(slices.Clone(substrate), refUnitNS, refUnitSecret, refUnitHelm, refWiringGit, refWiringKustomz),
	}
	if len(f.inventories) != len(want) {
		t.Fatalf("recorded %d inventories, want %d (substrate, one per unit, wiring)", len(f.inventories), len(want))
	}
	for i, wantRefs := range want {
		got := sortedRefs(decodeSnapshot(t, f.inventories[i]))
		if !slices.Equal(got, sortedRefs(wantRefs)) {
			t.Errorf("inventory record %d is not the exact owned set:\n got %v\nwant %v", i, got, sortedRefs(wantRefs))
		}
	}
}

// decodeSnapshot parses one recorded inventory payload back into the object
// references it claims are owned. It takes the payload the fake captured at
// record time, not the stored ConfigMap, so each snapshot is read as it was.
func decodeSnapshot(t *testing.T, payload string) []ObjectRef {
	t.Helper()
	var refs []ObjectRef
	if err := json.Unmarshal([]byte(payload), &refs); err != nil {
		t.Fatalf("decode inventory payload %q: %v", payload, err)
	}
	return refs
}

// sortedRefs orders references the way the inventory does, so two sets compare
// independently of the order they were recorded in.
func sortedRefs(refs []ObjectRef) []ObjectRef {
	out := slices.Clone(refs)
	sort.Slice(out, func(i, j int) bool { return refKey(out[i]) < refKey(out[j]) })
	return out
}

// TestInstallEngineNoPrerequisites pins the empty list as a true no-op: with
// no units injected the call sequence is byte-identical to the pre-M11 run —
// substrate apply, record, kind-set wait, wiring record, wiring apply, wiring
// poll — with no extra record in between.
func TestInstallEngineNoPrerequisites(t *testing.T) {
	f := &fakeCluster{store: map[string]*unstructured.Unstructured{}, readyApply: true}
	a := testApplier(f)
	in := EngineInstall{
		Substrate: testSubstrateObjs(),
		Wiring:    testWiringObjs(),
		Wait:      EngineWait{Reconciled: alwaysReconciled},
	}
	if err := a.InstallEngine(t.Context(), in); err != nil {
		t.Fatalf("InstallEngine() error = %v", err)
	}

	want := []string{
		"apply:Namespace//flux-system",
		"apply:Deployment/flux-system/source-controller",
		inventoryApplyCall,
		"get:Namespace//flux-system",
		"get:Deployment/flux-system/source-controller",
		inventoryApplyCall,
		"apply:GitRepository/flux-system/flux-system",
		"apply:Kustomization/flux-system/flux-system",
		"live:GitRepository/flux-system/flux-system",
		"live:Kustomization/flux-system/flux-system",
	}
	if !slices.Equal(f.calls, want) {
		t.Errorf("call sequence changed with no prerequisites injected:\n got %v\nwant %v", f.calls, want)
	}
}

// TestInstallEngineNeverAppendsIntoCallerSlices pins the accumulation as
// allocation, not in-place growth: the caller's Substrate and unit slices are
// handed in with spare capacity holding a sentinel, and the run must leave both
// the lengths and the spare slots untouched. An append into a caller-owned
// backing array would silently overwrite whatever the edge kept there.
func TestInstallEngineNeverAppendsIntoCallerSlices(t *testing.T) {
	sentinel := newNamespace("sentinel", "Active")

	substrate := make([]*unstructured.Unstructured, 0, 4)
	substrate = append(substrate, testSubstrateObjs()...)
	substrateSpare := substrate[:cap(substrate)]
	substrateSpare[2], substrateSpare[3] = sentinel, sentinel

	unitObjs := make([]*unstructured.Unstructured, 0, 2)
	unitObjs = append(unitObjs, newNamespace("gateway-system", ""))
	unitSpare := unitObjs[:cap(unitObjs)]
	unitSpare[1] = sentinel

	f := &fakeCluster{store: map[string]*unstructured.Unstructured{}, readyApply: true}
	a := testApplier(f)
	in := EngineInstall{
		Substrate:     substrate,
		Prerequisites: []Unit{NewRawUnit("gateway-namespace", unitObjs)},
		Wiring:        testWiringObjs(),
		Wait:          EngineWait{Reconciled: alwaysReconciled},
	}
	if err := a.InstallEngine(t.Context(), in); err != nil {
		t.Fatalf("InstallEngine() error = %v", err)
	}

	if len(substrate) != 2 || len(unitObjs) != 1 {
		t.Errorf("caller slice lengths changed: substrate=%d unit=%d", len(substrate), len(unitObjs))
	}
	for i, spare := range map[string][]*unstructured.Unstructured{"substrate": substrateSpare[2:], "unit": unitSpare[1:]} {
		for j, o := range spare {
			if o != sentinel {
				t.Errorf("%s spare capacity slot %d was overwritten (%v) — the cumulative set must never append into a caller's backing array", i, j, o)
			}
		}
	}
}
