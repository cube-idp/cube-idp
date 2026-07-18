package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	oras "oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/registry/remote"

	"github.com/cube-idp/cube-idp/internal/diag"
	"github.com/cube-idp/cube-idp/internal/oci"
	"github.com/cube-idp/cube-idp/internal/oci/ocitest"
	"github.com/cube-idp/cube-idp/internal/plugin"
)

// seedPluginIndex publishes an index (and, when payload != nil, the hello
// platform blob it points at) to an in-process registry, points
// CUBE_IDP_PLUGIN_INDEX at it, and isolates HOME/XDG so InstallDir and the
// trust store stay in temp dirs. Mirrors the pack catalog cmd fixtures.
func seedPluginIndex(t *testing.T, payload []byte) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("exec-plugin tests are unix-only")
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	host := ocitest.LocalRegistry(t)

	digest := "sha256:" + strings.Repeat("a", 64)
	if payload != nil {
		digest = pushBlob(t, host, "hello", "0.1.0-"+runtime.GOOS+"-"+runtime.GOARCH, payload)
	}
	ref := fmt.Sprintf("oci://%s/plugins/hello:0.1.0-%s-%s", host, runtime.GOOS, runtime.GOARCH)
	idx := fmt.Sprintf(`{"schemaVersion":1,"plugins":[{"name":"hello","version":"0.1.0","description":"seed plugin proving the pipeline","platforms":{%q:{"ref":%q,"digest":%q}}}]}`,
		runtime.GOOS+"-"+runtime.GOARCH, ref, digest)
	pushIdx(t, host, idx)
	t.Setenv("CUBE_IDP_PLUGIN_INDEX", "oci://"+host+"/plugins/index:latest")
}

func pushBlob(t *testing.T, host, name, tag string, payload []byte) string {
	t.Helper()
	r, err := remote.NewRepository(host + "/plugins/" + name + ":" + tag)
	if err != nil {
		t.Fatal(err)
	}
	r.PlainHTTP = true
	ctx := context.Background()
	layer := content.NewDescriptorFromBytes(oci.PluginBlobMediaType, payload)
	if err := r.Push(ctx, layer, bytes.NewReader(payload)); err != nil {
		t.Fatal(err)
	}
	md, err := oras.PackManifest(ctx, r, oras.PackManifestVersion1_1, oci.PluginBlobMediaType, oras.PackManifestOptions{
		Layers:              []ocispec.Descriptor{layer},
		ManifestAnnotations: map[string]string{ocispec.AnnotationCreated: "1970-01-01T00:00:00Z"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Tag(ctx, md, tag); err != nil {
		t.Fatal(err)
	}
	return md.Digest.String()
}

func pushIdx(t *testing.T, host, idxJSON string) {
	t.Helper()
	r, err := remote.NewRepository(host + "/plugins/index:latest")
	if err != nil {
		t.Fatal(err)
	}
	r.PlainHTTP = true
	ctx := context.Background()
	layer := content.NewDescriptorFromBytes(oci.PluginIndexMediaType, []byte(idxJSON))
	if err := r.Push(ctx, layer, bytes.NewReader([]byte(idxJSON))); err != nil {
		t.Fatal(err)
	}
	md, err := oras.PackManifest(ctx, r, oras.PackManifestVersion1_1, oci.PluginIndexMediaType, oras.PackManifestOptions{
		Layers:              []ocispec.Descriptor{layer},
		ManifestAnnotations: map[string]string{ocispec.AnnotationCreated: "1970-01-01T00:00:00Z"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Tag(ctx, md, "latest"); err != nil {
		t.Fatal(err)
	}
}

// TestPluginInstallFromOfficialIndex: `plugin install hello --yes` (no
// --index) resolves the official index, pulls the platform blob by digest,
// and installs+trusts it.
func TestPluginInstallFromOfficialIndex(t *testing.T) {
	seedPluginIndex(t, []byte("#!/bin/sh\nexit 0\n"))

	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"plugin", "install", "hello", "--yes"})
	if err := root.Execute(); err != nil {
		t.Fatalf("plugin install from official index: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "installed and trusted") {
		t.Fatalf("expected a success note, got:\n%s", out.String())
	}
	if _, ok := plugin.Lookup("hello"); !ok {
		t.Fatal("plugin not discoverable after install")
	}
}

// TestPluginInstallOfficialIndexNonTTYRefuses: without --yes on a non-TTY
// (buffer stdin), the trust-consent seam refuses with CUBE-7104 rather than
// silently trusting.
func TestPluginInstallOfficialIndexNonTTYRefuses(t *testing.T) {
	seedPluginIndex(t, []byte("#!/bin/sh\nexit 0\n"))

	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(&bytes.Buffer{}) // non-TTY: prompts forbidden
	root.SetArgs([]string{"plugin", "install", "hello"})
	err := root.Execute()
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != diag.CodePluginUntrusted {
		t.Fatalf("want CUBE-7104 without --yes on non-TTY, got %v", err)
	}
}

// TestPluginListAvailableRendersIndexRows: `plugin list --available` reads
// the official index and lists each plugin's name/version.
func TestPluginListAvailableRendersIndexRows(t *testing.T) {
	seedPluginIndex(t, nil)

	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"plugin", "list", "--available"})
	if err := root.Execute(); err != nil {
		t.Fatalf("plugin list --available: %v\n%s", err, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "hello") || !strings.Contains(got, "0.1.0") {
		t.Fatalf("expected the index row for hello 0.1.0, got:\n%s", got)
	}
}

// TestPluginSearchFiltersIndex: `plugin search <term>` shows only matching
// index rows.
func TestPluginSearchFiltersIndex(t *testing.T) {
	seedPluginIndex(t, nil)

	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"plugin", "search", "hell"})
	if err := root.Execute(); err != nil {
		t.Fatalf("plugin search: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "hello") {
		t.Fatalf("expected hello in search results, got:\n%s", out.String())
	}
}
