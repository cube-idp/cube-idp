package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

// TestRecordInventoryWritesConfigMap checks the inventory ConfigMap is applied
// with the cube-idp label and carries every installed object as a ref.
func TestRecordInventoryWritesConfigMap(t *testing.T) {
	objs := []*unstructured.Unstructured{
		newNamespace("flux-system", ""),
		newCRD("gitrepositories.source.toolkit.fluxcd.io", false),
		newDeployment("source-controller", "flux-system", 1, 0, 1),
	}
	f := newFakeCluster()
	a := testApplier(f)
	if err := a.RecordInventory(t.Context(), objs); err != nil {
		t.Fatalf("RecordInventory() error = %v", err)
	}

	cm, ok := f.store["ConfigMap/"+testInvNS+"/"+InventoryName]
	if !ok {
		t.Fatalf("inventory ConfigMap not applied; store keys = %v", keysOf(f))
	}
	if cm.GetLabels()["app.kubernetes.io/managed-by"] != "cube-idp" {
		t.Errorf("inventory ConfigMap not labeled cube-idp-managed: %v", cm.GetLabels())
	}

	refs := decodeInventory(t, cm)
	if len(refs) != len(objs) {
		t.Fatalf("inventory has %d refs, want %d", len(refs), len(objs))
	}
	names := map[string]bool{}
	for _, r := range refs {
		names[r.Kind+"/"+r.Name] = true
	}
	for _, want := range []string{"Namespace/flux-system", "Deployment/source-controller"} {
		if !names[want] {
			t.Errorf("inventory missing %s (got %v)", want, names)
		}
	}
}

// TestRecordInventoryInjectedNamespace pins that placement is genuinely
// injected: the applier records into whatever namespace the edge supplied,
// and the kind-set wait's remediation names that same namespace — bootstrap
// carries no placement knowledge of its own.
func TestRecordInventoryInjectedNamespace(t *testing.T) {
	f := newFakeCluster()
	a := &Applier{k: f, interval: time.Millisecond, invNS: "custom-ns"}
	if err := a.RecordInventory(t.Context(), []*unstructured.Unstructured{newNamespace("custom-ns", "")}); err != nil {
		t.Fatalf("RecordInventory() error = %v", err)
	}
	if _, ok := f.store["ConfigMap/custom-ns/"+InventoryName]; !ok {
		t.Fatalf("inventory not recorded in the injected namespace; store keys = %v", keysOf(f))
	}

	notReady := newDeployment("controller", "custom-ns", 1, 0, 1)
	f.store[objKey(notReady)] = notReady
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
	defer cancel()
	err := a.WaitReady(ctx, []*unstructured.Unstructured{notReady})
	assertCode(t, err, CodeWaitTimeout)
	var coded *cubeerr.Coded
	_ = errors.As(err, &coded)
	if !strings.Contains(coded.Remediation, "custom-ns") {
		t.Errorf("remediation %q should name the injected namespace", coded.Remediation)
	}
}

// TestRecordInventoryDeterministic pins a stable, order-independent object list
// so the recorded ConfigMap is reproducible.
func TestRecordInventoryDeterministic(t *testing.T) {
	a := []*unstructured.Unstructured{
		newNamespace("flux-system", ""),
		newDeployment("source-controller", "flux-system", 1, 0, 1),
		newDeployment("kustomize-controller", "flux-system", 1, 0, 1),
	}
	b := []*unstructured.Unstructured{a[2], a[0], a[1]} // shuffled

	cmA, err := inventoryConfigMap(testInvNS, a)
	if err != nil {
		t.Fatal(err)
	}
	cmB, err := inventoryConfigMap(testInvNS, b)
	if err != nil {
		t.Fatal(err)
	}
	dataA, _, _ := unstructured.NestedString(cmA.Object, "data", inventoryKey)
	dataB, _, _ := unstructured.NestedString(cmB.Object, "data", inventoryKey)
	if dataA != dataB {
		t.Errorf("inventory not order-independent:\nA=%s\nB=%s", dataA, dataB)
	}
}

func decodeInventory(t *testing.T, cm *unstructured.Unstructured) []ObjectRef {
	t.Helper()
	raw, found, _ := unstructured.NestedString(cm.Object, "data", inventoryKey)
	if !found {
		t.Fatal("inventory ConfigMap has no data.objects")
	}
	var refs []ObjectRef
	if err := json.Unmarshal([]byte(raw), &refs); err != nil {
		t.Fatalf("inventory data is not valid JSON: %v", err)
	}
	return refs
}

func keysOf(f *fakeCluster) []string {
	out := make([]string, 0, len(f.store))
	for k := range f.store {
		out = append(out, k)
	}
	return out
}
