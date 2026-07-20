// officialindex.go is P10's official-index resolution path, living BESIDE
// the sha256-pinned git index in index.go (which keeps working unchanged).
// The plugins platform mirrors the packs platform: a discovery index
// artifact oci://ghcr.io/cube-idp/plugins/index:latest maps
// name→version→platform→{ref,digest}; InstallFromIndex resolves the running
// platform, pulls the per-platform binary BY DIGEST (never by tag), writes
// it executable to InstallDir(), and hands off to the EXISTING sha256
// trust-consent flow (EnsureTrusted — CUBE-7104 non-TTY refusal is a frozen
// security contract, untouched here).
//
// The index fetch mirrors the pack catalog (internal/pack/catalog.go): 24h
// mtime-TTL cache keyed by ref, pulled through the shared single-blob OCI
// fetch. The cache pattern is COPIED, not imported (plugin must not import
// pack). There is deliberately NO built-in fallback catalog — plugins have
// no hardcoded index, so an offline cold-cache fetch is a typed error whose
// Note points at the git-index path.
package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/cube-idp/cube-idp/internal/diag"
	"github.com/cube-idp/cube-idp/internal/oci"
)

// DefaultPluginIndexRef is the published plugins discovery index.
const DefaultPluginIndexRef = "oci://ghcr.io/cube-idp/plugins/index:latest"

// EnvPluginIndex overrides the index ref — mirrors and tests point it at
// their own registry (oci://host/repo:tag form).
const EnvPluginIndex = "CUBE_IDP_PLUGIN_INDEX"

// pluginIndexTTL is how long a fetched index answers from disk before the
// registry is consulted again — the same 24h the pack catalog uses, so
// menu-driven commands
// (`plugin list --available`, `plugin search`) do not hit the network every
// invocation.
const pluginIndexTTL = 24 * time.Hour

// PluginIndex is the parsed plugins discovery index (schemaVersion 1,
// additive-only within the version).
type PluginIndex struct {
	SchemaVersion int             `json:"schemaVersion"`
	Plugins       []IndexedPlugin `json:"plugins"`
}

// IndexedPlugin is one published plugin: name-sorted in the artifact, with a
// per-platform map keyed "<os>-<arch>".
type IndexedPlugin struct {
	Name        string                   `json:"name"`
	Version     string                   `json:"version"`
	Description string                   `json:"description"`
	Platforms   map[string]IndexPlatform `json:"platforms"`
}

// IndexPlatform pins one platform build: ref carries the oci:// scheme
// (packs-catalog style); digest is the OCI MANIFEST digest — the pull is by
// digest, never by tag.
type IndexPlatform struct {
	Ref    string `json:"ref"`
	Digest string `json:"digest"`
}

// FetchPluginIndex pulls the discovery index (DefaultPluginIndexRef, override
// via CUBE_IDP_PLUGIN_INDEX), caching the raw JSON 24h keyed by ref. A cold
// cache with an unreachable registry is a typed error (no built-in
// fallback); a corrupt or schema-incompatible index is an error too.
func FetchPluginIndex(ctx context.Context) (*PluginIndex, error) {
	ref := os.Getenv(EnvPluginIndex)
	if ref == "" {
		ref = DefaultPluginIndexRef
	}
	rest, ok := strings.CutPrefix(ref, "oci://")
	if !ok {
		return nil, diag.New(diag.CodePluginTrustIO,
			fmt.Sprintf("plugin index ref %q is not an oci:// reference", ref),
			"set CUBE_IDP_PLUGIN_INDEX to the form oci://host/repo:tag, or unset it to use the published index")
	}

	cacheDir, err := pluginCacheDir()
	if err != nil {
		return nil, err
	}
	cachePath := filepath.Join(cacheDir, pluginIndexCacheName(ref))
	if raw, ok := freshIndexCache(cachePath); ok {
		if idx, err := parsePluginIndex(raw); err == nil {
			return idx, nil
		}
		// A corrupt cache file must not wedge the index until the TTL
		// expires — fall through and refetch.
	}

	raw, err := oci.PullBlob(ctx, rest)
	if err != nil {
		return nil, diag.Wrap(err, diag.CodePluginTrustIO,
			fmt.Sprintf("cannot fetch the plugin index %q", ref),
			"check network connectivity, or install from a sha256-pinned git index with `plugin install <name> --index <git-url>`")
	}
	idx, err := parsePluginIndex(raw)
	if err != nil {
		return nil, err
	}
	writeIndexCache(cacheDir, cachePath, raw)
	return idx, nil
}

// parsePluginIndex decodes and gate-checks an index payload. schemaVersion
// must be exactly 1; a zero-plugin index is rejected with the same suspicion
// the pack catalog applies to an empty catalog (it would mean the index was wiped).
func parsePluginIndex(raw []byte) (*PluginIndex, error) {
	var idx PluginIndex
	if err := json.Unmarshal(raw, &idx); err != nil {
		return nil, diag.Wrap(err, diag.CodePluginTrustIO, "plugin index is not valid JSON",
			"re-publish the index, or check CUBE_IDP_PLUGIN_INDEX")
	}
	if idx.SchemaVersion != 1 {
		return nil, diag.New(diag.CodePluginTrustIO,
			fmt.Sprintf("plugin index has schemaVersion %d, want 1", idx.SchemaVersion),
			"upgrade cube-idp to a release that understands this index")
	}
	if len(idx.Plugins) == 0 {
		return nil, diag.New(diag.CodePluginTrustIO, "plugin index lists no plugins",
			"re-publish the index, or check CUBE_IDP_PLUGIN_INDEX")
	}
	return &idx, nil
}

// resolve finds the requested plugin (and optional version) in the index.
// An empty version matches the sole listed version for that name (the index
// carries one entry per plugin). Absent name/version → CUBE-7101.
func (idx *PluginIndex) resolve(name, version string) (*IndexedPlugin, error) {
	for i := range idx.Plugins {
		p := &idx.Plugins[i]
		if p.Name != name {
			continue
		}
		if version == "" || p.Version == version {
			return p, nil
		}
	}
	if version != "" {
		return nil, diag.New(diag.CodePluginNotFound,
			fmt.Sprintf("plugin %q@%s is not in the official index", name, version),
			"run `cube-idp plugin list --available` to see published plugins and versions")
	}
	return nil, diag.New(diag.CodePluginNotFound,
		fmt.Sprintf("plugin %q is not in the official index", name),
		"run `cube-idp plugin list --available` to see published plugins")
}

// selectIndexPlatform picks the entry for the running GOOS/GOARCH, or a typed
// CUBE-7106 error naming the platforms that ARE available.
func selectIndexPlatform(p *IndexedPlugin) (IndexPlatform, error) {
	key := runtime.GOOS + "-" + runtime.GOARCH
	if plat, ok := p.Platforms[key]; ok {
		return plat, nil
	}
	avail := make([]string, 0, len(p.Platforms))
	for k := range p.Platforms {
		avail = append(avail, k)
	}
	sort.Strings(avail)
	return IndexPlatform{}, diag.New(diag.CodePluginNoPlatform,
		fmt.Sprintf("plugin %q has no %s build (available: %s)", p.Name, key, strings.Join(avail, ", ")),
		"ask the plugin maintainers to publish a build for your platform, or install a compatible one")
}

// InstallFromIndex resolves name[@version] against the official index, pulls
// the per-platform binary BY DIGEST, writes it executable to InstallDir(),
// and hands off to the EXISTING trust-consent flow. autoTrust (the `--yes`
// twin) records trust directly — explicit consent by flag, the same doctrine
// `cube-idp trust --yes` uses. Otherwise EnsureTrusted decides: interactive
// consents through the ui.Confirm seam; non-interactive without prior trust
// refuses with CUBE-7104 (the binary is left discoverable-but-untrusted,
// completable via `cube-idp plugin trust <name>`). The pull is digest-verified
// end to end by PullBlob, so a tampered blob never lands.
func InstallFromIndex(ctx context.Context, name, version string, autoTrust, interactive bool) error {
	idx, err := FetchPluginIndex(ctx)
	if err != nil {
		return err
	}
	entry, err := idx.resolve(name, version)
	if err != nil {
		return err
	}
	plat, err := selectIndexPlatform(entry)
	if err != nil {
		return err
	}

	pullRef := strings.TrimPrefix(plat.Ref, "oci://")
	// Pull BY DIGEST, never by tag: rebuild the ref as repo@digest so
	// a moved tag can never redirect the install.
	repo := pullRef
	if i := strings.LastIndexAny(repo, ":@"); i != -1 && !strings.Contains(repo[i:], "/") {
		repo = repo[:i]
	}
	if plat.Digest == "" {
		return diag.New(diag.CodePluginTrustIO,
			fmt.Sprintf("plugin %q %s entry carries no digest", name, runtime.GOOS+"-"+runtime.GOARCH),
			"re-publish the index — every platform entry must pin a digest")
	}
	byDigest := repo + "@" + plat.Digest

	binData, err := oci.PullBlob(ctx, byDigest)
	if err != nil {
		return err
	}

	installedPath, err := atomicInstall(name, binData)
	if err != nil {
		return err
	}
	// Hand off to the EXISTING trust-consent flow. Unlike the git index
	// (which auto-trusts a sha-proven archive), the official-index install
	// goes through consent so the operator explicitly approves running the
	// binary. --yes records trust directly; otherwise EnsureTrusted prompts
	// (interactive) or refuses with the frozen CUBE-7104 (non-interactive).
	if autoTrust {
		return Trust(name, installedPath)
	}
	return EnsureTrusted(name, installedPath, interactive)
}

// pluginCacheDir returns (creating if needed) the plugin index cache dir:
// $XDG_CACHE_HOME/cube-idp/plugins, falling back to os.UserCacheDir().
func pluginCacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", diag.Wrap(err, diag.CodePluginTrustIO, "cannot locate the user cache directory",
			"set $HOME (or $XDG_CACHE_HOME)")
	}
	dir := filepath.Join(base, "cube-idp", "plugins")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", diag.Wrap(err, diag.CodePluginTrustIO, "cannot create "+dir, "check permissions on your cache directory")
	}
	return dir, nil
}

// pluginIndexCacheName keys the cache file by ref, so a CUBE_IDP_PLUGIN_INDEX
// mirror never answers from the default index's cache (or vice versa).
func pluginIndexCacheName(ref string) string {
	sum := sha256.Sum256([]byte(ref))
	return fmt.Sprintf("index-%x.json", sum[:8])
}

// freshIndexCache returns the cached payload iff the file exists and its
// mtime is within pluginIndexTTL.
func freshIndexCache(path string) ([]byte, bool) {
	info, err := os.Stat(path)
	if err != nil || time.Since(info.ModTime()) >= pluginIndexTTL {
		return nil, false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	return raw, true
}

// writeIndexCache stores raw at path via temp-file + rename (atomic on one
// filesystem). Best-effort: a failed cache write only costs the next call a
// re-pull.
func writeIndexCache(cacheDir, path string, raw []byte) {
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
