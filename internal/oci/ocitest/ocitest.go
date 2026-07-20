// Package ocitest holds test-only OCI fixtures shared across packages that
// exercise pack push/pull against a real (in-process) registry —
// internal/oci's own PushPackDir round-trip test and internal/bundle's
// vendor tests. It exists so both call sites share one
// go-containerregistry-based in-process registry helper instead of
// copy-pasting it: go-containerregistry is a TEST-ONLY dependency
// (production code is pure oras-go v2, Owner Decisions #2), and this
// package is that dependency's one home outside individual _test.go files.
package ocitest

import (
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
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

// LocalRegistryWithBasicAuth is LocalRegistry behind a gate: every request
// must carry the given basic-auth credentials or is refused with a 401
// Basic challenge — the in-process stand-in for a private registry (a
// private GHCR pack namespace). Returns the registry's host:port.
func LocalRegistryWithBasicAuth(t *testing.T, username, password string) string {
	t.Helper()
	inner := registry.New(registry.Logger(log.New(io.Discard, "", 0)))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != username || p != password {
			w.Header().Set("WWW-Authenticate", `Basic realm="ocitest"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		inner.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return u.Host
}

// SetDockerAuth points DOCKER_CONFIG (which oras-go's docker credential
// store honors) at a fresh temp dir whose config.json holds inline
// basic-auth credentials for host, isolating the test from the developer's
// real ~/.docker/config.json and its keychain credsStore. t.Setenv restores
// the variable on cleanup (and, as a side effect, forbids t.Parallel in the
// calling test — the price of mutating process env).
func SetDockerAuth(t *testing.T, host, username, password string) {
	t.Helper()
	dir := t.TempDir()
	cfg := fmt.Sprintf(`{"auths":{%q:{"auth":%q}}}`,
		host, base64.StdEncoding.EncodeToString([]byte(username+":"+password)))
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DOCKER_CONFIG", dir)
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
