package gateway

import (
	"errors"
	"io/fs"
	"testing"

	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

// TestCRDsPayloadProvenance pins the embedded payload: crdsPayload() must
// verify the sha256 pin and hand back a defensive copy, so a caller that
// mutates the result cannot corrupt the embed for the next reader.
func TestCRDsPayloadProvenance(t *testing.T) {
	got, err := crdsPayload()
	if err != nil {
		t.Fatalf("crdsPayload() error = %v — embedded payload out of sync with its sha256 pin", err)
	}
	if len(got) == 0 {
		t.Fatal("crdsPayload() returned no bytes")
	}

	got[0] ^= 0xff
	if _, err := crdsPayload(); err != nil {
		t.Fatalf("re-read after mutating the result failed: %v — crdsPayload() aliased the embed", err)
	}
}

// TestCRDsPackObjects parses the real embedded payload into apply-ready
// objects.
//
// The object count is a pin, not a property of the domain: a Gateway API
// bump changes the asset, crdsSHA256, and possibly this number, and all
// three move together in the one `make gateway-api-manifests` commit. That
// is deliberate — asserting only a lower bound would let a version bump
// survive silently, which is exactly what a provenance pin exists to
// prevent.
func TestCRDsPackObjects(t *testing.T) {
	objs, err := CRDsPackObjects()
	if err != nil {
		t.Fatalf("CRDsPackObjects() error = %v", err)
	}
	if len(objs) != 12 {
		t.Errorf("CRDsPackObjects() = %d objects, want the 12 pinned by Gateway API %s", len(objs), CRDsVersion)
	}
	kinds := map[string]int{}
	for _, o := range objs {
		if o.GetName() == "" || o.GetKind() == "" {
			t.Errorf("parsed object missing kind/name: %v", o.Object)
		}
		kinds[o.GetKind()]++
	}
	// The bootstrap kind-set waits CRD Established, so the payload must
	// carry the kind that wait is keyed on.
	if kinds["CustomResourceDefinition"] == 0 {
		t.Errorf("embedded payload has no CustomResourceDefinition (parsed kinds: %v)", kinds)
	}
}

// TestPackFSRoots asserts both accessors hand back a filesystem rooted at
// the pack, which is the shape pack.Load requires at the composition edge.
func TestPackFSRoots(t *testing.T) {
	cases := []struct {
		name    string
		open    func() (fs.FS, error)
		payload string
	}{
		{"gateway-api-crds", CRDsPackFS, crdsPayloadPath},
		{"traefik-gateway", HelmPackFS, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fsys, err := tc.open()
			if err != nil {
				t.Fatalf("open pack FS: %v", err)
			}
			if _, err := fs.Stat(fsys, "pack.cue"); err != nil {
				t.Errorf("pack.cue is not at the FS root: %v", err)
			}
			if tc.payload == "" {
				return
			}
			if _, err := fs.Stat(fsys, tc.payload); err != nil {
				t.Errorf("payload %s missing from the pack FS: %v", tc.payload, err)
			}
		})
	}
}

// TestParseManifestsRejectsGarbage pins the coded parse failure: a payload
// that is not YAML at all is CUBE-GWY-002, asserted by code and never by
// message.
func TestParseManifestsRejectsGarbage(t *testing.T) {
	_, err := parseManifests([]byte("\tthis: is: not: yaml\n\t\tat all"))
	var coded *cubeerr.Coded
	if !errors.As(err, &coded) {
		t.Fatalf("parseManifests(garbage) = %v, want *cubeerr.Coded", err)
	}
	if coded.Code != CodePackParse {
		t.Fatalf("code = %s, want %s", coded.Code, CodePackParse)
	}
}
