---
status: "accepted"
date: 2026-07-20
decision-makers: "cube-idp maintainers"
---

# 6. Per-Pack Delivery Mode: oci or repo

## Context and Problem Statement

A cube-idp cube installs a set of packs. Each pack's rendered manifests have to reach the
cluster somehow, and there are two useful ways to get them there: push the render to the
in-cluster OCI registry (zot) and register an OCI source, or push the render into a Gitea
repository and register a git source instead. The git route buys an editable, in-cluster
fork — a user can edit manifests in the Gitea UI and the engine reconciles them — at the
cost of requiring Gitea to be running in the same cube.

The open question was where that choice lives. A cube-wide "delivery mode" would force all
packs into the same transport, which is wrong: Gitea itself cannot be delivered through
Gitea, and most packs never need editability. The choice also has to be visible after the
fact — an operator inspecting a running cube needs to know which packs are repo-delivered
without re-reading cube.yaml — and it has to fail early, at config load, rather than
halfway through a cluster mutation.

A separate question about the gateway pack's data-plane Service and the CoreDNS `*.<host>`
rewrite target was once considered here; it is decided in ADR-0012, not in this record.

## Decision

Delivery is selected per pack via a pack-level field, never as a cube-wide mode, and its
value is the two-member CUE enum `oci|repo`. An empty `PackRef.Delivery` maps to `oci` and
is byte-compatible with it; `pack install --via repo` sets the key, while `--via oci`
writes none.

Every pack's Pack record carries `delivery`, and the Pack CRD exposes a DELIVERY printer
column so repo-delivered packs are visible in `kubectl get packs`.

A cube declaring any `delivery: repo` pack must also include the gitea pack, and the gitea
pack itself may not be repo-delivered. Both violations fail config load with typed error
CUBE-7304.

This decision does not govern the CoreDNS rewrite target. See ADR-0012 for the
authoritative statement of how `up` derives the `*.<host>` rewrite target from the gateway
pack's `gatewayService:` block.

## Consequences

* Good, because a single cube can mix transports — the platform packs stay on OCI while the
  one pack a team wants to fork is repo-delivered.
* Good, because absent means `oci`: existing cube.yaml files and pack records need no
  migration, and `--via oci` leaves the file byte-identical to no flag at all.
* Good, because the gitea coupling is caught at config load, before any cluster mutation,
  with a stable code (CUBE-7304) rather than a mid-`up` failure.
* Bad, because repo delivery makes the gitea pack a hard dependency of any cube that uses
  it, coupling an otherwise optional pack into the cube's validity.
* Bad, because the same rendered pack can now exist behind two different source types,
  so debugging a reconciliation problem requires first checking which transport is in play.
* Bad, because gitea presence is detected by a ref-substring convention (`strings.Contains(p.Ref, "gitea")`)
  rather than resolved pack identity, which is approximate.

## Implementation Status

**This decision is implemented.** Confirmed against the code on 2026-07-20.

| Decision | Implemented at |
| --- | --- |
| Gitea delivery is selected per pack via a pack-level field, not as a cube-wide delivery mode. | `internal/config/types.go` |
| Every pack's Pack record carries `delivery: oci\|repo` (an empty `PackRef.Delivery` maps to `oci`) and the Pack CRD exposes a DELIVERY printer column. | `internal/pack/expose.go`, `internal/pack/manifests/pack-crd.yaml` |
| A cube declaring any `delivery: repo` pack must also include the gitea pack, and the gitea pack itself may not be repo-delivered; both fail config load with CUBE-7304. | `internal/config/load.go`, `internal/diag/codes.go` |

### Verification

* [ ] `internal/config/schema.cue` constrains `delivery?: "oci" | "repo"` on `spec.packs` entries and declares no top-level `delivery` key.
* [ ] `internal/pack/expose.go` rewrites an empty `delivery` argument to `"oci"` before writing `spec["delivery"]`.
* [ ] `internal/pack/manifests/pack-crd.yaml` declares the `delivery` schema field and line 61 declares `- {name: DELIVERY, type: string, jsonPath: .spec.delivery}`.
* [ ] `internal/config/load.go` returns `diag.CodeRepoDeliveryConfig` when a pack whose ref contains `gitea` declares `delivery: repo`; lines 220-225 return the same code when any pack is `delivery: repo` and no gitea pack is present.
* [ ] `internal/diag/codes.go` binds `CodeRepoDeliveryConfig` to `CUBE-7304`.
* [ ] `cmd/pack.go` rejects a `--via` value other than `oci` or `repo`, and `cmd/pack.go` defaults the flag to `oci`.
* [ ] `internal/up/up.go` (`deliverPack`) dispatches on `ref.Delivery == "repo"` and has no third arm.
* [ ] `grep -rn '"ssa"' internal/` returns no delivery-marker hit — the enum has exactly two members.

## History

An earlier form of this decision specified a three-value delivery marker — `oci`, `repo`,
and `ssa`, the last for prerequisite packs applied directly by the CLI via server-side
apply — alongside the `pack install --via repo` flag. The `ssa` value was never added. The
marker settled as the two-value `oci|repo` enum pinned by `internal/config/schema.cue`;
everything else in that earlier statement (the marker itself, the empty-to-`oci` default,
and `--via repo` driving Gitea repo creation and push via `internal/gitea/client.go`
`EnsureRepo` and `internal/gitea/client.go` `SyncDir`) is implemented as stated.

## More Information

Origin: mined from the archived planning corpus (`docs/archive/superpowers/`) during the
2026-07-20 documentation audit; the underlying statements were validated against the code
before this record was written.

Member origins:

* `docs/archive/superpowers/specs/2026-07-18-cube-idp-phase5-roadmap-design.md:45` — delivery is per pack, not cube-wide.
* `docs/archive/superpowers/plans/2026-07-18-cube-idp-phase5.md:261` — the Pack record field and the DELIVERY printer column.
* `docs/archive/superpowers/plans/2026-07-18-cube-idp-phase5.md:3432` — the gitea coupling rules and CUBE-7304.
* `docs/archive/superpowers/specs/2026-07-19-cube-idp-prerequisites-packs-design.md:73` — the superseded three-value marker.

Delivery is consumed per-ref at `internal/up/up.go` (`deliverPack`) and
`internal/pack/depgraph.go`.
