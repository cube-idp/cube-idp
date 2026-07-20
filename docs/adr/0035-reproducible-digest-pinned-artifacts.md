---
status: "accepted"
date: 2026-07-20
decision-makers: "cube-idp maintainers"
---

# 35. Reproducible Installs: Digest-Pinned Artifacts and Deterministic Republishing

## Context and Problem Statement

cube-idp installs a platform by pulling packs from OCI registries and git, and container
images from wherever those packs point. Every one of those inputs is addressable by a
mutable tag. If the CLI consumes tags, two installs run a week apart from the same
`cube.yaml` can produce different clusters, and nobody can say which artifact was actually
deployed — the failure is silent and only surfaces long after the fact.

The same mutability problem shows up on the producing side. If publishing stamps wall-clock
time into an artifact, republishing byte-identical content yields a fresh digest. That
breaks the "unchanged pack republish is a no-op" property CI depends on, and it means a
digest tells you nothing about content.

Reproducibility is also a prerequisite for air-gapped installs: a bundle can only vendor
exactly what will be installed if what will be installed is already frozen. And the drift
detector has to cope with records whose own content embeds fetch-time-resolved digests,
which would otherwise be reported as perpetual drift on every run.

## Decision

Every consumed pack is pinned by resolved digest, never by mutable tag; the image list is
recorded verbatim from rendered manifests and is not itself digest-resolved.

`cube.lock` is a committed, per-platform lockfile recording pack references pinned by
resolved digest (`oci:<sha256>` digest, `git:<commit sha>`, or `dir:<dirhash>` for
local/http/s3 refs, which have no upstream pin protocol) together with the full image
list, making installs reproducible and feeding air-gap vendoring.

The air-gap bundle carries a verbatim `cube.lock` copy so that what is vendored is exactly
what the lock pins. Vendoring derives engine images from the lock's engine entry and
vendors the engine pack source, with no CLI-embedded default engine image list. See
ADR-0009 for the authoritative statement of the bundle format, its `formatVersion` gate,
and bundle verification.

OCI pushdir stamps a content-derived fixed-epoch `org.opencontainers.image.created`
annotation (`1970-01-01T00:00:00Z`) rather than `time.Now()`, so identical content always
republishes to an identical digest.

The online e2e leg consumes published packs pinned by digest from a committed
`tests/e2e/packs.lock`, and `diff` treats the CubeLock record as identity-only
(`orphanOnly`) because its spec embeds fetch-resolved digests that would otherwise
fabricate perpetual drift.

Commit subjects follow conventional-commit prefixes (`feat:`/`fix:`/`docs:`) so the release
changelog can group on them. This is a convention the changelog consumes, not a gate: no
commit-message check runs in CI. CI does gate merges on `go vet ./...` and
`go test ./... -short`.

## Consequences

* Good, because two installs from the same committed `cube.lock` resolve to byte-identical
  packs and images, so "what is deployed" is answerable from the repository alone.
* Good, because a fixed-epoch created annotation makes digests a function of content:
  republishing an unchanged pack is a true no-op, and CI can skip on digest equality.
* Good, because vendoring reads the lock rather than an embedded list, so the bundle can
  never drift from what a connected install would have pulled — including the engine.
* Good, because e2e runs against digest-pinned published packs, so a test failure is a real
  regression rather than an upstream pack having moved under the tag.
* Bad, because pins must be refreshed deliberately; picking up an upstream fix requires a
  lock update and a commit rather than happening on the next run.
* Bad, because the bundle manifest is versioned and any `formatVersion` other than 2 is
  rejected outright, so bundle format changes are a breaking change for existing artifacts
  (see ADR-0009).
* Bad, because the CubeLock record is compared on identity only, so genuine changes inside
  that record's spec are invisible to `diff`.

## Implementation Status

**This decision is implemented.** Confirmed against the code on 2026-07-20.

| Decision | Implemented at |
| --- | --- |
| `cube.lock` is a committed, per-platform lockfile recording pack references pinned by resolved digest (`oci:<sha256>`, `git:<commit sha>`, or `dir:<dirhash>` for local/http/s3 refs) together with the full image list. | `internal/lock/lock.go`, `internal/pack/source.go` |
| The image list is extracted verbatim from the rendered manifests (plus any images the pack declares) and is not itself digest-resolved. | `internal/lock/images.go` |
| The bundle embeds a verbatim `cube.lock` copy, whose bytes the manifest's `lockDigest` is taken over. Bundle format and verification are owned by ADR-0009. | `internal/bundle/bundle.go` |
| Bundle vendoring derives the engine's images from the lock's engine entry and vendors the engine pack source itself, with no CLI-embedded default engine image list. | `internal/bundle/vendor.go` |
| OCI pushdir stamps a fixed-epoch `org.opencontainers.image.created` annotation (`1970-01-01T00:00:00Z`) instead of `time.Now()`, so identical content republishes to an identical digest. | `internal/oci/pushdir.go` |
| The online e2e leg consumes published packs pinned by digest from a committed `tests/e2e/packs.lock`, never by mutable tag. | `tests/e2e/packs.lock:1-11` |
| `diff` treats the CubeLock record as identity-only (`orphanOnly`) because its spec embeds fetch-resolved digests that would otherwise fabricate perpetual drift. | `internal/diff/diff.go` |
| The release changelog groups commits on conventional-commit prefixes (`feat:`/`fix:`/`docs:`). The prefixes are a convention the changelog consumes, not a CI-enforced gate. | `.goreleaser.yaml` |
| CI gates merges on `go vet ./...` and `go test ./... -short`. | `.github/workflows/ci.yaml` |
| (Superseded) The v0.1.0 release is private end to end: repository, binaries, packs, and GHCR packages created by `GITHUB_TOKEN` so they stay repo-linked and private. | `README.md` |

### Verification

- [ ] `internal/lock/lock.go` defines `File{APIVersion,Kind,Engine,Packs}` and
      `Entry{Ref,Name,Version,Resolved,RenderedHash,Images}`, and `PathFor` writes the file
      literally as `cube.lock`.
- [ ] `internal/bundle/bundle.go` reads a verbatim `cube.lock` out of the opened bundle
      (format-version gating itself is verified by ADR-0009).
- [ ] `internal/bundle/vendor.go` prepends `lf.Engine.Entry()` onto `lf.Packs`, and no
      default engine image list is embedded in the CLI.
- [ ] `grep -n "time.Now()" internal/oci/pushdir.go internal/oci/push.go` returns nothing;
      both set `AnnotationCreated` to `1970-01-01T00:00:00Z`.
- [ ] Every entry in `tests/e2e/packs.lock` matches
      `oci://ghcr.io/cube-idp/packs/<name>@sha256:` — no mutable tag appears in the file.
- [ ] `internal/diff/diff.go` appends `identityStub(packGVK, …)` to `orphanOnly` for each
      pack's record and for the engine pack's own record.
- [ ] `.goreleaser.yaml` changelog groups key on `^feat(\(.*\))?:`, `^fix(\(.*\))?:` and
      `^docs(\(.*\))?:`.

## History

The release was originally private end to end: a private repository, binaries and packs
consumable only by authorized users, and GHCR packages created by `GITHUB_TOKEN` so they
were auto-linked to the repository and stayed private, with public launch deferred. That is
superseded. Packs and plugins are now published to public `ghcr` namespaces (see the
engine-as-pack work); only the binary release remains private —
`README.md` states that all packs come from the public `cube-idp/packs` monorepo by
default so a downloaded binary works standalone.

## More Information

Origin: mined from the archived planning corpus (`docs/archive/superpowers/`) during the
2026-07-20 documentation audit; the underlying statements were validated against the code
before this record was written.

Member provenance:

- `docs/archive/superpowers/specs/2026-07-13-cube-idp-architecture-design.md:169` — `cube.lock`
- `docs/archive/superpowers/plans/2026-07-15-cube-idp-phase4-first-release.md:492` — bundle
  manifest formatVersion 2
- `docs/archive/superpowers/plans/2026-07-15-cube-idp-phase4-first-release.md:1396` — fixed-epoch
  created annotation
- `docs/archive/superpowers/plans/2026-07-19-cube-idp-engine-as-pack.md:1107` — engine vendoring
  from the lock
- `docs/archive/superpowers/plans/2026-07-18-cube-idp-phase5.md:2878` — digest-pinned e2e packs
