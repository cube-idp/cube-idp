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
	store         map[string]*unstructured.Unstructured
	calls         []string
	applyErr      error
	getErr        error  // if set, get fails with it (wait error-path tests)
	liveErr       error  // live fails with it while liveErrPolls is non-zero
	liveErrPolls  int    // -1: every live call fails; n>0: the first n fail, then recover
	failApplyKind string // if set, apply of this kind fails (partial-apply tests)
	readyApply    bool   // store kind-set objects with ready status (for wait paths)
}

func newFakeCluster(seed ...*unstructured.Unstructured) *fakeCluster {
	f := &fakeCluster{store: map[string]*unstructured.Unstructured{}}
	for _, o := range seed {
		f.store[objKey(o)] = o
	}
	return f
}

// testApplier builds an Applier over a fake cluster with the fast test poll
// interval and the standard inventory namespace injected.
func testApplier(k cluster) *Applier {
	return &Applier{k: k, interval: time.Millisecond, invNS: InventoryNamespace}
}

func objKey(o *unstructured.Unstructured) string {
	return o.GetKind() + "/" + o.GetNamespace() + "/" + o.GetName()
}

func (f *fakeCluster) apply(_ context.Context, obj *unstructured.Unstructured) error {
	f.calls = append(f.calls, "apply:"+objKey(obj))
	if f.applyErr != nil {
		return f.applyErr
	}
	if f.failApplyKind != "" && obj.GetKind() == f.failApplyKind {
		return newApplyError(obj, errors.New("simulated apply failure"))
	}
	stored := obj
	if f.readyApply {
		stored = readied(obj)
	}
	f.store[objKey(obj)] = stored
	return nil
}

// readied returns a copy of a kind-set object stamped with ready status, so a
// fake with readyApply lets WaitReady pass over just-applied objects.
func readied(o *unstructured.Unstructured) *unstructured.Unstructured {
	c := o.DeepCopy()
	switch c.GetKind() {
	case "Namespace":
		_ = unstructured.SetNestedField(c.Object, "Active", "status", "phase")
	case "CustomResourceDefinition":
		_ = unstructured.SetNestedSlice(c.Object,
			[]any{map[string]any{"type": "Established", "status": "True"}}, "status", "conditions")
	case "Deployment", "StatefulSet":
		_ = unstructured.SetNestedField(c.Object, c.GetGeneration(), "status", "observedGeneration")
		replicas, _, _ := unstructured.NestedInt64(c.Object, "spec", "replicas")
		if replicas == 0 {
			replicas = 1
		}
		_ = unstructured.SetNestedField(c.Object, replicas, "status", "readyReplicas")
	}
	return c
}

func firstCallWithPrefix(calls []string, prefix string) int {
	for i, c := range calls {
		if strings.HasPrefix(c, prefix) {
			return i
		}
	}
	return -1
}

func callIndex(calls []string, want string) int {
	for i, c := range calls {
		if c == want {
			return i
		}
	}
	return -1
}

func (f *fakeCluster) get(_ context.Context, obj *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	f.calls = append(f.calls, "get:"+objKey(obj))
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.lookup(obj)
}

func (f *fakeCluster) live(_ context.Context, obj *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	f.calls = append(f.calls, "live:"+objKey(obj))
	if f.liveErr != nil && f.liveErrPolls != 0 {
		if f.liveErrPolls > 0 {
			f.liveErrPolls--
		}
		return nil, f.liveErr
	}
	return f.lookup(obj)
}

func (f *fakeCluster) lookup(obj *unstructured.Unstructured) (*unstructured.Unstructured, error) {
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
	a := testApplier(f)
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
	a := testApplier(&fakeCluster{store: map[string]*unstructured.Unstructured{}, applyErr: sentinel})
	err := a.Apply(t.Context(), []*unstructured.Unstructured{newNamespace("flux-system", "")})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Apply() error = %v, want %v", err, sentinel)
	}
}
