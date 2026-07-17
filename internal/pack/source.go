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

	"golang.org/x/mod/sumdb/dirhash"

	"github.com/cube-idp/cube-idp/internal/diag"
)

// Fetch resolves ref to a local, on-disk pack directory and parses its
// pack.cue. ref forms (spec §4.4):
//
//	local directory path
//	oci://host/repo:tag  (or @digest)
//	<host>/<org>/<repo>[//subdir]@<tag|branch|sha>  (bare git grammar)
//	git::…, s3::…, http(s)://… (explicit go-getter forms)
//
// Any other URL scheme is rejected as CUBE-4001.
func Fetch(ctx context.Context, ref, cacheDir string) (*Pack, error) {
	switch {
	case strings.HasPrefix(ref, "oci://"):
		dir, digest, err := pullOCI(ctx, strings.TrimPrefix(ref, "oci://"), cacheDir)
		if err != nil {
			return nil, err
		}
		p, err := loadMeta(dir)
		if err != nil {
			return nil, err
		}
		p.Pinned = "oci:" + digest
		return p, nil
	case isGitRef(ref):
		return fetchGit(ctx, ref, cacheDir)
	case isGetterRef(ref):
		dst := filepath.Join(cacheDir, "getter", sanitizeRef(ref))
		if err := fetchGetter(ctx, ref, dst); err != nil {
			return nil, err
		}
		p, err := loadMeta(dst)
		if err != nil {
			return nil, err
		}
		// http/s3 refs have no upstream pin protocol: pin the fetched tree
		// with the same dirhash used for local directories.
		h, err := dirPin(dst)
		if err != nil {
			return nil, err
		}
		p.Pinned = h
		return p, nil
	case strings.Contains(ref, "://"):
		return nil, diag.New(diag.CodePackRefInvalid, fmt.Sprintf("unsupported pack ref scheme in %q", ref),
			"use a local directory path, oci://host/repo:tag, github.com/org/repo//path@rev, or an explicit go-getter URL (git::…, s3::…, https://…)")
	default: // local directory
		abs, err := filepath.Abs(ref)
		if err != nil {
			return nil, diag.Wrap(err, diag.CodePackRefInvalid, "bad pack path", "use a valid directory path")
		}
		if info, err := os.Stat(abs); err != nil || !info.IsDir() {
			return nil, diag.New(diag.CodePackRefInvalid, fmt.Sprintf("pack path %q is not a directory", ref),
				"use a valid directory path, or oci://host/repo:tag")
		}
		p, err := loadMeta(abs)
		if err != nil {
			return nil, err
		}
		h, err := dirPin(abs)
		if err != nil {
			return nil, err
		}
		p.Pinned = h
		return p, nil
	}
}

// dirPin computes the cube.lock pin for a plain, on-disk pack directory:
// local directory refs and http/s3 getter refs (Task 4), which have no
// upstream pin protocol of their own. Task 7's ResolveRemote reuses it.
func dirPin(abs string) (string, error) {
	h, err := dirhash.HashDir(abs, "", dirhash.Hash1)
	if err != nil {
		return "", diag.Wrap(err, diag.CodePackRefInvalid, "cannot hash pack directory",
			"check file permissions under the pack directory")
	}
	return "dir:" + h, nil
}

// pullOCI pulls the OCI artifact identified by ref (host/repo:tag, "oci://"
// already trimmed) into cacheDir and returns the extracted pack directory
// plus the pulled manifest digest (fed into Pack.Pinned as "oci:<digest>").
// Auth comes from the ambient docker credential chain (RegistryClient),
// falling back to anonymous; plain HTTP is used for 127.0.0.1/localhost
// registries (the zot port-forward tunnel). Every failure in this family
// (bad ref, network, corrupt artifact, extraction) reports CUBE-4012.
// Note: the digest-keyed cache only skips re-extraction — the registry
// round-trip (manifest resolve + blob fetch into the staging store) still
// happens on every call, so desc.Digest is always freshly resolved and
// never needs to be recovered from the cache-hit path.
func pullOCI(ctx context.Context, ref, cacheDir string) (dir string, digest string, err error) {
	repo, err := remote.NewRepository(ref)
	if err != nil {
		return "", "", diag.Wrap(err, diag.CodePackOCIErr, fmt.Sprintf("invalid OCI pack ref %q", ref),
			"use the form oci://host/repo:tag")
	}
	client, err := RegistryClient()
	if err != nil {
		return "", "", diag.Wrap(err, diag.CodePackOCIErr, "cannot load docker credential store",
			"check ~/.docker/config.json (run `docker login <registry>` to create it)")
	}
	repo.Client = client
	if IsLocalRegistryHost(repo.Reference.Registry) {
		repo.PlainHTTP = true
	}
	tagOrDigest := repo.Reference.Reference
	if tagOrDigest == "" {
		return "", "", diag.New(diag.CodePackOCIErr, fmt.Sprintf("OCI pack ref %q has no tag or digest", ref),
			"use the form oci://host/repo:tag")
	}

	staging, err := os.MkdirTemp(cacheDir, "pull-*")
	if err != nil {
		return "", "", diag.Wrap(err, diag.CodePackOCIErr, "cannot create pack cache staging dir", "check cacheDir permissions")
	}
	defer os.RemoveAll(staging)

	store, err := oci.New(staging)
	if err != nil {
		return "", "", diag.Wrap(err, diag.CodePackOCIErr, "cannot create local OCI content store", "check cacheDir permissions")
	}

	desc, err := oras.Copy(ctx, repo, tagOrDigest, store, tagOrDigest, oras.DefaultCopyOptions)
	if err != nil {
		return "", "", diag.Wrap(err, diag.CodePackOCIErr, fmt.Sprintf("cannot pull pack %q", ref),
			"check the pack reference, registry availability, and network — a 401/403 from a private registry means missing credentials (run `docker login <host>`); re-run with the same command")
	}

	destDir := filepath.Join(cacheDir, sanitizeRepoDigest(repo.Reference.Repository, string(desc.Digest)))
	if info, err := os.Stat(filepath.Join(destDir, "pack.cue")); err == nil && !info.IsDir() {
		return destDir, string(desc.Digest), nil // already extracted by a previous pull
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", "", diag.Wrap(err, diag.CodePackOCIErr, "cannot create pack cache dir", "check cacheDir permissions")
	}
	if err := extractManifest(ctx, store, desc, destDir); err != nil {
		return "", "", err
	}
	return destDir, string(desc.Digest), nil
}

// IsLocalRegistryHost reports whether host (optionally host:port) is a
// loopback registry — the only case where plain HTTP is acceptable; the ONE
// shared definition (Phase 4 R8).
func IsLocalRegistryHost(host string) bool {
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
