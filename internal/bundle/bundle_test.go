package bundle

import (
	"archive/tar"
	"context"
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

	"github.com/rafpe/cube-idp/internal/diag"
	"github.com/rafpe/cube-idp/internal/lock"
	"github.com/rafpe/cube-idp/internal/oci"
	"github.com/rafpe/cube-idp/internal/oci/ocitest"
)

// TestMain keeps every test in this file fully local (in-process registry,
// random.Image — no network, per Task 6's constraints): the production
// defaults for engineInstallImages/registryInstallImages derive images from
// the REAL Flux/Argo CD and zot manifests, which reference real registry
// images (e.g. ghcr.io/fluxcd/...). Tests neutralize both seams here;
// TestVendorImagesIncludesEngineAndRegistry restores synthetic (still
// local) values to prove the union logic itself.
func TestMain(m *testing.M) {
	engineInstallImages = func(string) ([]string, error) { return nil, nil }
	registryInstallImages = func() ([]string, error) { return nil, nil }
	os.Exit(m.Run())
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
		Engine: lock.EngineLock{Type: "flux"},
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
		Engine: lock.EngineLock{Type: "flux"},
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
	if err := Vendor(context.Background(), writeLockFixture(t), out, "", os.Stderr); err != nil {
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
	if o.Manifest.FormatVersion != 1 {
		t.Fatalf("formatVersion: got %d, want 1", o.Manifest.FormatVersion)
	}
	if o.Manifest.Platform != "linux/"+runtime.GOARCH {
		t.Fatalf("platform: got %q, want linux/%s (default is always linux, host arch)", o.Manifest.Platform, runtime.GOARCH)
	}
	if o.Lock == nil || len(o.Lock.Packs) != 1 || o.Lock.Packs[0].Name != "demo" {
		t.Fatalf("embedded lock not round-tripped: %+v", o.Lock)
	}
}

func TestVendorMissingLock(t *testing.T) {
	err := Vendor(context.Background(), "nope.lock", filepath.Join(t.TempDir(), "b.tgz"), "", os.Stderr)
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-7001" {
		t.Fatalf("want CUBE-7001, got %v", err)
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
	if err := Vendor(context.Background(), writeLockFixture(t), out, "", os.Stderr); err != nil {
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
	if err := Vendor(context.Background(), lockPath, out, "", os.Stderr); err != nil {
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
	if err := Vendor(context.Background(), lockPath, out, "", os.Stderr); err != nil {
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
	if err := Vendor(context.Background(), lockPath, out, "linux/arm64", os.Stderr); err != nil {
		t.Fatalf("matching platform: %v", err)
	}

	out2 := filepath.Join(t.TempDir(), "bundle2.tar.gz")
	err := Vendor(context.Background(), lockPath, out2, "linux/bogus-arch", os.Stderr)
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
// images): it temporarily overrides engineInstallImages/
// registryInstallImages (TestMain's no-op defaults) with synthetic values
// pointing at two more images on the same in-process registry, so the whole
// test stays network-free while still exercising the real union/pull path.
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

	origEng, origReg := engineInstallImages, registryInstallImages
	engineInstallImages = func(string) ([]string, error) { return []string{engImgRef}, nil }
	registryInstallImages = func() ([]string, error) { return []string{regImgRef}, nil }
	t.Cleanup(func() { engineInstallImages, registryInstallImages = origEng, origReg })

	lf := &lock.File{
		APIVersion: "cube-idp.dev/v1alpha1", Kind: "CubeLock",
		Engine: lock.EngineLock{Type: "flux"},
		Packs: []lock.Entry{{
			Ref: packRef, Name: "demo", Version: "0.9.9", Resolved: "oci:" + digest,
		}},
	}
	lockPath := filepath.Join(t.TempDir(), "cube.lock")
	if err := lock.Write(lockPath, lf); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(t.TempDir(), "bundle.tar.gz")
	if err := Vendor(context.Background(), lockPath, out, "", os.Stderr); err != nil {
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
