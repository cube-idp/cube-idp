---
status: "accepted"
date: 2026-07-20
decision-makers: "cube-idp maintainers"
---

# 5. Pack Dependency Graph: Declaration, Implicit Edges, and Deterministic Delivery Ordering

## Context and Problem Statement

A cube installs a list of packs. Some packs cannot be applied until another pack is already
in the cluster: a pack rendering `HTTPRoute` objects needs the Gateway API CRDs the gateway
pack ships, and a pack delivered as a git repository needs the in-cluster Gitea serving
before its repo exists. Earlier the CLI hoisted specific packs into fixed positions in the
delivery loop, which encoded one-off ordering rules in imperative code and did not generalise
to user-declared dependencies.

Two further constraints shape the answer. Delivery engines differ: Flux orders reconciliation
in-cluster through `Kustomization spec.dependsOn`, while Argo CD has no cross-Application
ordering primitive the CLI is willing to rely on. And ordering must be reproducible — a cube
that declares no dependencies must deliver in exactly the order written, byte for byte, so
adding the feature is not a silent behaviour change for existing cubes.

## Decision

Dependencies are declared as pack **names** (the `pack.cue` `name` field), never as pack refs,
and support no semver ranges or version constraints. They may be declared on two surfaces —
`pack.cue dependsOn?: [...string]` and `cube.yaml packs[].dependsOn?: [...string]` — and the
resolved graph is the union of both plus two implicit, never-declarable edges: any pack whose
render contains a `gateway.networking.k8s.io` object depends on the gateway pack, and any
`delivery: repo` pack depends on the pack named `gitea` (CUBE-4018 when absent).

The gateway pack is the graph root: it is delivered first unconditionally, and any `dependsOn`
reaching `ResolveOrder` on `packs[0]`/`refs[0]` is rejected with CUBE-4020. In practice only the
`pack.cue` surface can carry one — `spec.gateway` has no `dependsOn` field in the schema and the
gateway ref is synthesized as a bare `{Ref: ...}` by both callers, so the `cube.yaml` half of that
guard is reachable only from a direct `ResolveOrder` call. Graph resolution lives in one pure function,
`pack.ResolveOrder`, shared by `up` and `diff`, so both walk an identical delivery order over
index-aligned pack/ref/render slices with the gateway at index 0. `up.Run` splits into
fetch+render, graph, and deliver passes, so a graph error aborts before any cluster mutation.

The resolved explicit-plus-implicit dependency list is recorded on the Pack object and carried
above the engine seam on `pack.Rendered.DependsOn`, set only by `up`/`diff` and never by
`RenderWith`. For cubes with no dependencies, delivery objects remain byte-identical because
`spec.dependsOn` is emitted only when non-empty. Both engines name every delivery
`cube-idp-<pack name>` via their own `deliveryName()`; `up` re-derives the same prefix at
`internal/up/up.go` (health lookup) and `internal/up/up.go` (wave gate), so the
convention is duplicated across those call sites rather than centralised in one function.

Delivery order is a Kahn topological sort with a deterministic tie-break on declared `cube.yaml`
order, so a cube with no declared dependencies delivers byte-for-byte in the order written, and
ordering semantics stay owned by the CLI rather than any external controller. For engines that
do not order deliveries natively, the topological order is grouped into waves gated inside `up`
by a bounded health check on the previous wave's delivery names — not by Argo CD sync-wave
annotations or an app-of-apps restructuring. Dependency cycles are detected over the full
explicit-plus-implicit graph at `up` and `diff` time before any pack is delivered and fail with
the cycle path printed; a pack declaring itself is a 1-cycle and fails the same way.

This ADR is the authoritative statement of the delivery-ordering algorithm (Kahn sort, declared
order tie-break, gateway-first root) and of both implicit edges. ADR-0021 owns only the
engine-seam translation of an already-resolved order (`DeliverGit`'s `DependsOn` parameter,
`OrdersDeliveries()`, flux `spec.dependsOn` vs the argocd annotation plus caller wave gating);
ADR-0033 owns only `cube.lock`'s shape and non-mutating preview semantics; ADR-0037 owns only the
Gateway API routing surface; ADR-0039 owns only gateway token substitution and the
`httproutes` CRD-established wait. Where any of those records restates an ordering or
dependency-edge rule, this ADR is the one to change.

## Consequences

* Good, because ordering is derived from a single declarative graph rather than hoisted slots,
 so new ordering rules are edges, not new branches in the delivery loop.
* Good, because `up` and `diff` share one pure resolver, making `diff` a faithful preview of
 what `up` will apply.
* Good, because graph errors (unknown name CUBE-4018, cycle CUBE-4019, gateway dependsOn
 CUBE-4020) surface before any cluster mutation.
* Good, because dependency-free cubes are unchanged byte-for-byte — the feature is additive.
* Bad, because engines without native ordering pay a wall-clock wave gate: each dependent pack
 waits on a bounded health poll, so delivery is slower than fire-and-forget.
* Bad, because names, not refs, are the dependency identity, so two packs in one cube may not
 share a `pack.cue` name — a constraint packs cannot see from their own source.
* Bad, because the absence of version constraints means a dependency is satisfied by any version
 of the named pack; compatibility is the cube author's problem.
* Bad, because the two implicit edges are invisible in `cube.yaml` — a reader must know the rules
 or read the resolved `DEPENDS-ON` column to see the real graph.

## Implementation Status

**This decision is implemented.** Confirmed against the code on 2026-07-20.

| Decision | Implemented at |
| --- | --- |
| Dependencies are declared as pack names (the `pack.cue` name), never as pack refs, because names are the stable identity across oci/local/git sources. | `internal/pack/depgraph.go`, `internal/config/types.go` |
| Dependencies may be declared on two surfaces — `pack.cue dependsOn` and `cube.yaml packs[].dependsOn` — and the resolved graph is the union of both. | `internal/pack/depgraph.go` |
| `dependsOn` accepts pack names only and supports no semver ranges or version constraints. | `internal/pack/depgraph.go`, `internal/config/schema.cue` |
| The `cube.yaml` schema declares `dependsOn?: [...string & != ""]` and marshals it with omitempty discipline, so an absent value is an absent key, never an explicit null. | `internal/config/schema.cue`, `internal/config/types.go` |
| Two dependency edges are derived implicitly and can never be declared: a render containing a `gateway.networking.k8s.io` object depends on the gateway pack, and a `delivery: repo` pack depends on gitea. | `internal/pack/depgraph.go` |
| Any pack whose rendered output contains a `gateway.networking.k8s.io` object implicitly depends on the gateway pack — a render-derived edge, not a blanket one — eliminating the CRD-ordering race. | `internal/pack/depgraph.go` |
| Any `delivery: repo` pack implicitly depends on the pack named gitea, and a repo-delivery pack in a cube with no gitea pack fails typed as CUBE-4018 rather than panicking. | `internal/pack/depgraph.go` |
| Gitea remains an optional pack rather than a core component: repo-delivered packs fail load when gitea is absent, and the gitea-before-repo-packs guarantee is preserved by the graph rather than by hoisting gitea to position 1. | `internal/pack/depgraph.go`, `internal/config/load.go` |
| The gateway pack is delivered first unconditionally, and any `dependsOn` reaching `ResolveOrder` on `packs[0]`/`refs[0]` is rejected with CUBE-4020. In practice only the `pack.cue` surface can carry one: `spec.gateway` has no `dependsOn` field in the schema, and the gateway ref is synthesized as a bare `{Ref: ...}` by both callers, so `refs[0].DependsOn` is empty outside a direct `ResolveOrder` test. | `internal/pack/depgraph.go`, `internal/config/schema.cue`, `internal/up/up.go`, `internal/diff/diff.go` |
| The gateway pack is the graph root: `spec.gateway` has no `dependsOn` field, while other packs naming the gateway explicitly is permitted and merely redundant with the implicit edge. | `internal/pack/depgraph.go`, `internal/config/schema.cue` |
| Graph resolution lives in one pure function `pack.ResolveOrder`, taking index-aligned packs/refs/rendered with the gateway always at index 0. | `internal/pack/depgraph.go` |
| `pack.ResolveOrder` returns the delivery order as indices into those slices, plus a per-pack map of resolved dependency names sorted alphabetically, omitting the key entirely for packs with no dependencies. | `internal/pack/depgraph.go` |
| `up.Run` and `diff.desiredState` resolve and walk one and the same dependency order via the shared `pack.ResolveOrder`. | `internal/diff/diff.go`, `internal/up/up.go` |
| `up.Run` splits its per-pack loop into a fetch+render pass, a graph pass, and a deliver pass, so graph errors abort before any delivery mutation occurs. | `internal/up/up.go` |
| `orderPackRefs` only prepends the gateway pack ref; all other ordering, including the gitea-before-repo-packs guarantee, lives in `pack.ResolveOrder`. | `internal/up/up.go` |
| Resolved dependency names are carried above the engine seam on `pack.Rendered.DependsOn`, set by `up`/`diff` after the graph pass and never by `RenderWith`. | `internal/pack/pack.go` |
| `diff` sets the same resolved `DependsOn` before its Deliver calls, so it previews byte-identical objects to what `up` applies. | `internal/diff/diff.go` |
| The `dependsOn` recorded on a Pack object is the RESOLVED list — explicit union implicit — reflecting what actually gated delivery. | `internal/up/up.go`, `internal/pack/expose.go` |
| `kubectl get packs` exposes a DELIVERY printer column followed by a `DEPENDS-ON` column listing the resolved dependencies, backed by an append-only widening of the Pack record with `spec.dependsOn`. | `internal/pack/manifests/pack-crd.yaml` |
| Both engines name every delivery `cube-idp-<pack name>` in their own `deliveryName()`; `up` re-derives the same prefix for the health lookup and the wave gate, so the convention is duplicated rather than centralised. | `internal/engine/flux/deliver.go`, `internal/engine/argocd/deliver.go`, `internal/up/up.go` |
| Dependency ordering is a Kahn topological sort with a deterministic tie-break on declared order, so a dependency-free cube delivers byte-for-byte in declared order. | `internal/pack/depgraph.go` |
| For cubes with no dependencies, flux delivery objects are byte-identical to before, because `spec.dependsOn` is emitted only when non-empty. | `internal/engine/flux/deliver.go` |
| For engines that cannot order deliveries natively, `up` polls each dependency's component health until Ready before applying the dependent pack, bounded by a timeout; `waitDepsHealthy` returns immediately for a pack with no dependencies. | `internal/up/up.go` |
| Argo CD ordering is implemented by wave-gating in `up` plus documentation, not by sync-wave annotations or an app-of-apps restructuring. | `internal/up/up.go` |
| An engine's `Deliver` output must reference the pushed artifact as `oci://<registry.InClusterURL>/<repo>` including the artifact tag; the engine contract suite asserts only that the artifact is referenced, not how it is delivered. | `internal/engine/contract/contract.go` |
| The argocd engine pack sets `reposerver.oci.layer.media.types` to the flux-style artifact media types cube-idp pushes to zot, which is load-bearing for OCI pack delivery. | `tests/packs_render_test.go` |

### Verification

- [ ] `internal/pack/depgraph.go` exposes `ResolveOrder(packs []*Pack, refs []config.PackRef, rendered []*Rendered) ([]int, map[string][]string, error)`.
- [ ] `internal/pack/depgraph.go` rejects a `dependsOn` on `packs[0]` or `refs[0]` with `diag.CodePackDepGateway` (CUBE-4020, `internal/diag/codes.go`); `TestResolveOrderGatewayPackCueDependsOnIsCUBE4020` and `TestResolveOrderGatewayRefDependsOnIsCUBE4020` cover both surfaces.
- [ ] `internal/pack/depgraph.go` unions `packs[i].DependsOn` with `refs[i].DependsOn`; every entry resolves through `idxByName` built from `p.Name`, so a ref string can never match.
- [ ] `internal/pack/depgraph.go` sets `edges[i][0]` when any rendered object's group is `gateway.networking.k8s.io`; `TestResolveOrderImplicitGatewayEdge` fences it.
- [ ] `internal/pack/depgraph.go` adds the repo→gitea edge and returns CUBE-4018 when no pack is named gitea; `TestResolveOrderRepoDeliveryNoGiteaIsCUBE4018` covers the typed failure.
- [ ] `internal/pack/depgraph.go` is a Kahn sort whose inner scan breaks at the first ready index; `TestResolveOrderNoDepsDeclaredOrder` pins declared-order output. Cycles produce `diag.CodePackDepCycle` (CUBE-4019) with the path printed; a self-dependency is a 1-cycle.
- [ ] `internal/up/up.go` and `internal/diff/diff.go` are the only callers of `pack.ResolveOrder`.
- [ ] `internal/up/up.go` calls `resolveAndDeliverPacks` after the fetch+render loop ends at line 402, and the resolver's error returns before the delivery loop at `internal/up/up.go`.
- [ ] `internal/up/up.go` returns nil immediately when `len(deps) == 0` and otherwise polls `eng.Health` until every `cube-idp-<dep>` is Ready, failing CUBE-3011 past the deadline; it is invoked only when `!deps.eng.OrdersDeliveries()` (`internal/up/up.go`).
- [ ] `grep -r 'sync-wave\|app-of-apps' internal/ cmd/` returns nothing.
- [ ] `grep -rn '"cube-idp-" *+' internal/ | grep -v _test` returns exactly the two engine `deliveryName()`s (`internal/engine/flux/deliver.go`, `internal/engine/argocd/deliver.go`), the two `up` re-derivations (`internal/up/up.go`, plus the explanatory comment at `internal/up/up.go`), and the unrelated spoke service-account name at `internal/spoke/bootstrap.go` — a new hit outside that list means a third pack-delivery naming site was added.
- [ ] `internal/engine/flux/deliver.go` sets `spec.dependsOn` only `if len(r.DependsOn) > 0`.
- [ ] `internal/pack/manifests/pack-crd.yaml` declares the DELIVERY and DEPENDS-ON printer columns in that order.

## History

Delivery order used to be partly hoisted in the CLI's pack loop: whenever any repo-delivered
pack existed, gitea was placed in a fixed slot immediately after the gateway pack, and repo
delivery began with a bounded readiness poll of the Gitea API. That fixed slot has been replaced
by the dependency graph's implicit repo→gitea edge in `pack.ResolveOrder`, so gitea's position
is now derived rather than hoisted, and `orderPackRefs` (`internal/up/up.go`) only prepends
the gateway ref. The bounded Gitea readiness poll itself survives, in `giteaSession`
(`internal/up/up.go`).

## More Information

Origin: mined from the archived planning corpus (`docs/archive/superpowers/`) during the
2026-07-20 documentation audit; the underlying statements were validated against the code before
this record was written. Member origins:

- `docs/archive/superpowers/specs/2026-07-19-cube-idp-pack-depends-and-cubelock-crd-design.md:90` — dependencies are pack names, not refs.
- `docs/archive/superpowers/specs/2026-07-19-cube-idp-pack-depends-and-cubelock-crd-design.md:92` — the two implicit, never-declarable edges.
- `docs/archive/superpowers/specs/2026-07-19-cube-idp-pack-depends-and-cubelock-crd-design.md:140` — `pack.ResolveOrder` as the single shared resolver.
- `docs/archive/superpowers/specs/2026-07-19-cube-idp-pack-depends-and-cubelock-crd-design.md:461` — Argo CD ordering by wave-gating in `up`.
- `docs/archive/superpowers/plans/2026-07-19-cube-idp-pack-depends-and-cubelock-crd.md:185` — Kahn sort with declared-order tie-break.
