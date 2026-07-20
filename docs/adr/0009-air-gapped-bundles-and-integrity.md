---
status: "accepted"
date: 2026-07-20
decision-makers: "cube-idp maintainers"
---

# 9. Air-Gapped Bundles: Vendoring, Offline Install, and Integrity Verification

## Context and Problem Statement

cube-idp installs platforms by pulling packs from OCI registries, charts from chart
repositories, and container images into the cluster. None of that works in an environment
with no outbound network — the exact environment where a reproducible installer is most
valuable.

Supporting air-gapped installs raises three questions that have to be answered together.
First, how does content leave the connected side: one command producing one artefact, or a
family of commands and flags. Second, how does the disconnected side trust that artefact —
a tarball that crosses an air gap by USB stick or an internal file share is precisely the
kind of file that gets truncated, swapped, or edited in transit. Third, how do failures
report themselves: a closed, documented set of diagnostic codes is what lets operators and
CI distinguish "your bundle is corrupt" from "your provider can't do this" without reading
Go source.

Bundles are also long-lived. A bundle built months ago must still open against a newer
binary, which constrains how the embedded lock file may be digested.

## Decision

Air-gapped installation is supported by `vendor`, which pins and localizes every pack,
chart, and image reference from `cube.lock` into a single self-contained bundle tarball.
That bundle is consumed by `up --bundle <file>`, which installs fully offline by
pre-loading the bundled images into the cluster nodes. Registry mirrors are configuration
(`spec.cluster.registry.mirrors`), not CLI flags.

Bundles embed `cube.lock` verbatim, and the manifest's digest is taken over those bytes
rather than over the lock's parsed shape, so the digest survives lock-schema changes that
do not alter the bytes. Bundle compatibility itself is gated separately by
`manifest.formatVersion`, currently 2; older format versions are rejected outright with
CUBE-7003. See ADR-0035 for the authoritative statement of the bundle manifest shape and
its field list.

`bundle.Verify` recomputes the content hash of every pack tree and every image tar and
compares it against the manifest. A tampered, truncated, or swapped file cannot pass. A
missing manifest hash or any mismatch fails with CUBE-7004 naming the offending pack or
image.

Vendor and bundle failures use a closed 70xx set of codes: CUBE-7001 (lock missing or
unreadable), CUBE-7002 (vendor-side pull failed), CUBE-7003 (bundle unreadable or corrupt,
including format-version rejection and extraction-cap trips), CUBE-7004 (bundle incomplete
or content-hash mismatch), CUBE-7005 (`--bundle` unsupported for the provider), and
CUBE-7006 (bundled-image load into cluster nodes failed). No new codes are allocated for
bundle integrity.

There are no CUBE codes for pack-signature verification, because in-binary cryptographic
verification is not implemented; the plugin trust model is sha256 consent, not signing.

Bundle commands route through `ui.RunPipeline` like every other command, so they render in
plain, live, and JSON modes. See ADR-0024 for the authoritative statement of the plain
projection rule; this ADR asserts nothing bundle-specific about it.

## Consequences

* Good, because air-gapped install is one produce command and one consume flag, rather
  than a surface of bundle subcommands and mirror flags to learn.
* Good, because integrity is checked by recomputed content hashes, so corruption and
  tampering are caught before anything reaches a cluster — presence-and-size checks would
  not catch a swapped image tar.
* Good, because digesting the lock over raw bytes keeps the digest valid across lock-schema
  changes that do not alter the bytes; bundle compatibility is gated separately by
  `manifest.formatVersion`.
* Bad, because that separate gate is strict: a bundle built at `formatVersion` 1 does not
  open at all, and must be re-vendored.
* Good, because the closed 70xx code set makes bundle failures machine-triageable and
  keeps the diagnostic surface from growing per integrity feature.
* Bad, because full re-hashing of every pack tree and image tar costs time and I/O
  proportional to bundle size on every open.
* Bad, because `up --bundle` only works with providers that can node-load images (kind,
  k3d); the `existing` provider is excluded and must fail with CUBE-7005.
* Bad, because bundles carry no cryptographic provenance — integrity is verified against
  the bundle's own manifest, which detects corruption but not an attacker who rebuilds the
  manifest too.

## Implementation Status

**This decision is implemented.** Confirmed against the code on 2026-07-20.

| Decision | Implemented at |
| --- | --- |
| Air-gapped install is `vendor`, a single command producing one bundle tarball from `cube.lock`. | `cmd/vendor.go:12-38` |
| The bundle is consumed by `up --bundle <file>`, threaded as `up.Options.Bundle`. | `cmd/up.go:23,34` |
| Registry mirrors are configuration (`spec.cluster.registry.mirrors`), not CLI flags. | `internal/config/types.go:78-81` |
| The manifest's `lockDigest` is computed as sha256 over the raw embedded `cube.lock` bytes, not the parsed shape. | `internal/bundle/vendor.go:105-110` (produce side); `internal/bundle/bundle.go:202-206` (check side) |
| Bundle compatibility is gated by `manifest.formatVersion`, currently 2; any other version is rejected with CUBE-7003. | `internal/bundle/bundle.go:66-67,116-121` |
| `bundle.Verify` recomputes the content hash of every pack tree and image tar against the manifest; a missing hash or any mismatch fails with CUBE-7004 naming the pack or image. | `internal/bundle/bundle.go:208-241` |
| Vendor/bundle failures use exactly CUBE-7001 through CUBE-7006. The 7xxx range is further partitioned: 70xx vendor/air-gap, 71xx exec-plugin, 72xx sync, 73xx repo — only the 70xx band is this ADR's closed set. | `internal/diag/codes.go:138-143` (70xx band, 7001 at 138 through 7006 at 143); `internal/diag/codes.go:148-153,158-159,164-168` (71xx/72xx/73xx) |
| No CUBE code is declared anywhere in the catalog for pack-signature verification; the catalog-exhaustiveness test fences the code set so an undeclared code cannot be used. | `internal/diag/codes.go` (whole catalog); `internal/diag/codes_test.go:298` (`TestCatalogExhaustive`) |

### Verification

- [ ] `cmd/vendor.go` defines a single `vendor` command whose `RunE` calls `bundle.Vendor` (`cmd/vendor.go:28-33`), and `grep -rn 'Use:' cmd/ | grep -c bundle` returns 0 — no separate `bundle` subcommand exists.
- [ ] `cmd/up.go` exposes `--bundle` ("install fully offline from a cube-idp vendor bundle") and threads it as `up.Options.Bundle`.
- [ ] `internal/bundle/bundle.go` compares `"sha256:"+hex(sha256(raw cube.lock bytes))` against `Manifest.LockDigest`.
- [ ] `internal/bundle/bundle.go` `Verify` calls `dirhash.HashDir` for every locked pack and `sha256File` for every manifest image, returning `diag.CodeVendorIncomplete` (CUBE-7004) on a missing hash or mismatch, with the pack or image name in the message.
- [ ] `internal/diag/codes.go` declares exactly `CodeVendorLockMissing` (7001), `CodeVendorPullFail` (7002), `CodeVendorBundleCorrupt` (7003), `CodeVendorIncomplete` (7004), `CodeBundleNoImageLoader` (7005), `CodeBundleImageLoadFail` (7006) in the vendor/bundle band.
- [ ] `grep -rniE 'cosign|sigstore' internal cmd` returns hits only under `testdata/` (vendored Flux/Argo CRD fixtures), never in cube-idp code; the only `signature` hits in Go sources are `internal/trust/ca.go:221` (`x509.KeyUsageDigitalSignature`, local-CA TLS) and comment prose. No signature-verification code path and no corresponding CUBE code exist.
- [ ] `internal/bundle/bundle.go` rejects a manifest whose `formatVersion` is not the current one with `diag.CodeVendorBundleCorrupt` (CUBE-7003).
- [ ] `cmd/vendor.go:28-33` wraps the vendor run in `ui.RunPipeline`, so bundle commands share the standard renderer selection. (The plain-projection rule itself is ADR-0024's; do not re-verify it here.)

## History

The CUBE code partition originally described the 8xxx range as reserved and unallocated.
That range was subsequently allocated, so the domain partition now has no free reserved
block; the 0xxx-7xxx partition survives unchanged. The authoritative statement of the CUBE
code-range partition lives in the diagnostics ADR, not here.

An earlier scoping decision excluded the k3d provider, vendoring and `--bundle`, exec
plugins, `sync --watch`, `repo create`, and pack-catalog buildout from the then-current
delivery phase. All of those have since shipped — `internal/cluster/k3dp`,
`internal/bundle`, `internal/plugin`, `sync --watch`, `repo create`, and the pack catalog
— so the exclusion no longer holds and this ADR records the delivered state.

Bundle integrity itself was previously a presence-and-size check. It was replaced by the
full content-hash recomputation described above.

## More Information

Origin: mined from the archived planning corpus (`docs/archive/superpowers/`) during the
2026-07-20 documentation audit; the underlying statements were validated against the code
before this record was written.

- `docs/archive/superpowers/research/2026-07-13-cube-idp-brainstorm/synthesis.md:72` — vendor/`--bundle` as the air-gap mechanism, mirrors as configuration.
- `docs/archive/superpowers/specs/2026-07-19-cube-idp-pack-depends-and-cubelock-crd-design.md:355` — lock embedded verbatim, digest over bytes. (This source also speaks of old bundles "opening via the legacy lift"; no such mechanism exists in the code, and the ADR text above does not carry that claim forward.)
- `docs/archive/superpowers/plans/2026-07-15-cube-idp-phase4-first-release.md:620` — content-hash verification in `bundle.Verify`.
- `docs/archive/superpowers/plans/2026-07-15-cube-idp-phase4-first-release.md:38` — the closed CUBE-7001..7006 set.
- `docs/archive/superpowers/plans/2026-07-18-cube-idp-phase5.md:183` — no CUBE codes for pack-signature verification.
