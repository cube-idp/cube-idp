// pullauth_test.go pins the PULL side of the docker credential chain: packs
// published to a private registry (private GHCR namespaces — the CUBE-4012
// report of 2026-07-17) must be fetchable and resolvable with `docker login`
// credentials, exactly as PushPackDir already publishes with them. The tests
// live here rather than in internal/pack because seeding the authed registry
// needs PushPackDir, and internal/pack cannot import internal/oci (cycle).
package oci

import (
	"context"
	"testing"

	"github.com/cube-idp/cube-idp/internal/oci/ocitest"
	"github.com/cube-idp/cube-idp/internal/pack"
)

// TestFetchUsesDockerCredentials: pack.Fetch must authenticate with the
// ambient docker credential chain when the registry demands it, instead of
// pulling anonymously and surfacing the registry's 401 as CUBE-4012.
func TestFetchUsesDockerCredentials(t *testing.T) {
	host := ocitest.LocalRegistryWithBasicAuth(t, "cube", "s3cret")
	ocitest.SetDockerAuth(t, host, "cube", "s3cret")
	dir := writeDemoPack(t)
	ref := "oci://" + host + "/packs/demo:0.9.9"

	if _, err := PushPackDir(context.Background(), dir, ref); err != nil {
		t.Fatalf("PushPackDir through basic-auth registry: %v", err)
	}
	p, err := pack.Fetch(context.Background(), ref, t.TempDir())
	if err != nil {
		t.Fatalf("Fetch through basic-auth registry: %v", err)
	}
	if p.Name != "demo" || p.Version != "0.9.9" {
		t.Fatalf("round-trip metadata: %+v", p)
	}
}

// TestResolveRemoteUsesDockerCredentials: the upgrade --plan probe must see
// the same registries Fetch sees — including private ones.
func TestResolveRemoteUsesDockerCredentials(t *testing.T) {
	host := ocitest.LocalRegistryWithBasicAuth(t, "cube", "s3cret")
	ocitest.SetDockerAuth(t, host, "cube", "s3cret")
	dir := writeDemoPack(t)
	ref := "oci://" + host + "/packs/demo:0.9.9"

	digest, err := PushPackDir(context.Background(), dir, ref)
	if err != nil {
		t.Fatalf("PushPackDir through basic-auth registry: %v", err)
	}
	pin, err := pack.ResolveRemote(context.Background(), ref, t.TempDir())
	if err != nil {
		t.Fatalf("ResolveRemote through basic-auth registry: %v", err)
	}
	if pin != "oci:"+digest {
		t.Fatalf("pin %q, want %q", pin, "oci:"+digest)
	}
}
