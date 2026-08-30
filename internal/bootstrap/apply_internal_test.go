package bootstrap

import (
	"errors"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

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
