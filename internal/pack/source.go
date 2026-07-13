package pack

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	oras "oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/oci"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"

	"github.com/rafpe/cube-idp/internal/diag"
)

// Fetch resolves ref to a local, on-disk pack directory and parses its
// pack.cue. ref forms (spec §4.4 MVP):
//
//	local directory path
//	oci://host/repo:tag  (or @digest)
//
// git refs land in Phase 2. Any other URL scheme is rejected as CUBE-4001.
func Fetch(ctx context.Context, ref, cacheDir string) (*Pack, error) {
	switch {
	case strings.HasPrefix(ref, "oci://"):
		dir, err := pullOCI(ctx, strings.TrimPrefix(ref, "oci://"), cacheDir)
		if err != nil {
			return nil, err
		}
		return loadMeta(dir)
	case strings.Contains(ref, "://"):
		return nil, diag.New(diag.CodePackRefInvalid, fmt.Sprintf("unsupported pack ref scheme in %q", ref),
			"use a local directory path or oci://host/repo:tag (git refs arrive in Phase 2)")
	default: // local directory
		abs, err := filepath.Abs(ref)
		if err != nil {
			return nil, diag.Wrap(err, diag.CodePackRefInvalid, "bad pack path", "use a valid directory path")
		}
		if info, err := os.Stat(abs); err != nil || !info.IsDir() {
			return nil, diag.New(diag.CodePackRefInvalid, fmt.Sprintf("pack path %q is not a directory", ref),
				"use a valid directory path, or oci://host/repo:tag")
		}
		return loadMeta(abs)
	}
}

// pullOCI pulls the OCI artifact identified by ref (host/repo:tag, "oci://"
// already trimmed) into cacheDir and returns the extracted pack directory.
// Anonymous auth only (Phase 1); plain HTTP is used for 127.0.0.1/localhost
// registries (the zot port-forward tunnel). Every failure in this family
// (bad ref, network, corrupt artifact, extraction) reports CUBE-4012.
// Note: the digest-keyed cache only skips re-extraction — the registry
// round-trip (manifest resolve + blob fetch into the staging store) still
// happens on every call.
func pullOCI(ctx context.Context, ref, cacheDir string) (string, error) {
	repo, err := remote.NewRepository(ref)
	if err != nil {
		return "", diag.Wrap(err, diag.CodePackOCIErr, fmt.Sprintf("invalid OCI pack ref %q", ref),
			"use the form oci://host/repo:tag")
	}
	repo.Client = auth.DefaultClient
	if isLocalRegistryHost(repo.Reference.Registry) {
		repo.PlainHTTP = true
	}
	tagOrDigest := repo.Reference.Reference
	if tagOrDigest == "" {
		return "", diag.New(diag.CodePackOCIErr, fmt.Sprintf("OCI pack ref %q has no tag or digest", ref),
			"use the form oci://host/repo:tag")
	}

	staging, err := os.MkdirTemp(cacheDir, "pull-*")
	if err != nil {
		return "", diag.Wrap(err, diag.CodePackOCIErr, "cannot create pack cache staging dir", "check cacheDir permissions")
	}
	defer os.RemoveAll(staging)

	store, err := oci.New(staging)
	if err != nil {
		return "", diag.Wrap(err, diag.CodePackOCIErr, "cannot create local OCI content store", "check cacheDir permissions")
	}

	desc, err := oras.Copy(ctx, repo, tagOrDigest, store, tagOrDigest, oras.DefaultCopyOptions)
	if err != nil {
		return "", diag.Wrap(err, diag.CodePackOCIErr, fmt.Sprintf("cannot pull pack %q", ref),
			"check the pack reference, registry availability, and network; re-run with the same command")
	}

	destDir := filepath.Join(cacheDir, sanitizeRepoDigest(repo.Reference.Repository, string(desc.Digest)))
	if info, err := os.Stat(filepath.Join(destDir, "pack.cue")); err == nil && !info.IsDir() {
		return destDir, nil // already extracted by a previous pull
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", diag.Wrap(err, diag.CodePackOCIErr, "cannot create pack cache dir", "check cacheDir permissions")
	}
	if err := extractManifest(ctx, store, desc, destDir); err != nil {
		return "", err
	}
	return destDir, nil
}

func isLocalRegistryHost(host string) bool {
	h := host
	if i := strings.IndexByte(h, ':'); i != -1 {
		h = h[:i]
	}
	return h == "127.0.0.1" || h == "localhost"
}

func sanitizeRepoDigest(repo, digest string) string {
	safe := strings.NewReplacer("/", "_", ":", "-").Replace(repo)
	return safe + "@" + strings.ReplaceAll(digest, ":", "-")
}

// extractManifest reads the pulled manifest out of store and materializes
// its layers under destDir. Two artifact shapes are supported:
//   - oras-style: one layer per file, each carrying an
//     org.opencontainers.image.title annotation with its relative path.
//   - Flux-style: a single gzipped tarball layer holding the whole pack
//     directory.
func extractManifest(ctx context.Context, store *oci.Store, desc ocispec.Descriptor, destDir string) error {
	rc, err := store.Fetch(ctx, desc)
	if err != nil {
		return diag.Wrap(err, diag.CodePackOCIErr, "cannot read pulled pack manifest", "this is a cube-idp bug — please report it")
	}
	var manifest ocispec.Manifest
	err = func() error {
		defer rc.Close()
		return json.NewDecoder(rc).Decode(&manifest)
	}()
	if err != nil {
		return diag.Wrap(err, diag.CodePackOCIErr, "pulled pack manifest is not valid OCI JSON", "this is a cube-idp bug — please report it")
	}

	wrote := false
	for _, layer := range manifest.Layers {
		if err := extractLayer(ctx, store, layer, destDir); err != nil {
			return err
		}
		wrote = true
	}
	if !wrote {
		return diag.New(diag.CodePackOCIErr, "pulled pack artifact has no layers", "check the pack was published correctly")
	}
	return nil
}

func extractLayer(ctx context.Context, store *oci.Store, layer ocispec.Descriptor, destDir string) error {
	rc, err := store.Fetch(ctx, layer)
	if err != nil {
		return diag.Wrap(err, diag.CodePackOCIErr, "cannot read pulled pack layer", "this is a cube-idp bug — please report it")
	}
	defer rc.Close()

	switch {
	case strings.Contains(layer.MediaType, "tar+gzip") || strings.Contains(layer.MediaType, "tar.gzip"):
		gr, err := gzip.NewReader(rc)
		if err != nil {
			return diag.Wrap(err, diag.CodePackOCIErr, "pulled pack layer is not valid gzip", "this is a cube-idp bug — please report it")
		}
		defer gr.Close()
		return untar(gr, destDir)
	case strings.Contains(layer.MediaType, "tar"):
		return untar(rc, destDir)
	default:
		title := layer.Annotations[ocispec.AnnotationTitle]
		if title == "" {
			return nil // unrecognized, untitled blob (e.g. a config blob) — skip
		}
		return writeFile(destDir, title, rc)
	}
}

func untar(r io.Reader, destDir string) error {
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return diag.Wrap(err, diag.CodePackOCIErr, "pulled pack tarball is corrupt", "this is a cube-idp bug — please report it")
		}
		target, err := safeJoin(destDir, hdr.Name)
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return diag.Wrap(err, diag.CodePackOCIErr, "cannot extract pulled pack", "check cacheDir permissions")
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return diag.Wrap(err, diag.CodePackOCIErr, "cannot extract pulled pack", "check cacheDir permissions")
			}
			if err := writeFile(filepath.Dir(target), filepath.Base(target), tr); err != nil {
				return err
			}
		}
	}
}

func writeFile(destDir, relPath string, r io.Reader) error {
	target, err := safeJoin(destDir, relPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return diag.Wrap(err, diag.CodePackOCIErr, "cannot extract pulled pack", "check cacheDir permissions")
	}
	f, err := os.Create(target)
	if err != nil {
		return diag.Wrap(err, diag.CodePackOCIErr, "cannot extract pulled pack", "check cacheDir permissions")
	}
	defer f.Close()
	if _, err := io.Copy(f, r); err != nil {
		return diag.Wrap(err, diag.CodePackOCIErr, "cannot extract pulled pack", "check cacheDir permissions")
	}
	return nil
}

// safeJoin joins destDir and rel, rejecting any path that would escape
// destDir (zip-slip protection for hostile/malformed tar entries).
func safeJoin(destDir, rel string) (string, error) {
	target := filepath.Join(destDir, rel)
	if target != destDir && !strings.HasPrefix(target, destDir+string(filepath.Separator)) {
		return "", diag.New(diag.CodePackOCIErr, fmt.Sprintf("pulled pack contains an unsafe path %q", rel),
			"this is a cube-idp bug — please report it")
	}
	return target, nil
}
