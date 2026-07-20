---
status: "accepted"
date: 2026-07-20
decision-makers: "cube-idp maintainers"
---

# 18. The GitOps Engine Interface Seam

## Context and Problem Statement

cube-idp delivers workloads to a cluster through an in-cluster GitOps controller. Two such
controllers are supported — Flux and Argo CD — and they model delivery in incompatible ways:
Flux uses `OCIRepository`/`GitRepository` plus `Kustomization`, Argo CD uses a single
`Application`. Without a hard boundary, those engine-native shapes leak upward into pack
authoring, config schema and orchestration code, and switching engines becomes a rewrite
rather than a setting.

A second pressure is scope. A GitOps engine already reconciles continuously in-cluster. If the
CLI also installed the engine, tuned it via config, embedded its manifests in the binary, and
ran controllers of its own, it would duplicate the engine's job and own two competing sources
of truth about cluster state.

This ADR fixes the shape of the boundary: what the engine interface may contain, who applies
objects, who installs the engine, and what may cross the seam.

## Decision

The GitOps engine is swappable behind a single Go interface whose surface is exactly
`Deliver`, `DeliverGit`, `DeliverSelf`, `Poke`, `Health`, `Uninstall` and `OrdersDeliveries`.

Engines are pure translators. `Deliver`, `DeliverGit` and `DeliverSelf` return engine-native
objects for the caller to apply rather than touching the cluster themselves; engines carry no
embedded manifests, no tuning fields and no install responsibility. Continuous reconciliation
is delegated entirely to the in-cluster engine — the CLI ships no controllers of its own, and
its responsibility ends once desired state is applied and healthy.

Both implementations (`flux`, `argocd`) are compiled into the binary and constructed via
`internal/engine/factory`, which takes the full `config.EngineSpec` and lives in its own
package to avoid an import cycle. There is no plugin mechanism for engines, and no engine type
leaks above the seam into user-authored YAML: packs describe neutral intent and each engine
compiles it into engine-native resources.

A shared contract test pins engine behaviour across implementations: `Health` must not error
before install on a fresh cluster, and `Uninstall` must not error on an empty one.

## Consequences

* Good, because adding or swapping an engine is confined to one package plus a factory case —
  no change to pack format, config schema or orchestration.
* Good, because returning objects instead of applying them keeps `Deliver` pure and testable
  and leaves exactly one apply path (the shared applier) for inventory tracking.
* Good, because delegating reconciliation to the engine removes any need for a controller
  manager in the CLI, and keeps a single record of truth in-cluster.
* Good, because the shared contract test makes engine parity falsifiable rather than assumed.
* Bad, because engines that do not natively order deliveries force the orchestrator to gate
  waves itself, which is why `OrdersDeliveries` exists and every implementation must answer it
  consciously.
* Bad, because no plugin mechanism means a third-party engine requires a fork or an upstream
  contribution.
* Bad, because dropping engine tuning removed a configuration surface users had; migration is
  handled by an explicit config guard rather than silent acceptance.

## Implementation Status

**This decision is implemented.** Confirmed against the code on 2026-07-20.

| Decision | Implemented at |
| --- | --- |
| The `engine.Engine` interface is exactly Deliver, DeliverGit, DeliverSelf, Poke, Health, Uninstall, OrdersDeliveries — engines are pure translators with no install responsibility, and Deliver returns engine-native objects for the caller to apply. | `internal/engine/engine.go:59-99` |
| The engine contract asserts that Health must not error before the engine is installed and that Uninstall must not error on an empty cluster. | `internal/engine/contract/contract.go:196` |
| Both engine implementations (flux, argocd) are compiled into the binary as Go code; there is no plugin mechanism, and packs remain data-only. | `internal/engine/factory/factory.go:22` |
| The engine factory takes the full `config.EngineSpec` and lives in its own `internal/engine/factory` package to avoid an import cycle. | `internal/engine/factory/factory.go:22` |
| The engine self-artifact is named `cube-engine` with tag `latest`. | `internal/engine/engine.go:31` |
| The GitOps engine is swappable behind a Go interface with Flux (default) and Argo CD implementations compiled in. | `internal/engine/engine.go:59` |
| No engine types leak above the seam: packs describe neutral intent and each engine compiles it into engine-native resources, with no engine-coupled URI scheme in user-authored YAML. | `internal/engine/engine.go:1` |
| Continuous reconciliation is delegated entirely to the in-cluster engine; the CLI ships no controllers, and the cluster's record of truth is the engine's own resources plus an SSA inventory plus the inert `Pack` CRD. | `internal/engine/engine.go:52` |
| Engine implementations carry no `embed.FS`, no tuning fields, no `NewTuned`, and no argocd namespace-defaulting transform; the tuning types, schema block, embedded manifest trees and install-manifest contract check are deleted outright. | `internal/engine/engine.go:57` |

### Verification

- [ ] `internal/engine/engine.go:59-99` declares `type Engine interface` with exactly the seven
      methods and no `Install` or `InstallManifests`.
- [ ] `Deliver` in `internal/engine/engine.go` returns `[]*unstructured.Unstructured` rather
      than applying; the apply happens in `internal/up/up.go` via the shared applier.
- [ ] `internal/engine/contract/contract.go:196` contains the `health_tolerates_fresh_cluster`
      subtest asserting both "Health must not error before the engine is installed" and
      "Uninstall must not error on an empty cluster".
- [ ] `internal/engine/factory/factory.go:22` is `func New(spec config.EngineSpec) (engine.Engine, error)`
      switching on `spec.Type` over `flux` and `argocd`, erroring `CUBE-3001` otherwise.
- [ ] `internal/engine/engine.go:31` declares `const SelfArtifactName = "cube-engine"`, and
      `internal/up/up.go:1212` declares `const engineSelfTag = "latest"`.
- [ ] Engine-native types (`OCIRepository`/`Kustomization`, `Application`) appear only in
      `internal/engine/flux/` and `internal/engine/argocd/`, never in `internal/engine/engine.go`.
- [ ] No controller manager exists: grepping `internal/` and `cmd/` for `NewManager` returns
      nothing.
- [ ] `internal/engine/tune.go` does not exist, and `internal/engine/flux/flux.go` and
      `internal/engine/argocd/argocd.go` declare no `embed.FS`.
- [ ] `internal/config/load.go:85-100` emits `CUBE-0012` when a config still sets
      `engine.tuning`.

## More Information

Origin: mined from the archived planning corpus (`docs/archive/superpowers/`) during the
2026-07-20 documentation audit; the underlying statements were validated against the code
before this record was written.

Member provenance:

- `docs/archive/superpowers/specs/2026-07-13-cube-idp-architecture-design.md:148` — engine types never
  leak above the seam.
- `docs/archive/superpowers/specs/2026-07-13-cube-idp-architecture-design.md:58` — reconciliation
  delegated to the in-cluster engine.
- `docs/archive/superpowers/plans/2026-07-13-cube-idp-phase2-draft.md:27` — both engines compiled in,
  no plugins.
- `docs/archive/superpowers/plans/2026-07-18-cube-idp-phase5.md:1784` — factory takes the full
  `EngineSpec` and lives in its own package.
- `docs/archive/superpowers/specs/2026-07-19-cube-idp-engine-as-pack-design.md:185` — the final
  interface surface after install left the seam.

Related: ADR 0007 (engine as a pack), which moved the engine's own install out of this
interface, and ADR 0002 (pack format data-only contract).
