---
status: "accepted"
date: 2026-07-20
decision-makers: "cube-idp maintainers"
---

# 8. Pack and Plugin Distribution: OCI Artifacts, Catalog Index, and Fallbacks

## Context and Problem Statement

cube-idp ships two kinds of extension content that must reach users who have only
downloaded the binary: **packs** (the Kubernetes workload bundles the CLI installs into a
cube) and **plugins** (external `cube-idp-<name>` executables that extend the CLI).

Both need a distribution channel that answers the same questions: where does an artifact
live, how does a user *discover* what is available without a new binary release, how is an
artifact pinned so a tag cannot be moved underneath a user, and what happens when the
registry is unreachable. Compiling a catalog into the binary makes discovery impossible to
update without a release; making discovery purely remote makes the CLI unusable offline
and drags network calls into CI and end-to-end test paths. Packs and plugins also do not
carry the same risk: an unreachable pack catalog should degrade a user's menu, while
silently offering a stale or guessed *plugin* list would push someone toward executing a
binary the tool cannot vouch for.

The intended publishing side — a public `cube-idp/plugins` monorepo with a folder per
plugin, `<name>/vX.Y.Z` tags and keyless GitHub attestations — is a producer-side
convention that lives outside this repository and is therefore not verifiable from this
codebase; only the consumer half (index resolution, digest-pinned pull, trust) is asserted
as implemented below.

## Decision

Each pack publishes as an OCI artifact at `oci://ghcr.io/cube-idp/packs/<name>`, with a
catalog index artifact at `.../packs/index` whose schema is
`{schemaVersion: 1, packs: [{name, version, description, ref, digest}]}` sorted by name.
`pack index build` requires contract-v1 descriptions and `name == directory` for every
pack, and treats a zero-pack index as a typed error so an accidental empty index cannot
wipe the published catalog.

The catalog is fetched from `oci://ghcr.io/cube-idp/packs/index:latest` (overridable via
`CUBE_IDP_PACK_INDEX`) and cached for 24 hours. On network failure the fetch errors rather
than serving a stale cache, and callers fall back to the built-in hardcoded catalog —
which is never deleted — while printing a note. Network access stays off automated paths:
the `init` wizard loads the remote catalog only on the interactive path, so flag-driven
runs, CI and e2e never touch the network.

Plugins mirror this platform without its fallback. Exec plugins follow the krew model:
`cube-idp-<name>` binaries on PATH, an env-var contract, a sha256-pinned git index, and an
explicit first-run trust warning. They publish as
per-platform single-layer OCI blobs at
`oci://ghcr.io/cube-idp/plugins/<name>:<ver>-<os>-<arch>` with artifactType and layer type
`application/vnd.cube-idp.plugin.v1`, discoverable via an index artifact. `plugin install`
selects `platforms["<GOOS>-<GOARCH>"]`, pulls by digest never by tag, writes a 0755
executable to `plugin.InstallDir()`, and hands off to the trust-consent flow. When the
plugin index is unreachable there is no built-in fallback catalog: listing and search fail
with a typed error.

## Consequences

* Good, because new packs and plugins become discoverable by republishing an index
  artifact, with no new binary release.
* Good, because every install resolves a pinned manifest digest, so a moved tag cannot
  change what a user receives.
* Good, because a downloaded binary works out of the box: `init` writes published
  `oci://ghcr.io/cube-idp/packs/...` refs and needs no repository checkout.
* Good, because CI and the e2e suite are hermetic — the network is only reached on the
  interactive wizard path.
* Bad, because the built-in pack catalog is a permanent second source of truth that must
  be kept in sync with the packs' `pack.cue` descriptions by hand.
* Bad, because an offline user sees a pack list that may be older than the published one,
  announced only by a single advisory line.
* Bad, because plugin discovery has no offline mode at all: a cold cache plus no network
  means `plugin list --available` and `plugin search` simply fail.
* Bad, because the 24-hour cache means a freshly published pack or plugin can stay
  invisible to a user for up to a day.

## Implementation Status

**This decision is implemented.** Confirmed against the code on 2026-07-20.

| Decision | Implemented at |
| --- | --- |
| Each pack publishes as an OCI artifact at `oci://ghcr.io/cube-idp/packs/<name>`, with a catalog index artifact at `.../packs/index` listing name, version, description and ref for every pack. | `internal/pack/catalog.go:25` and `46-52` |
| The catalog index schema is `{schemaVersion: 1, packs: [{name, version, description, ref, digest}]}` sorted by name; `pack index build` requires contract-v1 descriptions and `name == directory`, and rejects a zero-pack index as a typed error. | `cmd/pack_publish.go:165-221`, `internal/pack/catalog.go:38-52` and `108-124` |
| The catalog is fetched from `oci://ghcr.io/cube-idp/packs/index:latest` (override `CUBE_IDP_PACK_INDEX`) and cached 24h; network failure errors rather than serving a stale cache, and callers fall back to the never-deleted built-in catalog with a printed note. | `internal/pack/catalog.go:24-33` and `61-101`, `cmd/pack.go:76-81` and `131-144` |
| `cube-idp init`'s wizard loads the remote catalog only on the interactive path, so flag-driven runs, CI and e2e never touch the network. | `cmd/init.go:93-99` |
| `init` writes default pack refs as `oci://ghcr.io/cube-idp/packs/...` so the gateway pack resolves out-of-repo, while `init --local` produces repo-relative refs for checkout development. | `internal/config/types.go:248-253`, `cmd/init.go:121-126` and `142-146` |
| Exec plugins follow the krew model: `cube-idp-<name>` binaries on PATH, an env-var contract, a sha256-pinned git index, and an explicit first-run trust warning. | `internal/plugin/discover.go:18` and `39-45` (PATH + `cube-idp-` prefix), `internal/plugin/exec.go:35` and `36-40` (trust gate, env contract), `internal/plugin/index.go:60-66` and `260-266` (sha256-pinned index), `internal/plugin/trust.go:143` (`EnsureTrusted`) |
| Plugins are discoverable through a published index artifact at `oci://ghcr.io/cube-idp/plugins/index:latest`. | `internal/plugin/officialindex.go:36` |
| Plugin artifacts are per-platform single-layer OCI blobs using `application/vnd.cube-idp.plugin.v1` as artifactType and layer type, discoverable through an index at `oci://ghcr.io/cube-idp/plugins/index:latest` with media type `application/vnd.cube-idp.plugin.index.v1`. | `internal/oci/pull.go:25-34` (both media types), `internal/plugin/officialindex.go:36` and `50-70` (index ref and schema) |
| `plugin install <name>[@version]` resolves the official index (override `CUBE_IDP_PLUGIN_INDEX`, 24h cache), selects `platforms["<GOOS>-<GOARCH>"]`, pulls by digest never by tag, writes a 0755 executable to `plugin.InstallDir()` and hands off to the trust-consent flow. | `internal/plugin/officialindex.go:35-46`, `76-113`, `161-174`, `185-231`; `internal/plugin/index.go:316-336` (0755 `atomicInstall`) |
| Plugins have no built-in fallback catalog: an unreachable official index makes `plugin list --available` / `search` fail with a typed error whose note points at the git-index path. | `internal/plugin/officialindex.go:101-106`, surfaced by `cmd/plugin.go:122-126` (`renderAvailable`, called from `71-72` and `112`) |
| Pack discovery uses the remote index artifact rather than a hardcoded in-binary catalog, so install, wizard, list and search find packs without a new binary release. *(superseded — see History)* | `internal/pack/catalog.go:61` |

### Verification

- [ ] `internal/pack/catalog.go` pins `DefaultIndexRef = "oci://ghcr.io/cube-idp/packs/index:latest"`, `EnvPackIndex = "CUBE_IDP_PACK_INDEX"` and `catalogCacheTTL = 24 * time.Hour`.
- [ ] `internal/pack/catalog.go`'s `CatalogEntry` has exactly the five fields `Name, Version, Description, Ref, Digest`, and `parseCatalog` rejects both `schemaVersion != 1` and a zero-pack index.
- [ ] `cmd/pack_publish.go` rejects a pack whose `pack.cue` name differs from its directory, rejects a pack with an empty description, sorts entries by name, and returns a typed error when no pack directories are found.
- [ ] `internal/pack/catalog.go`'s `FetchCatalog` returns `(nil, err)` on a pull failure — it never falls back to an expired cache entry.
- [ ] `cmd/pack.go` still defines `packCatalog` / `builtinCatalogEntries()`, and `loadPackCatalog` returns them with a warning when `pack.FetchCatalog` errors.
- [ ] `cmd/init.go` calls `loadPackCatalog` only inside the `if wizardApplicable(c)` branch; no other call site in the file reaches the catalog.
- [ ] `internal/config/types.go`'s `Default` writes gateway and pack refs under `oci://ghcr.io/cube-idp/packs/`, and `cmd/init.go` rewrites them to `<localAbs>/packs/<name>` only when `--local` is given.
- [ ] `internal/oci/pull.go` defines `PluginBlobMediaType = "application/vnd.cube-idp.plugin.v1"` and `PluginIndexMediaType = "application/vnd.cube-idp.plugin.index.v1"`.
- [ ] `internal/plugin/officialindex.go` declares `PluginIndex{SchemaVersion, Plugins}` and `IndexedPlugin{Name, Version, Description, Platforms}` keyed `"<os>-<arch>"`, and `selectIndexPlatform` keys on `runtime.GOOS + "-" + runtime.GOARCH`.
- [ ] `internal/plugin/officialindex.go` errors when a platform entry has an empty `Digest`, builds the pull ref as `repo + "@" + plat.Digest`, and ends in `Trust` / `EnsureTrusted` after `atomicInstall`.
- [ ] `internal/plugin/officialindex.go` wraps a failed index `oci.PullBlob` into `diag.CodePluginTrustIO` pointing at `plugin install <name> --index <git-url>`; `grep -r builtin internal/plugin` finds no fallback catalog.
- [ ] `internal/plugin/exec.go` passes `CUBE_IDP_KUBECONFIG`, `CUBE_IDP_CUBE_NAME`, `CUBE_IDP_REGISTRY` and `CUBE_IDP_CA` to the plugin, `internal/plugin/discover.go` resolves the `cube-idp-` prefix on PATH then `InstallDir()`, and `internal/plugin/index.go` verifies `Platform.SHA256` before install.

## History

Pack discovery was originally decided as a *replacement*: the remote index artifact would
supersede the hardcoded catalog compiled into the binary, so that `pack install`, the
`init` wizard, `pack list --available` and `pack search` could find packs without a new
binary release. That half shipped — the remote index is primary and drives every catalog
surface.

The removal half did not. The built-in `packCatalog` was deliberately kept rather than
deleted, and is now the permanent offline fallback: `loadPackCatalog` degrades to
`builtinCatalogEntries()` with a warning when the index is unreachable
(`cmd/pack.go:71-81`, `126-144`). What ships is remote-index-first with a permanent
built-in fallback, not a pure remote catalog.

## More Information

Origin: mined from the archived planning corpus (`docs/archive/superpowers/`) during the
2026-07-20 documentation audit; the underlying statements were validated against the code
before this record was written.

- `docs/archive/superpowers/plans/2026-07-18-cube-idp-phase5.md:2576` — pack catalog index schema and `pack index build` guards
- `docs/archive/superpowers/plans/2026-07-18-cube-idp-phase5.md:3322` — remote catalog fetch, 24h cache, built-in fallback
- `docs/archive/superpowers/plans/2026-07-18-cube-idp-phase5.md:3713` — plugin OCI artifact and index media types
- `docs/archive/superpowers/plans/2026-07-18-cube-idp-phase5.md:3810` — no built-in plugin fallback catalog
- `docs/archive/superpowers/specs/2026-07-18-cube-idp-phase5-roadmap-design.md:71` — packs published under `ghcr.io/cube-idp/packs`
