// officialindex_test.go exercises the P10 official-index resolution path:
// FetchPluginIndex pulls oci://ghcr.io/cube-idp/plugins/index:latest (GT17
// schema), InstallFromIndex resolves name→platform→digest, pulls the
// per-platform blob BY DIGEST, writes it executable to InstallDir(), and
// hands off to the EXISTING sha256 trust-consent flow (EnsureTrusted —
// CUBE-7104 non-TTY refusal unchanged). Fixtures are pushed to an in-process
// registry with internal/oci, mirroring the pack catalog tests.
package plugin

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"testing"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	oras "oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/registry/remote"

	"github.com/cube-idp/cube-idp/internal/diag"
	"github.com/cube-idp/cube-idp/internal/oci"
	"github.com/cube-idp/cube-idp/internal/oci/ocitest"
)

// isolatePluginHome points HOME/XDG_* at throwaway temp dirs so InstallDir(),
// the trust store, and the plugin index cache never touch the real machine,
// and returns a shared registry host for the test's fixtures.
func isolatePluginHome(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("exec-plugin tests are unix-only")
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	return ocitest.LocalRegistry(t)
}

// pushPlatformBlob publishes payload as a single-layer plugin blob artifact
// at oci://<host>/plugins/<name>:<ver>-<os>-<arch> and returns the pushed
// manifest digest — the shape the plugins-repo publish.yml produces.
func pushPlatformBlob(t *testing.T, host, name, tag string, payload []byte) string {
	t.Helper()
	ref := host + "/plugins/" + name
	r, err := remote.NewRepository(ref + ":" + tag)
	if err != nil {
		t.Fatalf("NewRepository: %v", err)
	}
	r.PlainHTTP = true
	ctx := context.Background()
	layer := content.NewDescriptorFromBytes(oci.PluginBlobMediaType, payload)
	if err := r.Push(ctx, layer, bytes.NewReader(payload)); err != nil {
		t.Fatalf("push layer: %v", err)
	}
	md, err := oras.PackManifest(ctx, r, oras.PackManifestVersion1_1, oci.PluginBlobMediaType, oras.PackManifestOptions{
		Layers:              []ocispec.Descriptor{layer},
		ManifestAnnotations: map[string]string{ocispec.AnnotationCreated: "1970-01-01T00:00:00Z"},
	})
	if err != nil {
		t.Fatalf("PackManifest: %v", err)
	}
	if err := r.Tag(ctx, md, tag); err != nil {
		t.Fatalf("tag: %v", err)
	}
	return md.Digest.String()
}

// pushIndexArtifact publishes indexJSON as a single-layer index artifact at
// oci://<host>/plugins/index:latest with the GT17 index media type, and
// points CUBE_IDP_PLUGIN_INDEX at it.
func pushIndexArtifact(t *testing.T, host, indexJSON string) {
	t.Helper()
	ref := host + "/plugins/index"
	r, err := remote.NewRepository(ref + ":latest")
	if err != nil {
		t.Fatalf("NewRepository index: %v", err)
	}
	r.PlainHTTP = true
	ctx := context.Background()
	layer := content.NewDescriptorFromBytes(oci.PluginIndexMediaType, []byte(indexJSON))
	if err := r.Push(ctx, layer, bytes.NewReader([]byte(indexJSON))); err != nil {
		t.Fatalf("push index layer: %v", err)
	}
	md, err := oras.PackManifest(ctx, r, oras.PackManifestVersion1_1, oci.PluginIndexMediaType, oras.PackManifestOptions{
		Layers:              []ocispec.Descriptor{layer},
		ManifestAnnotations: map[string]string{ocispec.AnnotationCreated: "1970-01-01T00:00:00Z"},
	})
	if err != nil {
		t.Fatalf("PackManifest index: %v", err)
	}
	if err := r.Tag(ctx, md, "latest"); err != nil {
		t.Fatalf("tag index: %v", err)
	}
	t.Setenv(EnvPluginIndex, "oci://"+ref+":latest")
}

// indexWith builds a GT17 index whose sole plugin "hello" carries a
// current-platform entry pinned to digest, plus one bogus other-platform
// entry to prove selection is exact. host is the registry the per-platform
// ref points at — the fixtures push the blob there, and InstallFromIndex
// pulls repo@digest off it (in production this host is ghcr.io).
func indexWith(host, name, version, digest string) string {
	ref := fmt.Sprintf("oci://%s/plugins/%s:%s-%s-%s", host, name, version, runtime.GOOS, runtime.GOARCH)
	return fmt.Sprintf(`{
  "schemaVersion": 1,
  "plugins": [
    {
      "name": %q,
      "version": %q,
      "description": "seed plugin proving the pipeline",
      "platforms": {
        %q: {"ref": %q, "digest": %q},
        "plan9-mips": {"ref": "oci://%s/plugins/%s:%s-plan9-mips", "digest": "sha256:%064d"}
      }
    }
  ]
}`, name, version, runtime.GOOS+"-"+runtime.GOARCH, ref, digest, host, name, version, 0)
}

func TestFetchPluginIndexParsesGT17Schema(t *testing.T) {
	host := isolatePluginHome(t)
	pushIndexArtifact(t, host, indexWith(host, "hello", "0.1.0", "sha256:"+repeat64('a')))

	idx, err := FetchPluginIndex(context.Background())
	if err != nil {
		t.Fatalf("FetchPluginIndex: %v", err)
	}
	if idx.SchemaVersion != 1 || len(idx.Plugins) != 1 {
		t.Fatalf("unexpected index: %+v", idx)
	}
	p := idx.Plugins[0]
	if p.Name != "hello" || p.Version != "0.1.0" {
		t.Fatalf("plugin entry: %+v", p)
	}
	key := runtime.GOOS + "-" + runtime.GOARCH
	if _, ok := p.Platforms[key]; !ok {
		t.Fatalf("expected a %s platform entry, got %+v", key, p.Platforms)
	}
}

// TestInstallFromIndexWritesExecutableAndTrusts: the happy path against the
// ocitest fake — with prompts allowed the binary lands executable in
// InstallDir() and is trusted afterward.
func TestInstallFromIndexWritesExecutableAndTrusts(t *testing.T) {
	host := isolatePluginHome(t)
	payload := []byte("#!/bin/sh\necho cube-idp-hello 0.1.0\n")
	tag := "0.1.0-" + runtime.GOOS + "-" + runtime.GOARCH
	digest := pushPlatformBlob(t, host, "hello", tag, payload)
	pushIndexArtifact(t, host, indexWith(host, "hello", "0.1.0", digest))

	if err := InstallFromIndex(context.Background(), "hello", "", true, true); err != nil {
		t.Fatalf("InstallFromIndex: %v", err)
	}
	p, ok := Lookup("hello")
	if !ok {
		t.Fatal("installed plugin not discoverable")
	}
	info, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat installed binary: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("installed binary is not executable: mode %v", info.Mode())
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("installed bytes = %q, want %q", got, payload)
	}
	if err := EnsureTrusted("hello", p, false); err != nil {
		t.Fatalf("a consented install must be trusted: %v", err)
	}
}

// TestInstallFromIndexNonTTYRefusesCUBE7104: the trust-consent seam is
// unchanged — a non-interactive install (interactive=false, no prior trust)
// refuses with CUBE-7104. The binary may land on disk (discoverable as
// untrusted, completable via `plugin trust`), but it is NOT trusted.
func TestInstallFromIndexNonTTYRefusesCUBE7104(t *testing.T) {
	host := isolatePluginHome(t)
	payload := []byte("#!/bin/sh\necho cube-idp-hello 0.1.0\n")
	tag := "0.1.0-" + runtime.GOOS + "-" + runtime.GOARCH
	digest := pushPlatformBlob(t, host, "hello", tag, payload)
	pushIndexArtifact(t, host, indexWith(host, "hello", "0.1.0", digest))

	err := InstallFromIndex(context.Background(), "hello", "", false, false)
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != diag.CodePluginUntrusted {
		t.Fatalf("want CUBE-7104 on non-TTY install, got %v", err)
	}
	if p, ok := Lookup("hello"); ok {
		if e := EnsureTrusted("hello", p, false); e == nil {
			t.Fatal("a refused install must not be trusted")
		}
	}
}

// TestInstallFromIndexMissingPlatformErrors: an index whose plugin has no
// entry for the running platform is a typed CUBE-7106 error that names the
// available platforms.
func TestInstallFromIndexMissingPlatformErrors(t *testing.T) {
	host := isolatePluginHome(t)
	// An index listing hello with ONLY a plan9-mips build (never the host).
	idx := fmt.Sprintf(`{
  "schemaVersion": 1,
  "plugins": [
    {"name": "hello", "version": "0.1.0", "description": "x",
     "platforms": {"plan9-mips": {"ref": "oci://ghcr.io/cube-idp/plugins/hello:0.1.0-plan9-mips", "digest": "sha256:%064d"}}}
  ]
}`, 0)
	pushIndexArtifact(t, host, idx)

	err := InstallFromIndex(context.Background(), "hello", "", true, true)
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != diag.CodePluginNoPlatform {
		t.Fatalf("want CUBE-7106 for a missing platform, got %v", err)
	}
	if want := "plan9-mips"; !bytesContains(err.Error(), want) {
		t.Fatalf("error should name available platforms (%q), got %q", want, err.Error())
	}
}

// TestInstallFromIndexUnknownPluginErrors: a name not in the index is a
// typed not-found (CUBE-7101).
func TestInstallFromIndexUnknownPluginErrors(t *testing.T) {
	host := isolatePluginHome(t)
	pushIndexArtifact(t, host, indexWith(host, "hello", "0.1.0", "sha256:"+repeat64('a')))

	err := InstallFromIndex(context.Background(), "absent", "", true, true)
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != diag.CodePluginNotFound {
		t.Fatalf("want CUBE-7101 for an unknown plugin, got %v", err)
	}
}

// TestInstallFromIndexVersionMismatchErrors: requesting @version that the
// index does not carry for that plugin is a not-found (CUBE-7101).
func TestInstallFromIndexVersionMismatchErrors(t *testing.T) {
	host := isolatePluginHome(t)
	pushIndexArtifact(t, host, indexWith(host, "hello", "0.1.0", "sha256:"+repeat64('a')))

	err := InstallFromIndex(context.Background(), "hello", "9.9.9", true, true)
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != diag.CodePluginNotFound {
		t.Fatalf("want CUBE-7101 for a version mismatch, got %v", err)
	}
}

// TestFetchPluginIndexOfflineErrors: no cache + unreachable registry → typed
// error (there is NO built-in plugin catalog fallback — plugins have no
// hardcoded index).
func TestFetchPluginIndexOfflineErrors(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv(EnvPluginIndex, "oci://127.0.0.1:1/plugins/index:latest")

	if _, err := FetchPluginIndex(context.Background()); err == nil {
		t.Fatal("an unreachable index with a cold cache must error")
	}
}

func repeat64(c byte) string { return string(bytes.Repeat([]byte{c}, 64)) }

func bytesContains(s, sub string) bool { return bytes.Contains([]byte(s), []byte(sub)) }
