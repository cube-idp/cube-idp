<!-- cube:doc area=packs code=internal/pack,internal/config,internal/oci,internal/up adrs=0002,0003,0004,0005,0006,0008,0009,0045 -->
# Architecture — packs

Governing decisions: ADR-0002 (format), ADR-0003 (refs/pinning),
ADR-0004 (values/extra manifests), ADR-0005 (deps/ordering),
ADR-0006 (per-pack delivery mode), ADR-0008 (distribution),
ADR-0009 (air-gap/integrity), ADR-0045 (prerequisites — pre-engine packs).
User contract: ../reference/pack-contract-v1.md

<!-- cube:section area=packs topic=format code=internal/pack -->
## Format
_To be filled by the first behavior-changing PR in this area (CLAUDE.md §3)._

<!-- cube:section area=packs topic=refs code=internal/pack,internal/refval -->
## Refs and pinning
_To be filled by the first behavior-changing PR in this area (CLAUDE.md §3)._

<!-- cube:section area=packs topic=values code=internal/pack -->
## Values and extra manifests
_To be filled by the first behavior-changing PR in this area (CLAUDE.md §3)._

<!-- cube:section area=packs topic=dependencies code=internal/pack,internal/lock,internal/up adrs=0005,0045 -->
## Dependencies and ordering

### Prerequisites (pre-engine packs) — ADR-0045

`spec.prerequisites` is a list of ordinary packs the CLI applies by SSA
**before** the engine, in **declared list order** — the bootstrap ground the
GitOps engine stands on (cluster-scoped CRDs, a policy controller, etc.).
Mechanically (`internal/up/up.go` `deliverPrerequisite`, run right after the
Pack CRD step and before the engine install):

- Each prerequisite runs the same `Fetch → RenderResolved → SSA(wait) →
  RecordInventory` pipeline the engine install uses — minus self-management
  (prerequisites are always CLI-owned, never engine-reconciled). `wait=true`
  (kstatus) blocks until each settles, so an earlier prerequisite's
  CRDs/namespaces are Established for a later one — and for the engine.
- They take **no** `delivery`/`dependsOn`: never engine-delivered, no place in
  the dependency graph. List order is the only ordering contract (no graph
  among prerequisites — ADR-0045 decision).
- They appear in `cube.lock` (first, in list order) and in `kubectl get packs`
  (`DELIVERY: prerequisite`, `READY: yes` by construction) like any pack, and
  `down` removes them via the inventory cascade with no `down`-side change.
- A ref present in both `spec.prerequisites` and `spec.packs` is rejected as
  **CUBE-0016** (one owner per pack). Only `{ref, valuesRef?, values?,
  extraManifests?}` are permitted on a prerequisite entry (`schema.cue`).

### Capability inference — the implicit gateway edge is conditional

ADR-0005 defines an implicit, never-declarable edge: a pack whose render
contains a `gateway.networking.k8s.io` object depends on the gateway pack (for
its Gateway API CRDs), eliminating the CRD-ordering race. **ADR-0045 makes that
edge conditional:** when a prerequisite provides the Gateway API group (it ships
those CRDs — `pack.ProvidedGroups` reads `spec.group` from a prerequisite's
rendered `CustomResourceDefinition`s), the edge is **suppressed** — the CRDs are
already Established by the pre-engine prerequisite, so an HTTPRoute-bearing pack
needs no ordering behind the gateway pack (`ResolveOrder`'s `providedGroups`
parameter). With no CRD-bearing prerequisite the graph is unchanged.

Consequently `up.Run` also **skips** its late `waitCRDEstablished` gate before
the registry HTTPRoute when a prerequisite provides the group (#25): the
Gateway API CRD check is validated up front (at pre-engine delivery) instead of
failing late during deployment.

<!-- cube:section area=packs topic=distribution code=internal/oci,internal/bundle -->
## Distribution
_To be filled by the first behavior-changing PR in this area (CLAUDE.md §3)._
