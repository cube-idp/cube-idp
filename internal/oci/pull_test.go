// pull_test.go pins the single-blob OCI pull (P10): the plugins platform
// (GT17) publishes each per-platform binary as a one-layer artifact whose
// layer media type is application/vnd.cube-idp.plugin.v1, and `plugin
// install` pulls it BY DIGEST off the discovery index. PullBlob is the
// generic fetch that returns those raw layer bytes. Tests live in this
// package (not internal/plugin) because seeding the artifact needs an
// in-process registry and go-containerregistry is the ocitest test-only
// dependency's one home.
package oci

import (
	"bytes"
	"context"
	"errors"
	"testing"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	oras "oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/registry/remote"

	"github.com/cube-idp/cube-idp/internal/diag"
	"github.com/cube-idp/cube-idp/internal/oci/ocitest"
	"github.com/cube-idp/cube-idp/internal/pack"
)

// pushBlobArtifact publishes payload as a single-layer blob artifact at
// oci://<host>/<repo>:<tag> with the GT17 plugin layer media type, and
// returns the pushed manifest digest — the digest form `plugin install`
// pins from the index. Byte-for-byte the shape the plugins-repo publish.yml
// produces via `oras push … --artifact-type application/vnd.cube-idp.plugin.v1`.
func pushBlobArtifact(t *testing.T, host, repo, tag string, payload []byte) string {
	t.Helper()
	ref := host + "/" + repo
	r, err := remote.NewRepository(ref + ":" + tag)
	if err != nil {
		t.Fatalf("NewRepository: %v", err)
	}
	r.PlainHTTP = true
	if client, cerr := pack.RegistryClient(); cerr == nil {
		r.Client = client // honor SetDockerAuth for authed-registry fixtures
	}

	ctx := context.Background()
	layer := content.NewDescriptorFromBytes(PluginBlobMediaType, payload)
	if err := r.Push(ctx, layer, bytes.NewReader(payload)); err != nil {
		t.Fatalf("push layer: %v", err)
	}
	manifestDesc, err := oras.PackManifest(ctx, r, oras.PackManifestVersion1_1, PluginBlobMediaType, oras.PackManifestOptions{
		Layers:              []ocispec.Descriptor{layer},
		ManifestAnnotations: map[string]string{ocispec.AnnotationCreated: "1970-01-01T00:00:00Z"},
	})
	if err != nil {
		t.Fatalf("PackManifest: %v", err)
	}
	if err := r.Tag(ctx, manifestDesc, tag); err != nil {
		t.Fatalf("tag: %v", err)
	}
	return manifestDesc.Digest.String()
}

// TestPullBlobByDigestReturnsLayerBytes: PullBlob resolves a digest-pinned
// ref and returns exactly the single layer's bytes.
func TestPullBlobByDigestReturnsLayerBytes(t *testing.T) {
	host := ocitest.LocalRegistry(t)
	payload := []byte("#!/bin/sh\necho cube-idp-hello 0.1.0\n")
	digest := pushBlobArtifact(t, host, "plugins/hello", "0.1.0-linux-amd64", payload)

	got, err := PullBlob(context.Background(), host+"/plugins/hello@"+digest)
	if err != nil {
		t.Fatalf("PullBlob: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("PullBlob returned %q, want %q", got, payload)
	}
}

// TestPullBlobAuthenticatesWithDockerCredentials: a private registry that
// demands basic auth must be pulled with the ambient docker credential
// chain, mirroring pullOCI — not surfaced as an anonymous 401.
func TestPullBlobAuthenticatesWithDockerCredentials(t *testing.T) {
	host := ocitest.LocalRegistryWithBasicAuth(t, "cube", "s3cret")
	ocitest.SetDockerAuth(t, host, "cube", "s3cret")
	payload := []byte("binary-bytes")
	digest := pushBlobArtifact(t, host, "plugins/hello", "0.1.0-linux-amd64", payload)

	got, err := PullBlob(context.Background(), host+"/plugins/hello@"+digest)
	if err != nil {
		t.Fatalf("PullBlob through basic-auth registry: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("PullBlob returned %q, want %q", got, payload)
	}
}

// TestPullBlobRejectsRefWithoutReference: a ref with no tag or digest is a
// typed CUBE-7102 error, never a silent pull of "latest".
func TestPullBlobRejectsRefWithoutReference(t *testing.T) {
	_, err := PullBlob(context.Background(), "example.invalid/plugins/hello")
	if err == nil {
		t.Fatal("a ref with no tag/digest must error")
	}
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != diag.CodePluginTrustIO {
		t.Fatalf("want CUBE-7102, got %v", err)
	}
}
