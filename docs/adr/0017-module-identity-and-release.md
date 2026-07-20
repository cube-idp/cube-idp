---
status: "accepted"
date: 2026-07-20
decision-makers: "cube-idp maintainers"
---

# 17. Module Identity, Release Artifacts, and Toolchain Pinning

## Context and Problem Statement

cube-idp is a Go CLI distributed as prebuilt binaries. Three things about the project's
identity have to be stable and stated once, because every other piece of tooling derives
from them: the Go module path that importers and build-time ldflags reference, the Go
toolchain version that CI and release builds compile with, and the shape and destination
of the published release artifacts.

The project moved from a personal GitHub namespace (`github.com/rafpe/cube-idp`) to an
organization namespace (`github.com/cube-idp/cube-idp`). A Go module path is not a
cosmetic label — it is baked into every import statement, into the `-X ...` ldflags that
stamp version metadata into the binary, and into the release target. If any of these
drift apart, imports break for downstream consumers or a release publishes to the wrong
repository. Likewise, a toolchain version written into a workflow file is a second source
of truth that silently diverges from `go.mod`.

## Decision

The Go module path is `github.com/cube-idp/cube-idp`. All Go imports and all goreleaser
ldflags reference that path. GitHub's transfer redirect keeps `go get` on the former
`github.com/rafpe/cube-idp` path resolving, but the module itself declares the new path,
so source imports must be updated to match.

The Go toolchain version is never hardcoded. CI and release workflows resolve it from
`go.mod` via `go-version-file`, making `go.mod` the single source of truth for the
toolchain.

Releases are checksummed multi-platform binaries for darwin and linux on arm64 and
amd64, published to the `cube-idp/cube-idp` repository as non-draft releases with
prerelease detection set to `auto`.

This record does not decide the terminal UI technology. See ADR-0026 for the authoritative
statement of the Charm v2 import-path rule and the `internal/ui/theme` leaf-package
constraint.

## Consequences

* Good, because a single module path means imports, ldflags and release target cannot
 drift apart — a mismatch fails the build rather than shipping a mislabelled binary.
* Good, because `go-version-file: go.mod` removes an entire class of "CI uses a different
 Go than my machine" bugs; bumping `go.mod` bumps CI.
* Bad, because the rename breaks the imports of any external consumer that has the old
 path written into its source, even though `go get` on the old path still resolves
 through the transfer redirect.
* Bad, because pinning the *toolchain* but installing tools (`kind`, `setup-envtest`)
 with `@latest` leaves CI exposed to upstream tool changes — the pinning discipline was
 applied to the toolchain only.

## Implementation Status

**This decision is implemented.** Confirmed against the code on 2026-07-20.

| Decision | Implemented at |
| --- | --- |
| The Go module path is `github.com/cube-idp/cube-idp`. | `go.mod` |
| Build-time ldflags stamp version metadata against that same module path. | `.goreleaser.yaml` |
| Release builds cover darwin/linux × amd64/arm64 and emit a sha256 `checksums.txt`. | `.goreleaser.yaml` |
| Releases publish to the `cube-idp/cube-idp` repository as non-draft releases with prerelease detection set to auto. | `.goreleaser.yaml` |
| The Go toolchain is resolved from `go.mod` via `go-version-file: go.mod` rather than a hardcoded version. | `.github/workflows/ci.yaml`; `.github/workflows/release.yaml` |

### Verification

- [ ] `go.mod` declares `module github.com/cube-idp/cube-idp`.
- [ ] `grep -rn "rafpe" go.mod .goreleaser.yaml .github/workflows/` returns nothing.
- [ ] `.goreleaser.yaml` stamps ldflags against `github.com/cube-idp/cube-idp/cmd`.
- [ ] `.goreleaser.yaml` sets release github owner `cube-idp`, name `cube-idp`,
      `draft: false`, `prerelease: auto`.
- [ ] `.goreleaser.yaml` builds `goos: [darwin, linux]` × `goarch: [amd64, arm64]` and
      emits `checksums.txt`.
- [ ] `grep -c 'go-version-file: go.mod' .github/workflows/*.yaml` totals the same count as
      `grep -c 'uses: actions/setup-go' .github/workflows/*.yaml` (currently 4: ci.yaml
      13/35/61, release.yaml 14).
- [ ] `grep -rn 'go-version:' .github/workflows/` returns nothing (no hardcoded Go version).

## History

The release target was originally the personal `RafPe/cube-idp` repository. The
organization migration moved it to owner `cube-idp` / name `cube-idp`; the draft and
prerelease halves of that original decision carried over unchanged.

CI was originally required to pin *all* tool versions explicitly — a pinned
`sigs.k8s.io/kind` CLI and a Makefile-pinned `setup-envtest`. Only the
`go-version-file: go.mod` clause survived. `.github/workflows/ci.yaml` installs
`sigs.k8s.io/kind@latest`, the adjacent k3d install curls an unpinned `install.sh` from
`main`, and `Makefile:16,19,23` run `setup-envtest@latest` (only the Kubernetes asset
version `1.33` is fixed). The explicit-tool-pinning rule is therefore not in force; the
toolchain-from-`go.mod` rule is.

## More Information

Origin: mined from the archived planning corpus (`docs/archive/superpowers/`) during the
2026-07-20 documentation audit; the underlying statements were validated against the code
before this record was written.

- `docs/archive/superpowers/specs/2026-07-16-org-migration-design.md:15` — module path
 (decision row 2: rename to `github.com/cube-idp/cube-idp`).
- `docs/archive/superpowers/plans/2026-07-15-cube-idp-phase4-first-release.md:305` — release
 artifact shape and target.
- `docs/archive/superpowers/plans/2026-07-15-cube-idp-phase4-first-release.md:19` — toolchain
 resolved from `go.mod` (`go-version-file: go.mod`); the accompanying tool-version pinning
 rule is largely superseded.

The Charm v2 line and the `internal/ui/theme` leaf-package rule are decided in ADR-0026,
which carries their provenance.
