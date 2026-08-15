package bootstrap

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

var (
	gvkNamespace  = schema.GroupVersionKind{Version: "v1", Kind: "Namespace"}
	gvkDeployment = schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}
	gvkCRD        = schema.GroupVersionKind{Group: "apiextensions.k8s.io", Version: "v1", Kind: "CustomResourceDefinition"}
)

// fakeCluster is a hand-rolled in-memory cluster seam (CLAUDE.md: mocks are
// hand-rolled function-field structs) — the client-go fake dynamic client
// cannot model server-side apply for unstructured objects.
type fakeCluster struct {
	store    map[string]*unstructured.Unstructured
	applyErr error
}

func newFakeCluster(seed ...*unstructured.Unstructured) *fakeCluster {
	f := &fakeCluster{store: map[string]*unstructured.Unstructured{}}
	for _, o := range seed {
		f.store[objKey(o)] = o
	}
	return f
}

func objKey(o *unstructured.Unstructured) string {
	return o.GetKind() + "/" + o.GetNamespace() + "/" + o.GetName()
}

func (f *fakeCluster) apply(_ context.Context, obj *unstructured.Unstructured) error {
	if f.applyErr != nil {
		return f.applyErr
	}
	f.store[objKey(obj)] = obj
	return nil
}

func (f *fakeCluster) get(_ context.Context, obj *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	if o, ok := f.store[objKey(obj)]; ok {
		return o, nil
	}
	return nil, apierrors.NewNotFound(schema.GroupResource{Resource: obj.GetKind()}, obj.GetName())
}

func newNamespace(name, phase string) *unstructured.Unstructured {
	o := &unstructured.Unstructured{}
	o.SetGroupVersionKind(gvkNamespace)
	o.SetName(name)
	if phase != "" {
		_ = unstructured.SetNestedField(o.Object, phase, "status", "phase")
	}
	return o
}

func newCRD(name string, established bool) *unstructured.Unstructured {
	o := &unstructured.Unstructured{}
	o.SetGroupVersionKind(gvkCRD)
	o.SetName(name)
	if established {
		_ = unstructured.SetNestedSlice(o.Object,
			[]any{map[string]any{"type": "Established", "status": "True"}},
			"status", "conditions")
	}
	return o
}

func newDeployment(name, namespace string, gen, ready, replicas int64) *unstructured.Unstructured {
	o := &unstructured.Unstructured{}
	o.SetGroupVersionKind(gvkDeployment)
	o.SetName(name)
	o.SetNamespace(namespace)
	o.SetGeneration(gen)
	_ = unstructured.SetNestedField(o.Object, replicas, "spec", "replicas")
	_ = unstructured.SetNestedField(o.Object, gen, "status", "observedGeneration")
	_ = unstructured.SetNestedField(o.Object, ready, "status", "readyReplicas")
	return o
}

func assertCode(t *testing.T, err error, want cubeerr.Code) {
	t.Helper()
	var coded *cubeerr.Coded
	if !errors.As(err, &coded) || coded.Code != want {
		t.Fatalf("error = %v, want code %s", err, want)
	}
}

// TestApplyAppliesEveryObject checks Apply drives the seam once per object.
func TestApplyAppliesEveryObject(t *testing.T) {
	f := newFakeCluster()
	a := &Applier{k: f, interval: time.Millisecond}
	objs := []*unstructured.Unstructured{
		newNamespace("flux-system", ""),
		newCRD("gitrepositories.source.toolkit.fluxcd.io", false),
		newDeployment("source-controller", "flux-system", 1, 0, 1),
	}
	if err := a.Apply(t.Context(), objs); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if len(f.store) != len(objs) {
		t.Errorf("applied %d objects, want %d", len(f.store), len(objs))
	}
}

// TestApplyPropagatesError stops and returns the first apply failure.
func TestApplyPropagatesError(t *testing.T) {
	sentinel := errors.New("apply boom")
	a := &Applier{k: &fakeCluster{store: map[string]*unstructured.Unstructured{}, applyErr: sentinel}, interval: time.Millisecond}
	err := a.Apply(t.Context(), []*unstructured.Unstructured{newNamespace("flux-system", "")})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Apply() error = %v, want %v", err, sentinel)
	}
}

// TestWaitReadyAllReady returns promptly when every kind-set object is ready.
func TestWaitReadyAllReady(t *testing.T) {
	objs := []*unstructured.Unstructured{
		newNamespace("flux-system", "Active"),
		newCRD("gitrepositories.source.toolkit.fluxcd.io", true),
		newDeployment("source-controller", "flux-system", 1, 1, 1),
	}
	a := &Applier{k: newFakeCluster(objs...), interval: time.Millisecond}
	if err := a.WaitReady(t.Context(), objs); err != nil {
		t.Fatalf("WaitReady() error = %v, want nil", err)
	}
}

// TestWaitReadyTimeout reports CUBE-BST-005 naming a workload that never
// reaches its ready replica count.
func TestWaitReadyTimeout(t *testing.T) {
	notReady := newDeployment("source-controller", "flux-system", 1, 0, 1)
	a := &Applier{k: newFakeCluster(notReady), interval: time.Millisecond}
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
	defer cancel()

	err := a.WaitReady(ctx, []*unstructured.Unstructured{notReady})
	assertCode(t, err, CodeWaitTimeout)
	if !strings.Contains(err.Error(), "source-controller") {
		t.Errorf("wait error %q should name the pending object", err)
	}
}

// TestWaitReadyMissingObjectTimesOut covers the not-yet-created path: an
// absent object is not ready, so the wait keeps polling until ctx is done.
func TestWaitReadyMissingObjectTimesOut(t *testing.T) {
	obj := newNamespace("flux-system", "")
	a := &Applier{k: newFakeCluster(), interval: time.Millisecond}
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
	a := &Applier{k: newFakeCluster(), interval: time.Millisecond}
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
			a := &Applier{k: newFakeCluster(tt.obj), interval: time.Millisecond}
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
