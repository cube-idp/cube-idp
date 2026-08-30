package bootstrap

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

// neverReconciledJudge keeps every polled object pending with a fixed reason.
func neverReconciledJudge(*unstructured.Unstructured) (bool, string, error) {
	return false, "artifact not ready", nil
}

// TestInstallEngineUnitWaitByFlavor pins one wait per flavor: a raw unit times
// out on the kind-set as CUBE-BST-005, a CR unit times out on the injected
// judgment as CUBE-BST-009 and passes when the judgment does, and an inert unit
// is ready on apply — never polled at all. Every timeout names the unit, so a
// shared code still points at one step.
func TestInstallEngineUnitWaitByFlavor(t *testing.T) {
	tests := []struct {
		name        string
		unit        Unit
		wantCode    cubeerr.Code
		wantSubject string
		neverPoll   []string
	}{
		{
			name:        "raw unit never reaches the kind-set",
			unit:        NewRawUnit("gateway-namespace", []*unstructured.Unstructured{newNamespace("gateway-system", "")}),
			wantCode:    CodeWaitTimeout,
			wantSubject: "prerequisite unit gateway-namespace",
		},
		{
			name: "cr unit never reconciles",
			unit: NewCRUnit("gateway", []*unstructured.Unstructured{newHelmRelease("gateway", "gateway-system")},
				neverReconciledJudge),
			wantCode:    CodeReconcileTimeout,
			wantSubject: "prerequisite unit gateway",
		},
		{
			name: "cr unit reconciles",
			unit: NewCRUnit("gateway", []*unstructured.Unstructured{newHelmRelease("gateway", "gateway-system")},
				alwaysReconciled),
		},
		{
			name:      "inert unit is ready on apply",
			unit:      NewInertUnit("gateway-ca", []*unstructured.Unstructured{newSecret("gateway-ca", "gateway-system")}),
			neverPoll: []string{"get:Secret/gateway-system/gateway-ca", "live:Secret/gateway-system/gateway-ca"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeCluster()
			a := testApplier(f)
			ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
			defer cancel()

			err := a.InstallEngine(ctx, EngineInstall{
				Substrate:     testSubstrateObjs(),
				Prerequisites: []Unit{tt.unit},
			})
			if tt.wantCode == "" {
				if err != nil {
					t.Fatalf("InstallEngine() error = %v, want nil", err)
				}
			} else {
				assertCode(t, err, tt.wantCode)
				if !strings.Contains(err.Error(), tt.wantSubject) {
					t.Errorf("error %q must name %q, the prerequisite unit that timed out", err, tt.wantSubject)
				}
			}
			for _, call := range tt.neverPoll {
				if callIndex(f.calls, call) != -1 {
					t.Errorf("unit must not be polled (%s); calls=%v", call, f.calls)
				}
			}
		})
	}
}

// TestInstallEngineUnitDispatchIsDeclaredNotInferred pins the flavor as the
// edge's declaration rather than an inference from the objects' kinds: a
// Deployment inside a CR unit is judged, never kind-set-waited (it would hang
// under-replicated forever), and a HelmRelease inside a raw unit is kind-set
// filtered — not polled at all, and no judgment is consulted for it.
func TestInstallEngineUnitDispatchIsDeclaredNotInferred(t *testing.T) {
	t.Run("declared cr unit judges a kind-set kind", func(t *testing.T) {
		// Under-replicated: a kind-set wait would never clear.
		dep := newDeployment("gateway", "gateway-system", 1, 0, 1)
		f := newFakeCluster()
		a := testApplier(f)
		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()

		judged := 0
		judge := func(*unstructured.Unstructured) (bool, string, error) {
			judged++
			return true, "", nil
		}
		unit := NewCRUnit("gateway", []*unstructured.Unstructured{dep}, judge)
		err := a.InstallEngine(ctx, EngineInstall{Substrate: testSubstrateObjs(), Prerequisites: []Unit{unit}})
		if err != nil {
			t.Fatalf("InstallEngine() error = %v, want the declared judgment to decide", err)
		}
		if judged == 0 {
			t.Error("the injected judgment was never consulted for a declared CR unit")
		}
		if callIndex(f.calls, "get:Deployment/gateway-system/gateway") != -1 {
			t.Errorf("a declared CR unit must not be kind-set-waited; calls=%v", f.calls)
		}
	})

	t.Run("declared raw unit skips a non-kind-set kind", func(t *testing.T) {
		hr := newHelmRelease("gateway", "gateway-system")
		f := newFakeCluster()
		a := testApplier(f)
		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()

		unit := NewRawUnit("gateway", []*unstructured.Unstructured{hr})
		if err := a.InstallEngine(ctx, EngineInstall{Substrate: testSubstrateObjs(), Prerequisites: []Unit{unit}}); err != nil {
			t.Fatalf("InstallEngine() error = %v, want nil (a raw unit outside the kind-set is ready on apply)", err)
		}
		for _, call := range []string{"get:HelmRelease/gateway-system/gateway", "live:HelmRelease/gateway-system/gateway"} {
			if callIndex(f.calls, call) != -1 {
				t.Errorf("a declared raw unit must not poll outside the kind-set (%s); calls=%v", call, f.calls)
			}
		}
	})
}

// TestInstallEngineUnitApplyFailure: a unit's apply failure stops the run
// there — later units are never touched — and the record that preceded it
// already names the failed unit's objects, so `down` can clean what may have
// partially landed.
func TestInstallEngineUnitApplyFailure(t *testing.T) {
	f := &fakeCluster{
		store:         map[string]*unstructured.Unstructured{},
		readyApply:    true,
		failApplyKind: "Secret",
	}
	a := testApplier(f)
	if err := a.InstallEngine(t.Context(), testUnitInstall(alwaysReconciled)); err == nil {
		t.Fatal("InstallEngine() = nil, want the unit's apply failure")
	}

	if callIndex(f.calls, "apply:HelmRelease/gateway-system/gateway") != -1 {
		t.Errorf("a later unit was installed after an apply failure; calls=%v", f.calls)
	}
	if callIndex(f.calls, "apply:GitRepository/flux-system/flux-system") != -1 {
		t.Errorf("the wiring was applied after a unit's apply failure; calls=%v", f.calls)
	}
	if len(f.inventories) == 0 {
		t.Fatal("no inventory recorded")
	}
	last := f.inventories[len(f.inventories)-1]
	if !strings.Contains(last, "gateway-ca") {
		t.Errorf("the record preceding the failed apply must name its objects:\n%s", last)
	}
}

// TestInstallEngineUnitCodedJudgeErrorPassesThrough pins the no-retag rule on
// the unit path: a judgment error already carrying a code (the composer's own,
// raised in the unit's predicate) surfaces untouched — never rewrapped as the
// bootstrap poll failure or the unit's timeout.
func TestInstallEngineUnitCodedJudgeErrorPassesThrough(t *testing.T) {
	f := &fakeCluster{store: map[string]*unstructured.Unstructured{}, readyApply: true}
	a := testApplier(f)
	composerErr := cubeerr.Wrap(cubeerr.Code("CUBE-GWY-002"), "gateway not recognized", "none", nil)
	judge := func(*unstructured.Unstructured) (bool, string, error) { return false, "", composerErr }

	unit := NewCRUnit("gateway", []*unstructured.Unstructured{newHelmRelease("gateway", "gateway-system")}, judge)
	err := a.InstallEngine(t.Context(), EngineInstall{Substrate: testSubstrateObjs(), Prerequisites: []Unit{unit}})
	assertCode(t, err, cubeerr.Code("CUBE-GWY-002"))
	if !errors.Is(err, composerErr) {
		t.Errorf("InstallEngine() = %v, want the composer's coded error passed through unchanged", err)
	}
}

// TestInstallEngineCRUnitWithoutJudgmentFailsPreflight pins the composition
// defect as a hard, early failure: a CR unit built with no judgment would
// otherwise skip its wait and count as ready on apply. It is CUBE-BST-010
// before the first record, so a defective run installs nothing at all — not
// even the units ordered ahead of it.
func TestInstallEngineCRUnitWithoutJudgmentFailsPreflight(t *testing.T) {
	f := newFakeCluster()
	a := testApplier(f)
	units := []Unit{
		NewRawUnit("gateway-namespace", []*unstructured.Unstructured{newNamespace("gateway-system", "")}),
		NewCRUnit("gateway", []*unstructured.Unstructured{newHelmRelease("gateway", "gateway-system")}, nil),
	}

	err := a.InstallEngine(t.Context(), EngineInstall{Substrate: testSubstrateObjs(), Prerequisites: units})
	assertCode(t, err, CodePollFailed)
	if !strings.Contains(err.Error(), "prerequisite unit gateway") {
		t.Errorf("error %q must name the unit whose composition is defective", err)
	}
	if len(f.calls) != 0 {
		t.Errorf("a pre-flight defect must apply and record nothing; calls=%v", f.calls)
	}
}
