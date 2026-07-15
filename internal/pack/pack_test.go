package pack

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/rafpe/cube-idp/internal/diag"
)

func TestFetchLocalDirAndMetadata(t *testing.T) {
	p, err := Fetch(context.Background(), "testdata/demo", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "demo" || p.Version != "0.1.0" {
		t.Fatalf("metadata: %+v", p)
	}
}

func TestFetchLocalDirSetsPinned(t *testing.T) {
	p, err := Fetch(context.Background(), "testdata/demo", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Pinned) < 5 || p.Pinned[:4] != "dir:" {
		t.Fatalf("local packs must be pinned by dirhash, got %q", p.Pinned)
	}
}

func TestFetchUnknownScheme(t *testing.T) {
	_, err := Fetch(context.Background(), "svn://old/school", t.TempDir())
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-4001" {
		t.Fatalf("want CUBE-4001, got %v", err)
	}
}

func TestRenderValidatesValues(t *testing.T) {
	p, _ := Fetch(context.Background(), "testdata/demo", t.TempDir())
	_, err := p.Render(map[string]any{"replicas": -3})
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-4002" {
		t.Fatalf("want CUBE-4002, got %v", err)
	}
}

func TestRenderManifests(t *testing.T) {
	p, _ := Fetch(context.Background(), "testdata/demo", t.TempDir())
	r, err := p.Render(map[string]any{"replicas": 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Objects) != 1 || r.Objects[0].GetKind() != "ConfigMap" {
		t.Fatalf("objects: %+v", r.Objects)
	}
}

func TestRenderHelmChart(t *testing.T) {
	if testing.Short() {
		t.Skip("network")
	}
	p, err := Fetch(context.Background(), "testdata/demo-helm", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r, err := p.Render(nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, o := range r.Objects {
		if o.GetKind() == "Deployment" {
			found = true
		}
	}
	if !found {
		t.Fatal("helm render produced no Deployment")
	}
}

func TestRenderKustomizeOverlay(t *testing.T) {
	p, err := Fetch(context.Background(), "testdata/demo-kustomize", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r, err := p.Render(nil)
	if err != nil {
		t.Fatal(err)
	}
	// exactly one object: kustomization governs manifests/, no double-count
	if len(r.Objects) != 1 {
		t.Fatalf("want 1 object (no double-render of manifests/), got %d", len(r.Objects))
	}
	msg, _, _ := unstructured.NestedString(r.Objects[0].Object, "data", "message")
	if msg != "patched" {
		t.Fatalf("kustomize patch not applied, message=%q", msg)
	}
}

// TestRenderKustomizationStatOtherErrorSurfaces covers (e): Render must
// distinguish a genuinely missing kustomization.yaml (fs.ErrNotExist, fall
// back to walking manifests/) from any OTHER stat error, which must surface
// as a typed error rather than being silently treated as "absent". A
// self-referential symlink at kustomization.yaml's path makes os.Stat fail
// with ELOOP ("too many levels of symbolic links") — a real, non-not-exist
// error, reproducible without platform-specific permission tricks.
func TestRenderKustomizationStatOtherErrorSurfaces(t *testing.T) {
	dir := writePack(t, `name: "x"
version: "0.1.0"
`)
	loop := filepath.Join(dir, "kustomization.yaml")
	if err := os.Symlink(loop, loop); err != nil {
		t.Fatal(err)
	}
	p := &Pack{Name: "x", Version: "0.1.0", Dir: dir}
	_, err := p.Render(nil)
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != diag.CodePackManifestErr {
		t.Fatalf("want CodePackManifestErr surfaced for a real kustomization.yaml stat error, got %v", err)
	}
}

// TestGatewayServiceParsing pins the R7b data contract: a pack.cue
// gatewayService: block parses into Pack.GatewayService.
func TestGatewayServiceParsing(t *testing.T) {
	dir := writePack(t, `name: "gwp"
version: "0.1.0"
gatewayService: {name: "cube-idp-gateway", namespace: "envoy-gateway"}
`)
	p, err := Fetch(context.Background(), dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if p.GatewayService == nil || p.GatewayService.Name != "cube-idp-gateway" || p.GatewayService.Namespace != "envoy-gateway" {
		t.Fatalf("gatewayService: %+v", p.GatewayService)
	}
}

// TestGatewayServiceOptional: packs predating the field load as before —
// GatewayService stays nil, no error.
func TestGatewayServiceOptional(t *testing.T) {
	dir := writePack(t, "name: \"plain\"\nversion: \"0.1.0\"\n")
	p, err := Fetch(context.Background(), dir, "")
	if err != nil || p.GatewayService != nil {
		t.Fatalf("want nil GatewayService, got %+v (err %v)", p.GatewayService, err)
	}
}

// TestGatewayServiceMalformed: a gatewayService: block missing namespace is
// rejected as CUBE-4003 (CodePackCueInvalid), the images: precedent — R7b
// allocates no new code.
func TestGatewayServiceMalformed(t *testing.T) {
	dir := writePack(t, "name: \"gwp\"\nversion: \"0.1.0\"\ngatewayService: {name: \"x\"}\n")
	_, err := Fetch(context.Background(), dir, "")
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-4003" {
		t.Fatalf("want CUBE-4003, got %v", err)
	}
}

func TestRenderKustomizeFailureIsTyped(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "pack.cue"), []byte("name: \"bad\"\nversion: \"0.1.0\"\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "kustomization.yaml"), []byte("resources: [does-not-exist.yaml]\n"), 0o644)
	p, err := Fetch(context.Background(), dir, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Render(nil)
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-4008" {
		t.Fatalf("want CUBE-4008, got %v", err)
	}
}
