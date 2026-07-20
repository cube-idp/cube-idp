---
status: "accepted"
date: 2026-07-20
decision-makers: "cube-idp maintainers"
---

# 21. Translating Dependency Order into Engine-Native Ordering Intent

## Context and Problem Statement

Packs declare dependencies on other packs in the same cube. The CLI resolves those
declarations into a graph and produces a deterministic order, but ordering the *resolution*
is only half the problem: the delivery objects handed to a GitOps engine also have to
reconcile in that order inside the cluster.

The two supported engines disagree about who owns that sequencing. Flux can express
ordering natively — a Kustomization has `spec.dependsOn` and the controller will not
reconcile it until its prerequisites are ready. Argo CD has no equivalent cross-Application
ordering primitive, so something outside the engine has to gate delivery until each
dependency is healthy.

The naive fix is for `up` to branch on engine type. That leaks engine knowledge into the
orchestrator, and every future engine silently inherits whichever branch it falls into
rather than being forced to state what it actually supports.

## Decision

Pack dependencies are resolved into a graph by the CLI and then translated into
engine-native ordering intent *below* the engine seam. `Engine.DeliverGit` takes the
resolved dependency list as a parameter, and each engine self-describes its ordering
capability through `OrdersDeliveries() bool`, so `up` never type-switches on engine type.

Flux returns `true` and adds name-only `spec.dependsOn` entries (`cube-idp-<name>`, always
in the `flux-system` namespace) to its Kustomization, but only when dependencies exist. It
therefore skips the wave machinery entirely, preserving delivery parallelism.

Argo CD returns `false`, records dependencies as the Application annotation
`cube-idp.dev/depends-on` (comma-separated) for humans and tooling, and is wave-gated by
the caller. Its `syncPolicy` is left untouched.

The engine contract test asserts that every implementation translates a non-empty
`DependsOn` into *some* engine-native ordering intent, forcing future engines to answer the
question consciously rather than inherit a default.

There is no in-cluster `Cube` or `CubeLock` record. The gateway dependency edge is
render-derived, and `cube.lock` remains a local `apiVersion`/`kind` file.

## Consequences

* Good, because the orchestrator has no engine-specific branches in the delivery path — a
  new engine is added by implementing the interface, not by editing `up`.
* Good, because flux keeps full delivery parallelism: ordering is enforced by the
  controller in-cluster rather than by serialising the CLI's apply loop.
* Good, because the contract test makes "did you think about ordering?" a compile-and-test
  gate for every engine implementation.
* Good, because argo CD users still get a machine-readable record of the dependency edges
  even though the engine cannot act on them.
* Bad, because argo CD delivery is slower: the caller must block on dependency health
  before applying each dependent pack.
* Bad, because the two engines express the same intent in structurally different shapes,
  so the contract test can only assert that *something* was emitted, not that it is
  semantically equivalent.
* Bad, because widening `DeliverGit` with a `dependsOn` parameter was a breaking interface
  change that every implementation and test fake had to absorb.
* Bad, because the flux dependency reference is name-only and therefore silently assumes
  every cube-idp Kustomization stays in `flux-system`.

## Implementation Status

**This decision is implemented.** Confirmed against the code on 2026-07-20.

| Decision | Implemented at |
| --- | --- |
| `Engine` exposes `OrdersDeliveries() bool`, declaring whether the engine orders deliveries natively; flux returns true and therefore never enters the wave gate. | `internal/engine/engine.go:99` |
| Engines translate pack dependencies below the engine seam and self-describe ordering capability, so `up` never type-switches on engine type and wave-gates delivery itself whenever the method returns false. | `internal/engine/engine.go:99` |
| `Engine.DeliverGit` widens to `DeliverGit(ctx, name string, src GitSource, dependsOn []string)`, forcing every implementation and fake to accept resolved dependencies. | `internal/engine/engine.go:70` |
| The flux engine adds name-only `spec.dependsOn` entries (`cube-idp-<name>`, same `flux-system` namespace) to the Kustomization only when dependencies exist, and skips the wave machinery entirely. | `internal/engine/flux/delivergit.go:53` |
| Flux dependency references are name-only because every cube-idp Kustomization lives in the `flux-system` namespace. | `internal/engine/flux/deliver.go:64` |
| The argocd engine reports `OrdersDeliveries() == false` and records dependencies as the Application annotation `cube-idp.dev/depends-on` (comma-separated), leaving `syncPolicy` untouched. | `internal/engine/argocd/deliver.go:50` |
| The engine contract test asserts that every implementation translates a non-empty `DependsOn` into engine-native ordering intent. | `internal/engine/contract/contract.go:139` |
| There is no in-cluster `Cube` record; the gateway dependency edge is render-derived and argocd ordering is `up`'s own documented wave gate. | `internal/pack/depgraph.go:67` |
| Pack dependencies are declared and resolved into a graph with cycle detection and translated per delivery engine. *(Superseded in part — see History.)* | `internal/pack/depgraph.go:29` |

### Verification

- [ ] `internal/engine/engine.go:99` declares `OrdersDeliveries() bool` on the `Engine`
      interface; `internal/engine/flux/flux.go:37` returns `true` and
      `internal/engine/argocd/argocd.go:68` returns `false`.
- [ ] `internal/engine/engine.go:70` declares
      `DeliverGit(ctx, name string, src GitSource, dependsOn []string)`, and both
      `internal/engine/flux/delivergit.go:17` and `internal/engine/argocd/delivergit.go:18`
      match that signature.
- [ ] `grep -rn 'OrdersDeliveries' internal/up/up.go` shows the wave gate guarded by
      `if !deps.eng.OrdersDeliveries()` (around `internal/up/up.go:632`) and no type switch
      on engine type anywhere in the delivery path.
- [ ] `internal/engine/flux/deliver.go:64` (`dependsOnRefs`) emits only
      `{"name": cube-idp-<dep>}` — no `namespace` field.
- [ ] `internal/engine/flux/delivergit.go:53` sets `spec.dependsOn` only when
      `len(dependsOn) > 0`.
- [ ] `internal/engine/argocd/deliver.go:50` sets
      `metadata.annotations["cube-idp.dev/depends-on"]` only when the list is non-empty,
      and the `syncPolicy` block below it is unconditional.
- [ ] `go test ./internal/engine/contract/...` runs `deliver_translates_depends_on`
      (`internal/engine/contract/contract.go:139`) for every registered implementation and
      fails an engine that drops a non-empty `DependsOn`.
- [ ] `internal/pack/depgraph.go:29` (`ResolveOrder`) detects cycles via `cycleError`
      (`internal/pack/depgraph.go:141`, diagnostic `CUBE-4019` at
      `internal/diag/codes.go:113`).
- [ ] `internal/pack/depgraph.go:67` adds the implicit gateway edge by scanning rendered
      objects for group `gateway.networking.k8s.io`.
- [ ] `internal/lock/lock.go:17-22` shows `File` with only `APIVersion`/`Kind`/`Engine`/
      `Packs` — no `metadata`/`spec`, and no CubeLock CRD manifest or apply path exists
      under `internal/`.

## History

The original dependency decision paired graph resolution with `cube.lock` as a KRM object
backed by an in-cluster inert `CubeLock` CRD record. Dependency resolution shipped as
described, but the in-cluster CRD record was dropped: `cube.lock` remains a purely local
`apiVersion`/`kind` file (`internal/lock/lock.go:17-22`), there is no `CubeLock` CRD
manifest, and there is no in-cluster `Cube` record. The gateway dependency edge is instead
derived at render time.

## More Information

Origin: mined from the archived planning corpus (`docs/archive/superpowers/`) during the
2026-07-20 documentation audit; the underlying statements were validated against the code
before this record was written.

Member origins:

- `docs/archive/superpowers/plans/2026-07-19-cube-idp-pack-depends-and-cubelock-crd.md:599` —
  `OrdersDeliveries()` and the flux wave-gate skip.
- `docs/archive/superpowers/plans/2026-07-19-cube-idp-pack-depends-and-cubelock-crd.md:31` — no
  in-cluster Cube record; render-derived gateway edge.
- `docs/archive/superpowers/plans/2026-07-19-cube-idp-pack-depends-and-cubelock-crd.md:559` —
  name-only flux dependency references.
- `docs/archive/superpowers/specs/2026-07-19-cube-idp-pack-depends-and-cubelock-crd-design.md:226`
  — the `DeliverGit` signature widening.
- `docs/archive/superpowers/specs/2026-07-19-cube-idp-pack-depends-and-cubelock-crd-design.md:262`
  — the argocd `cube-idp.dev/depends-on` annotation.

Related: ADR 0005 (pack dependency graph and ordering), ADR 0006 (per-pack delivery mode).
