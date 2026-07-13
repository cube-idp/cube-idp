package apply

import (
	"context"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestDiffReportsCreatedConfiguredUnchanged(t *testing.T) {
	a, err := New(testREST, "diffcube")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	base := []*unstructured.Unstructured{ns("dt1"), cm("same", "dt1", nil), cm("drift", "dt1", nil)}
	if err := a.Apply(ctx, base, true, 30*time.Second); err != nil {
		t.Fatal(err)
	}

	changedCM := cm("drift", "dt1", nil)
	changedCM.Object["data"] = map[string]any{"k": "NEW"}
	desired := []*unstructured.Unstructured{
		ns("dt1"), cm("same", "dt1", nil), changedCM, cm("brandnew", "dt1", nil),
	}
	changes, err := a.Diff(ctx, desired)
	if err != nil {
		t.Fatal(err)
	}
	byRef := map[string]string{}
	for _, c := range changes {
		byRef[c.Ref] = c.Action
	}
	if byRef["/ConfigMap/dt1/brandnew"] != "created" {
		t.Fatalf("brandnew: %v", byRef)
	}
	if byRef["/ConfigMap/dt1/drift"] != "configured" {
		t.Fatalf("drift: %v", byRef)
	}
	if byRef["/ConfigMap/dt1/same"] != "unchanged" {
		t.Fatalf("same: %v", byRef)
	}
}
