package bootstrap

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

// alwaysReconciled is the trivial passing judgment.
func alwaysReconciled(*unstructured.Unstructured) (bool, string, error) { return true, "", nil }

// TestWaitReconciledAllReconciled returns promptly when the judgment passes
// every object.
func TestWaitReconciledAllReconciled(t *testing.T) {
	obj := newDeployment("source-controller", "flux-system", 1, 1, 1)
	a := testApplier(newFakeCluster(obj))
	if err := a.WaitReconciled(t.Context(), []*unstructured.Unstructured{obj}, alwaysReconciled); err != nil {
		t.Fatalf("WaitReconciled() error = %v, want nil", err)
	}
}

// TestWaitReconciledSkipsWithoutInputs: a nil judgment (none injected — the
// degenerate wiring) or an empty object list is a no-op that never polls.
func TestWaitReconciledSkipsWithoutInputs(t *testing.T) {
	obj := newDeployment("source-controller", "flux-system", 1, 1, 1)
	f := newFakeCluster(obj)
	a := testApplier(f)
	if err := a.WaitReconciled(t.Context(), []*unstructured.Unstructured{obj}, nil); err != nil {
		t.Fatalf("WaitReconciled(nil judge) error = %v, want nil", err)
	}
	if err := a.WaitReconciled(t.Context(), nil, alwaysReconciled); err != nil {
		t.Fatalf("WaitReconciled(no objects) error = %v, want nil", err)
	}
	if n := firstCallWithPrefix(f.calls, "live:"); n != -1 {
		t.Errorf("skipped waits must not poll; calls=%v", f.calls)
	}
}

// TestWaitReconciledTimeout reports CUBE-BST-009 naming the pending object
// with the judgment's reason.
func TestWaitReconciledTimeout(t *testing.T) {
	obj := newDeployment("source-controller", "flux-system", 1, 1, 1)
	a := testApplier(newFakeCluster(obj))
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
	defer cancel()

	judge := func(*unstructured.Unstructured) (bool, string, error) {
		return false, "artifact not ready", nil
	}
	err := a.WaitReconciled(ctx, []*unstructured.Unstructured{obj}, judge)
	assertCode(t, err, CodeReconcileTimeout)
	for _, want := range []string{"source-controller", "artifact not ready"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should contain %q", err, want)
		}
	}
}

// TestWaitReconciledCodedJudgmentPassesThrough pins the no-retag rule: a
// judgment error that already carries a cubeerr code (the driver's own) keeps
// it — never rewrapped as timeout or poll failure.
func TestWaitReconciledCodedJudgmentPassesThrough(t *testing.T) {
	obj := newDeployment("source-controller", "flux-system", 1, 1, 1)
	a := testApplier(newFakeCluster(obj))
	driverErr := cubeerr.Wrap(cubeerr.Code("CUBE-ENG-005"), "unrecognized object", "none", nil)

	judge := func(*unstructured.Unstructured) (bool, string, error) { return false, "", driverErr }
	err := a.WaitReconciled(t.Context(), []*unstructured.Unstructured{obj}, judge)
	if !errors.Is(err, driverErr) {
		t.Fatalf("WaitReconciled() = %v, want the driver's coded error passed through", err)
	}
	assertCode(t, err, cubeerr.Code("CUBE-ENG-005"))
}

// TestWaitReconciledUncodedJudgmentIsPollFailure: a judgment failure with no
// code of its own is coded CUBE-BST-010 at the failure point.
func TestWaitReconciledUncodedJudgmentIsPollFailure(t *testing.T) {
	obj := newDeployment("source-controller", "flux-system", 1, 1, 1)
	a := testApplier(newFakeCluster(obj))
	judge := func(*unstructured.Unstructured) (bool, string, error) {
		return false, "", errors.New("judgment boom")
	}
	assertCode(t, a.WaitReconciled(t.Context(), []*unstructured.Unstructured{obj}, judge), CodePollFailed)
}

// TestWaitReconciledNotFoundIsPending: an object not yet created (Flux has not
// delivered it) keeps the wait polling to the deadline — CUBE-BST-009, never a
// terminal read error.
func TestWaitReconciledNotFoundIsPending(t *testing.T) {
	obj := newDeployment("argocd-server", "argocd", 1, 1, 1)
	a := testApplier(newFakeCluster()) // empty store: every live read is NotFound
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
	defer cancel()

	err := a.WaitReconciled(ctx, []*unstructured.Unstructured{obj}, alwaysReconciled)
	assertCode(t, err, CodeReconcileTimeout)
	if !strings.Contains(err.Error(), "not created yet") {
		t.Errorf("error %q should carry the pending reason", err)
	}
}

// TestWaitReconciledNoMatchIsPending: a kind the cluster does not serve yet
// (its CRD still arriving through the source) is pending until the deadline —
// CUBE-BST-009, never CUBE-BST-003 or CUBE-BST-010.
func TestWaitReconciledNoMatchIsPending(t *testing.T) {
	obj := newDeployment("argocd-server", "argocd", 1, 1, 1)
	f := newFakeCluster(obj)
	f.liveErr = &meta.NoKindMatchError{GroupKind: schema.GroupKind{Group: "argoproj.io", Kind: "Application"}}
	f.liveErrPolls = -1
	a := testApplier(f)
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
	defer cancel()

	err := a.WaitReconciled(ctx, []*unstructured.Unstructured{obj}, alwaysReconciled)
	assertCode(t, err, CodeReconcileTimeout)
	if !strings.Contains(err.Error(), "kind not served") {
		t.Errorf("error %q should carry the pending reason", err)
	}
}

// TestWaitReconciledNoMatchRecovers pins the novel C4 behavior end to end: a
// kind unserved for several polls (its CRD arriving through the source)
// stays pending, then reconciles once discovery finds it.
func TestWaitReconciledNoMatchRecovers(t *testing.T) {
	obj := newDeployment("argocd-server", "argocd", 1, 1, 1)
	f := newFakeCluster(obj)
	f.liveErr = &meta.NoKindMatchError{GroupKind: schema.GroupKind{Group: "argoproj.io", Kind: "Application"}}
	f.liveErrPolls = 3
	a := testApplier(f)

	if err := a.WaitReconciled(t.Context(), []*unstructured.Unstructured{obj}, alwaysReconciled); err != nil {
		t.Fatalf("WaitReconciled() error = %v, want nil once the kind is served", err)
	}
}

// TestWaitReconciledPermanentReadError: a read failure waiting cannot fix is
// CUBE-BST-010 immediately, not a timeout.
func TestWaitReconciledPermanentReadError(t *testing.T) {
	obj := newDeployment("source-controller", "flux-system", 1, 1, 1)
	f := newFakeCluster(obj)
	f.liveErr = apierrors.NewForbidden(schema.GroupResource{Resource: "deployments"}, "source-controller", errors.New("rbac"))
	f.liveErrPolls = -1
	a := testApplier(f)
	assertCode(t, a.WaitReconciled(t.Context(), []*unstructured.Unstructured{obj}, alwaysReconciled), CodePollFailed)
}

// TestWaitReconciledRecovers: an object pending on early polls reconciles once
// the judgment flips — the wait ends nil.
func TestWaitReconciledRecovers(t *testing.T) {
	obj := newDeployment("source-controller", "flux-system", 1, 1, 1)
	a := testApplier(newFakeCluster(obj))
	polls := 0
	judge := func(*unstructured.Unstructured) (bool, string, error) {
		polls++
		return polls >= 3, "still fetching", nil
	}
	if err := a.WaitReconciled(t.Context(), []*unstructured.Unstructured{obj}, judge); err != nil {
		t.Fatalf("WaitReconciled() error = %v, want nil after recovery", err)
	}
}

// TestInstallEngineThreePhases drives the full sequence: kind-set wait, then
// the applied sync wiring polled reconciled (phase 2), then the declared
// engine object polled by identity (phase 3) — present in the cluster because
// tier 1 delivered it, but never applied by bootstrap.
func TestInstallEngineThreePhases(t *testing.T) {
	engObj := newDeployment("argocd-server", "argocd", 1, 1, 1)
	f := &fakeCluster{store: map[string]*unstructured.Unstructured{objKey(engObj): engObj}, readyApply: true}
	a := testApplier(f)
	engine := &v1alpha1.EngineSpec{
		Provider: v1alpha1.EngineProviderFlux,
		Source: &v1alpha1.EngineSource{
			Kind: v1alpha1.EngineSourceGit, URL: "https://github.com/org/fleet",
			Ref: "main", Path: "./", Interval: "10m",
		},
	}
	w := EngineWait{Reconciled: alwaysReconciled, EngineObjects: []*unstructured.Unstructured{engObj}}
	if err := a.InstallEngine(t.Context(), engine, testSubstrateObjs(), w); err != nil {
		t.Fatalf("InstallEngine() error = %v", err)
	}

	kindSetWait := firstCallWithPrefix(f.calls, "get:")
	inventoryKeyStr := "apply:ConfigMap/" + InventoryNamespace + "/" + InventoryName
	reRecord := lastCallIndex(f.calls, inventoryKeyStr)
	gitApply := callIndex(f.calls, "apply:GitRepository/flux-system/flux-system")
	gitPoll := callIndex(f.calls, "live:GitRepository/flux-system/flux-system")
	kzPoll := callIndex(f.calls, "live:Kustomization/flux-system/flux-system")
	engPoll := callIndex(f.calls, "live:Deployment/argocd/argocd-server")

	if kindSetWait < 0 || gitApply < kindSetWait {
		t.Errorf("phase 1 (kind-set wait) must precede the wiring apply; calls=%v", f.calls)
	}
	if reRecord < 0 || gitApply < reRecord {
		t.Errorf("the re-recorded inventory must precede the wiring apply; calls=%v", f.calls)
	}
	if gitApply < 0 || gitPoll < gitApply || kzPoll < gitApply {
		t.Errorf("phase 2 must poll both wiring objects after applying them; calls=%v", f.calls)
	}
	if engPoll < gitPoll || engPoll < kzPoll {
		t.Errorf("phase 3 must start after the last phase-2 poll; calls=%v", f.calls)
	}
	if callIndex(f.calls, "apply:"+objKey(engObj)) != -1 {
		t.Errorf("declared engine objects must never be applied by bootstrap; calls=%v", f.calls)
	}
}

// lastCallIndex returns the index of the final occurrence of want in calls.
func lastCallIndex(calls []string, want string) int {
	last := -1
	for i, c := range calls {
		if c == want {
			last = i
		}
	}
	return last
}

// TestInstallEngineSharedTimeoutBudget pins the one-total-budget rule: when
// phase 2 consumes the whole deadline, phase 3 gets nothing — the declared
// engine object is never polled and the failure is phase 2's CUBE-BST-009.
func TestInstallEngineSharedTimeoutBudget(t *testing.T) {
	engObj := newDeployment("argocd-server", "argocd", 1, 1, 1)
	f := &fakeCluster{store: map[string]*unstructured.Unstructured{objKey(engObj): engObj}, readyApply: true}
	a := testApplier(f)
	engine := &v1alpha1.EngineSpec{
		Provider: v1alpha1.EngineProviderFlux,
		Source: &v1alpha1.EngineSource{
			Kind: v1alpha1.EngineSourceGit, URL: "https://github.com/org/fleet",
			Ref: "main", Path: "./", Interval: "10m",
		},
	}
	neverReconciled := func(*unstructured.Unstructured) (bool, string, error) {
		return false, "artifact not ready", nil
	}
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
	defer cancel()

	err := a.InstallEngine(ctx, engine, testSubstrateObjs(), EngineWait{
		Reconciled:    neverReconciled,
		EngineObjects: []*unstructured.Unstructured{engObj},
	})
	assertCode(t, err, CodeReconcileTimeout)
	if callIndex(f.calls, "live:"+objKey(engObj)) != -1 {
		t.Errorf("phase 3 must not run once phase 2 exhausted the shared budget; calls=%v", f.calls)
	}
}

// TestInstallEngineSkipsEmptyPhases: with no judgment and no engine objects
// (the degenerate flux wiring today) InstallEngine never polls reconciliation.
func TestInstallEngineSkipsEmptyPhases(t *testing.T) {
	f := &fakeCluster{store: map[string]*unstructured.Unstructured{}, readyApply: true}
	a := testApplier(f)
	if err := a.InstallEngine(t.Context(), &v1alpha1.EngineSpec{}, testSubstrateObjs(), EngineWait{}); err != nil {
		t.Fatalf("InstallEngine() error = %v", err)
	}
	if n := firstCallWithPrefix(f.calls, "live:"); n != -1 {
		t.Errorf("no reconciliation inputs: nothing must be polled; calls=%v", f.calls)
	}
}
