# cube-idp pack dependencies (`dependsOn`) + CubeLock as a first-class object

Date: 2026-07-19
Status: PROPOSED (owner review pending)
Prior art: `2026-07-13-cube-idp-architecture-design.md` (D2 engine seam, D11
records), `2026-07-18-cube-idp-phase5-roadmap-design.md` (decision 13 gitea
guarantee, W0.T1 contract freeze), `docs/pack-contract-v1.md` (§6 additive
compatibility), Phase 5 ledger A10 FINDINGS (the CRD-ordering race and its
`dependsOn` recommendation), A11 HANDOFF (floci-ui → floci ordering via the
conformance `EXTRA_PACK` workaround).

This spec addresses two owner backlog items:

1. **DependsOn for Packs** — declared inter-pack dependencies, with cycle
   detection ("figure out if there are circular dependencies").
2. **CubeLock should be a proper CRD object, same as Cube** — today
   `cube.lock` is a YAML file whose shape is not even a well-formed KRM
   object, and nothing about it is visible in-cluster.

They ship together because they meet in the same code: `up.Run`'s pack loop
produces both the delivery order (feature 1) and the lock entries
(feature 2), and both grow the same append-only D11 record surface.

---

## 1. Motivation — evidence from the tree

### 1.1 Ordering today is two hard-coded special cases

`internal/up/up.go` `orderPackRefs`: the gateway pack is prepended
("everything else depends on ingress existing"), and when any pack has
`delivery: repo`, gitea is hoisted directly behind the gateway
(decision 13). That is the entire ordering model. The engines then
reconcile **all** pack deliveries concurrently — flux applies every
`Kustomization` at once; ordering of *apply* in `up` does not order
*reconciliation*.

### 1.2 The race is real and on record

Phase 5 ledger, A10 (floci) FINDINGS(8): the floci Kustomization's first
dry-run fired ~1s before the traefik pack created the HTTPRoute CRD →
"no matches for kind HTTPRoute" → flux scheduled the retry 10m out →
the 5m health window elapsed → conformance timed out (CUBE-3004). The
A10 HANDOFF names the fix: "a flux dependsOn". Every HTTPRoute-bearing
pack (A7, A8, A9, A10, A11 — and any future exposed pack) rides this
race today and wins it only by sub-second luck.

### 1.3 Real inter-pack dependencies already exist

- floci-ui → floci (A11: `FLOCI_ENDPOINT` points at floci's Service; the
  conformance harness needed a bespoke `CUBE_IDP_CONFORMANCE_EXTRA_PACK_DIR`
  mechanism to deliver floci first — a workaround for the missing feature).
- kyverno-policies → kyverno (policies are CRs of kyverno's CRDs; same
  CRD-ordering race class as A10, different CRD).
- `delivery: repo` packs → gitea (today: the load-time guard + hoist +
  readiness gate of decision 13 — an implicit dependency in all but name).
- grafana → prometheus-stack (datasource wiring), argo-rollouts'
  traffic-shifting follow-up → gateway pack.

### 1.4 CubeLock is not a proper object

`internal/lock/lock.go` `File`:

```yaml
# cube.lock today
apiVersion: cube-idp.dev/v1alpha1
kind: CubeLock
engine: {type: flux}     # top-level — no spec
packs: [...]             # top-level — no spec
```

No `metadata`, no `spec`, `Read` never checks `kind`/`apiVersion`, and
there is no in-cluster trace of it. Compare: `Cube` (cube.yaml) is a full
apiVersion/kind/metadata/spec document validated by `schema.cue`, and
`Pack` is an actual inert CRD (`packs.cube-idp.dev`,
`internal/pack/manifests/pack-crd.yaml`) with printer columns and
`kubectl get packs` discoverability (D11). CubeLock is the odd one out —
and because the lock only exists as a file on the machine that ran `up`,
a converged cluster cannot answer "what exactly was delivered here" from
the cluster itself.

---

## 2. Design decisions (DD1–DD10)

| # | Decision | Choice |
| --- | --- | --- |
| DD1 | Dependency vocabulary | Dependencies are **pack names** (pack.cue `name`), never refs — names are the stable identity; refs vary by source (oci/local/git) |
| DD2 | Declaration surfaces | **Both** pack.cue `dependsOn?: [...string]` (author-known, e.g. floci-ui→floci) and cube.yaml `packs[].dependsOn?: [...string]` (composition-known); the graph is the **union** |
| DD3 | Implicit edges | Two derived edges, never declared: (a) any pack whose **render contains a `gateway.networking.k8s.io` object** → gateway pack (kills the A10 race class at the root); (b) any `delivery: repo` pack → gitea (formalizes decision 13's hoist) |
| DD4 | Cycle handling | Kahn topological sort at `up`/`diff` time over the full graph (explicit ∪ implicit); a cycle is a **typed load-order error** (CUBE-4019) printing the cycle path — never a silent break |
| DD5 | Engine translation | Behind the D2 seam, engines self-describe: flux translates deps to native `Kustomization.spec.dependsOn` (steady-state correct); argocd has **no cross-Application ordering** (upstream argo-cd#7437), so `up` wave-gates delivery for engines that report no native ordering, and the deps are recorded as an Application annotation |
| DD6 | Ordering vs. today | Topo order with a **stable tie-break = declared cube.yaml order**. Cubes without deps or repo delivery get today's order byte-for-byte (gateway first, declared order after). Repo-delivery cubes keep the decision-13 **guarantee** (gitea strictly before every repo-delivered pack) but not the physical hoist-to-position-1 — an optimization the `giteaSession` bounded readiness gate already backstops. `orderPackRefs`'s special cases are subsumed |
| DD7 | CubeLock file shape | KRM-ified in place: `metadata.name` = cube name, body under `spec`; apiVersion **stays `v1alpha1`** (shape is distinguishable structurally; a version bump buys nothing). `Read` gains a transparent **legacy lift** — old-shape files parse forever, next `Write` emits the new shape |
| DD8 | CubeLock in-cluster | A second **inert CRD** (`cubelocks.cube-idp.dev`, cluster-scoped, D11 pattern) + one CubeLock record written by `up` at the same point the file is written. The **file stays the source of truth** (vendor/bundle contract); the record is a projection for discoverability |
| DD9 | Version constraints | **Not in v1** of this feature: `dependsOn` is names only, no semver ranges. Ranges need a resolver and a conflict story — parked until a real pack needs it (YAGNI) |
| DD10 | Uninstall ordering | **Out of scope**: `down` remains bulk inventory-driven deletion. Flux prune on dependents is eventual-consistent and `down` deletes the whole cube anyway |

---

## 3. Feature 1 — `dependsOn` for packs

### 3.1 Declaration surfaces

**pack.cue** (contract v1 §2 — additive, allowed by §6):

```cue
name:        "floci-ui"
version:     "0.1.0"
description: "web console for the floci cloud emulator"
dependsOn:   ["floci"]        // NEW, optional: pack names this pack needs healthy first
```

Loader: `internal/pack/pack.go` `loadMeta` gains the same
optional-field treatment as `images:`/`gatewayService:` — absent field →
`nil`, packs predating the field load exactly as before; a non-list or
non-string entry is CUBE-4003 (pack.cue invalid) like every other
malformed field. `Pack` gains `DependsOn []string`.

**cube.yaml** (`internal/config/schema.cue` + `types.go` `PackRef`):

```yaml
spec:
  packs:
    - ref: oci://ghcr.io/cube-idp/packs/kyverno:0.2.0
    - ref: oci://ghcr.io/cube-idp/packs/kyverno-policies:0.2.0
      dependsOn: ["kyverno"]          # NEW, optional
```

Schema: `dependsOn?: [...string & != ""]`. Marshaling follows the
established omitempty discipline (absent key, never an explicit null —
see the `ClusterSpec` comment in types.go). The gateway entry
(`spec.gateway`) deliberately gets **no** `dependsOn` field: the gateway
pack is the graph root; a *pack.cue* `dependsOn` on the pack serving as
the gateway is rejected at graph-build time (CUBE-4020).

### 3.2 Graph resolution — `internal/pack/depgraph.go` (new)

One pure function, shared by `up` and `diff` so both walk the identical
order:

```go
// ResolveOrder builds the dependency graph over the fetched packs
// (explicit pack.cue deps ∪ explicit cube.yaml deps ∪ implicit edges),
// validates it, and returns the delivery order as indices into refs,
// plus the resolved per-pack dependency list (for engine translation
// and the Pack record). packs, refs and rendered are index-aligned;
// index 0 is always the gateway pack.
func ResolveOrder(packs []*Pack, refs []config.PackRef, rendered []*Rendered) (order []int, deps map[string][]string, err error)
```

Rules, in order:

1. **Name resolution.** Dependency names must match the `name` of a pack
   in this cube (gateway pack included). Unknown name → **CUBE-4018**
   listing the installed pack names ("did you forget to add X to
   spec.packs?"). Duplicate pack names across refs are already impossible
   (delivery object names `cube-idp-<name>` would collide); the graph
   builder asserts it and reports the existing duplicate-delivery failure
   mode early with the same message.
2. **Gateway is the root.** The gateway pack (index 0) declaring or
   receiving a `dependsOn` **of its own** → **CUBE-4020** ("the gateway
   pack is delivered first unconditionally and cannot depend on other
   packs"). Other packs *may* name the gateway pack explicitly — the edge
   is simply redundant with DD3(a) and harmless.
3. **Implicit edges** (DD3): scan each `Rendered.Objects` — any object in
   group `gateway.networking.k8s.io` (HTTPRoute today; Gateway/etc. for
   exotic packs) adds `<pack> → <gateway pack>`, except on the gateway
   pack itself. Any `refs[i].Delivery == "repo"` adds `<pack> → gitea`
   (the decision-13 load guard already guarantees gitea is present).
4. **Self-dependency** is a 1-cycle → CUBE-4019.
5. **Kahn's algorithm** with a deterministic tie-break: among ready
   nodes, keep declared cube.yaml order (gateway always index 0). Leftover
   nodes ⇒ cycle → **CUBE-4019** with the cycle path rendered
   `floci-ui → floci → floci-ui` (found via DFS on the residual graph).
   Deterministic order matters: the lock file, the progress `n-of-m`
   enumeration, and the golden-output fences all observe it.

New diag codes (registry + `explain` completeness fence,
`internal/diag/codes.go` — pack range, next free is 4018 per the P8/P9
HANDOFFs):

```go
CodePackDepUnknown Code = "CUBE-4018" // dependsOn names a pack not in this cube
CodePackDepCycle   Code = "CUBE-4019" // pack dependency cycle (path in message)
CodePackDepGateway Code = "CUBE-4020" // gateway pack cannot declare/receive its own dependsOn
```

### 3.3 `up.Run` restructure: fetch/render pass, then deliver pass

Today the loop interleaves fetch → render → lock-entry → deliver per
pack. Implicit-edge detection (DD3) needs every render before the first
delivery, so the loop splits:

1. **Fetch+render pass** — for each ref (declared order): `pack.Fetch`
   (disk-cached; re-runs are cheap), `RenderWith`, lock-entry assembly.
   Progress: the existing `ProgressN("pack", "delivering …")` step splits
   into `ProgressN("pack-fetch", …)` and the delivery step below — a
   renderer/plain-output change that must sweep the golden fences (TE
   conventions; same class of change as U-lane steps).
2. **Graph pass** — `pack.ResolveOrder(...)`; errors here abort before
   any delivery mutation (better failure position than today, where a
   bad later pack aborts a half-delivered loop).
3. **Deliver pass** — in topo order: push + `deliverPack` exactly as
   today, with the pack's resolved deps threaded through (§3.4). The
   gitea hoist branch of `orderPackRefs` is deleted — the implicit
   repo→gitea edge keeps its correctness guarantee (DD6; the
   `giteaSession` gate keeps the wait bounded either way);
   `orderPackRefs` shrinks to "prepend gateway".

`diff.desiredState` mirrors passes 1–2 (it already fetches+renders
everything; it gains the same `ResolveOrder` call) so `diff` previews
byte-identical delivery objects, including the flux `spec.dependsOn`
below — otherwise every diff on a dep-bearing cube would fabricate
drift.

`cube.lock` entries stay in **declared order** (stable against dep
refactors); the delivery order is derivable, not stored.

### 3.4 Engine translation (D2: intent above the seam, translation below)

Intent threading: `pack.Rendered` gains `DependsOn []string` (resolved
names, set by `up`/`diff` after the graph pass — not by RenderWith).
`Engine.DeliverGit` (which takes no `Rendered`) widens by one param:
`DeliverGit(ctx, name string, src GitSource, dependsOn []string)`.
Compiler forces every engine fake to follow (established practice — see
DeliverSelf/P8 HANDOFF).

The seam also gains one capability method so `up` never type-switches on
engines:

```go
// OrdersDeliveries reports whether the engine natively sequences a
// delivery behind its dependencies (flux: Kustomization.spec.dependsOn).
// When false, the up orchestrator wave-gates delivery itself.
OrdersDeliveries() bool
```

**flux (`OrdersDeliveries() → true`)** — `Deliver`/`DeliverGit` add to
the Kustomization, when deps exist:

```yaml
spec:
  dependsOn:
    - name: cube-idp-floci      # same namespace (flux-system) — name-only refs
```

Flux then refuses to reconcile the dependent until the dep Kustomization
is Ready — and ours all carry `wait: true`, so Ready means healthy, not
just applied. This is steady-state correct: upgrades, self-heal
re-reconciles, and `poke` all keep respecting the edge. The A10 race
dies structurally: an HTTPRoute-bearing pack's first dry-run cannot fire
before the gateway pack (CRDs included) is Ready. The
`waitCRDEstablished(httpRouteCRD)` gate in `up.Run` stays — it guards
the registry HTTPRoute that `up` itself applies, outside any pack.

**argocd (`OrdersDeliveries() → false`)** — no native cross-Application
dependsOn exists (argo-cd#7437, open since 2021). Translation:

- The Application carries the deps as an annotation
  `cube-idp.dev/depends-on: "floci,gitea"` — observability + a future
  upgrade path if argocd ever grows native support; syncPolicy is
  untouched.
- `up`'s deliver pass groups the topo order into **waves** (all packs
  whose deps are satisfied by previous waves) and, between waves, runs a
  bounded health gate on exactly the previous wave's delivery names —
  the `giteaSession` bounded-poll pattern over `eng.Health`, capped by
  the existing healthTimeout budget. Timeout → **CUBE-3011** ("pack %s
  waits on %v — dependency did not become healthy within %s; re-run
  `cube-idp up`"). For flux the wave machinery is skipped entirely
  (single wave, apply-all, engine orders) — today's wall-clock
  parallelism is preserved.

Documented asymmetry (README + machine-readable docs): with argocd the
ordering holds **at delivery time only**; steady-state, Applications
self-heal independently. With flux the edge holds always. This is the
honest ceiling of each engine, stated rather than papered over.

### 3.5 Surfaces

- **Pack record (D11)** — `PackObject` widens (append-only surface per
  the P8 HANDOFF): `spec.dependsOn: [...string]` (resolved deps, incl.
  implicit — what actually gated delivery), printer column `DEPENDS-ON`
  appended after `DELIVERY` in `internal/pack/manifests/pack-crd.yaml`.
  `kubectl get packs` now shows the graph.
- **Contract doc** — `docs/pack-contract-v1.md` §2 gains the optional
  `dependsOn` row (additive, §6-compatible); its conformance section
  notes that the harness delivers a pack's declared dep closure (generalizing
  A11's `EXTRA_PACK` mechanism: deps resolve from the packs repo
  checkout by name).
- **explain/doctor** — CUBE-4018/4019/4020/3011 registered (the explain
  completeness fence enforces this mechanically).

### 3.6 Interactions considered

- **Health window**: flux dep chains serialize reconciliation; a chain
  deeper than ~2 can eat the healthTimeout budget. Kept global (no
  per-pack timeouts — no-infinite-spinner rule intact); the CUBE-3004
  remediation text gains "deep dependsOn chains serialize startup".
- **Bundles/air-gap**: `resolveBundleRefs` rewrites refs before the
  fetch pass; the graph builds identically from bundle-local packs. No
  format change to bundles.
- **Spokes**: unaffected — packs never deliver to spokes (decision 9).
- **selfManage/cube-engine**: not a pack, carries no deps, unaffected.
- **`pack install` (single-pack path)**: validates its dep names against
  the live cube's Pack records and warns-not-blocks on absent deps (the
  cube.yaml path is the authoritative one; RECONCILE at planning time
  against the current `pack install` flow).

---

## 4. Feature 2 — CubeLock: proper object + in-cluster record

### 4.1 File shape (DD7)

```yaml
# cube.lock — new shape
apiVersion: cube-idp.dev/v1alpha1
kind: CubeLock
metadata:
  name: dev                    # = cube.yaml metadata.name
spec:
  engine:
    type: flux
  packs:
    - ref: oci://ghcr.io/cube-idp/packs/traefik:0.2.0
      name: traefik
      version: 0.2.0
      resolved: "oci:sha256:…"
      renderedHash: "sha256:…"
      images: [...]
```

`internal/lock/lock.go`:

- `File` becomes `{APIVersion, Kind, Metadata{Name}, Spec{Engine EngineLock, Packs []Entry}}`;
  `Entry` unchanged.
- `Write` unchanged in mechanics (deterministic sigs.k8s.io/yaml).
- `Read` gains (a) a **legacy lift**: if `spec` is absent but top-level
  `engine`/`packs` are present, lift them into `Spec` in-memory (old
  locks parse forever; the next `up` writes the new shape — cube.lock is
  derived state, regeneration is its documented recovery path); (b)
  **identity checks**: a present file whose `kind` is not `CubeLock` or
  whose apiVersion group is not `cube-idp.dev` is CUBE-0003 with the
  existing "delete it and re-run `cube-idp up`" remediation. No CUE
  schema for the lock — it is machine-written; struct decode + identity
  check is proportionate.

Consumers touched (mechanical `lf.Packs` → `lf.Spec.Packs`):
`internal/up/up.go` (assembly — also now sets `Metadata.Name` from the
cube), `internal/up/bundle.go`, `internal/upgrade/plan.go`,
`internal/diff/diff.go`, `internal/bundle/vendor.go` + `bundle.go`.
Bundles embed cube.lock verbatim with a digest — old bundles keep
opening via the legacy lift (the digest is over bytes, not shape).

### 4.2 In-cluster record (DD8)

New inert CRD, owned by `internal/lock` (mirroring `pack.CRD()`):

- `internal/lock/manifests/cubelock-crd.yaml` —
  `cubelocks.cube-idp.dev`, cluster-scoped, `kind: CubeLock`, v1alpha1,
  schema mirroring `spec` above plus `spec.packCount` (int, written
  explicitly because JSONPath cannot compute lengths). Printer columns:
  `ENGINE` (.spec.engine.type), `PACKS` (.spec.packCount), and the
  implicit AGE.
- `lock.CRD()` accessor + `lock.LockObject(cube *config.Cube, f *File) *unstructured.Unstructured`
  building the record: `metadata.name` = cube name, labels
  `app.kubernetes.io/part-of: cube-idp`, spec = the file's spec +
  packCount.

`up.Run` wiring:

- The existing "packs-crd" step applies **both** CRDs (Pack + CubeLock)
  and records them in inventory; step text becomes "record CRDs
  established" (golden-fence sweep, same class as §3.3's).
- At the existing "lock" step (immediately after `lock.Write`): apply +
  `RecordInventory` the CubeLock record. Step line becomes
  "cube.lock written (%d packs) — try \`kubectl get cubelocks\`".
  Written *before* the health gate deliberately: the lock records what
  was **delivered**, not what is healthy (health lives on Pack records);
  an `up` aborted at the health gate still leaves an accurate lock
  record, exactly like the file.
- `down` needs no change: the record and CRD ride the inventory cascade.
- `diff.desiredState`: the CRD joins the `pack.CRD()` desired block; the
  record joins `orphanOnly` (identity-stub — its spec embeds
  fetch-resolved digests, so re-deriving it in a dry-run would fabricate
  drift on every diff; identity is all orphan detection needs).

The file remains the **source of truth** and the only input to
vendor/bundle (offline paths never need a cluster). The record is a
projection whose one job is: a running cluster can answer "what was
delivered here, from which pins" via `kubectl` alone — from any machine,
no checkout required.

Not in scope (parked as an explicit follow-up, not silently implied):
`status`/`doctor`/`upgrade --plan` *reading* the in-cluster record when
no local cube.lock exists. The record write is designed so that
follow-up is purely additive.

### 4.3 What about `Cube` itself in-cluster?

The owner item says "same as Cube" — read as "as well-formed as Cube
already is". An in-cluster **Cube** record (`kubectl get cubes` showing
engine/gateway/pack-count) would complete the trio and costs one more
D11-pattern record, but it duplicates most of CubeLock's projection and
cube.yaml is user-owned config, not derived state. **Deliberately out of
scope**; listed in §7 as an owner question with a lean-no
recommendation.

---

## 5. Testing

Per established gates (unit + envtest + e2e + fences):

- **depgraph unit**: diamond graph order-stability, tie-break = declared
  order, unknown name (4018), self-dep + 2-cycle + 3-cycle paths (4019),
  gateway-dep rejection (4020), implicit HTTPRoute edge detected from
  rendered objects, implicit repo→gitea edge, a cube without
  deps/repo-delivery reproduces today's `orderPackRefs` output exactly,
  and a repo-delivery cube keeps gitea strictly before every
  repo-delivered pack (regression fences for DD6, both halves).
- **flux contract test** (`engine/flux/contract_test.go` +
  `deliver` tests): Kustomization carries `spec.dependsOn` iff deps
  exist; DeliverGit same; golden manifests updated.
- **argocd**: annotation present; wave-gating unit-tested in `up` with
  the faked engine (`OrdersDeliveries() false`) — deliver call order,
  bounded gate timeout → CUBE-3011.
- **lock unit**: new-shape round-trip, legacy lift (old fixture parses,
  re-Write emits new shape), wrong-kind → CUBE-0003; bundle open over a
  legacy-lock fixture.
- **up envtest**: CubeLock CRD established, record applied + inventoried,
  `down` prunes it.
- **e2e (local recipe per memory)**: two-pack dep chain on flux —
  `CUBE_IDP_E2E=1 CUBE_IDP_E2E_GATEWAY_PORT=18443 … -run TestPackDependsOn`;
  assert the dependent's Kustomization reconciles only after the dep is
  Ready and `kubectl get packs` shows DEPENDS-ON.
- **Fences**: golden step lines (pack-fetch split, records-crd, lock step
  text), explain completeness (new codes), command-tree untouched.

## 6. Compatibility

- No-deps cubes: byte-identical delivery objects except none (flux adds
  `dependsOn` only when non-empty); delivery order unchanged (DD6);
  lock file gains metadata/spec shape on next `up` (legacy readers in
  the tree all go through `lock.Read`, which lifts).
- pack contract v1: `dependsOn` is additive (§6). Packs declaring it
  still load on pre-feature binaries — `loadMeta` only looks up known
  paths, so an old binary ignores the field and simply doesn't order the
  delivery. Stated in the contract doc row.
- Pack record: append-only widening (spec.dependsOn + DEPENDS-ON column)
  per the sanctioned surface.

## 7. Open questions for the owner

1. **Implicit gateway edge breadth (DD3a)**: spec'd as "packs rendering
   Gateway API objects". Alternative: *every* pack depends on the
   gateway (matches the old comment's doctrine) — simpler to explain,
   but serializes all of startup behind gateway health and slows `up`
   wall-clock for no correctness gain on non-exposed packs.
   Recommendation: keep the render-derived edge.
2. **Argo CD wave gating (DD5)**: accept the delivery-time-only ordering
   asymmetry, or additionally document argocd as "dependsOn
   best-effort"? Recommendation: wave-gate + document; no sync-wave /
   app-of-apps restructuring.
3. **In-cluster Cube record (§4.3)**: complete the trio now, later, or
   never? Recommendation: not now; revisit if/when `status` learns to
   read in-cluster records.
4. **Slot in the ledger**: this is post-F1 material (touches golden
   fences F1 freezes). Proposed as the first Phase 6 items — or, if F1
   has not been claimed yet, before it to avoid a double docs sweep.

## 8. Execution sketch (sizing, one agent per task)

| Task | Scope | Depends |
| --- | --- | --- |
| DEP1 | `depgraph.go` + pack.cue/schema.cue `dependsOn` loaders + CUBE-4018/19/20 + unit fences | — |
| DEP2 | `up.Run` two-pass restructure + `orderPackRefs` retirement + `diff.desiredState` mirror + step-line fences | DEP1 |
| DEP3 | Engine seam: `Rendered.DependsOn`, `OrdersDeliveries`, flux `spec.dependsOn`, argocd annotation + wave gate + CUBE-3011 | DEP2 |
| DEP4 | Pack record DEPENDS-ON + contract-doc/README/explain sweep + e2e dep-chain leg | DEP3 |
| LOCK1 | lock KRM shape + legacy lift + consumer sweep + CUBE-0003 identity checks | — (parallel with DEP1-3) |
| LOCK2 | cubelock CRD + record + up/down/diff wiring + envtest + docs | LOCK1; merges after DEP2 (shared up.go seams) |

Packs-repo follow-ups (separate repo, after DEP4): declare
`dependsOn: ["floci"]` in floci-ui, `["kyverno"]` in kyverno-policies;
conformance harness dep-closure delivery replacing `EXTRA_PACK` for
in-repo deps.
