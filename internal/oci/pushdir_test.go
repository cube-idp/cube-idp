package oci

import (
	"context"
	"io"
	"log"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/registry"

	"github.com/rafpe/cube-idp/internal/pack"
)

// localRegistry starts an in-process OCI registry (go-containerregistry's
// test registry — a TEST-ONLY dependency; production stays pure oras-go v2)
// and returns its host:port. httptest servers listen on 127.0.0.1 over plain
// HTTP, which exercises the same insecure-transport gate
// (isLocalRegistryHost -> PlainHTTP) the zot port-forward tunnel uses.
func localRegistry(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(registry.New(registry.Logger(log.New(io.Discard, "", 0))))
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return u.Host
}

func writeDemoPack(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	must := func(p, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	must(filepath.Join(dir, "pack.cue"), "name: \"demo\"\nversion: \"0.9.9\"\n")
	must(filepath.Join(dir, "manifests", "cm.yaml"),
		"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n  namespace: default\n")
	return dir
}

// TestPushPackDirRoundTripsThroughFetch is the whole contract of Task 3:
// PushPackDir must produce an artifact pack.Fetch's pullOCI can consume —
// push a pack directory, Fetch it back over the wire, and Render it.
func TestPushPackDirRoundTripsThroughFetch(t *testing.T) {
	host := localRegistry(t)
	dir := writeDemoPack(t)
	ref := "oci://" + host + "/packs/demo:0.9.9"

	digest, err := PushPackDir(context.Background(), dir, ref, "latest") // --also-tag path exercised in the same round-trip
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(digest, "sha256:") {
		t.Fatalf("digest: %q", digest)
	}

	p, err := pack.Fetch(context.Background(), ref, t.TempDir())
	if err != nil {
		t.Fatalf("Fetch after push: %v", err)
	}
	if p.Name != "demo" || p.Version != "0.9.9" {
		t.Fatalf("round-trip metadata: %+v", p)
	}
	r, err := p.Render(nil)
	if err != nil || len(r.Objects) != 1 {
		t.Fatalf("round-trip render: %v (%d objects)", err, len(r.Objects))
	}

	// --also-tag: the same digest must be fetchable via the extra tag.
	if p2, err := pack.Fetch(context.Background(), "oci://"+host+"/packs/demo:latest", t.TempDir()); err != nil || p2.Pinned != "oci:"+digest {
		t.Fatalf("also-tag fetch: %v (pinned %q, want oci:%s)", err, p2.Pinned, digest)
	}
}

// TestPushPackDirRejectsNonOCIRef pins the CUBE-4015 guard: anything that is
// not an oci:// ref fails fast, before any network or filesystem work.
func TestPushPackDirRejectsNonOCIRef(t *testing.T) {
	_, err := PushPackDir(context.Background(), t.TempDir(), "https://example.com/repo:tag")
	if err == nil || !strings.Contains(err.Error(), "CUBE-4015") {
		t.Fatalf("want CUBE-4015 for non-oci ref, got %v", err)
	}
}

// TestPushPackDirRequiresTag pins that a ref without :tag (or @digest) is
// rejected as CUBE-4015 — tag-defaulting is the CLI's job (cmd/pack.go), not
// the library's.
func TestPushPackDirRequiresTag(t *testing.T) {
	host := localRegistry(t)
	dir := writeDemoPack(t)
	_, err := PushPackDir(context.Background(), dir, "oci://"+host+"/packs/demo")
	if err == nil || !strings.Contains(err.Error(), "CUBE-4015") {
		t.Fatalf("want CUBE-4015 for untagged ref, got %v", err)
	}
}
