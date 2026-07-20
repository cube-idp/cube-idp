// pushdir.go publishes pack SOURCE directories (pack.cue + manifests/ +
// chart.yaml — the input side of the pack lifecycle) as OCI artifacts, the
// symmetric operation to push.go's PushRendered (which ships RENDERED
// manifests for engine delivery). The catalog workflow (`cube-idp pack
// push`) uses this to publish packs that `pack.Fetch(ctx, "oci://…", cache)`
// later pulls.
//
// Artifact shape: identical to PushRendered's flux shape (verified
// 2026-07-14: pack.Fetch's extractManifest handles flux-style single tar.gz
// layers) — one gzip-compressed tar layer of media type
// fluxContentMediaType holding the whole pack directory tree, packed by
// oras.PackManifest under fluxConfigMediaType with the same three
// org.opencontainers.image.* annotations push.go writes. The round-trip test
// (TestPushPackDirRoundTripsThroughFetch) is the arbiter of this contract.
//
// Unlike PushRendered — which only ever talks to the in-cluster zot tunnel
// and hardcodes PlainHTTP — PushPackDir targets real registries: it uses the
// ambient docker credential chain (docker login / GITHUB_TOKEN via
// docker/login-action in CI) and enables PlainHTTP only for
// 127.0.0.1/localhost hosts (same gate as internal/pack's pullOCI).
package oci

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	oras "oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/errdef"
	"oras.land/oras-go/v2/registry/remote"

	"github.com/cube-idp/cube-idp/internal/diag"
	"github.com/cube-idp/cube-idp/internal/pack"
)

// PushPackDir pushes the pack source directory at dir to ociRef (form:
// oci://host/repo:tag) as an artifact pack.Fetch can pull, returning the
// pushed manifest digest ("sha256:…"). alsoTags are extra tags applied to
// the same pushed manifest (one push, N tags — Owner Decisions #13;
// `pack push --also-tag latest` feeds this). The ref must carry an explicit
// tag: tag-defaulting from pack.cue's version is the CLI's job (cmd/pack.go).
// Every failure reports CUBE-4015.
func PushPackDir(ctx context.Context, dir, ociRef string, alsoTags ...string) (string, error) {
	if !strings.HasPrefix(ociRef, "oci://") {
		return "", diag.New(diag.CodePackPushFail,
			fmt.Sprintf("pack push target %q is not an oci:// reference", ociRef),
			"use the form oci://host/repo:tag")
	}
	repo, err := remote.NewRepository(strings.TrimPrefix(ociRef, "oci://"))
	if err != nil {
		return "", diag.Wrap(err, diag.CodePackPushFail, fmt.Sprintf("invalid OCI pack ref %q", ociRef),
			"use the form oci://host/repo:tag")
	}
	tag := repo.Reference.Reference
	if tag == "" {
		return "", diag.New(diag.CodePackPushFail, fmt.Sprintf("OCI pack ref %q has no tag", ociRef),
			"use the form oci://host/repo:tag (cube-idp pack push defaults the tag to the pack's version when omitted)")
	}

	// Ambient docker credential chain (pack.RegistryClient — one client
	// construction shared with the pull paths): docker login / CI
	// docker/login-action. A missing or unreadable docker config is not
	// fatal for anonymous-push registries, but surfacing it early beats a
	// cryptic 401 later.
	client, err := pack.RegistryClient()
	if err != nil {
		return "", diag.Wrap(err, diag.CodePackPushFail, "cannot load docker credential store",
			"check ~/.docker/config.json (run `docker login <registry>` to create it)")
	}
	repo.Client = client
	// Same gate as internal/pack's pullOCI: plain HTTP only for the loopback
	// registries (zot tunnel, in-process test registries).
	if pack.IsLocalRegistryHost(repo.Reference.Registry) {
		repo.PlainHTTP = true
	}

	digest, err := pushPackDirTo(ctx, dir, repo, append([]string{tag}, alsoTags...))
	if err != nil {
		return "", diag.Wrap(err, diag.CodePackPushFail,
			fmt.Sprintf("failed to push pack directory %s to %s", dir, ociRef),
			"check registry credentials (docker login) and that the tag is writable")
	}
	return digest, nil
}

// pushPackDirTo is the network-free seam PushPackDir delegates to (the
// pushRenderedTo pattern): production passes a *remote.Repository; tests may
// pass any oras.Target. It archives dir, packs the manifest, and applies
// every tag in tags to the one pushed manifest, returning its digest.
func pushPackDirTo(ctx context.Context, dir string, store oras.Target, tags []string) (string, error) {
	layer, err := buildDirLayer(dir)
	if err != nil {
		return "", err
	}

	layerDesc := content.NewDescriptorFromBytes(fluxContentMediaType, layer)
	if err := store.Push(ctx, layerDesc, bytes.NewReader(layer)); err != nil && !errors.Is(err, errdef.ErrAlreadyExists) {
		return "", fmt.Errorf("pushing pack layer: %w", err)
	}

	annotations := map[string]string{
		// fixed epoch, NOT wall time: identical content must republish to an
		// identical digest so the CI pack republish is a true no-op
		// (annotation consumers only need a valid RFC3339 value).
		ocispec.AnnotationCreated:  "1970-01-01T00:00:00Z",
		ocispec.AnnotationSource:   "cube-idp",
		ocispec.AnnotationRevision: tags[0],
	}
	manifestDesc, err := oras.PackManifest(ctx, store, oras.PackManifestVersion1_0, fluxConfigMediaType, oras.PackManifestOptions{
		Layers:              []ocispec.Descriptor{layerDesc},
		ManifestAnnotations: annotations,
	})
	if err != nil {
		return "", fmt.Errorf("packing manifest: %w", err)
	}

	for _, t := range tags {
		if err := store.Tag(ctx, manifestDesc, t); err != nil {
			return "", fmt.Errorf("tagging %s: %w", t, err)
		}
	}
	return manifestDesc.Digest.String(), nil
}

// buildDirLayer archives the pack directory tree at root as a
// gzip-compressed tar — buildArtifactLayer's tar/gzip mechanics generalized
// from one synthetic all.yaml entry to a real directory walk. Directories
// and regular files only (symlinks and irregular files are skipped: packs
// are data-only directories, and pullOCI's untar would not materialize them
// anyway); entry names are slash-separated paths relative to root, in
// filepath.WalkDir's deterministic lexical order.
func buildDirLayer(root string) ([]byte, error) {
	var tgz bytes.Buffer
	gw := gzip.NewWriter(&tgz)
	tw := tar.NewWriter(gw)

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil // the root itself needs no tar entry
		}
		name := filepath.ToSlash(rel)
		switch {
		case d.IsDir():
			return tw.WriteHeader(&tar.Header{
				Name:     name + "/",
				Typeflag: tar.TypeDir,
				Mode:     0o755,
			})
		case d.Type().IsRegular():
			info, err := d.Info()
			if err != nil {
				return err
			}
			if err := tw.WriteHeader(&tar.Header{
				Name:     name,
				Typeflag: tar.TypeReg,
				Mode:     0o644,
				Size:     info.Size(),
			}); err != nil {
				return err
			}
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			defer f.Close()
			_, err = io.Copy(tw, f)
			return err
		default:
			return nil // symlinks etc.: not part of the pack contract
		}
	})
	if err != nil {
		return nil, diag.Wrap(err, diag.CodePackPushFail,
			fmt.Sprintf("cannot archive pack directory %s", root),
			"check the directory exists and its files are readable")
	}
	if err := tw.Close(); err != nil {
		return nil, diag.Wrap(err, diag.CodePackPushFail, "cannot finalize pack archive tar",
			"this is a cube-idp bug — please report it")
	}
	if err := gw.Close(); err != nil {
		return nil, diag.Wrap(err, diag.CodePackPushFail, "cannot finalize pack archive gzip",
			"this is a cube-idp bug — please report it")
	}
	return tgz.Bytes(), nil
}
