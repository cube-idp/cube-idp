---
status: "accepted"
date: 2026-07-20
decision-makers: "cube-idp maintainers"
---

# 3. Pack and File Ref Grammar, Digest Pinning, and Fetch Guards

## Context and Problem Statement

cube-idp delivers cluster content as *packs*: directories of manifests, charts and CUE
that users name by a single string in `cube.yaml`. That string may point at a working
copy on disk, an OCI artifact in a registry, a subdirectory of a git repository, or an
arbitrary URL. Without a closed grammar, every new source form becomes an ad-hoc branch
in the fetcher and an unpredictable failure mode for users.

Two further problems follow from fetching remote content. First, reproducibility: a
mutable tag or branch means two `up` runs against the same `cube.yaml` can deliver
different bytes, so each resolved pack needs a recorded, content-addressed pin. Second,
safety: fetched archives and repositories can contain symlinks and traversal paths that
escape the cache directory, and a partially-written cache entry must never be observed as
complete.

Separately, `cube.yaml` can reference a single remote *configuration file* (the provider
config ref) rather than a pack. That is a different shape — one YAML document, no
`pack.cue`, no dependency graph — and conflating it with pack resolution would force
pack semantics onto something that is neither versioned nor pinned.

## Decision

A pack reference is a non-empty string resolving to one of: a local directory path,
`oci://host/repo:tag`, `github.com/org/repo//path@rev`, or an explicit go-getter URL.
Any other scheme fails with CUBE-4001, whose remediation enumerates the accepted forms.

Official packs are published and consumed under the GHCR namespace
`ghcr.io/cube-idp/packs/<name>`, with no intermediate repo segment. Default refs emitted
by the CLI, by `init --local`, by the repo's own `cube.yaml` and by the e2e suite resolve
against a packs-repo checkout, skipping with an actionable message when that checkout is
absent.

Every resolved pack records a typed pin string in `Pack.Pinned`: `oci:<digest>` from the
resolved manifest digest, `git+<full-sha>` for git refs, and `dir:h1:<hash>` for local
directories and http/s3 getter fetches. A bare git ref without an explicit revision pin
fails with CUBE-4007 before any fetch occurs.

Fetching enforces symlink disabling, a path-traversal guard, an atomic tmp-dir plus
rename, and a digest-keyed cache under `$HOME/.cache/cube-idp/packs`. Pack trees are
guarded before hashing: `pack.GuardTree` strips symlinks under CUBE-4014 rather than
rejecting the pack, so content hashes are computed over real files only.

Pack manifests apply exactly as rendered, with no implicit namespace; every namespaced
manifest must carry `metadata.namespace` itself.

Remote *file* refs are a distinct, unpinned path. `FetchFile` never parses `pack.cue`; a
direct-file URL ref fetches that file, while a directory-shaped ref must contain exactly
one top-level `*.yaml`/`*.yml` file or the fetch fails. A fetched empty document decodes
to an empty non-nil map, and a valid YAML document that is not a mapping is a resolve
error. `compose.Resolve` is the resolver itself, with no intermediate layer beneath it,
and returns no pin.

## Consequences

* Good, because the accepted ref forms are enumerated in one place and the rejection
  message tells the user exactly what is allowed.
* Good, because every delivered pack is content-addressed, so a run can be reproduced and
  drift is detectable.
* Good, because refusing unpinned git refs fails fast, before network I/O, instead of
  silently delivering whatever a branch points at today.
* Good, because symlink stripping, the traversal guard and tmp-plus-rename mean a hostile
  or malformed archive cannot escape the cache or leave a half-written entry behind.
* Bad, because stripping symlinks silently changes the pack tree: a pack that legitimately
  relies on a symlink is altered without an error, and the author only learns from the
  rendered result.
* Bad, because "no implicit namespace" pushes work onto pack authors — a namespaced
  manifest that omits `metadata.namespace` lands in `default` rather than failing.
* Bad, because file refs are deliberately unpinned, so provider config pulled from a
  remote URL is not reproducible the way packs are.
* Bad, because the local-development and e2e paths depend on a sibling packs-repo
  checkout; without it those suites skip rather than run.

## Implementation Status

**This decision is implemented.** Confirmed against the code on 2026-07-20.

| Decision | Implemented at |
| --- | --- |
| A pack reference resolves to a local directory, `oci://host/repo:tag`, `github.com/org/repo//path@rev`, or an explicit go-getter URL; any other scheme fails with CUBE-4001 listing the accepted forms. | `internal/pack/source.go:62-64` |
| Pack pin strings are typed by prefix — `oci:<digest>`, `git+<sha>`, `dir:h1:<hash>` — and recorded in `Pack.Pinned`. | `internal/pack/source.go:87-96` |
| Bare git refs must carry an explicit revision pin; an unpinned bare git ref fails with CUBE-4007 before any fetch occurs. | `internal/pack/resolve.go:88` |
| Ref fetching enforces symlink disabling, the GuardTree path-traversal guard (CUBE-4014), atomic tmp-dir plus rename, and a digest-keyed cache under `$HOME/.cache/cube-idp/packs`. | `internal/pack/getter.go:117-145` |
| Official packs are published and consumed under `ghcr.io/cube-idp/packs/<name>` with no redundant repo segment. | `internal/pack/catalog.go:25` |
| Default pack refs emitted by the CLI and the repo's own `cube.yaml` point at that same GHCR namespace. | `internal/config/types.go:117-118` |
| `cube-idp init --local` points at a packs-repo checkout rather than the cube-idp repo. | `cmd/init.go:143` |
| The e2e suite resolves packs via `CUBE_IDP_E2E_PACKS_DIR` (default `../cube-idp-packs/packs`) and skips with an actionable message when absent. | `tests/e2e/e2e_test.go:451` |
| Pack manifests apply exactly as rendered with no implicit namespace, so every namespaced manifest must carry `metadata.namespace`. | `internal/apply/applier.go`, `internal/pack/helm.go:182-190` |
| `FetchFile` never parses `pack.cue`; a ref resolving to a directory must contain exactly one top-level `*.yaml`/`*.yml` file or the fetch fails. | `internal/pack/fetchfile.go:13-92` |
| A direct-file URL ref fetches that file, while a directory-shaped ref must contain exactly one top-level `*.yaml`/`*.yml` file. | `internal/pack/fetchfile.go:20-90` |
| An empty fetched document decodes to an empty non-nil map; a valid YAML document that is not a mapping is a resolve error. | `internal/cluster/compose/compose.go:34-48` |
| `cube-idp pack list` without `--available` is a typed CUBE-0007 refusal rather than invented output. | `cmd/pack.go:192` |
| A `cube.yaml` still containing `spec.engine.tuning` is rejected at load time with CUBE-0012 pointing at `engine.values`. | `internal/config/load.go:97` |
| A dependency cycle is detected by a Kahn topological sort over the full explicit-plus-implicit graph and fails with CUBE-4019 printing the cycle path; a self-dependency is a 1-cycle and fails the same way. | `internal/pack/depgraph.go:100-140` |
| The cycle is detected before any pack is delivered and surfaced by both `up` and `diff`; `diff` validates the same graph but discards the delivery order. | `internal/pack/depgraph.go:142` |
| For engines without native ordering, `up` gates each dependent pack on a bounded health check of its dependencies, failing with CUBE-3011 on timeout. | `internal/up/up.go:632-635` |
| cube-idp ships no OpenChoreo integration in any form; the `kgateway` and `openbao` packs are parked with it. | `internal/config/types.go:249-252` |

### Verification

- [ ] `internal/pack/source.go` rejects a ref containing `://` with an unrecognised scheme under `diag.CodePackRefInvalid` (CUBE-4001), and the fix line names all four accepted forms.
- [ ] `internal/pack/guards.go` `GuardTree` removes symlinks from a fetched tree and raises `diag.CodePackGuardTrip` (CUBE-4014, `internal/diag/codes.go:105`) only when removal fails — it never raises CUBE-4001.
- [ ] `internal/pack/getter.go:123` sets `DisableSymlinks: true`, `getter.go:137` calls `GuardTree(tmp)`, and `getter.go:144` renames the tmp dir into place.
- [ ] `internal/pack/resolve.go:88` returns `diag.CodePackRefUnpin` (CUBE-4007) for an unpinned git pack ref before any fetch.
- [ ] `internal/pack/source.go:99` builds the `dir:` pin from `dirhash.Hash1`, and `internal/pack/pack.go:59-66` documents all three pin forms.
- [ ] `grep -rn "SetNamespace" internal/apply` returns no call site (only the error message at `internal/apply/inventory.go:164`).
- [ ] `internal/pack/fetchfile.go` contains no `pack.cue` parse, and `singleYAML` errors unless exactly one top-level `*.yaml`/`*.yml` is present.
- [ ] `internal/cluster/compose/compose.go:45-47` normalises a JSON-null decode to a non-nil empty map, and lines 41-44 return CUBE-1005 for a non-mapping document.
- [ ] `internal/pack/depgraph.go:142` raises `diag.CodePackDepCycle` (CUBE-4019, `internal/diag/codes.go:113`); `internal/up/up.go:607` and `internal/diff/diff.go:257` both call `pack.ResolveOrder`.
- [ ] `grep -rin "openchoreo\|kgateway\|openbao" internal cmd` returns no hits.

## History

The pack grammar was originally narrower: pack sources were limited to exactly three
reference forms — a local `./dir`, a commit-pinned `github.com/org/repo//path@vX`, and an
`oci://` reference — fetched with oras-go v2. That was widened to include explicit
go-getter forms (`git::`, `s3::`, `http(s)://`), each pinned by directory hash. oras-go v2
remains the OCI fetcher, and git refs are no longer required to be commit-pinned at the
grammar level — tags and branches resolve, with the unpinned case rejected separately at
resolve time.

Symlink handling also changed. Two earlier statements specified that the pack content
hasher *rejects* symlinked pack directories with CUBE-4001. The shipped behaviour is that
`pack.GuardTree` silently strips symlinks, raising CUBE-4014 (`CodePackGuardTrip`) only
when the removal itself fails; CUBE-4001 now means "unsupported pack ref scheme", an
unrelated condition. The underlying intent — packs are symlink-free by the time they are
hashed — is unchanged.

On the file-ref side, `compose.Resolve` was originally specified as a thin wrapper over a
separate `refval.Resolve` layer that preserved CUBE-1005 wrapping and returned a pin for
`up` to record. No `refval` package exists; `compose.Resolve` is the resolver itself,
calling `pack.FetchFile` directly, and `Compose` returns only the merged map. It does keep
the CUBE-1005 wrapping and RFC 7386 `forProvider` merge semantics. Finally, CUBE-0013 was
once assigned to a remote `-f` ref that failed to fetch or did not yield exactly one YAML
document; that feature was never built, and CUBE-0013 is now the engine-pack name mismatch
check in `pack.VerifyEnginePackRef` (`internal/diag/codes.go:18`,
`internal/pack/enginepack.go:31-40`).

## More Information

Origin: mined from the archived planning corpus (`docs/archive/superpowers/`) during the
2026-07-20 documentation audit; the underlying statements were validated against the code
before this record was written.

- `docs/archive/superpowers/plans/2026-07-13-cube-idp-phase2-draft.md:1570` — pack ref grammar and CUBE-4001.
- `docs/archive/superpowers/plans/2026-07-13-cube-idp-phase3-draft.md:141` — typed pin prefixes.
- `docs/archive/superpowers/specs/2026-07-19-cube-idp-valuesref-remote-config-design.md:148` — fetch guards and cache.
- `docs/archive/superpowers/specs/2026-07-16-org-migration-design.md:61` — GHCR packs namespace.
- `docs/archive/superpowers/specs/2026-07-19-cube-idp-valuesref-remote-config-design.md:100` — direct-file vs directory-shaped file refs.
