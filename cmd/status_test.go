package cmd

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"github.com/fluxcd/cli-utils/pkg/object"
)

func TestFormatInventory(t *testing.T) {
	// Fixed 3-object slice: one cluster-scoped, two namespaced, deliberately out of order
	inv := []object.ObjMetadata{
		{
			GroupKind: schema.GroupKind{Group: "apps", Kind: "Deployment"},
			Namespace: "default",
			Name:      "my-app",
		},
		{
			GroupKind: schema.GroupKind{Group: "", Kind: "Namespace"},
			Namespace: "", // cluster-scoped
			Name:      "kube-system",
		},
		{
			GroupKind: schema.GroupKind{Group: "", Kind: "ConfigMap"},
			Namespace: "default",
			Name:      "app-config",
		},
	}

	output := formatInventory(inv)

	// Expected: sorted by Kind, then Namespace, then Name
	// Sorted order:
	// 1. ConfigMap (default, app-config)
	// 2. Deployment (default, my-app)
	// 3. Namespace (-, kube-system)
	// Note: tabwriter formats with spaces for column alignment
	expected := "KIND       NAMESPACE NAME\n" +
		"ConfigMap  default   app-config\n" +
		"Deployment default   my-app\n" +
		"Namespace  -         kube-system\n"

	if output != expected {
		t.Errorf("formatInventory output mismatch.\nGot:\n%q\n\nExpected:\n%q", output, expected)
	}
}
