---
status: "accepted"
date: 2026-07-20
decision-makers: "cube-idp maintainers"
---

# 20. Engine Self-Management and the Single-Owner Apply Rule

## Context and Problem Statement

cube-idp installs a GitOps engine (Flux or Argo CD) into the cluster and then uses that
engine to deliver packs. The engine itself is rendered from an engine pack, so it is a
natural candidate for being delivered the same way everything else is — reconciled
continuously by the engine rather than re-applied by the CLI on every `up`.

That creates two hazards. First, ownership: if the CLI keeps server-side applying the
engine objects while the engine is also reconciling them from an artifact, two writers
fight over the same fields and drift correction becomes non-deterministic. Second,
disruption: flipping a cluster from CLI-owned to self-owned must not, by itself, restart
the control plane the user is running on. A third constraint is offline operation — the
engine must be able to reconcile itself in an air-gapped cluster, so it cannot depend on
the Git server.

The decision below fixes when the CLI applies engine objects directly, where the engine's
own artifact lives, and how the engine appears in the Pack inventory.

## Decision

Engine self-management is opt-in via the boolean `spec.engine.selfManage`. It is never
flipped on by default and never always-on.

When enabled, cube-idp renders the engine pack itself and pushes the finished YAML as the
artifact `packs/cube-engine:latest` to the in-cluster zot registry — never to Gitea — so the
engine never sees customization as a concept. It then attaches an engine-native
self-source with pruning disabled: for Flux, an `OCIRepository` plus a `Kustomization` in
`flux-system`; for Argo CD, an `Application` over its own namespace.

When `selfManage` is on, direct server-side apply of engine objects happens only on first
install and on unhealthy-engine recovery; when it is off, every `up` SSAs the engine as
before. Once the self-source exists, later `up` runs render, push and
poke, but never SSA — preserving a single owner. Enabling `selfManage` never by itself
restarts the engine, because the SSA'd state and the first pushed artifact are
byte-identical renders of the same objects. When `selfManage` is false the engine's
Health is never consulted for an SSA decision, keeping the pre-self-management install
path unchanged.

The rendered engine pack objects are what `up` applies, what the inventory records, and
what the self artifact carries. The engine gets its own Pack record with delivery
`engine`, no `dependsOn`, `customized` derived from whether `engine.values` is non-empty,
and `READY` true by construction after the health gate — except under `selfManage`, where
it reports the self-source's component health. `diff` mirrors this by rendering the engine
pack and emitting an engine Pack-record identity stub.

## Consequences

* Good, because exactly one writer owns the engine objects at any time, so drift
  correction is deterministic instead of a race between the CLI and the engine.
* Good, because a self-managed engine corrects its own drift between `up` runs.
* Good, because sourcing the self artifact from the in-cluster registry rather than Gitea
  keeps self-management working in air-gapped clusters.
* Good, because one render feeds SSA, the inventory and the pushed artifact, so enabling
  the feature cannot produce a restart-inducing diff.
* Good, because the engine appears in the same Pack inventory as everything else, so
  `diff` and teardown reason about it uniformly.
* Bad, because a self-managed engine that breaks its own reconciliation must be recovered
  through the unhealthy-at-start escape hatch rather than by a plain re-apply.
* Bad, because pruning must stay disabled on the self-source, so objects removed from the
  engine pack are not garbage-collected by the engine.
* Bad, because one render now feeds two delivery paths — direct SSA and the pushed
  artifact — and they must not diverge.

## Implementation Status

**This decision is implemented.** Confirmed against the code on 2026-07-20.

| Decision | Implemented at |
| --- | --- |
| Engine self-management is opt-in via the boolean `engine.selfManage` — never defaulted on, never always-on. | `internal/config/types.go:111` |
| Self-management is always sourced from the in-cluster zot registry (`oci://<zot>/packs/cube-engine`), never from Gitea, with pruning disabled on the self-source. | `internal/engine/flux/deliverself.go:43`, `internal/engine/argocd/deliverself.go:46` |
| Under `selfManage`, cube-idp renders the engine manifests itself and pushes the finished YAML as the `packs/cube-engine` artifact, so the engine never sees customization as a concept. | `internal/up/up.go:1249` |
| The self-source objects are a Flux `OCIRepository` + `Kustomization` with `prune: false` in `flux-system`. | `internal/engine/flux/deliverself.go:32-64` |
| For Argo CD the self-source is a single `Application` in the Argo CD namespace (`argocd.Namespace`, `= "argocd"`) with automated sync and `prune: false`. | `internal/engine/argocd/deliverself.go:34-57`, `internal/engine/argocd/argocd.go:52` |
| The engine-native self-source is attached by the engine's own `DeliverSelf` implementation. | `internal/engine/flux/deliverself.go:32` |
| Direct SSA of engine manifests happens only on first install and on unhealthy-engine recovery; a healthy self-managed engine is never SSA'd. | `internal/up/up.go:1220` |
| When `selfManage` is false, engine Health is never consulted for an SSA decision. | `internal/up/up.go:1220` |
| One render feeds SSA, the inventory and the pushed artifact, so the SSA'd state and the first pushed artifact are byte-identical renders and enabling `selfManage` does not by itself restart the engine. | `internal/up/up.go:261,264,269,1255` |
| The engine gets its own Pack record row with delivery `engine`, `customized` from whether `engine.values` is non-empty, and no `dependsOn`. | `internal/up/up.go:542` |
| The engine Pack record's `READY` is true by construction after the health gate, except under `selfManage` where it reports the `cube-engine` self-source's component health. | `internal/up/up.go:538-541` |
| `diff`'s desired state renders the engine pack, mirroring `up.Run`, and emits an engine Pack-record identity stub. | `internal/diff/diff.go:198` |

### Verification

- [ ] `internal/config/types.go:111` declares `SelfManage bool` with `yaml:"selfManage,omitempty"`, and `grep -rn "SelfManage" internal/config/*.go` returns only that field declaration and its doc comment at `:104` — no assignment.
- [ ] `internal/up/up.go:1220` — `installNeedsSSA` returns `true` immediately when `selfManage` is false, and otherwise `!engineHealthyAtStart(...)`.
- [ ] `internal/up/up.go:261` sets `installObjs := engineRendered.Objects`, and that same slice reaches both the SSA call and `deliverEngineSelf` (`internal/up/up.go:1249`).
- [ ] `internal/engine/flux/deliverself.go:54` — the `Kustomization` spec sets `"prune": false`.
- [ ] `internal/engine/argocd/deliverself.go:52` — the `Application` sets `"automated": {"prune": false, "selfHeal": true}`.
- [ ] `internal/engine/engine.go:31` defines `SelfArtifactName = "cube-engine"`, and both `DeliverSelf` implementations build their source URL from the in-cluster registry address, not a Gitea clone URL.
- [ ] `internal/up/up.go:542` appends the engine Pack record with the literal delivery `"engine"`, `len(cube.Spec.Engine.Values) > 0` for `customized`, and `nil` for `dependsOn`.
- [ ] `internal/diff/diff.go:198` calls `pack.FetchRenderEngine(ctx, cube.Spec.Engine, cube.Spec.Gateway, cube.Spec.Engine.PackRef(), dir)` — the same arguments as `up.Run` — and `grep -c "InstallManifests" internal/diff/diff.go` returns 0.

## More Information

Origin: mined from the archived planning corpus (`docs/archive/superpowers/`) during the
2026-07-20 documentation audit; the underlying statements were validated against the code
before this record was written.

Member origins:

- `docs/archive/superpowers/specs/2026-07-19-cube-idp-engine-as-pack-design.md:72`
- `docs/archive/superpowers/specs/2026-07-19-cube-idp-engine-as-pack-design.md:122`
- `docs/archive/superpowers/specs/2026-07-18-cube-idp-phase5-roadmap-design.md:221`
- `docs/archive/superpowers/plans/2026-07-18-cube-idp-phase5.md:3600`
- `docs/archive/superpowers/plans/2026-07-19-cube-idp-engine-as-pack.md:913`
