// Package ocitest holds test-only OCI fixtures shared across packages that
// exercise pack push/pull against a real (in-process) registry —
// internal/oci's own PushPackDir round-trip test and internal/bundle's
// vendor tests (Task 6). It exists so both call sites share one
// go-containerregistry-based in-process registry helper instead of
// copy-pasting it: go-containerregistry is a TEST-ONLY dependency
// (production code is pure oras-go v2, Owner Decisions #2), and this
// package is that dependency's one home outside individual _test.go files.
package ocitest

import (
	"io"
	"log"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-containerregistry/pkg/registry"
)

// LocalRegistry starts an in-process OCI registry (go-containerregistry's
// test registry) and returns its host:port. httptest servers listen on
// 127.0.0.1 over plain HTTP, which exercises the same insecure-transport
// gate (isLocalRegistryHost -> PlainHTTP) the zot port-forward tunnel uses
// in production.
func LocalRegistry(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(registry.New(registry.Logger(log.New(io.Discard, "", 0))))
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return u.Host
}

// WriteDemoPack writes a minimal, valid pack directory (pack.cue + one
// manifest) to a fresh t.TempDir() and returns its path.
func WriteDemoPack(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	must := func(p, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	must(filepath.Join(dir, "pack.cue"), "name: \"demo\"\nversion: \"0.9.9\"\n")
	must(filepath.Join(dir, "manifests", "cm.yaml"),
		"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n  namespace: default\n")
	return dir
}
