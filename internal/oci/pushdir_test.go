package oci

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cube-idp/cube-idp/internal/oci/ocitest"
	"github.com/cube-idp/cube-idp/internal/pack"
)

// localRegistry and writeDemoPack delegate to the shared ocitest package
// both this file and internal/bundle's vendor tests need the same
// in-process-registry-plus-demo-pack fixture, so it lives in one place
// rather than being copy-pasted per package.
func localRegistry(t *testing.T) string { return ocitest.LocalRegistry(t) }
func writeDemoPack(t *testing.T) string { return ocitest.WriteDemoPack(t) }

// TestPushPackDirRoundTripsThroughFetch is the whole contract of PushPackDir:
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

// TestPushPackDirIsContentAddressed proves that pushing the identical pack
// directory twice produces the identical digest — a fixed epoch annotation
// (not wall-clock time.Now) is what makes the CI pack republish a true no-op
// republish. The sleep spans a wall-clock second boundary: with the old
// time.Now()-based annotation, RFC3339's second granularity meant this test
// only intermittently caught the bug (two fast in-process pushes often land
// in the same second); the delay makes the RED failure reliable.
func TestPushPackDirIsContentAddressed(t *testing.T) {
	host := localRegistry(t)
	dir := writeDemoPack(t)
	ref := "oci://" + host + "/packs/demo:0.9.9"

	digest1, err := PushPackDir(context.Background(), dir, ref)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond)
	digest2, err := PushPackDir(context.Background(), dir, ref)
	if err != nil {
		t.Fatal(err)
	}
	if digest1 != digest2 {
		t.Fatalf("republish of identical content changed digest: %q != %q", digest1, digest2)
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
