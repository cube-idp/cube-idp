package bootstrap

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// TestWaitReadyAllReady returns promptly when every kind-set object is ready.
func TestWaitReadyAllReady(t *testing.T) {
	objs := []*unstructured.Unstructured{
		newNamespace("flux-system", "Active"),
		newCRD("gitrepositories.source.toolkit.fluxcd.io", true),
		newDeployment("source-controller", "flux-system", 1, 1, 1),
	}
	a := testApplier(newFakeCluster(objs...))
	if err := a.WaitReady(t.Context(), objs); err != nil {
		t.Fatalf("WaitReady() error = %v, want nil", err)
	}
}

// TestWaitReadyTimeout reports CUBE-BST-005 naming a workload that never
// reaches its ready replica count.
func TestWaitReadyTimeout(t *testing.T) {
	notReady := newDeployment("source-controller", "flux-system", 1, 0, 1)
	a := testApplier(newFakeCluster(notReady))
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
	defer cancel()

	err := a.WaitReady(ctx, []*unstructured.Unstructured{notReady})
	assertCode(t, err, CodeWaitTimeout)
	if !strings.Contains(err.Error(), "source-controller") {
		t.Errorf("wait error %q should name the pending object", err)
	}
}

// TestWaitReadyPreservesCodedError pins the no-retag rule (#155): an error
// already carrying a cubeerr code (here a CUBE-BST-003 mapping miss surfacing
// mid-wait) passes through WaitReady unchanged instead of being rewrapped in
// CUBE-BST-005.
func TestWaitReadyPreservesCodedError(t *testing.T) {
	obj := newDeployment("source-controller", "flux-system", 1, 0, 1)
	f := newFakeCluster(obj)
	mappingErr := newMappingError(obj, errors.New("no matches for kind"))
	f.getErr = mappingErr
	a := testApplier(f)

	err := a.WaitReady(t.Context(), []*unstructured.Unstructured{obj})
	assertCode(t, err, CodeRESTMapping)
	if !errors.Is(err, mappingErr) {
		t.Errorf("WaitReady() = %v, want the coded cause passed through unchanged", err)
	}
}

// TestWaitReadyCodesPermanentErrorAtFailurePoint pins the other branch: a
// readiness read that fails with an ordinary (uncoded) error is CUBE-BST-010
// at the failure point — not the CUBE-BST-005 timeout it used to be rewrapped
// as. The generous deadline proves the code arrives from the failure, not from
// a deadline: a timeout could not fire inside it.
func TestWaitReadyCodesPermanentErrorAtFailurePoint(t *testing.T) {
	obj := newDeployment("source-controller", "flux-system", 1, 0, 1)
	f := newFakeCluster(obj)
	f.getErr = errors.New("connection refused")
	a := testApplier(f)

	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	start := time.Now()
	err := a.WaitReady(ctx, []*unstructured.Unstructured{obj})
	assertCode(t, err, CodePollFailed)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("WaitReady() took %s — the error must surface at the failure point, not at the deadline", elapsed)
	}
}

// TestWaitReadyMissingObjectTimesOut covers the not-yet-created path: an
// absent object is not ready, so the wait keeps polling until ctx is done.
func TestWaitReadyMissingObjectTimesOut(t *testing.T) {
	obj := newNamespace("flux-system", "")
	a := testApplier(newFakeCluster())
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
	defer cancel()
	assertCode(t, a.WaitReady(ctx, []*unstructured.Unstructured{obj}), CodeWaitTimeout)
}

// TestWaitReadySkipsNonKindSet ignores objects outside the bootstrap kind-set
// (their readiness is the engine's concern), so a lone Service is "ready".
func TestWaitReadySkipsNonKindSet(t *testing.T) {
	svc := &unstructured.Unstructured{}
	svc.SetGroupVersionKind(schema.GroupVersionKind{Version: "v1", Kind: "Service"})
	svc.SetName("source-controller")
	svc.SetNamespace("flux-system")
	a := testApplier(newFakeCluster())
	if err := a.WaitReady(t.Context(), []*unstructured.Unstructured{svc}); err != nil {
		t.Fatalf("WaitReady() error = %v, want nil (non-kind-set object must be skipped)", err)
	}
}

// TestWaitReadyPredicates pins each kind's readiness predicate through the
// exported wait path, ready and not-ready rows side by side.
func TestWaitReadyPredicates(t *testing.T) {
	tests := []struct {
		name  string
		obj   *unstructured.Unstructured
		ready bool
	}{
		{"namespace active", newNamespace("flux-system", "Active"), true},
		{"namespace terminating", newNamespace("flux-system", "Terminating"), false},
		{"crd established", newCRD("gitrepositories.x", true), true},
		{"crd not established", newCRD("gitrepositories.x", false), false},
		{"deployment ready", newDeployment("sc", "flux-system", 2, 1, 1), true},
		{"deployment under-replicated", newDeployment("sc", "flux-system", 2, 0, 1), false},
		{"deployment stale generation", staleGeneration(newDeployment("sc", "flux-system", 3, 1, 1)), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := testApplier(newFakeCluster(tt.obj))
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
			defer cancel()
			err := a.WaitReady(ctx, []*unstructured.Unstructured{tt.obj})
			if tt.ready && err != nil {
				t.Fatalf("WaitReady() = %v, want ready", err)
			}
			if !tt.ready && err == nil {
				t.Fatal("WaitReady() = nil, want not-ready timeout")
			}
		})
	}
}

// staleGeneration rewinds status.observedGeneration behind metadata.generation.
func staleGeneration(o *unstructured.Unstructured) *unstructured.Unstructured {
	_ = unstructured.SetNestedField(o.Object, o.GetGeneration()-1, "status", "observedGeneration")
	return o
}
