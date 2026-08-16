package bootstrap

import (
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

func gvrOf(gvk schema.GroupVersionKind) schema.GroupVersionResource {
	plural, _ := meta.UnsafeGuessKindToResource(gvk)
	return plural
}

func testMapper() meta.RESTMapper {
	m := meta.NewDefaultRESTMapper(nil)
	m.Add(gvkNamespace, meta.RESTScopeRoot)
	m.Add(gvkCRD, meta.RESTScopeRoot)
	m.Add(gvkDeployment, meta.RESTScopeNamespace)
	return m
}

func testDynamicCluster(seed ...*unstructured.Unstructured) *dynamicCluster {
	listKinds := map[schema.GroupVersionResource]string{
		gvrOf(gvkNamespace):  "NamespaceList",
		gvrOf(gvkCRD):        "CustomResourceDefinitionList",
		gvrOf(gvkDeployment): "DeploymentList",
	}
	objs := make([]runtime.Object, len(seed))
	for i, o := range seed {
		objs[i] = o
	}
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), listKinds, objs...)
	return &dynamicCluster{dyn: dyn, mapper: testMapper()}
}

// TestDynamicClusterGetResolvesScope exercises the real GVK→resource mapping
// and namespace scoping against the client-go dynamic fake: a cluster-scoped
// Namespace is read at the root, a namespaced Deployment under its namespace.
func TestDynamicClusterGetResolvesScope(t *testing.T) {
	ns := newNamespace("flux-system", "Active")
	dep := newDeployment("source-controller", "flux-system", 1, 1, 1)
	c := testDynamicCluster(ns, dep)

	got, err := c.get(t.Context(), ns)
	if err != nil {
		t.Fatalf("get(Namespace) error = %v", err)
	}
	if got.GetName() != "flux-system" {
		t.Errorf("got %q, want flux-system", got.GetName())
	}
	if _, err := c.get(t.Context(), dep); err != nil {
		t.Errorf("get(namespaced Deployment) error = %v", err)
	}
}

// TestDynamicClusterMappingError pins CUBE-BST-003 for a kind the cluster's
// REST mapper does not know (and never learns — no Reset support).
func TestDynamicClusterMappingError(t *testing.T) {
	c := testDynamicCluster()
	unknown := &unstructured.Unstructured{}
	unknown.SetGroupVersionKind(schema.GroupVersionKind{Group: "example.com", Version: "v1", Kind: "Widget"})
	unknown.SetName("w")
	assertCode(t, c.apply(t.Context(), unknown), CodeRESTMapping)
}

// resettableMapper models a discovery-cache RESTMapper primed before a CRD was
// installed: RESTMapping misses until Reset() rediscovers (as client-go's
// DeferredDiscoveryRESTMapper does after invalidation).
type resettableMapper struct {
	meta.RESTMapper
	ready  bool
	resets int
}

func (m *resettableMapper) RESTMapping(gk schema.GroupKind, versions ...string) (*meta.RESTMapping, error) {
	if !m.ready {
		return nil, &meta.NoKindMatchError{GroupKind: gk}
	}
	return m.RESTMapper.RESTMapping(gk, versions...)
}

func (m *resettableMapper) Reset() { m.resets++; m.ready = true }

// TestDynamicClusterRefreshesMapperOnMiss: a mapping miss on a stale discovery
// cache triggers exactly one Reset()+retry, so a kind registered after the
// mapper was primed (e.g. a Flux CR just after its CRD) still resolves.
func TestDynamicClusterRefreshesMapperOnMiss(t *testing.T) {
	ns := newNamespace("flux-system", "Active")
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{gvrOf(gvkNamespace): "NamespaceList"},
		ns,
	)
	m := &resettableMapper{RESTMapper: testMapper()} // ready == false
	c := &dynamicCluster{dyn: dyn, mapper: m}

	if _, err := c.get(t.Context(), ns); err != nil {
		t.Fatalf("get() error = %v — reset-retry did not recover the mapping", err)
	}
	if m.resets != 1 {
		t.Errorf("mapper reset %d times, want exactly 1", m.resets)
	}
}
