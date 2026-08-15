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
// REST mapper does not know.
func TestDynamicClusterMappingError(t *testing.T) {
	c := testDynamicCluster()
	unknown := &unstructured.Unstructured{}
	unknown.SetGroupVersionKind(schema.GroupVersionKind{Group: "example.com", Version: "v1", Kind: "Widget"})
	unknown.SetName("w")
	assertCode(t, c.apply(t.Context(), unknown), CodeRESTMapping)
}
