package bundle

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"

	"github.com/cube-idp/cube-idp/internal/diag"
	"github.com/cube-idp/cube-idp/internal/lock"
	"github.com/cube-idp/cube-idp/internal/oci"
	"github.com/cube-idp/cube-idp/internal/oci/ocitest"
	"github.com/cube-idp/cube-idp/internal/ui"
)

// vendorForTest drives Vendor through ui.RunPipeline with ModePlain forced
// (a bytes.Buffer target is never a TTY, so this is deterministic without
// SetMode) — the sanctioned test-construction path for a *ui.Console (Task
// R3: no new ui test-constructor API; route through RunPipeline instead,
// mirroring cmd/vendor.go's real call shape). Every pre-R3 call site in
// this file that passed os.Stderr as Vendor's io.Writer now calls this
// helper instead; none of those tests assert output bytes (that is
// vendor_pipeline_test.go's job), so discarding the buffer here is fine.
func vendorForTest(t *testing.T, lockPath, outPath, platform string) error {
	t.Helper()
	var buf bytes.Buffer
	return ui.RunPipeline(context.Background(), "vendor", &buf,
		func(ctx context.Context, con *ui.Console) error {
			con.Start("vendor", "")
			return Vendor(ctx, lockPath, outPath, platform, con)
		})
}

// TestMain keeps every test in this file fully local (in-process registry,
// random.Image — no network, per Task 6's constraints): the production
// default for registryInstallImages derives images from the REAL zot
// manifests, which reference real registry images. Tests neutralize that
// seam here; TestVendorImagesIncludesEngineAndRegistry restores a synthetic
// (still local) value to prove the union logic itself. (Engine-as-pack: the
// engine's images now ride the lock's engine entry — lf.Engine.Images —
// vendored like every pack's Entry.Images, so there is no engine image seam
// to neutralize; fixtures point the engine entry at a local in-process pack.)
func TestMain(m *testing.M) {
	registryInstallImages = func() ([]string, error) { return nil, nil }
	os.Exit(m.Run())
}

// writeEngineLockEntry pushes a minimal cube-engine-flux pack to host (the
// caller's in-process registry) and returns a fully-pinned lock.EngineLock
// mirroring Task 6's shape (type + the six pack fields). Engine-as-pack
// vendors the engine pack like every chart pack, so every lock fixture in
// this file must carry a fetchable engine entry — Vendor now rejects a lock
// with no engine.ref and vendorPacks fetches the engine pack.
func writeEngineLockEntry(t *testing.T, host string, images []string) lock.EngineLock {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pack.cue"),
		[]byte("name: \"cube-engine-flux\"\nversion: \"0.1.0\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "manifests"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifests", "ns.yaml"),
		[]byte("apiVersion: v1\nkind: Namespace\nmetadata:\n  name: flux-system\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ref := "oci://" + host + "/packs/cube-engine-flux:0.1.0"
	digest, err := oci.PushPackDir(context.Background(), dir, ref)
	if err != nil {
		t.Fatal(err)
	}
	return lock.EngineLock{
		Type: "flux", Ref: ref, Name: "cube-engine-flux", Version: "0.1.0",
		Resolved: "oci:" + digest, RenderedHash: "h1", Images: images,
	}
}

// writeLockFixture pushes ocitest's demo pack to an in-process registry and
// writes a cube.lock pinning it via the REAL lock package (no hand-rolled
// YAML), returning the lock's path. This is the arrangement every
// packs-only test in this file shares.
func writeLockFixture(t *testing.T) string {
	t.Helper()
	host := ocitest.LocalRegistry(t)
	dir := ocitest.WriteDemoPack(t)
	ref := "oci://" + host + "/packs/demo:0.9.9"

	digest, err := oci.PushPackDir(context.Background(), dir, ref)
	if err != nil {
		t.Fatal(err)
	}

	lf := &lock.File{
		APIVersion: "cube-idp.dev/v1alpha1", Kind: "CubeLock",
		Engine: writeEngineLockEntry(t, host, nil),
		Packs: []lock.Entry{{
			Ref: ref, Name: "demo", Version: "0.9.9",
			Resolved: "oci:" + digest, Images: nil,
		}},
	}
	lockPath := filepath.Join(t.TempDir(), "cube.lock")
	if err := lock.Write(lockPath, lf); err != nil {
		t.Fatal(err)
	}
	return lockPath
}

// writeLockFixtureWithImage is writeLockFixture plus one image pushed to
// the same registry and pinned in the pack's Entry.Images — the Step 3
// image-path arrangement.
func writeLockFixtureWithImage(t *testing.T, goos, goarch string) (lockPath, imgRef string) {
	t.Helper()
	host := ocitest.LocalRegistry(t)
	dir := ocitest.WriteDemoPack(t)
	ref := "oci://" + host + "/packs/demo:0.9.9"

	digest, err := oci.PushPackDir(context.Background(), dir, ref)
	if err != nil {
		t.Fatal(err)
	}

	imgRef = host + "/images/demo:v1"
	pushTestImage(t, imgRef, goos, goarch)

	lf := &lock.File{
		APIVersion: "cube-idp.dev/v1alpha1", Kind: "CubeLock",
		Engine: writeEngineLockEntry(t, host, nil),
		Packs: []lock.Entry{{
			Ref: ref, Name: "demo", Version: "0.9.9",
			Resolved: "oci:" + digest, Images: []string{imgRef},
		}},
	}
	lockPath = filepath.Join(t.TempDir(), "cube.lock")
	if err := lock.Write(lockPath, lf); err != nil {
		t.Fatal(err)
	}
	return lockPath, imgRef
}

// pushTestImage pushes a small random image to dst (host:port/repo:tag)
// with its config's OS/Architecture set explicitly — random.Image alone
// leaves both empty, which would make every --platform selection fail
// oras's platform match. go-containerregistry is a TEST-ONLY dependency
// here (Owner Decisions #2); production vendor code never imports it.
func pushTestImage(t *testing.T, dst, goos, goarch string) {
	t.Helper()
	img, err := random.Image(64, 1)
	if err != nil {
		t.Fatal(err)
	}
	cf, err := img.ConfigFile()
	if err != nil {
		t.Fatal(err)
	}
	cf = cf.DeepCopy()
	cf.OS = goos
	cf.Architecture = goarch
	img, err = mutate.ConfigFile(img, cf)
	if err != nil {
		t.Fatal(err)
	}
	if err := crane.Push(img, dst, crane.Insecure); err != nil {
		t.Fatal(err)
	}
}

// tarHasEntry reports whether the plain (uncompressed) tar at tarPath
// contains an entry named name.
func tarHasEntry(t *testing.T, tarPath, name string) bool {
	t.Helper()
	f, err := os.Open(tarPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	tr := tar.NewReader(f)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return false
		}
		if err != nil {
			t.Fatal(err)
		}
		if hdr.Name == name {
			return true
		}
	}
}

// TestVendorThenOpenRoundTrip is the whole contract: Vendor -> Open ->
// Verify -> PackDir must produce a usable pack directory from a
// synthetic lock, no network beyond the in-process test registry.
func TestVendorThenOpenRoundTrip(t *testing.T) {
	out := filepath.Join(t.TempDir(), "bundle.tar.gz")
	if err := vendorForTest(t, writeLockFixture(t), out, ""); err != nil {
		t.Fatal(err)
	}

	o, err := Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()
	if err := o.Verify(); err != nil {
		t.Fatal(err)
	}
	dir, err := o.PackDir("demo")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "pack.cue")); err != nil {
		t.Fatalf("bundled pack dir is not a pack: %v", err)
	}
	if o.Manifest.FormatVersion != 2 {
		t.Fatalf("formatVersion: got %d, want 2", o.Manifest.FormatVersion)
	}
	if o.Manifest.Platform != "linux/"+runtime.GOARCH {
		t.Fatalf("platform: got %q, want linux/%s (default is always linux, host arch)", o.Manifest.Platform, runtime.GOARCH)
	}
	if o.Lock == nil || len(o.Lock.Packs) != 1 || o.Lock.Packs[0].Name != "demo" {
		t.Fatalf("embedded lock not round-tripped: %+v", o.Lock)
	}
}

func TestVendorMissingLock(t *testing.T) {
	err := vendorForTest(t, "nope.lock", filepath.Join(t.TempDir(), "b.tgz"), "")
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-7001" {
		t.Fatalf("want CUBE-7001, got %v", err)
	}
}

// TestVendorRejectsPreEnginePackLock pins the migration posture: a lock
// with no engine pack entry cannot produce a complete bundle.
func TestVendorRejectsPreEnginePackLock(t *testing.T) {
	dir := t.TempDir()
	lp := filepath.Join(dir, "cube.lock")
	os.WriteFile(lp, []byte("apiVersion: cube-idp.dev/v1alpha1\nkind: CubeLock\nengine:\n  type: flux\npacks: []\n"), 0o644)
	err := vendorForTest(t, lp, filepath.Join(dir, "out.tar"), "") // reuse the file's existing console/test helper
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != diag.CodeVendorLockMissing {
		t.Fatalf("want CUBE-7001-family rejection, got %v", err)
	}
	if !strings.Contains(de.Summary, "engine") {
		t.Fatalf("summary must say the engine entry is missing: %q", de.Summary)
	}
}

func TestOpenRejectsGarbage(t *testing.T) {
	p := filepath.Join(t.TempDir(), "garbage.tgz")
	os.WriteFile(p, []byte("not a tarball"), 0o644)
	_, err := Open(p)
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-7003" {
		t.Fatalf("want CUBE-7003, got %v", err)
	}
}

// TestVerifyDetectsTampering truncates a bundled pack.cue to zero bytes
// after a successful Open — still present, no longer usable — and asserts
// Verify catches the content corruption, not just outright absence.
func TestVerifyDetectsTampering(t *testing.T) {
	out := filepath.Join(t.TempDir(), "bundle.tar.gz")
	if err := vendorForTest(t, writeLockFixture(t), out, ""); err != nil {
		t.Fatal(err)
	}
	o, err := Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()

	packCue := filepath.Join(o.Dir, "packs", "demo", "pack.cue")
	if err := os.Truncate(packCue, 0); err != nil {
		t.Fatal(err)
	}

	err = o.Verify()
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-7004" {
		t.Fatalf("want CUBE-7004 after truncating pack.cue, got %v", err)
	}
}

// TestVerifyDetectsMissingImageTar deletes a bundle's image tar after Open
// and asserts Verify reports CUBE-7004 naming the missing image ref.
func TestVerifyDetectsMissingImageTar(t *testing.T) {
	lockPath, imgRef := writeLockFixtureWithImage(t, "linux", runtime.GOARCH)
	out := filepath.Join(t.TempDir(), "bundle.tar.gz")
	if err := vendorForTest(t, lockPath, out, ""); err != nil {
		t.Fatal(err)
	}

	o, err := Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()
	tarPath, ok := o.ImageTars()[imgRef]
	if !ok {
		t.Fatalf("ImageTars() missing %q", imgRef)
	}
	if err := os.Remove(tarPath); err != nil {
		t.Fatal(err)
	}

	err = o.Verify()
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-7004" || !strings.Contains(err.Error(), imgRef) {
		t.Fatalf("want CUBE-7004 naming %q, got %v", imgRef, err)
	}
}

// TestVendorBundlesImages is Step 3's image-path coverage: an image hosted
// on the in-process registry, pinned in Entry.Images, ends up as its own
// OCI-layout tar inside the bundle.
func TestVendorBundlesImages(t *testing.T) {
	lockPath, imgRef := writeLockFixtureWithImage(t, "linux", runtime.GOARCH)
	out := filepath.Join(t.TempDir(), "bundle.tar.gz")
	if err := vendorForTest(t, lockPath, out, ""); err != nil {
		t.Fatal(err)
	}

	o, err := Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()
	if err := o.Verify(); err != nil {
		t.Fatal(err)
	}

	tars := o.ImageTars()
	tarPath, ok := tars[imgRef]
	if !ok {
		t.Fatalf("ImageTars() missing %q: %+v", imgRef, tars)
	}
	if _, err := os.Stat(tarPath); err != nil {
		t.Fatalf("image tar missing on disk: %v", err)
	}
	if !tarHasEntry(t, tarPath, "index.json") {
		t.Fatalf("image tar %s has no index.json — not an OCI layout", tarPath)
	}
}

// TestVendorPlatformPin covers --platform end to end: a matching platform
// succeeds, and a platform the pushed image doesn't have fails as
// CUBE-7002 (oras's own ErrNotFound, wrapped).
func TestVendorPlatformPin(t *testing.T) {
	lockPath, _ := writeLockFixtureWithImage(t, "linux", "arm64")

	out := filepath.Join(t.TempDir(), "bundle.tar.gz")
	if err := vendorForTest(t, lockPath, out, "linux/arm64"); err != nil {
		t.Fatalf("matching platform: %v", err)
	}

	out2 := filepath.Join(t.TempDir(), "bundle2.tar.gz")
	err := vendorForTest(t, lockPath, out2, "linux/bogus-arch")
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-7002" {
		t.Fatalf("want CUBE-7002 for a platform the image doesn't have, got %v", err)
	}
}

func TestNormalizeImageRef(t *testing.T) {
	cases := map[string]string{
		"nginx:1.27":              "docker.io/library/nginx:1.27",
		"envoyproxy/envoy:v1.29":  "docker.io/envoyproxy/envoy:v1.29",
		"gcr.io/foo/bar:v1":       "gcr.io/foo/bar:v1",
		"127.0.0.1:5000/repo:tag": "127.0.0.1:5000/repo:tag",
		"localhost:5000/repo:tag": "localhost:5000/repo:tag",
		"nginx@sha256:deadbeef00": "docker.io/library/nginx@sha256:deadbeef00",
		"docker.io/library/redis": "docker.io/library/redis", // already fully-qualified, no tag

		// CUBE-7002 regression: lock entries can already carry an explicit
		// "docker.io/" host (as recorded from a manifest) with a bare,
		// unnamespaced repo — Docker Hub's OFFICIAL images live under
		// "library/", so "docker.io/traefik" 401s (no anonymous token is
		// ever granted for a bare top-level name) unless "library/" is
		// still injected even though the host looks already-qualified.
		"docker.io/traefik:v3.7.6":       "docker.io/library/traefik:v3.7.6",
		"docker.io/traefik":              "docker.io/library/traefik",
		"docker.io/traefik@sha256:dead0": "docker.io/library/traefik@sha256:dead0",
		// Namespaced repos under an explicit docker.io host must pass
		// through untouched — they're not official images.
		"docker.io/envoyproxy/envoy:v1.29": "docker.io/envoyproxy/envoy:v1.29",
		// A non-docker.io registry must never be touched, explicit
		// namespace or not.
		"docker.gitea.com/gitea:1.21": "docker.gitea.com/gitea:1.21",
	}
	for in, want := range cases {
		if got := normalizeImageRef(in); got != want {
			t.Errorf("normalizeImageRef(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestOCIRefWithDigest(t *testing.T) {
	got := ociRefWithDigest("oci://127.0.0.1:5000/packs/demo:0.9.9", "sha256:abc123")
	want := "oci://127.0.0.1:5000/packs/demo@sha256:abc123"
	if got != want {
		t.Fatalf("ociRefWithDigest = %q, want %q", got, want)
	}
}

// TestVendorImagesIncludesEngineAndRegistry proves the image union itself
// (per-pack Entry.Images + engine install images + registry install
// images): the engine images ride the lock's engine entry (lf.Engine.Images,
// engine-as-pack) and the registry seam is overridden (TestMain's no-op
// default) with synthetic values pointing at two more images on the same
// in-process registry, so the whole test stays network-free while still
// exercising the real union/pull path.
func TestVendorImagesIncludesEngineAndRegistry(t *testing.T) {
	host := ocitest.LocalRegistry(t)
	packDir := ocitest.WriteDemoPack(t)
	packRef := "oci://" + host + "/packs/demo:0.9.9"
	digest, err := oci.PushPackDir(context.Background(), packDir, packRef)
	if err != nil {
		t.Fatal(err)
	}

	engImgRef := host + "/images/engine:v1"
	regImgRef := host + "/images/registry:v1"
	pushTestImage(t, engImgRef, "linux", runtime.GOARCH)
	pushTestImage(t, regImgRef, "linux", runtime.GOARCH)

	origReg := registryInstallImages
	registryInstallImages = func() ([]string, error) { return []string{regImgRef}, nil }
	t.Cleanup(func() { registryInstallImages = origReg })

	lf := &lock.File{
		APIVersion: "cube-idp.dev/v1alpha1", Kind: "CubeLock",
		Engine: writeEngineLockEntry(t, host, []string{engImgRef}),
		Packs: []lock.Entry{{
			Ref: packRef, Name: "demo", Version: "0.9.9", Resolved: "oci:" + digest,
		}},
	}
	lockPath := filepath.Join(t.TempDir(), "cube.lock")
	if err := lock.Write(lockPath, lf); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(t.TempDir(), "bundle.tar.gz")
	if err := vendorForTest(t, lockPath, out, ""); err != nil {
		t.Fatal(err)
	}
	o, err := Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()
	if err := o.Verify(); err != nil {
		t.Fatal(err)
	}
	tars := o.ImageTars()
	for _, ref := range []string{engImgRef, regImgRef} {
		if _, ok := tars[ref]; !ok {
			t.Fatalf("ImageTars() missing %q (engine/registry image union): %+v", ref, tars)
		}
	}
}

func TestParsePlatformRejectsMalformed(t *testing.T) {
	_, err := parsePlatform("bogus")
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-7002" {
		t.Fatalf("want CUBE-7002 for a malformed --platform, got %v", err)
	}
}

// flipOneByte XORs the last byte of the file at path with 0xFF, in place —
// same length, different content: exactly what a presence+size check cannot
// catch but a content-hash check must.
func flipOneByte(t *testing.T, path string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 {
		t.Fatalf("cannot flip a byte in empty file %s", path)
	}
	raw[len(raw)-1] ^= 0xFF
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

// downgradeManifestVersion extracts the tar.gz at bundlePath, rewrites
// manifest.json's formatVersion to v, and re-archives it over the original
// path — used to synthesize a bundle claiming an unsupported (old) format
// without hand-rolling a whole manifest.
func downgradeManifestVersion(t *testing.T, bundlePath string, v int) {
	t.Helper()
	dir := t.TempDir()
	if err := extractTarGz(bundlePath, dir); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(dir, "manifest.json")
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	m["formatVersion"] = v
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, out, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := tarGzDir(dir, bundlePath); err != nil {
		t.Fatal(err)
	}
}

// deleteManifestImageEntry extracts the tar.gz at bundlePath, removes ref's
// entries from BOTH manifest.json's "images" and "imageHashes" maps, and
// re-archives it over the original path — generalizes
// downgradeManifestVersion's in-archive tamper pattern (Open parses
// manifest.json once at open time into o.Manifest, so tampering must land
// on the archive itself, before Open, not on an already-Opened bundle's
// in-memory struct).
func deleteManifestImageEntry(t *testing.T, bundlePath, ref string) {
	t.Helper()
	dir := t.TempDir()
	if err := extractTarGz(bundlePath, dir); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(dir, "manifest.json")
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"images", "imageHashes"} {
		if sub, ok := m[key].(map[string]any); ok {
			delete(sub, ref)
		}
	}
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, out, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := tarGzDir(dir, bundlePath); err != nil {
		t.Fatal(err)
	}
}

// TestVerifyDetectsDeletedImageEntry: a manifest edited to remove an image's
// entries from BOTH "images" and "imageHashes" — the deletion the owner-
// approved review finding calls out — must still fail Verify. Before the
// lock-anchored completeness cross-check, the manifest-driven hash loop had
// nothing left to check (the ref is gone from Manifest.Images too), so
// Verify passed on a bundle silently missing a lock-pinned image; the
// air-gapped `up` would then load fewer images and fail later as an opaque
// ImagePullBackOff instead of a loud CUBE-7004 here.
func TestVerifyDetectsDeletedImageEntry(t *testing.T) {
	lockPath, imgRef := writeLockFixtureWithImage(t, "linux", runtime.GOARCH)
	out := filepath.Join(t.TempDir(), "bundle.tar.gz")
	if err := vendorForTest(t, lockPath, out, ""); err != nil {
		t.Fatal(err)
	}

	deleteManifestImageEntry(t, out, imgRef)

	o, err := Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()

	if _, ok := o.Manifest.Images[imgRef]; ok {
		t.Fatalf("tamper helper did not remove %q from manifest.Images", imgRef)
	}

	err = o.Verify()
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-7004" {
		t.Fatalf("want CUBE-7004 after deleting image entry, got %v", err)
	}
	if !strings.Contains(err.Error(), imgRef) {
		t.Fatalf("want error naming deleted ref %q, got %v", imgRef, err)
	}
	if !strings.Contains(err.Error(), `"demo"`) {
		t.Fatalf("want error naming pinning pack %q, got %v", "demo", err)
	}
}

// TestOpenRejectsV1Bundle: a bundle whose manifest says formatVersion 1 is
// refused with CUBE-7003 and the format-upgraded remediation — bundles are
// ephemeral transport artifacts, no compatibility shim (spec §5.2).
func TestOpenRejectsV1Bundle(t *testing.T) {
	out := filepath.Join(t.TempDir(), "bundle.tar.gz")
	if err := vendorForTest(t, writeLockFixture(t), out, ""); err != nil {
		t.Fatal(err)
	}
	downgradeManifestVersion(t, out, 1) // helper below: rewrites formatVersion in-archive
	_, err := Open(out)
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-7003" {
		t.Fatalf("want CUBE-7003 for a v1 bundle, got %v", err)
	}
	if !strings.Contains(de.Remediation, "bundle format upgraded") {
		t.Fatalf("remediation must name the format upgrade, got %q", de.Remediation)
	}
}

// TestVerifyDetectsPackContentSwap: flip one byte in a pack file WITHOUT
// changing its size — presence+size verification (the pre-R2 state) cannot
// catch this; the dirhash comparison must, naming the pack.
func TestVerifyDetectsPackContentSwap(t *testing.T) {
	out := filepath.Join(t.TempDir(), "bundle.tar.gz")
	if err := vendorForTest(t, writeLockFixture(t), out, ""); err != nil {
		t.Fatal(err)
	}
	o, err := Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()
	flipOneByte(t, filepath.Join(o.Dir, "packs", "demo", "pack.cue"))
	err = o.Verify()
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-7004" || !strings.Contains(err.Error(), `pack "demo"`) {
		t.Fatalf("want CUBE-7004 naming pack demo, got %v", err)
	}
}

// TestVerifyDetectsImageContentSwap: same-size byte flip inside an image tar.
func TestVerifyDetectsImageContentSwap(t *testing.T) {
	lockPath, imgRef := writeLockFixtureWithImage(t, "linux", runtime.GOARCH)
	out := filepath.Join(t.TempDir(), "bundle.tar.gz")
	if err := vendorForTest(t, lockPath, out, ""); err != nil {
		t.Fatal(err)
	}
	o, err := Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()
	flipOneByte(t, o.ImageTars()[imgRef])
	err = o.Verify()
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-7004" || !strings.Contains(err.Error(), imgRef) {
		t.Fatalf("want CUBE-7004 naming %q, got %v", imgRef, err)
	}
}

// TestExtractCaps: with the test-seam limits shrunk, an over-limit entry and
// an over-limit total are both CUBE-7003 (Open wraps extractTarGz's error).
func TestExtractCaps(t *testing.T) {
	out := filepath.Join(t.TempDir(), "bundle.tar.gz")
	if err := vendorForTest(t, writeLockFixture(t), out, ""); err != nil {
		t.Fatal(err)
	}
	restoreFile, restoreTotal := maxBundleFileBytes, maxBundleTotalBytes
	defer func() { maxBundleFileBytes, maxBundleTotalBytes = restoreFile, restoreTotal }()

	maxBundleFileBytes = 8 // every real entry exceeds this
	_, err := Open(out)
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-7003" {
		t.Fatalf("per-file cap: want CUBE-7003, got %v", err)
	}

	maxBundleFileBytes = restoreFile
	maxBundleTotalBytes = 64
	_, err = Open(out)
	if !errors.As(err, &de) || de.Code != "CUBE-7003" {
		t.Fatalf("total cap: want CUBE-7003, got %v", err)
	}
}
