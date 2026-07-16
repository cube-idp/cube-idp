package lock

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
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
