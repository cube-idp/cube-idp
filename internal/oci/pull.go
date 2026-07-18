// pull.go is the consumer twin of push.go for single-blob artifacts (P10,
// GT17). The plugins platform publishes each per-platform binary — and its
// discovery index — as a one-layer OCI artifact; PullBlob fetches such an
// artifact BY DIGEST (or tag) and returns the raw layer bytes, mirroring
// internal/pack.pullOCI's auth chain and loopback-PlainHTTP gate so private
// GHCR namespaces and the in-process test registry behave identically. It is
// deliberately generic (no plugin-specific decoding) so both the index
// artifact and the per-platform binary artifact share one fetch path.
package oci

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/registry/remote"

	"github.com/cube-idp/cube-idp/internal/diag"
	"github.com/cube-idp/cube-idp/internal/pack"
)

// Media types for the plugins platform (GT17). PluginBlobMediaType is both
// the oras --artifact-type and the single layer's media type on a
// per-platform binary artifact; PluginIndexMediaType is the artifactType on
// the discovery index (its layer is application/json). Kept here beside the
// only code that resolves them so the producer (plugins-repo CI) and this
// consumer name the exact same strings.
const (
	PluginBlobMediaType  = "application/vnd.cube-idp.plugin.v1"
	PluginIndexMediaType = "application/vnd.cube-idp.plugin.index.v1"
)

// maxBlobBytes caps how much of a single-blob artifact PullBlob will read
// into memory. Plugin binaries and the index JSON are both small; anything
// past this is refused rather than silently streamed unbounded.
const maxBlobBytes = 256 * 1024 * 1024 // 256 MiB

// PullBlob resolves ref (host/repo@digest or host/repo:tag, no "oci://"
// scheme) with the ambient docker credential chain, fetches its manifest,
// and returns the bytes of its single layer. Loopback registries
// (127.0.0.1/localhost) use plain HTTP, matching the zot tunnel and the
// in-process test registry. Every failure reports CUBE-7102 (plugin
// install/index IO) — the resolver in internal/plugin owns the higher-level
// codes (7101 not-found, 7106 no-platform).
func PullBlob(ctx context.Context, ref string) ([]byte, error) {
	const failRemediation = "check the reference, the registry's availability, and network — a 401/403 from a private registry means missing credentials (run `docker login <host>`)"

	repo, err := remote.NewRepository(ref)
	if err != nil {
		return nil, diag.Wrap(err, diag.CodePluginTrustIO, fmt.Sprintf("invalid OCI reference %q", ref),
			"use the form host/repo:tag or host/repo@sha256:…")
	}
	client, err := pack.RegistryClient()
	if err != nil {
		return nil, diag.Wrap(err, diag.CodePluginTrustIO, "cannot load docker credential store",
			"check ~/.docker/config.json (run `docker login <registry>` to create it)")
	}
	repo.Client = client
	if pack.IsLocalRegistryHost(repo.Reference.Registry) {
		repo.PlainHTTP = true
	}
	tagOrDigest := repo.Reference.Reference
	if tagOrDigest == "" {
		return nil, diag.New(diag.CodePluginTrustIO, fmt.Sprintf("OCI reference %q has no tag or digest", ref),
			"pin the reference by digest (host/repo@sha256:…) or tag (host/repo:tag)")
	}

	_, rc, err := repo.FetchReference(ctx, tagOrDigest)
	if err != nil {
		return nil, diag.Wrap(err, diag.CodePluginTrustIO, fmt.Sprintf("cannot fetch %q", ref), failRemediation)
	}
	manifestBytes, err := readAllCapped(rc)
	rc.Close()
	if err != nil {
		return nil, diag.Wrap(err, diag.CodePluginTrustIO, fmt.Sprintf("cannot read manifest for %q", ref), failRemediation)
	}

	var manifest ocispec.Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return nil, diag.Wrap(err, diag.CodePluginTrustIO, fmt.Sprintf("artifact %q has an unreadable manifest", ref),
			"the artifact is not a single-blob OCI image manifest — re-publish it")
	}
	// GT17 artifacts carry exactly one layer (the binary, or the index JSON).
	// Anything else is not a plugin artifact this binary understands.
	layers := manifest.Layers
	if len(layers) != 1 {
		return nil, diag.New(diag.CodePluginTrustIO,
			fmt.Sprintf("artifact %q has %d layers, want exactly 1", ref, len(layers)),
			"this is not a cube-idp single-blob plugin artifact — check the reference")
	}

	layerRC, err := repo.Fetch(ctx, layers[0])
	if err != nil {
		return nil, diag.Wrap(err, diag.CodePluginTrustIO, fmt.Sprintf("cannot fetch the blob of %q", ref), failRemediation)
	}
	defer layerRC.Close()
	// content.VerifyReader checks the fetched bytes against the layer
	// descriptor's size+digest — the pull is digest-verified end to end.
	verified := content.NewVerifyReader(layerRC, layers[0])
	data, err := readAllCapped(verified)
	if err != nil {
		return nil, diag.Wrap(err, diag.CodePluginTrustIO, fmt.Sprintf("cannot read the blob of %q", ref), failRemediation)
	}
	if err := verified.Verify(); err != nil {
		return nil, diag.Wrap(err, diag.CodePluginTrustIO, fmt.Sprintf("blob digest mismatch for %q", ref),
			"the artifact changed since it was pinned — do not install; report it")
	}
	return data, nil
}

// readAllCapped reads r into memory, refusing anything past maxBlobBytes
// rather than streaming unbounded content from a registry.
func readAllCapped(r io.Reader) ([]byte, error) {
	limited := io.LimitReader(r, maxBlobBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if len(data) > maxBlobBytes {
		return nil, fmt.Errorf("blob exceeds the %d-byte limit", maxBlobBytes)
	}
	return data, nil
}
