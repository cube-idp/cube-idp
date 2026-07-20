package pack

import (
	"context"
	"errors"
	"testing"

	"github.com/cube-idp/cube-idp/internal/diag"
)

// TestImagesParsed pins that a pack.cue images: list is parsed into
// Pack.Images unchanged, in declaration order (Decode preserves list order;
// unlike Entry.Images this field is never sorted/deduped — that merge
// happens once, downstream, in internal/up's lock assembly).
func TestImagesParsed(t *testing.T) {
	dir := writePack(t, `name: "envoy-gateway"
version: "0.1.0"
images: ["envoyproxy/envoy:v1.29.0", "docker.io/envoyproxy/gateway:v1.0.0"]
`)
	p, err := Fetch(context.Background(), dir, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"envoyproxy/envoy:v1.29.0", "docker.io/envoyproxy/gateway:v1.0.0"}
	if len(p.Images) != len(want) {
		t.Fatalf("images: got %v, want %v", p.Images, want)
	}
	for i := range want {
		if p.Images[i] != want[i] {
			t.Fatalf("images[%d]: got %q, want %q", i, p.Images[i], want[i])
		}
	}
}

// TestImagesIsOptional mirrors TestExposeIsOptional: a pack.cue predating
// this field (or simply not using it) loads with a nil Images, no error.
func TestImagesIsOptional(t *testing.T) {
	p, err := Fetch(context.Background(), "testdata/demo", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if p.Images != nil {
		t.Fatalf("want nil Images for a pack.cue with no images: field, got %v", p.Images)
	}
}

// TestImagesInvalidTypeIsCUBE4003 pins that a malformed images: field (not a
// list of strings) is rejected as the existing pack.cue error code, never
// silently dropped or accepted as an empty list.
func TestImagesInvalidTypeIsCUBE4003(t *testing.T) {
	dir := writePack(t, `name: "bad"
version: "0.1.0"
images: 42
`)
	_, err := Fetch(context.Background(), dir, t.TempDir())
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-4003" {
		t.Fatalf("want CUBE-4003, got %v", err)
	}
}
