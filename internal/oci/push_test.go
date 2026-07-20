package oci

import (
	"context"
	"encoding/json"
	"testing"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"oras.land/oras-go/v2/content/memory"

	"github.com/cube-idp/cube-idp/internal/pack"
)

// cmObj builds a minimal ConfigMap unstructured object for use as a rendered
// pack object in tests (no cmObj helper exists elsewhere in this repo).
func cmObj(name, namespace string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]interface{}{
			"name":      name,
			"namespace": namespace,
		},
	}}
}

// fetchManifest reads desc out of store and unmarshals it as an OCI image
// manifest.
func fetchManifest(t *testing.T, store *memory.Store, desc ocispec.Descriptor) ocispec.Manifest {
	t.Helper()
	rc, err := store.Fetch(context.Background(), desc)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	var manifest ocispec.Manifest
	if err := json.NewDecoder(rc).Decode(&manifest); err != nil {
		t.Fatal(err)
	}
	return manifest
}

// TestPushRenderedKeepsFluxMediaTypes pins the OCI artifact shape that
// source-controller (Flux's OCIRepository) consumes without any
// spec.layerSelector: a config blob of media type
// application/vnd.cncf.flux.config.v1+json and a single layer of media type
// application/vnd.cncf.flux.content.v1.tar+gzip. This is the whole risk of
// the rewrite off fluxcd/pkg/oci onto plain oras-go v2 — get these two
// constants wrong and flux delivery stops reconciling.
func TestPushRenderedKeepsFluxMediaTypes(t *testing.T) {
	r := &pack.Rendered{
		Name:    "demo",
		Version: "0.1.0",
		Objects: []*unstructured.Unstructured{cmObj("demo", "default")},
	}
	store := memory.New()

	ref, err := pushRenderedTo(context.Background(), r, store)
	if err != nil {
		t.Fatal(err)
	}
	if ref.Repo != "packs/demo" || ref.Tag != "0.1.0" {
		t.Fatalf("ArtifactRef drifted: %+v", ref)
	}

	desc, err := store.Resolve(context.Background(), "0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if ref.Digest == "" || ref.Digest != desc.Digest.String() {
		t.Fatalf("ArtifactRef.Digest: got %q, want the pushed manifest digest %q", ref.Digest, desc.Digest.String())
	}
	manifest := fetchManifest(t, store, desc)

	if manifest.Config.MediaType != fluxConfigMediaType {
		t.Fatalf("config mediaType: %s", manifest.Config.MediaType)
	}
	if len(manifest.Layers) != 1 || manifest.Layers[0].MediaType != fluxContentMediaType {
		t.Fatalf("layers: %+v", manifest.Layers)
	}
}

// TestPushRenderedIsContentAddressed mirrors pushdir_test.go's
// TestPushPackDirIsContentAddressed: pushing the identical rendered pack
// twice must produce the identical manifest digest. A fixed epoch
// annotation (not wall-clock time.Now) is what makes a republish of
// unchanged content a true no-op — unlike the PushPackDir
// version of this test, no time.Sleep across a wall-clock second boundary
// is needed here: the annotation is now a fixed constant, not time-based,
// so there is nothing for a fast in-process double-push to race against.
func TestPushRenderedIsContentAddressed(t *testing.T) {
	r := &pack.Rendered{
		Name:    "demo",
		Version: "0.1.0",
		Objects: []*unstructured.Unstructured{cmObj("demo", "default")},
	}
	store := memory.New()

	ref1, err := pushRenderedTo(context.Background(), r, store)
	if err != nil {
		t.Fatal(err)
	}
	ref2, err := pushRenderedTo(context.Background(), r, store)
	if err != nil {
		t.Fatal(err)
	}
	if ref1.Digest != ref2.Digest {
		t.Fatalf("republish of identical content changed digest: %q != %q", ref1.Digest, ref2.Digest)
	}
}
