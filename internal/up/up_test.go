package up

import (
	"reflect"
	"testing"
)

// TestMergeImagesUnion pins spec D14's lock-assembly merge: the sorted,
// deduplicated union of rendered-manifest images and a pack's declared
// (pack.cue images:) images — the pure step Run's pack loop calls per
// entry. This is the "focused unit" the D14 preparatory commit calls for,
// since Run itself needs a live cluster and isn't unit-testable here.
func TestMergeImagesUnion(t *testing.T) {
	got := mergeImages(
		[]string{"traefik:v3.1", "busybox:1.36"},
		[]string{"envoyproxy/envoy:v1.29.0", "busybox:1.36"}, // busybox is a deliberate overlap
	)
	want := []string{"busybox:1.36", "envoyproxy/envoy:v1.29.0", "traefik:v3.1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mergeImages = %v, want %v", got, want)
	}
}

// TestMergeImagesEmptyDeclared covers the common case — a pack.cue with no
// images: field (pk.Images is nil) — so the merge degenerates to the
// rendered-manifest list alone, sorted.
func TestMergeImagesEmptyDeclared(t *testing.T) {
	got := mergeImages([]string{"b:1", "a:1"}, nil)
	want := []string{"a:1", "b:1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mergeImages = %v, want %v", got, want)
	}
}
