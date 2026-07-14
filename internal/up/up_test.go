package up

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/rafpe/cube-idp/internal/config"
	"github.com/rafpe/cube-idp/internal/diag"
	"github.com/rafpe/cube-idp/internal/lock"
	"github.com/rafpe/cube-idp/internal/ui"
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

// TestResolveBundleRefs is the pure offline rule: every cube pack ref must
// resolve to a bundle-local directory (via the lock's name<->ref mapping,
// with a last-path-segment fallback for local-dir refs), or fail loudly with
// CUBE-7004 — never a silent network fetch.
func TestResolveBundleRefs(t *testing.T) {
	inBundle := map[string]string{"gitea": "/tmp/x/packs/gitea"} // pack name -> dir
	lk := &lock.File{Packs: []lock.Entry{
		{Ref: "oci://ghcr.io/rafpe/cube-idp/packs/gitea:0.1.0", Name: "gitea"},
	}}
	refs := []config.PackRef{{Ref: "oci://ghcr.io/rafpe/cube-idp/packs/gitea:0.1.0"}}
	resolved, err := resolveBundleRefs(refs, lk, func(name string) (string, bool) {
		d, ok := inBundle[name]
		return d, ok
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolved[0].Ref != "/tmp/x/packs/gitea" {
		t.Fatalf("resolved: %+v", resolved)
	}

	// A ref absent from both the lock and the bundle is CUBE-7004.
	_, err = resolveBundleRefs(
		[]config.PackRef{{Ref: "oci://ghcr.io/rafpe/cube-idp/packs/absent:1.0.0"}},
		&lock.File{},
		func(string) (string, bool) { return "", false },
	)
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-7004" {
		t.Fatalf("want CUBE-7004 for a ref missing from the bundle, got %v", err)
	}
}

// TestResolveBundleRefs_LocalDirFallback covers the fallback path: a ref the
// lock records verbatim as a local directory resolves by its last path
// segment (the pack name the bundle keyed it under) when no lock Ref match
// exists.
func TestResolveBundleRefs_LocalDirFallback(t *testing.T) {
	refs := []config.PackRef{{Ref: "/some/checkout/packs/traefik"}}
	resolved, err := resolveBundleRefs(refs, &lock.File{}, func(name string) (string, bool) {
		if name == "traefik" {
			return "/tmp/x/packs/traefik", true
		}
		return "", false
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolved[0].Ref != "/tmp/x/packs/traefik" {
		t.Fatalf("resolved: %+v", resolved)
	}
}

// TestResolveBundleRefs_PreservesValues verifies rewriting the Ref keeps the
// pack's Values overrides intact — only the source location changes.
func TestResolveBundleRefs_PreservesValues(t *testing.T) {
	refs := []config.PackRef{{
		Ref:    "oci://ghcr.io/rafpe/cube-idp/packs/gitea:0.1.0",
		Values: map[string]any{"replicas": 2},
	}}
	lk := &lock.File{Packs: []lock.Entry{{Ref: "oci://ghcr.io/rafpe/cube-idp/packs/gitea:0.1.0", Name: "gitea"}}}
	resolved, err := resolveBundleRefs(refs, lk, func(string) (string, bool) {
		return "/tmp/x/packs/gitea", true
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolved[0].Values["replicas"] != 2 {
		t.Fatalf("values lost: %+v", resolved[0])
	}
}

// TestStepFetchSourcePlainOutput pins the per-pack resolved-fetch-source line
// (Task 13 review): the plain stream must name the ACTUAL source pack.Fetch
// will read — the oci:// ref online, the bundle-local dir (under a
// cube-idp-bundle-* staging dir) in --bundle mode — so the e2e's offline
// assertions ("every fetch source points into the bundle", "no fetch source
// is oci://") are falsifiable from output alone: an online run demonstrably
// prints oci:// here, a bundle run demonstrably does not.
func TestStepFetchSourcePlainOutput(t *testing.T) {
	emit := func(refs []config.PackRef) string {
		var out bytes.Buffer // never a TTY -> plain renderer
		err := ui.RunPipeline(context.Background(), "up", &out,
			func(_ context.Context, con *ui.Console) error {
				for _, pr := range refs {
					stepFetchSource(con, pr.Ref)
				}
				return nil
			})
		if err != nil {
			t.Fatal(err)
		}
		return out.String()
	}

	// Online: the network ref itself must appear, byte-for-byte.
	online := emit([]config.PackRef{{Ref: "oci://ghcr.io/rafpe/cube-idp/packs/gitea:0.1.0"}})
	if want := "▸ [pack] fetching oci://ghcr.io/rafpe/cube-idp/packs/gitea:0.1.0\n"; !strings.Contains(online, want) {
		t.Fatalf("online fetch-source line missing %q in:\n%s", want, online)
	}

	// Bundle: refs resolved via resolveBundleRefs print the bundle-local dir
	// and no oci:// ref survives.
	lk := &lock.File{Packs: []lock.Entry{{Ref: "oci://ghcr.io/x/packs/gitea:0.1.0", Name: "gitea"}}}
	resolved, err := resolveBundleRefs(
		[]config.PackRef{{Ref: "oci://ghcr.io/x/packs/gitea:0.1.0"}}, lk,
		func(name string) (string, bool) { return "/tmp/cube-idp-bundle-123/packs/" + name, true })
	if err != nil {
		t.Fatal(err)
	}
	offline := emit(resolved)
	if want := "▸ [pack] fetching /tmp/cube-idp-bundle-123/packs/gitea\n"; !strings.Contains(offline, want) {
		t.Fatalf("bundle fetch-source line missing %q in:\n%s", want, offline)
	}
	if strings.Contains(offline, "oci://") {
		t.Fatalf("bundle fetch-source output leaked an oci:// ref:\n%s", offline)
	}
}
