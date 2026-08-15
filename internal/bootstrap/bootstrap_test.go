package bootstrap_test

import (
	"strings"
	"testing"

	"github.com/cube-idp/cube-idp/internal/bootstrap"
)

// TestManifests pins the embedded Flux asset: Manifests() must verify its
// provenance (returning no error), carry the pinned version marker and the
// kinds the bootstrap kind-set waits on, and hand back a defensive copy.
func TestManifests(t *testing.T) {
	got, err := bootstrap.Manifests()
	if err != nil {
		t.Fatalf("Manifests() error = %v — embedded asset out of sync with its sha256 pin", err)
	}
	if len(got) == 0 {
		t.Fatal("Manifests() returned no bytes")
	}

	if marker := "Flux Version: " + bootstrap.FluxVersion; !strings.Contains(string(got), marker) {
		t.Errorf("manifests do not carry the pinned version marker %q", marker)
	}
	for _, kind := range []string{"kind: Namespace", "kind: CustomResourceDefinition", "kind: Deployment"} {
		if !strings.Contains(string(got), kind) {
			t.Errorf("manifests missing %q", kind)
		}
	}

	// The result must be a defensive copy: mutating it must not corrupt the
	// embedded bytes, so a second read still passes its provenance check.
	got[0] ^= 0xff
	if _, err := bootstrap.Manifests(); err != nil {
		t.Fatalf("re-read after mutating the result failed: %v — Manifests() aliased the embed", err)
	}
}

// TestFluxObjects parses the real embedded asset into apply-ready objects and
// confirms it carries the Namespace and CRDs the kind-set wait targets.
func TestFluxObjects(t *testing.T) {
	objs, err := bootstrap.FluxObjects()
	if err != nil {
		t.Fatalf("FluxObjects() error = %v", err)
	}
	if len(objs) == 0 {
		t.Fatal("FluxObjects() parsed no objects")
	}

	kinds := map[string]int{}
	for _, o := range objs {
		if o.GetName() == "" || o.GetKind() == "" {
			t.Errorf("parsed object missing kind/name: %v", o.Object)
		}
		kinds[o.GetKind()]++
	}
	for _, want := range []string{"Namespace", "CustomResourceDefinition", "Deployment"} {
		if kinds[want] == 0 {
			t.Errorf("embedded manifests have no %s (parsed kinds: %v)", want, kinds)
		}
	}
}
