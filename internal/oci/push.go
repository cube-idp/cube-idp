// Package oci pushes rendered packs as Flux-compatible OCI artifacts to the
// in-cluster zot registry (spec §4, "OCI push").
//
// Implementation note (Task 3.5): this used to shell out to
// github.com/fluxcd/pkg/oci's Client.Push (see git history for the previous
// implementation note on that API). This rewrite is on plain oras-go v2 —
// already a dependency via internal/pack's pull path — to retire the
// fluxcd/pkg/oci module as debt paydown. The artifact shape it produces is
// pinned to match fluxcd/pkg/oci v0.69.0's Client.Push(..., LayerTypeTarball)
// exactly, because that shape is what makes Flux's OCIRepository (embedded
// CRD at internal/engine/flux/manifests/install.yaml,
// source.toolkit.fluxcd.io/v1) reconcile without a spec.layerSelector:
// "When not specified, the first layer found in the artifact is selected."
//
//   - Config blob media type: application/vnd.cncf.flux.config.v1+json
//     (fluxcd/pkg/oci's CanonicalConfigMediaType). Content is the two-byte
//     empty JSON object "{}" — oras.PackManifest's standard stand-in for an
//     artifact with no meaningful config; source-controller does not
//     validate config bytes, only the manifest's config media type.
//   - Single layer media type: application/vnd.cncf.flux.content.v1.tar+gzip
//     (fluxcd/pkg/oci's CanonicalContentMediaType), a gzip-compressed tar
//     containing exactly one entry, "all.yaml" (the same entry name the
//     previous implementation wrote via os.WriteFile(dir, "all.yaml", ...)
//     before handing the directory to fluxcd/pkg/tar.Tar).
//   - Manifest annotations carry org.opencontainers.image.created/.source/
//     .revision — the same three keys fluxcd/pkg/oci.Metadata.ToAnnotations
//     produced (Source constant there was always literally "cube-idp").
//
// zot is plain HTTP everywhere in this design (see registry.InClusterURL /
// OCIRepository.spec.insecure), so PushRendered's production oras.Target is
// a *remote.Repository with PlainHTTP set — the equivalent of the previous
// implementation's crane.Insecure option for the 127.0.0.1 zot port-forward
// tunnel.
package oci

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"time"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"sigs.k8s.io/yaml"

	oras "oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/errdef"
	"oras.land/oras-go/v2/registry/remote"

	"github.com/rafpe/cube-idp/internal/diag"
	"github.com/rafpe/cube-idp/internal/engine"
	"github.com/rafpe/cube-idp/internal/pack"
)

// fluxConfigMediaType and fluxContentMediaType are fluxcd/pkg/oci v0.69.0's
// CanonicalConfigMediaType and CanonicalContentMediaType, reproduced here
// (that module is no longer a dependency — see the package doc). Do not
// change these: source-controller's OCIRepository relies on the content
// media type to auto-select the single layer, with no layerSelector
// configured on the Deliver side.
const (
	fluxConfigMediaType  = "application/vnd.cncf.flux.config.v1+json"
	fluxContentMediaType = "application/vnd.cncf.flux.content.v1.tar+gzip"

	// artifactEntryName is the tar entry name for the rendered multi-doc
	// YAML, matching what the previous fluxcd/pkg/oci-based implementation
	// wrote to the staging directory before archiving it.
	artifactEntryName = "all.yaml"
)

// PushRendered writes r.Objects as one all.yaml and pushes it as a
// Flux-compatible OCI artifact to <registryAddr>/packs/<name>:<version>.
// registryAddr is a plain host:port (no scheme) — e.g. the 127.0.0.1
// port-forward tunnel to the in-cluster zot registry.
func PushRendered(ctx context.Context, r *pack.Rendered, registryAddr string) (engine.ArtifactRef, error) {
	repoRef := fmt.Sprintf("%s/packs/%s", registryAddr, r.Name)
	repo, err := remote.NewRepository(repoRef)
	if err != nil {
		return engine.ArtifactRef{}, diag.Wrap(err, diag.CodeOCIPushFail,
			fmt.Sprintf("invalid registry reference %q", repoRef),
			"this is a cube-idp bug — please report it")
	}
	repo.PlainHTTP = true // zot is plain HTTP; see package doc above

	return pushRenderedTo(ctx, r, repo)
}

// pushRenderedTo is the network-free seam PushRendered delegates to:
// production passes a *remote.Repository (plain HTTP, as above); tests pass
// an in-memory oras.Target (oras-go v2's content/memory.Store).
func pushRenderedTo(ctx context.Context, r *pack.Rendered, store oras.Target) (engine.ArtifactRef, error) {
	layer, err := buildArtifactLayer(r)
	if err != nil {
		return engine.ArtifactRef{}, err
	}

	layerDesc := content.NewDescriptorFromBytes(fluxContentMediaType, layer)
	if err := store.Push(ctx, layerDesc, bytes.NewReader(layer)); err != nil && !errors.Is(err, errdef.ErrAlreadyExists) {
		return engine.ArtifactRef{}, diag.Wrap(err, diag.CodeOCIPushFail,
			fmt.Sprintf("failed to push pack %q layer to the in-cluster registry", r.Name),
			"re-run `cube-idp up`; check that the zot pod is running")
	}

	annotations := map[string]string{
		ocispec.AnnotationCreated:  time.Now().UTC().Format(time.RFC3339),
		ocispec.AnnotationSource:   "cube-idp",
		ocispec.AnnotationRevision: r.Version,
	}
	manifestDesc, err := oras.PackManifest(ctx, store, oras.PackManifestVersion1_0, fluxConfigMediaType, oras.PackManifestOptions{
		Layers:              []ocispec.Descriptor{layerDesc},
		ManifestAnnotations: annotations,
	})
	if err != nil {
		return engine.ArtifactRef{}, diag.Wrap(err, diag.CodeOCIPushFail,
			fmt.Sprintf("failed to push pack %q manifest to the in-cluster registry", r.Name),
			"re-run `cube-idp up`; check that the zot pod is running")
	}

	ref := engine.ArtifactRef{Repo: "packs/" + r.Name, Tag: r.Version}
	if err := store.Tag(ctx, manifestDesc, ref.Tag); err != nil {
		return engine.ArtifactRef{}, diag.Wrap(err, diag.CodeOCIPushFail,
			fmt.Sprintf("failed to tag pack %q %s in the in-cluster registry", r.Name, ref.Tag),
			"re-run `cube-idp up`; check that the zot pod is running")
	}
	return ref, nil
}

// buildArtifactLayer renders r.Objects as one multi-doc YAML file and
// archives it as a gzip-compressed tar containing a single entry,
// artifactEntryName — the Flux content layer shape (see package doc).
func buildArtifactLayer(r *pack.Rendered) ([]byte, error) {
	var yamlBuf bytes.Buffer
	for _, o := range r.Objects {
		y, err := yaml.Marshal(o.Object)
		if err != nil {
			return nil, diag.Wrap(err, diag.CodeOCIPushFail, "cannot marshal rendered object to YAML",
				"this is a cube-idp bug — please report it")
		}
		yamlBuf.WriteString("---\n")
		yamlBuf.Write(y)
	}

	var tgz bytes.Buffer
	gw := gzip.NewWriter(&tgz)
	tw := tar.NewWriter(gw)
	hdr := &tar.Header{
		Name:     artifactEntryName,
		Typeflag: tar.TypeReg,
		Mode:     0o644,
		Size:     int64(yamlBuf.Len()),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return nil, diag.Wrap(err, diag.CodeOCIPushFail, "cannot write staged artifact tar header",
			"this is a cube-idp bug — please report it")
	}
	if _, err := tw.Write(yamlBuf.Bytes()); err != nil {
		return nil, diag.Wrap(err, diag.CodeOCIPushFail, "cannot write staged artifact tar content",
			"this is a cube-idp bug — please report it")
	}
	if err := tw.Close(); err != nil {
		return nil, diag.Wrap(err, diag.CodeOCIPushFail, "cannot finalize staged artifact tar",
			"this is a cube-idp bug — please report it")
	}
	if err := gw.Close(); err != nil {
		return nil, diag.Wrap(err, diag.CodeOCIPushFail, "cannot finalize staged artifact gzip",
			"this is a cube-idp bug — please report it")
	}
	return tgz.Bytes(), nil
}
