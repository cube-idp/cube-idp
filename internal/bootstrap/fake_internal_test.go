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
	gvkNamespace   = schema.GroupVersionKind{Version: "v1", Kind: "Namespace"}
	gvkDeployment  = schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}
	gvkCRD         = schema.GroupVersionKind{Group: "apiextensions.k8s.io", Version: "v1", Kind: "CustomResourceDefinition"}
	gvkSecret      = schema.GroupVersionKind{Version: "v1", Kind: "Secret"}
	gvkHelmRelease = schema.GroupVersionKind{Group: "helm.toolkit.fluxcd.io", Version: "v2", Kind: "HelmRelease"}
)

// fakeCluster is a hand-rolled in-memory cluster seam (CLAUDE.md: mocks are
// hand-rolled function-field structs) — the client-go fake dynamic client
// cannot model server-side apply for unstructured objects.
type fakeCluster struct {
	store         map[string]*unstructured.Unstructured
	calls         []string
	inventories   []string // one payload snapshot per inventory apply, in order
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

// testInvNS is the inventory namespace the tests inject — the Applier
// records wherever it is told; the fact itself lives with the substrate.
const testInvNS = "flux-system"

// testApplier builds an Applier over a fake cluster with the fast test poll
// interval and the test inventory namespace injected.
func testApplier(k cluster) *Applier {
	return &Applier{k: k, interval: time.Millisecond, invNS: testInvNS}
}

func objKey(o *unstructured.Unstructured) string {
	return o.GetKind() + "/" + o.GetNamespace() + "/" + o.GetName()
}

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

// testWiringObjs is hand-rolled sync wiring the InstallEngine tests inject —
// bootstrap applies whatever wiring the edge's driver emitted, and knows
// nothing about its shape.
func testWiringObjs() []*unstructured.Unstructured {
	wiring := func(apiVersion, kind string) *unstructured.Unstructured {
		o := &unstructured.Unstructured{}
		o.SetAPIVersion(apiVersion)
		o.SetKind(kind)
		o.SetNamespace("flux-system")
		o.SetName("flux-system")
		return o
	}
	return []*unstructured.Unstructured{
		wiring("source.toolkit.fluxcd.io/v1", "GitRepository"),
		wiring("kustomize.toolkit.fluxcd.io/v1", "Kustomization"),
	}
}

func (f *fakeCluster) apply(_ context.Context, obj *unstructured.Unstructured) error {
	f.calls = append(f.calls, "apply:"+objKey(obj))
	if f.applyErr != nil {
		return f.applyErr
	}
	if f.failApplyKind != "" && obj.GetKind() == f.failApplyKind {
		return newApplyError(obj, errors.New("simulated apply failure"))
	}
	if obj.GetKind() == "ConfigMap" && obj.GetName() == InventoryName {
		payload, _, _ := unstructured.NestedString(obj.Object, "data", inventoryKey)
		f.inventories = append(f.inventories, payload)
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

// nthCallIndex returns the index of the n-th (0-based) occurrence of want.
// Every inventory record is the same call string, so the ordered sequence
// tests address them by occurrence: 0 is the substrate record, then one per
// prerequisite unit, then the wiring's.
func nthCallIndex(calls []string, want string, n int) int {
	seen := 0
	for i, c := range calls {
		if c != want {
			continue
		}
		if seen == n {
			return i
		}
		seen++
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

// newSecret builds a status-less object — the inert-unit content whose apply
// is its own readiness.
func newSecret(name, namespace string) *unstructured.Unstructured {
	o := &unstructured.Unstructured{}
	o.SetGroupVersionKind(gvkSecret)
	o.SetName(name)
	o.SetNamespace(namespace)
	return o
}

// newHelmRelease builds a custom resource outside the bootstrap kind-set —
// the CR-unit content whose readiness only an injected judgment can decide.
func newHelmRelease(name, namespace string) *unstructured.Unstructured {
	o := &unstructured.Unstructured{}
	o.SetGroupVersionKind(gvkHelmRelease)
	o.SetName(name)
	o.SetNamespace(namespace)
	return o
}

func assertCode(t *testing.T, err error, want cubeerr.Code) {
	t.Helper()
	var coded *cubeerr.Coded
	if !errors.As(err, &coded) || coded.Code != want {
		t.Fatalf("error = %v, want code %s", err, want)
	}
}
