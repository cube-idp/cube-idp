package substrate

import (
	"errors"
	"strings"
	"testing"

	"github.com/cube-idp/cube-idp/internal/cubeerr"
	"github.com/cube-idp/cube-idp/internal/engine"
)

// TestManifestsProvenance pins the embedded payload: manifests() must
// verify the sha256 pin, carry the upstream version marker (the
// v-prefixed spelling the substrate alone maps to), and hand back a
// defensive copy.
func TestManifestsProvenance(t *testing.T) {
	got, err := manifests()
	if err != nil {
		t.Fatalf("manifests() error = %v — embedded payload out of sync with its sha256 pin", err)
	}
	if len(got) == 0 {
		t.Fatal("manifests() returned no bytes")
	}
	if marker := "Flux Version: v" + Version; !strings.Contains(string(got), marker) {
		t.Errorf("payload does not carry the pinned version marker %q", marker)
	}

	// The result must be a copy: mutating it must not corrupt the embed,
	// so a second read still passes its provenance check.
	got[0] ^= 0xff
	if _, err := manifests(); err != nil {
		t.Fatalf("re-read after mutating the result failed: %v — manifests() aliased the embed", err)
	}
}

// TestObjects parses the real embedded payload into apply-ready objects
// carrying the kinds the bootstrap kind-set waits on.
func TestObjects(t *testing.T) {
	objs, err := Objects()
	if err != nil {
		t.Fatalf("Objects() error = %v", err)
	}
	if len(objs) == 0 {
		t.Fatal("Objects() parsed no objects")
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
			t.Errorf("embedded payload has no %s (parsed kinds: %v)", want, kinds)
		}
	}
}

// TestNamespaceFactMatchesPayload ties the exported namespace fact to
// content: the named namespace must exist as a Namespace object in the
// substrate pack's payload.
func TestNamespaceFactMatchesPayload(t *testing.T) {
	objs, err := Objects()
	if err != nil {
		t.Fatalf("Objects() error = %v", err)
	}
	for _, o := range objs {
		if o.GetKind() == "Namespace" && o.GetName() == Namespace {
			return
		}
	}
	t.Fatalf("payload has no Namespace object named %q — the substrate namespace fact does not match the content", Namespace)
}

// TestCheckVersion asserts the clean-SemVer version contract: empty and
// the pinned spelling pass; anything else — including the v-prefixed
// upstream spelling — is the coded mismatch.
func TestCheckVersion(t *testing.T) {
	cases := []struct {
		name      string
		requested string
		wantErr   bool
	}{
		{"empty selects the pin", "", false},
		{"clean pinned spelling", Version, false},
		{"v-prefixed spelling is a mismatch", "v" + Version, true},
		{"different version is a mismatch", "9.9.9", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckVersion(tc.requested)
			if !tc.wantErr {
				if err != nil {
					t.Fatalf("CheckVersion(%q) error = %v, want nil", tc.requested, err)
				}
				return
			}
			var coded *cubeerr.Coded
			if !errors.As(err, &coded) {
				t.Fatalf("CheckVersion(%q) = %v, want *cubeerr.Coded", tc.requested, err)
			}
			if coded.Code != engine.CodeVersionMismatch {
				t.Fatalf("code = %s, want %s", coded.Code, engine.CodeVersionMismatch)
			}
		})
	}
}
