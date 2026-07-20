---
status: "accepted"
date: 2026-07-20
decision-makers: "cube-idp maintainers"
---

# 2. Pack Format: A Versioned, Data-Only Directory Contract

## Context and Problem Statement

cube-idp installs optional platform capabilities into a Kubernetes cluster. Those
capabilities have to come from somewhere, be versionable, be shareable between users, and
be safe to fetch from a registry that the person running the CLI does not necessarily
control.

Two designs were available. One is the plugin model: a capability ships executable code
that the tool loads and runs. That requires a plugin protocol, a trust story for arbitrary
third-party code, and a growing core to host it. The other is the data model: a capability
ships only declarative content, and the CLI is the only thing that ever executes.

cube-idp needs the second, and it needs the shape of that data to be stable enough that
packs published today keep loading after the CLI gains new features. Without a frozen,
versioned contract, every new optional field silently breaks every pack that predates it,
and every pack author has to guess what the loader accepts.

## Decision

A pack is a **data-only directory**: a required `pack.cue` manifest — name, semver
version, optional `description`, `dependsOn`, `#Values` schema, optional `expose:`,
`images:`, and `gatewayService:` — plus manifests, a chart reference, values, or kustomize
overlays. Packs MUST NOT contain executable content.

The architecture is **Kernel+Packs**: the core stays minimal and every optional capability
beyond the kernel ships as a versioned, shareable, OCI-distributed pack.

The pack format is a **public API contract at version v1**, enforced by a CUE schema in a
conformance harness and revised only additively. Optional `pack.cue` fields load
permissively: an absent field yields nil and pre-feature packs load unchanged, while
malformed entries fail as `CUBE-4003`.

Raw manifests render in sorted filename order from top-level `.yaml`/`.yml` files under
`manifests/`, except when a root `kustomization.yaml` exists — in which case it becomes
the sole source of raw manifests and `manifests/` is consumed only through it. Helm chart
rendering is appended in both cases.

Pack values are validated against the pack's `#Values` schema before rendering. A pack
with no schema accepts user values unchanged; where a schema exists, values are unified
with it and must be fully concrete, with mismatches reported as `CUBE-4002`.

cube-idp adds no new plugin protocols, no reconciled CRDs, and no daemon. The sole
exception is the inert `Pack` record CRD written by `up`, inventory-tracked and watched by
no controller.

## Consequences

* Good, because a pack cannot execute anything: the fetch path can strip symlinks and skip
  non-regular files, and the blast radius of a hostile registry is bounded to the manifests
  it renders.
* Good, because permissive loading of optional fields means the CLI can grow `dependsOn`,
  `images:`, `gatewayService:`, and `description` without invalidating already-published
  packs.
* Good, because `#Values` validation runs before any cluster mutation, so a typo in user
  values fails the run rather than half-applying it.
* Good, because "no reconciled CRDs, no daemon" keeps the operational surface to a CLI —
  nothing runs in the cluster on cube-idp's behalf between invocations.
* Bad, because capabilities that genuinely need imperative logic cannot be expressed as a
  pack at all; they must move into the kernel or not exist.
* Bad, because "additive only" means design mistakes in v1 fields are permanent for the
  life of v1 — a field can be deprecated in documentation but not removed.
* Bad, because the kustomization/`manifests/` precedence rule is a real behavioral cliff:
  adding a root `kustomization.yaml` to an existing pack silently changes which manifests
  render.

## Implementation Status

**This decision is implemented.** Confirmed against the code on 2026-07-20.

| Decision | Implemented at |
| --- | --- |
| A pack is a data-only directory with a required `pack.cue` (name, semver, optional deps and `#Values`) plus `manifests/*.yaml`, HelmRelease, or chart refs, and contains no executable code. | `internal/pack/pack.go:117-176` |
| Raw manifests render in sorted filename order from top-level `.yaml`/`.yml` under `manifests/`; a root `kustomization.yaml` takes over as the sole raw-manifest source, with helm rendering appended in both cases. | `internal/pack/render.go:41-103` |
| `dependsOn` is optional and loaded like `images:`/`gatewayService:` — absent yields nil so pre-feature packs load unchanged, and a malformed entry fails as CUBE-4003. | `internal/pack/pack.go:153-159` |
| Pack values are validated against `#Values` before rendering: no schema means values pass through, otherwise they are unified and must be fully concrete, with mismatches as CUBE-4002. | `internal/pack/pack.go:264-273` |
| No new plugin protocols, no reconciled CRDs, no daemon — the sole exception is the inert cluster-scoped `Pack` record CRD, paired with the `pack.cue expose:` contract. | `internal/pack/manifests/pack-crd.yaml:1-14` |
| `description` is optional at load time for backward compatibility, but publishing from the official packs repo requires a one-line user-facing description. | `internal/pack/pack.go:52-56`, `cmd/pack_publish.go:187-191` |
| A pack is data only — `pack.cue` plus manifests, chart references, values, or kustomize overlays — and MUST NOT contain executable content. | `docs/pack-contract-v1.md:19-31` |
| The architecture is Kernel+Packs: a minimal core, with every optional capability shipping as a versioned, OCI-distributed pack. | `docs/pack-contract-v1.md:19-31` |
| The pack format is a versioned public API contract over `pack.cue` fields, the `manifests/` and `chart.yaml` layouts, and `${GATEWAY_HOST}`/`${GATEWAY_FQDN}` substitution, enforced by a conformance harness. | `internal/pack/contract_conformance_test.go:22` |
| The vocabulary triad's `tuning` entry is replaced by `values → chart render (packs and the engine alike)` as an additive v1.1 doc revision, leaving the pack-side contract untouched. | `docs/pack-contract-v1.md:168` |

### Verification

- [ ] `internal/pack/pack.go` rejects a pack directory with no `pack.cue`, and one missing
      `name` or `version`, as `CUBE-4003` (`diag.CodePackCueInvalid`).
- [ ] `internal/pack/pack.go:153-159` loads `dependsOn` behind an `Exists()` guard, leaving
      `Pack.DependsOn` nil when the field is absent — same shape as the `images:` and
      `gatewayService:` guards above and below it.
- [ ] `internal/pack/pack.go:264-273` returns the caller's values unchanged when `#Values`
      does not exist, and otherwise fails `unified.Validate(cue.Concrete(true))` mismatches
      as `CUBE-4002` (`diag.CodePackValuesInv`).
- [ ] `internal/pack/render.go:48` stats `kustomization.yaml` at the pack root before the
      `manifests/` walk, and the walk at `:66-77` sorts entries by filename and skips
      directories and any extension other than `.yaml`/`.yml`.
- [ ] `internal/pack/render.go:91-98` appends `chart.yaml` helm rendering after either
      branch.
- [ ] `grep -rn 'group: cube-idp.dev' internal --include=*.yaml` (excluding testdata) yields
      exactly one CRD: `packs.cube-idp.dev` in `internal/pack/manifests/pack-crd.yaml`, and
      no controller or daemon binary watches it.
- [ ] `internal/pack/guards.go` (`GuardTree`) removes every symlink from a fetched pack tree.
- [ ] `cmd/pack_publish.go:187-191` fails publish with `CUBE-4003` for any pack in the packs
      tree lacking a `description`.
- [ ] `go test ./internal/pack -run TestReposPacksSatisfyContractV1` passes (skips when the
      `packs/` tree is absent).

## History

`#Values` validation was originally scoped to a `cube-idp add <pack>` command. No `add`
command exists — the CLI's pack surface is `pack install` / `pack push` / `pack publish` —
and the validation moved into the `up`/pack-install path. The guarantee it carried is
preserved unchanged: values are validated before any cluster mutation occurs, since `up`'s
pass-1 fetch-and-render loop runs entirely ahead of the apply phase
(`internal/up/up.go:341-343`).

The contract itself has taken one additive revision, v1.1 (2026-07-19, engine-as-pack):
the `tuning` noun was retired in favour of `values` for the engine, which now installs from
a `cube-engine-<type>` pack. The pack-side contract was untouched, because engine packs are
ordinary packs.

## More Information

Origin: mined from the archived planning corpus (`docs/archive/superpowers/`) during the
2026-07-20 documentation audit; the underlying statements were validated against the code
before this record was written. Member origins, as `file:line` provenance in that corpus:

- `plans/2026-07-13-cube-idp-phase1-mvp.md:2010` — the data-only pack directory
- `plans/2026-07-13-cube-idp-phase2-draft.md:908` — manifest render order and kustomization precedence
- `specs/2026-07-13-cube-idp-architecture-design.md:51` — Kernel+Packs
- `specs/2026-07-18-cube-idp-phase5-roadmap-design.md:63` — the pack format as a versioned public API contract
- `specs/2026-07-19-cube-idp-engine-as-pack-design.md:244` — the v1.1 vocabulary amendment

See also `docs/pack-contract-v1.md`, the frozen v1 contract document, whose `CONTRACT.md`
in the `cube-idp/packs` repository is a verbatim copy.
