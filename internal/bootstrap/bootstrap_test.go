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
