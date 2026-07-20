package lock

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/cube-idp/cube-idp/internal/diag"
)

func deployment(image, initImage string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1", "kind": "Deployment",
		"metadata": map[string]any{"name": "d", "namespace": "n"},
		"spec": map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"initContainers": []any{map[string]any{"name": "i", "image": initImage}},
					"containers":     []any{map[string]any{"name": "c", "image": image}},
				},
			},
		},
	}}
}

func TestImagesFromWalksPodSpecs(t *testing.T) {
	objs := []*unstructured.Unstructured{
		deployment("nginx:1.27", "busybox:1.36"),
		deployment("nginx:1.27", "alpine:3.20"), // duplicate nginx must dedupe
	}
	got := ImagesFrom(objs)
	want := []string{"alpine:3.20", "busybox:1.36", "nginx:1.27"} // sorted, unique
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestRenderedHashDeterministic(t *testing.T) {
	objs := []*unstructured.Unstructured{deployment("nginx:1.27", "busybox:1.36")}
	h1, err := RenderedHash(objs)
	if err != nil {
		t.Fatal(err)
	}
	h2, _ := RenderedHash(objs)
	if h1 != h2 || len(h1) < len("sha256:")+64 {
		t.Fatalf("hash unstable or malformed: %q vs %q", h1, h2)
	}
	h3, _ := RenderedHash([]*unstructured.Unstructured{deployment("nginx:1.28", "busybox:1.36")})
	if h1 == h3 {
		t.Fatal("different content must hash differently")
	}
}

func TestWriteReadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cube.lock")
	f := &File{APIVersion: "cube-idp.dev/v1alpha1", Kind: "CubeLock",
		Engine: EngineLock{Type: "flux"},
		Packs: []Entry{{Ref: "./packs/gitea", Name: "gitea", Version: "0.1.0",
			Resolved: "dir:h1:abc", RenderedHash: "sha256:def", Images: []string{"gitea:1.22"}}}}
	if err := Write(path, f); err != nil {
		t.Fatal(err)
	}
	got, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, f) {
		t.Fatalf("round trip: %+v", got)
	}
}

func TestReadMissingIsNil(t *testing.T) {
	got, err := Read(filepath.Join(t.TempDir(), "cube.lock"))
	if err != nil || got != nil {
		t.Fatalf("missing lock must be (nil, nil), got %v %v", got, err)
	}
}

func TestReadCorrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cube.lock")
	os.WriteFile(path, []byte("{{{not yaml"), 0o644)
	_, err := Read(path)
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != diag.CodeLockCorrupt {
		t.Fatalf("want %s, got %v", diag.CodeLockCorrupt, err)
	}
}

// TestEngineLockEntryRoundTrip pins the engine-as-pack lock extension: the
// engine is a first-class reproducibility entry — EngineLock grew from a bare
// {Type} to the standard pack fields when the engine became a pack; old locks
// (type-only) still read, and Entry() projects the pack fields for bundle
// vendoring.
func TestEngineLockEntryRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cube.lock")
	f := &File{APIVersion: "cube-idp.dev/v1alpha1", Kind: "CubeLock",
		Engine: EngineLock{Type: "flux", Ref: "oci://ghcr.io/cube-idp/packs/cube-engine-flux:0.1.0",
			Name: "cube-engine-flux", Version: "0.1.0", Resolved: "oci:sha256:abc",
			RenderedHash: "h1", Images: []string{"ghcr.io/fluxcd/source-controller:v1.0.0"}}}
	if err := Write(p, f); err != nil {
		t.Fatal(err)
	}
	got, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Engine, f.Engine) {
		t.Fatalf("engine lock round-trip: %+v != %+v", got.Engine, f.Engine)
	}
	e := got.Engine.Entry()
	if e.Ref != f.Engine.Ref || e.Name != "cube-engine-flux" || e.RenderedHash != "h1" || len(e.Images) != 1 {
		t.Fatalf("Entry projection: %+v", e)
	}
	// Pre-engine-as-pack lock (type only) still reads.
	os.WriteFile(p, []byte("apiVersion: cube-idp.dev/v1alpha1\nkind: CubeLock\nengine:\n  type: argocd\npacks: []\n"), 0o644)
	old, err := Read(p)
	if err != nil || old.Engine.Type != "argocd" || old.Engine.Ref != "" {
		t.Fatalf("old lock compat: %+v, %v", old, err)
	}
}

// TestLockEntryValuesFieldsOmitEmpty pins RV2's omitempty discipline:
// ref-less entries must serialize byte-identically to pre-RV2 locks (the p6
// "stock records unchanged" rule) — absent valuesRef/valuesPin keys, never
// empty strings — while populated fields round-trip.
func TestLockEntryValuesFieldsOmitEmpty(t *testing.T) {
	f := &File{APIVersion: "cube-idp.dev/v1alpha1", Kind: "CubeLock",
		Engine: EngineLock{Type: "flux"},
		Packs:  []Entry{{Ref: "packs/x", Name: "x", Version: "1", Resolved: "dir:h", RenderedHash: "h"}}}
	p := filepath.Join(t.TempDir(), "cube.lock")
	if err := Write(p, f); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(p)
	if strings.Contains(string(raw), "valuesRef") || strings.Contains(string(raw), "valuesPin") {
		t.Fatalf("empty values fields serialized:\n%s", raw)
	}
	// And populated fields round-trip.
	f.Packs[0].ValuesRef, f.Packs[0].ValuesPin = "github.com/a/v//x@v1", "git+abc"
	if err := Write(p, f); err != nil {
		t.Fatal(err)
	}
	got, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Packs[0].ValuesRef != "github.com/a/v//x@v1" || got.Packs[0].ValuesPin != "git+abc" {
		t.Fatalf("round-trip lost values fields: %+v", got.Packs[0])
	}
}
