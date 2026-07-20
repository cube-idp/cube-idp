// catalog_test.go exercises FetchCatalog (the remote pack catalog) against an
// in-process OCI registry. It is an EXTERNAL test package (pack_test) on
// purpose: the index fixtures are pushed with internal/oci.PushPackDir —
// exactly what `pack index push` does in production — and internal/oci
// imports internal/pack, so an in-package test would be an import cycle.
package pack_test

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/registry"

	"github.com/cube-idp/cube-idp/internal/diag"
	"github.com/cube-idp/cube-idp/internal/oci"
	"github.com/cube-idp/cube-idp/internal/pack"
)

// catalogRegistry starts an in-process OCI registry (go-containerregistry's
// test registry — TEST-ONLY dependency, ocitest doctrine) and returns its
// host:port plus a shutdown func so the cache tests can kill the network
// mid-test. httptest listens on 127.0.0.1 over plain HTTP, hitting the same
// IsLocalRegistryHost → PlainHTTP gate the zot tunnel uses.
func catalogRegistry(t *testing.T) (host string, shutdown func()) {
	t.Helper()
	srv := httptest.NewServer(registry.New(registry.Logger(log.New(io.Discard, "", 0))))
	t.Cleanup(srv.Close) // safe if the test already called shutdown
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return u.Host, srv.Close
}

// pushIndex publishes raw as index.json in a one-file directory artifact at
// oci://<host>/packs/index:latest — byte-for-byte the `pack index push`
// artifact shape FetchCatalog consumes.
func pushIndex(t *testing.T, host string, raw string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.json"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := oci.PushPackDir(context.Background(), dir, "oci://"+host+"/packs/index:latest"); err != nil {
		t.Fatalf("pushing index fixture: %v", err)
	}
}

// catalogEnv isolates a catalog test: fresh $HOME (so DefaultCacheDir — and
// with it the index cache — is a throwaway temp dir, never the developer's
// real ~/.cache) and CUBE_IDP_PACK_INDEX pointed at the test registry.
func catalogEnv(t *testing.T, host string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv(pack.EnvPackIndex, "oci://"+host+"/packs/index:latest")
}

const validIndexJSON = `{
  "schemaVersion": 1,
  "packs": [
    {
      "name": "argocd",
      "version": "0.2.0",
      "description": "delivery UI",
      "ref": "oci://ghcr.io/cube-idp/packs/argocd:0.2.0",
      "digest": "sha256:1111111111111111111111111111111111111111111111111111111111111111"
    },
    {
      "name": "kargo",
      "version": "1.0.0",
      "description": "promotion pipelines",
      "ref": "oci://ghcr.io/cube-idp/packs/kargo:1.0.0",
      "digest": "sha256:2222222222222222222222222222222222222222222222222222222222222222"
    }
  ]
}
`

// TestCatalogFetchParsesIndex: a published schemaVersion-1 index round-trips
// through FetchCatalog with every field of every entry intact, in index
// (name-sorted) order.
func TestCatalogFetchParsesIndex(t *testing.T) {
	host, _ := catalogRegistry(t)
	catalogEnv(t, host)
	pushIndex(t, host, validIndexJSON)

	cat, err := pack.FetchCatalog(context.Background())
	if err != nil {
		t.Fatalf("FetchCatalog: %v", err)
	}
	if cat.SchemaVersion != 1 {
		t.Fatalf("schemaVersion = %d, want 1", cat.SchemaVersion)
	}
	if len(cat.Packs) != 2 {
		t.Fatalf("expected 2 entries, got %+v", cat.Packs)
	}
	want := []pack.CatalogEntry{
		{Name: "argocd", Version: "0.2.0", Description: "delivery UI",
			Ref:    "oci://ghcr.io/cube-idp/packs/argocd:0.2.0",
			Digest: "sha256:1111111111111111111111111111111111111111111111111111111111111111"},
		{Name: "kargo", Version: "1.0.0", Description: "promotion pipelines",
			Ref:    "oci://ghcr.io/cube-idp/packs/kargo:1.0.0",
			Digest: "sha256:2222222222222222222222222222222222222222222222222222222222222222"},
	}
	for i, w := range want {
		if cat.Packs[i] != w {
			t.Fatalf("entry %d = %+v, want %+v", i, cat.Packs[i], w)
		}
	}
}

// TestCatalogCorruptJSONErrors: an index artifact whose index.json is not
// valid JSON is a typed CUBE-4004 error — callers fall back to the built-in
// catalog, they never see half-parsed junk.
func TestCatalogCorruptJSONErrors(t *testing.T) {
	host, _ := catalogRegistry(t)
	catalogEnv(t, host)
	pushIndex(t, host, `{"schemaVersion": 1, "packs": [`)

	cat, err := pack.FetchCatalog(context.Background())
	if err == nil {
		t.Fatalf("corrupt index must error, got %+v", cat)
	}
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != diag.CodePackManifestErr {
		t.Fatalf("want CUBE-4004 (CodePackManifestErr), got %v", err)
	}
}

// TestCatalogBadSchemaVersionErrors: a schemaVersion this binary does not
// understand is an error (fallback), never a silent misparse.
func TestCatalogBadSchemaVersionErrors(t *testing.T) {
	host, _ := catalogRegistry(t)
	catalogEnv(t, host)
	pushIndex(t, host, `{"schemaVersion": 2, "packs": [{"name": "x"}]}`)

	if _, err := pack.FetchCatalog(context.Background()); err == nil {
		t.Fatal("unknown schemaVersion must error")
	}
}

// TestCatalogEmptyIndexErrors: a zero-pack index is rejected — the index
// builder refuses to produce one precisely because it would wipe the
// published catalog; the consumer treats it with the same suspicion.
func TestCatalogEmptyIndexErrors(t *testing.T) {
	host, _ := catalogRegistry(t)
	catalogEnv(t, host)
	pushIndex(t, host, `{"schemaVersion": 1, "packs": []}`)

	if _, err := pack.FetchCatalog(context.Background()); err == nil {
		t.Fatal("empty index must error so callers fall back to the built-in catalog")
	}
}

// TestCatalogCacheHitSkipsNetwork: within the 24h TTL the catalog is served
// from the cache file — proven by killing the registry and fetching again.
func TestCatalogCacheHitSkipsNetwork(t *testing.T) {
	host, shutdown := catalogRegistry(t)
	catalogEnv(t, host)
	pushIndex(t, host, validIndexJSON)

	first, err := pack.FetchCatalog(context.Background())
	if err != nil {
		t.Fatalf("first FetchCatalog: %v", err)
	}

	shutdown() // no registry from here on: only the cache can answer

	second, err := pack.FetchCatalog(context.Background())
	if err != nil {
		t.Fatalf("cached FetchCatalog after registry shutdown: %v", err)
	}
	if len(second.Packs) != len(first.Packs) || second.Packs[0] != first.Packs[0] {
		t.Fatalf("cache served different content: %+v vs %+v", second.Packs, first.Packs)
	}
}

// TestCatalogExpiredCacheRefetches: a cache file older than the TTL is
// ignored — the next fetch goes back to the network and sees new content.
func TestCatalogExpiredCacheRefetches(t *testing.T) {
	host, _ := catalogRegistry(t)
	catalogEnv(t, host)
	pushIndex(t, host, validIndexJSON)

	if _, err := pack.FetchCatalog(context.Background()); err != nil {
		t.Fatalf("first FetchCatalog: %v", err)
	}

	// The index moves on (a new pack appears)…
	pushIndex(t, host, `{
  "schemaVersion": 1,
  "packs": [
    {
      "name": "gitea",
      "version": "0.3.0",
      "description": "in-cluster git server",
      "ref": "oci://ghcr.io/cube-idp/packs/gitea:0.3.0",
      "digest": "sha256:3333333333333333333333333333333333333333333333333333333333333333"
    }
  ]
}
`)

	// …but a fresh cache still answers.
	cat, err := pack.FetchCatalog(context.Background())
	if err != nil {
		t.Fatalf("within-TTL FetchCatalog: %v", err)
	}
	if len(cat.Packs) != 2 {
		t.Fatalf("within the TTL the old cache must win, got %+v", cat.Packs)
	}

	// Backdate every cache file beyond the TTL: the next fetch re-pulls.
	home, _ := os.UserHomeDir()
	cacheDir := filepath.Join(home, ".cache", "cube-idp", "packs")
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		t.Fatalf("reading cache dir: %v", err)
	}
	old := time.Now().Add(-25 * time.Hour)
	for _, e := range entries {
		if err := os.Chtimes(filepath.Join(cacheDir, e.Name()), old, old); err != nil {
			t.Fatal(err)
		}
	}

	cat, err = pack.FetchCatalog(context.Background())
	if err != nil {
		t.Fatalf("post-TTL FetchCatalog: %v", err)
	}
	if len(cat.Packs) != 1 || cat.Packs[0].Name != "gitea" {
		t.Fatalf("expired cache must refetch the updated index, got %+v", cat.Packs)
	}
}

// TestCatalogUnreachableRegistryErrors: no cache + no network → (nil, err),
// the contract callers key their built-in fallback on. 127.0.0.1:1 refuses
// connections immediately, so this asserts the error path, not a timeout.
func TestCatalogUnreachableRegistryErrors(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(pack.EnvPackIndex, "oci://127.0.0.1:1/packs/index:latest")

	cat, err := pack.FetchCatalog(context.Background())
	if err == nil {
		t.Fatalf("unreachable registry with a cold cache must error, got %+v", cat)
	}
	if cat != nil {
		t.Fatalf("error path must return a nil catalog, got %+v", cat)
	}
}
