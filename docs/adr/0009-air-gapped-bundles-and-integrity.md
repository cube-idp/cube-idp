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
rather than over the lock's parsed shape, so older bundles keep opening via the legacy
lift.

`bundle.Verify` recomputes the content hash of every pack tree and every image tar and
compares it against the manifest. A tampered, truncated, or swapped file cannot pass. A
missing manifest hash or any mismatch fails with CUBE-7004 naming the offending pack or
image.

Vendor and bundle failures use a closed set of codes: CUBE-7001 (lock missing or
unreadable), CUBE-7002 (vendor-side pull failed), CUBE-7003 (bundle unreadable or corrupt,
including format-version rejection and extraction-cap trips), CUBE-7004 (bundle incomplete
or content-hash mismatch), CUBE-7005 (`--bundle` unsupported for the provider), and
CUBE-7006 (bundled-image load into cluster nodes failed). No new codes are allocated for
bundle integrity.

There are no CUBE codes for pack-signature verification, because in-binary cryptographic
verification is not implemented; the plugin trust model is sha256 consent, not signing.

Every bundle-producing and bundle-consuming command routes through the shared UI pipeline
and therefore has an equivalent plain projection, so CI output stays complete.

## Consequences

* Good, because air-gapped install is one produce command and one consume flag, rather
  than a surface of bundle subcommands and mirror flags to learn.
* Good, because integrity is checked by recomputed content hashes, so corruption and
  tampering are caught before anything reaches a cluster — presence-and-size checks would
  not catch a swapped image tar.
* Good, because digesting the lock over raw bytes keeps old bundles openable when the lock
  schema evolves.
* Good, because the closed 7xxx code set makes bundle failures machine-triageable and
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
| Air-gapped install is `vendor` producing one bundle tarball from `cube.lock`, consumed by `up --bundle <file>` installing fully offline; registry mirrors are configuration, not CLI flags. | `cmd/vendor.go:13-38` |
| Bundles embed `cube.lock` verbatim and the manifest digest is over those bytes, not the parsed shape, so old bundles still open. | `internal/bundle/bundle.go:197-206` |
| `bundle.Verify` recomputes the content hash of every pack tree and image tar against the manifest; a missing hash or any mismatch fails with CUBE-7004 naming the pack or image. | `internal/bundle/bundle.go:208-241` |
| Vendor/bundle failures use exactly CUBE-7001 through CUBE-7006; no new codes are allocated for bundle integrity. | `internal/diag/codes.go:138-143` |
| No CUBE codes exist for pack-signature verification, because in-binary cryptographic verification is not implemented. | `internal/diag/codes.go:138-143` |
| CUBE code ranges are partitioned by domain and the diag package doc enumerates them. | `internal/diag/diag.go:3-5` |
| Every live view has an equivalent plain projection so CI output stays complete; the rule covers renderers as well as `Printer`. | `internal/ui/render/plain.go:14-41` |

### Verification

- [ ] `cmd/vendor.go` defines a single `vendor` command whose `RunE` calls `bundle.Vendor`, and there is no separate `bundle create` command.
- [ ] `cmd/up.go` exposes `--bundle` ("install fully offline from a cube-idp vendor bundle") and threads it as `up.Options.Bundle`.
- [ ] `internal/bundle/bundle.go` compares `"sha256:"+hex(sha256(raw cube.lock bytes))` against `Manifest.LockDigest`.
- [ ] `internal/bundle/bundle.go` `Verify` calls `dirhash.HashDir` for every locked pack and `sha256File` for every manifest image, returning `diag.CodeVendorIncomplete` (CUBE-7004) on a missing hash or mismatch, with the pack or image name in the message.
- [ ] `internal/diag/codes.go` declares exactly `CodeVendorLockMissing` (7001), `CodeVendorPullFail` (7002), `CodeVendorBundleCorrupt` (7003), `CodeVendorIncomplete` (7004), `CodeBundleNoImageLoader` (7005), `CodeBundleImageLoadFail` (7006) in the vendor/bundle band.
- [ ] A grep for `cosign`, `sigstore`, or `signature` across `internal/` and `cmd/` finds no signature-verification code path and no corresponding CUBE code.
- [ ] `internal/bundle/bundle.go` rejects a manifest whose `formatVersion` is not the current one with `diag.CodeVendorBundleCorrupt` (CUBE-7003).
- [ ] `internal/ui/render/plain.go` `Plain` is a pure per-event function emitting no ANSI, and `internal/ui/render/` holds `plain.go`, `live.go`, `styled.go`, and `json.go` over one event stream.

## History

The CUBE code partition originally described the 8xxx range as reserved and unallocated.
That range was subsequently allocated to the spoke feature (CUBE-8001..8006), so the
domain partition now has no free reserved block; the 0xxx-7xxx partition survives
unchanged and `internal/diag/diag.go:3-5` documents 8xxx as spoke.

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
- `docs/archive/superpowers/specs/2026-07-19-cube-idp-pack-depends-and-cubelock-crd-design.md:355` — lock embedded verbatim, digest over bytes.
- `docs/archive/superpowers/plans/2026-07-15-cube-idp-phase4-first-release.md:620` — content-hash verification in `bundle.Verify`.
- `docs/archive/superpowers/plans/2026-07-15-cube-idp-phase4-first-release.md:38` — the closed CUBE-7001..7006 set.
- `docs/archive/superpowers/plans/2026-07-18-cube-idp-phase5.md:183` — no CUBE codes for pack-signature verification.
