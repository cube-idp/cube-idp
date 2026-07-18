// catalog.go is the consumer side of the P2 pack catalog index artifact
// (Phase 5 P6): FetchCatalog pulls oci://ghcr.io/cube-idp/packs/index:latest
// — the artifact `pack index push` publishes — and hands callers the parsed
// entry list. It deliberately reuses the pack pull machinery (pullOCI, the
// docker credential chain, the loopback PlainHTTP gate) instead of growing a
// second OCI client, and caches the raw index JSON in the pack cache dir
// with a 24h mtime TTL so menu-driven commands do not hit the network on
// every keystroke-adjacent invocation.
package pack

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cube-idp/cube-idp/internal/diag"
)

// DefaultIndexRef is the published pack catalog index artifact (GT9).
const DefaultIndexRef = "oci://ghcr.io/cube-idp/packs/index:latest"

// EnvPackIndex overrides the index ref — mirrors and tests point it at
// their own registry (oci://host/repo:tag form, like any pack ref).
const EnvPackIndex = "CUBE_IDP_PACK_INDEX"

// catalogCacheTTL is how long a fetched index answers from disk before the
// registry is consulted again.
const catalogCacheTTL = 24 * time.Hour

// Catalog is the parsed index artifact (P2 schema, schemaVersion 1,
// additive-only within the version — cmd/pack_publish.go's packIndex is the
// producer twin).
type Catalog struct {
	SchemaVersion int            `json:"schemaVersion"`
	Packs         []CatalogEntry `json:"packs"`
}

// CatalogEntry is one published pack: name-sorted in the artifact, ref of
// the form oci://ghcr.io/cube-idp/packs/<name>:<version>, digest the pinned
// manifest digest CI attested.
type CatalogEntry struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Ref         string `json:"ref"`
	Digest      string `json:"digest"`
}

// FetchCatalog pulls the catalog index artifact (DefaultIndexRef, override
// via CUBE_IDP_PACK_INDEX for tests/mirrors), caching the raw JSON 24h in
// the pack cache dir (mtime-based TTL, keyed by ref so switching mirrors
// never serves the wrong catalog). Network failure with a cold cache →
// (nil, err); callers fall back to the built-in catalog and say so. A
// corrupt or schema-incompatible index is an error too — never a
// half-parsed catalog.
func FetchCatalog(ctx context.Context) (*Catalog, error) {
	ref := os.Getenv(EnvPackIndex)
	if ref == "" {
		ref = DefaultIndexRef
	}
	rest, ok := strings.CutPrefix(ref, "oci://")
	if !ok {
		return nil, diag.New(diag.CodePackRefInvalid,
			fmt.Sprintf("catalog index ref %q is not an oci:// reference", ref),
			"set CUBE_IDP_PACK_INDEX to the form oci://host/repo:tag, or unset it to use the published index")
	}
	cacheDir, err := DefaultCacheDir()
	if err != nil {
		return nil, err
	}
	cachePath := filepath.Join(cacheDir, catalogCacheName(ref))
	if raw, ok := freshCatalogCache(cachePath); ok {
		if cat, err := parseCatalog(raw); err == nil {
			return cat, nil
		}
		// A corrupt cache file must not wedge the catalog until the TTL
		// expires — fall through and refetch from the registry.
	}

	dir, _, err := pullOCI(ctx, rest, cacheDir)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(filepath.Join(dir, "index.json"))
	if err != nil {
		return nil, diag.Wrap(err, diag.CodePackManifestErr,
			fmt.Sprintf("catalog index artifact %q carries no index.json", ref),
			"re-publish the index with `cube-idp pack index push`, or check CUBE_IDP_PACK_INDEX")
	}
	cat, err := parseCatalog(raw)
	if err != nil {
		return nil, err
	}
	writeCatalogCache(cacheDir, cachePath, raw)
	return cat, nil
}

// parseCatalog decodes and gate-checks an index payload. schemaVersion must
// be exactly 1 (an index this binary cannot understand is an error, not a
// guess), and a zero-pack index is rejected: `pack index build` refuses to
// produce one because it would wipe the published catalog, and the consumer
// treats an empty catalog with the same suspicion.
func parseCatalog(raw []byte) (*Catalog, error) {
	var cat Catalog
	if err := json.Unmarshal(raw, &cat); err != nil {
		return nil, diag.Wrap(err, diag.CodePackManifestErr, "pack catalog index is not valid JSON",
			"re-publish the index with `cube-idp pack index push`, or check CUBE_IDP_PACK_INDEX")
	}
	if cat.SchemaVersion != 1 {
		return nil, diag.New(diag.CodePackManifestErr,
			fmt.Sprintf("pack catalog index has schemaVersion %d, want 1", cat.SchemaVersion),
			"upgrade cube-idp to a release that understands this index")
	}
	if len(cat.Packs) == 0 {
		return nil, diag.New(diag.CodePackManifestErr, "pack catalog index lists no packs",
			"re-publish the index with `cube-idp pack index push` over a non-empty packs/ tree")
	}
	return &cat, nil
}

// catalogCacheName keys the cache file by the index ref, so a
// CUBE_IDP_PACK_INDEX mirror never answers from the default index's cache
// (or vice versa).
func catalogCacheName(ref string) string {
	sum := sha256.Sum256([]byte(ref))
	return fmt.Sprintf("index-%x.json", sum[:8])
}

// freshCatalogCache returns the cached index payload iff the cache file
// exists and its mtime is within catalogCacheTTL.
func freshCatalogCache(path string) ([]byte, bool) {
	info, err := os.Stat(path)
	if err != nil || time.Since(info.ModTime()) >= catalogCacheTTL {
		return nil, false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	return raw, true
}

// writeCatalogCache stores raw at path via temp-file + rename (atomic on one
// filesystem, so a concurrent FetchCatalog never reads a torn write).
// Best-effort by design: the catalog in hand is already good, and a failed
// cache write only costs the next call a re-pull.
func writeCatalogCache(cacheDir, path string, raw []byte) {
	tmp, err := os.CreateTemp(cacheDir, "index-*.tmp")
	if err != nil {
		return
	}
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
	}
}
