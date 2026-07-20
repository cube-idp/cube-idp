package bundle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	oras "oras.land/oras-go/v2"
	ocicontent "oras.land/oras-go/v2/content/oci"
	"oras.land/oras-go/v2/registry/remote"

	"golang.org/x/mod/sumdb/dirhash"

	"github.com/cube-idp/cube-idp/internal/diag"
	"github.com/cube-idp/cube-idp/internal/lock"
	"github.com/cube-idp/cube-idp/internal/pack"
	"github.com/cube-idp/cube-idp/internal/registry"
	"github.com/cube-idp/cube-idp/internal/ui"
)

// Vendor reads the cube.lock at lockPath and writes a self-contained
// air-gap bundle to outPath: every locked pack source at its pin, and every
// image referenced anywhere in the cube — per-pack Entry.Images (including
// images a pack declares in its own pack.cue images: list, which operators
// pull at runtime and which appear in no rendered manifest) plus the
// GitOps engine's and the in-cluster registry's own install images — as one
// per-image OCI-layout tar apiece. platform selects which image variant is pulled ("os/arch";
// "" defaults to "linux/"+runtime.GOARCH — bundle consumers are Linux
// cluster nodes, so the OS half is never the host's; only the arch follows
// the host as a pragmatic default).
//
// A bundle is complete or an error: any pull failure aborts the whole run
// with CUBE-7002 naming the artifact/image that failed — there is no
// partial-success bundle. The lockfile itself missing or unreadable is
// CUBE-7001.
//
// Vendor is a pure lock consumer: it never touches a cluster and never
// mutates cube.yaml/cube.lock.
func Vendor(ctx context.Context, lockPath, outPath, platform string, con *ui.Console) error {
	raw, err := os.ReadFile(lockPath)
	if err != nil {
		return diag.Wrap(err, diag.CodeVendorLockMissing, fmt.Sprintf("cannot read %s", lockPath),
			"run `cube-idp up` first — vendor bundles exactly what the lockfile pins")
	}
	lf, err := lock.Read(lockPath)
	if err != nil {
		return diag.Wrap(err, diag.CodeVendorLockMissing, lockPath+" is not a valid cube.lock",
			"re-run `cube-idp up` to regenerate it")
	}
	if lf == nil {
		// lock.Read's (nil, nil) missing-file case is unreachable here (the
		// raw read above would already have failed), but mapped explicitly
		// so a future refactor can't turn this into a nil-pointer panic.
		return diag.New(diag.CodeVendorLockMissing, lockPath+" is empty or missing", "run `cube-idp up` first")
	}
	if lf.Engine.Ref == "" {
		return diag.New(diag.CodeVendorLockMissing,
			lockPath+" predates the engine-as-pack change (no engine pack entry)",
			"re-run `cube-idp up` to regenerate cube.lock, then vendor again")
	}

	if platform == "" {
		// Bundle consumers are Linux cluster nodes and container images
		// publish only linux manifests, so the OS half is always "linux"
		// regardless of host — a darwin/windows default would always 404
		// against the registry's manifest list. Only arch follows the host,
		// as a pragmatic default for the common case.
		platform = "linux/" + runtime.GOARCH
	}
	plat, err := parsePlatform(platform)
	if err != nil {
		return err
	}

	stage, err := os.MkdirTemp("", "cube-idp-vendor-*")
	if err != nil {
		return diag.Wrap(err, diag.CodeVendorPullFail, "cannot create staging directory", "check TMPDIR permissions and disk space")
	}
	defer os.RemoveAll(stage)
	if err := os.WriteFile(filepath.Join(stage, "cube.lock"), raw, 0o644); err != nil {
		return diag.Wrap(err, diag.CodeVendorPullFail, "cannot stage cube.lock", "check disk space and permissions")
	}

	cacheDir := filepath.Join(stage, ".cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return diag.Wrap(err, diag.CodeVendorPullFail, "cannot create pack cache directory", "check disk space and permissions")
	}
	packHashes, err := vendorPacks(ctx, lf, cacheDir, stage, con)
	if err != nil {
		return err
	}

	imageTarIndex, imageHashes, err := vendorImages(ctx, lf, plat, stage, con)
	if err != nil {
		return err
	}

	sum := sha256.Sum256(raw)
	manifest := Manifest{
		FormatVersion: currentFormatVersion,
		Platform:      platform,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		LockDigest:    "sha256:" + hex.EncodeToString(sum[:]),
		PackHashes:    packHashes,
		Images:        imageTarIndex,
		ImageHashes:   imageHashes,
	}
	if err := writeJSON(filepath.Join(stage, "manifest.json"), manifest); err != nil {
		return diag.Wrap(err, diag.CodeVendorPullFail, "cannot write bundle manifest", "check disk space and permissions")
	}

	if err := tarGzDir(stage, outPath); err != nil {
		return diag.Wrap(err, diag.CodeVendorPullFail, "cannot write bundle "+outPath, "check disk space and permissions")
	}
	con.Step("vendor", "bundle written: %s (%s, %d packs, %d images)", outPath, platform, len(lf.Packs), len(imageTarIndex))
	return nil
}

// vendorPacks fetches every locked pack at its pinned revision into
// stage/packs/<name>. Entry.Resolved's prefix decides the re-fetch
// strategy: "oci:sha256:…" re-pulls the artifact BY DIGEST (beats a tag,
// which can move); "git+<sha>" / "dir:h1:…" re-fetch by the original Ref and
// assert the freshly-resolved Pinned still equals Resolved — a moved pin
// means the source changed since `up` ran, and vendor refuses to silently
// bundle something other than what cube.lock recorded. Returns each staged
// pack's dirhash.Hash1 content hash keyed by entry.Name, for Manifest.
// PackHashes — the same digest Verify recomputes later to catch tampering.
//
// Each pack is a Progress/Done pair: the live tree shows a spinner
// while the pull is in flight; on success Done prints the identical
// "▸ [vendor] pack %s (%s)" content Step used to print eagerly. This is a
// deliberate plain-output delta: before the event-stream migration the pack
// line printed BEFORE the pull attempt, so a failing pull still left it in
// plain output; now Stop prints nothing on error and the failure surfaces
// only as the run's terminal Diagnosis — no per-pack line for a pack that
// never finished.
func vendorPacks(ctx context.Context, lf *lock.File, cacheDir, stage string, con *ui.Console) (map[string]string, error) {
	// Engine-as-pack: the engine pack is vendored exactly like every chart
	// pack — prepended so the bundle contains packs/<engine.Name> and up's
	// resolveBundleRefs can rewrite the engine ref at `up --bundle` time.
	entries := append([]lock.Entry{lf.Engine.Entry()}, lf.Packs...)
	packHashes := make(map[string]string, len(entries))
	for _, entry := range entries {
		pr := con.Progress("vendor", fmt.Sprintf("pack %s (%s)", entry.Name, entry.Resolved))
		ref := entry.Ref
		if d, ok := strings.CutPrefix(entry.Resolved, "oci:"); ok {
			ref = ociRefWithDigest(entry.Ref, d)
		}
		fetched, err := pack.Fetch(ctx, ref, cacheDir)
		if err != nil {
			pr.Stop()
			return nil, diag.Wrap(err, diag.CodeVendorPullFail,
				fmt.Sprintf("cannot pull pack %q at its locked pin", entry.Name),
				"check network/registry access; if the artifact was deleted upstream, re-run `cube-idp up` to re-pin")
		}
		if fetched.Pinned != entry.Resolved {
			pr.Stop()
			return nil, diag.New(diag.CodeVendorPullFail,
				fmt.Sprintf("pack %q resolved to %s but cube.lock pins %s", entry.Name, fetched.Pinned, entry.Resolved),
				"the source moved since `up` — re-run `cube-idp up` to re-pin, then vendor again")
		}
		packDir := filepath.Join(stage, "packs", entry.Name)
		if err := copyTree(fetched.Dir, packDir); err != nil {
			pr.Stop()
			return nil, diag.Wrap(err, diag.CodeVendorPullFail,
				fmt.Sprintf("cannot stage pack %q", entry.Name), "check disk space and permissions")
		}
		h, err := dirhash.HashDir(packDir, "", dirhash.Hash1)
		if err != nil {
			pr.Stop()
			return nil, diag.Wrap(err, diag.CodeVendorPullFail,
				fmt.Sprintf("cannot hash staged pack %q", entry.Name), "check disk space and permissions")
		}
		packHashes[entry.Name] = h
		pr.Done("pack %s (%s)", entry.Name, entry.Resolved)
	}
	return packHashes, nil
}

// registryInstallImages is an indirection over the real registry image
// derivation below (a test seam only): internal/bundle's tests must stay
// fully local — in-process registry, random.Image, no network — but the
// production default derives images from the REAL zot manifests, which
// reference real registry images. Production always uses
// defaultRegistryInstallImages; bundle_test.go's TestMain neutralizes it by
// default and specific tests restore synthetic (still local) values to
// exercise the union logic itself. (The engine's images are no longer
// derived here: engine-as-pack records them in the lock's engine entry —
// lf.Engine.Images — vendored like every pack's Entry.Images.)
var registryInstallImages = defaultRegistryInstallImages

// defaultRegistryInstallImages returns every image the in-cluster zot
// registry's own install manifests reference.
func defaultRegistryInstallImages() ([]string, error) {
	objs, err := registry.Manifests()
	if err != nil {
		return nil, err
	}
	return lock.ImagesFrom(objs), nil
}

// vendorImages pulls the union of every image the cube references into its
// own per-image OCI layout under stage/images/<n>.tar, keyed by the
// ORIGINAL ref string in both the returned index and (via pullImageTar's
// Tag call) the layout's org.opencontainers.image.ref.name annotation
// (Owner Decisions #2). The union mirrors exactly how `up` derives images
// for delivery: every pack's locked Entry.Images (including the runtime
// images a pack declares in its own pack.cue images: list, which appear in
// no rendered manifest) plus the GitOps engine's own install images
// plus the in-cluster registry's install images — so an air-gapped install
// never comes up short an image `up` would otherwise have pulled live.
// Also returns each written tar's sha256, keyed by the ORIGINAL ref, for
// Manifest.ImageHashes — the same digest Verify recomputes later to catch
// tampering.
//
// Each image is a Progress/Done pair, mirroring vendorPacks — see its
// doc comment for the deliberate plain-output delta on the failure path.
func vendorImages(ctx context.Context, lf *lock.File, plat *ocispec.Platform, stage string, con *ui.Console) (index, imageHashes map[string]string, err error) {
	regImgs, err := registryInstallImages()
	if err != nil {
		return nil, nil, err
	}

	set := map[string]struct{}{}
	for _, entry := range lf.Packs {
		for _, img := range entry.Images {
			set[img] = struct{}{}
		}
	}
	// Engine-as-pack: the engine's install images are recorded in the lock's
	// engine entry (up derives them from the rendered engine pack), vendored
	// like every pack's Entry.Images — no engine/factory embed derivation.
	for _, img := range lf.Engine.Images {
		set[img] = struct{}{}
	}
	for _, img := range regImgs {
		set[img] = struct{}{}
	}
	images := make([]string, 0, len(set))
	for img := range set {
		images = append(images, img)
	}
	sort.Strings(images)

	index = make(map[string]string, len(images))
	imageHashes = make(map[string]string, len(images))
	for i, img := range images {
		pr := con.Progress("vendor", fmt.Sprintf("image %s", img))
		layoutDir := filepath.Join(stage, ".imagelayout", fmt.Sprint(i))
		if err := pullImageTar(ctx, img, layoutDir, plat); err != nil {
			pr.Stop()
			return nil, nil, err
		}
		relTar := filepath.Join("images", fmt.Sprintf("%d.tar", i))
		tarPath := filepath.Join(stage, relTar)
		if err := tarDir(layoutDir, tarPath); err != nil {
			pr.Stop()
			return nil, nil, diag.Wrap(err, diag.CodeVendorPullFail,
				fmt.Sprintf("cannot stage image %q", img), "check disk space and permissions")
		}
		h, err := sha256File(tarPath)
		if err != nil {
			pr.Stop()
			return nil, nil, diag.Wrap(err, diag.CodeVendorPullFail,
				fmt.Sprintf("cannot hash staged image %q", img), "check disk space and permissions")
		}
		index[img] = filepath.ToSlash(relTar)
		imageHashes[img] = h
		pr.Done("image %s", img)
	}
	return index, imageHashes, nil
}

// pullImageTar pulls img (any container image reference: a bare "name:tag",
// an "org/name:tag", or a fully-qualified "host/repo:tag") for platform plat
// into a fresh, single-image OCI layout at layoutDir, tagging the copied
// manifest with img itself so the layout's index.json carries img in its
// org.opencontainers.image.ref.name annotation.
func pullImageTar(ctx context.Context, img, layoutDir string, plat *ocispec.Platform) error {
	repo, err := remote.NewRepository(normalizeImageRef(img))
	if err != nil {
		return diag.Wrap(err, diag.CodeVendorPullFail, fmt.Sprintf("invalid image reference %q", img),
			"check the image reference in cube.lock/pack.cue")
	}
	client, err := pack.RegistryClient()
	if err != nil {
		return diag.Wrap(err, diag.CodeVendorPullFail, "cannot load docker credential store",
			"check ~/.docker/config.json (run `docker login <registry>` to create it)")
	}
	repo.Client = client
	if pack.IsLocalRegistryHost(repo.Reference.Registry) {
		repo.PlainHTTP = true
	}
	tagOrDigest := repo.Reference.Reference
	if tagOrDigest == "" {
		tagOrDigest = "latest"
	}

	if err := os.MkdirAll(layoutDir, 0o755); err != nil {
		return diag.Wrap(err, diag.CodeVendorPullFail, fmt.Sprintf("cannot create layout dir for image %q", img),
			"check disk space and permissions")
	}
	store, err := ocicontent.New(layoutDir)
	if err != nil {
		return diag.Wrap(err, diag.CodeVendorPullFail, fmt.Sprintf("cannot create local OCI layout for image %q", img),
			"check disk space and permissions")
	}

	opts := oras.DefaultCopyOptions
	opts.WithTargetPlatform(plat)
	if _, err := oras.Copy(ctx, repo, tagOrDigest, store, img, opts); err != nil {
		return diag.Wrap(err, diag.CodeVendorPullFail, fmt.Sprintf("cannot pull image %q", img),
			"check network/registry access and that the requested --platform exists for this image")
	}
	return nil
}

// parsePlatform parses an "os/arch" string into an ocispec.Platform, the
// shape oras.CopyOptions.WithTargetPlatform needs.
func parsePlatform(platform string) (*ocispec.Platform, error) {
	osName, arch, ok := strings.Cut(platform, "/")
	if !ok || osName == "" || arch == "" {
		return nil, diag.New(diag.CodeVendorPullFail, fmt.Sprintf("invalid --platform %q", platform),
			"use the form os/arch, e.g. linux/amd64")
	}
	return &ocispec.Platform{OS: osName, Architecture: arch}, nil
}

// normalizeImageRef defaults a bare or two-segment image reference to
// Docker Hub the way every container runtime does ("nginx:1.27" ->
// "docker.io/library/nginx:1.27", "envoyproxy/envoy:v1.29" ->
// "docker.io/envoyproxy/envoy:v1.29") — oras-go's registry.ParseReference
// requires an explicit host and does not perform this normalization itself.
// A reference is left untouched if its first path segment already looks
// like a host (contains '.' or ':', or is exactly "localhost") — EXCEPT
// docker.io itself, which still needs its own "library/" namespace
// injected for bare, unnamespaced repos: cube.lock entries are recorded
// verbatim as they appear in manifests, so an already-host-qualified but
// still-bare ref like "docker.io/traefik:v3.7.6" reaches here too (not
// just the fully bare "traefik:v3.7.6" form). Docker Hub's OFFICIAL images
// live under "library/" — a bare top-level repo name doesn't exist on the
// registry and anonymous tokens are never granted for it, so pulling
// "docker.io/traefik" 401s while "docker.io/library/traefik" succeeds.
// oras-go's remote.NewRepository maps the "docker.io" host to the
// registry-1.docker.io API endpoint on its own (registry.Reference.Host()),
// so that aliasing is NOT this function's job — only the repository path.
func normalizeImageRef(ref string) string {
	name, suffix := ref, ""
	if i := strings.IndexByte(ref, '@'); i != -1 {
		name, suffix = ref[:i], ref[i:]
	} else if i := strings.LastIndexByte(ref, ':'); i != -1 && !strings.Contains(ref[i:], "/") {
		name, suffix = ref[:i], ref[i:]
	}
	first, rest := name, ""
	if i := strings.IndexByte(name, '/'); i != -1 {
		first, rest = name[:i], name[i+1:]
	}
	if first == "docker.io" {
		if rest != "" && !strings.Contains(rest, "/") {
			return "docker.io/library/" + rest + suffix
		}
		return ref
	}
	if strings.ContainsAny(first, ".:") || first == "localhost" {
		return ref
	}
	if strings.Contains(name, "/") {
		return "docker.io/" + name + suffix
	}
	return "docker.io/library/" + name + suffix
}

// ociRefWithDigest rewrites an "oci://host/repo:tag" pack ref to pin by
// digest instead of tag: "oci://host/repo@sha256:…". Only the reference
// segment after the LAST '/' is inspected for a ':', so a registry host
// carrying its own port (oci://127.0.0.1:5000/packs/demo:0.1.0) is never
// mistaken for the tag separator.
func ociRefWithDigest(ref, digest string) string {
	trimmed := strings.TrimPrefix(ref, "oci://")
	repo := trimmed
	if i := strings.LastIndexByte(trimmed, '/'); i != -1 {
		if j := strings.IndexByte(trimmed[i+1:], ':'); j != -1 {
			repo = trimmed[:i+1+j]
		}
	} else if j := strings.IndexByte(trimmed, ':'); j != -1 {
		repo = trimmed[:j]
	}
	return "oci://" + repo + "@" + digest
}
