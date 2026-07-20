# cube-idp `spec.prerequisites`: CLI-delivered bootstrap packs before the engine

Date: 2026-07-19
Status: DRAFT — shape settled by owner, **due diligence required before
ratification** (see §5). This document is deliberately self-contained: an
agent picking it up needs no other conversation context, only the
referenced files.
Prior art / hard dependency:
`2026-07-19-cube-idp-engine-as-pack-design.md` (PROPOSED) — this feature
generalizes the pre-engine pack plumbing that spec introduces and MUST
land after it. Related:
`2026-07-19-cube-idp-pack-depends-and-cubelock-crd-design.md` (p6
depgraph), `docs/pack-contract-v1.md`.

---

## 1. Goal

Let the operator declare packs that are rendered and applied by the CLI
**during cluster onboarding — new and existing clusters alike — before
the engine and everything the engine delivers**:

```yaml
apiVersion: cube-idp.dev/v1alpha1
kind: Cube
spec:
  prerequisites:                 # top-level; ordinary PackRef entries
    - ref: oci://ghcr.io/cube-idp/packs/gateway-api-crds:0.1.0
  engine:
    type: flux
  gateway:
    ref: oci://ghcr.io/cube-idp/packs/traefik:0.3.0
```

Canonical first use case: Gateway API CRDs as a prerequisite, so every
gateway-kind object is bootstrap-safe (today the CRDs arrive only when
the engine delivers the gateway pack — see §2.2).

## 2. Context a cold reader needs

### 2.1 Bootstrap order today (internal/up/up.go Run)

cluster → zot registry (CLI SSA) → Pack CRD (CLI SSA) → **engine install
(CLI SSA of the engine pack render, per the engine-as-pack spec)** →
gateway TLS → packs fetched/rendered → cube.lock → packs delivered *via
the engine* (flux Kustomization / argocd Application) → gateway-CRD wait
→ registry HTTPRoute → health → selfManage handover → Pack records.

Everything before the engine is CLI-owned (SSA + inventory, no drift
correction); everything after is engine-owned. Operators currently have
NO way to put anything into the CLI-owned segment.

### 2.2 The CRD-ordering problem this dissolves

Gateway API CRDs ship inside the traefik gateway pack
(`packs/traefik/manifests/00-gateway-api-crds.yaml`) and land only when
the engine delivers it; envoy-gateway's controller installs them at
runtime, later still. Consequences today: `up` hand-rolls a 3-minute
`waitCRDEstablished` gate (up.go:885, CUBE-5005) before applying the
registry HTTPRoute; the engine pack must stay install-only (engine-as-
pack spec D5) because a gateway-kind object would fail bootstrap SSA;
the p6 depgraph carries a hardcoded implicit edge (any HTTPRoute-bearing
pack → gateway pack, internal/pack/depgraph.go:66).

### 2.3 Settled shape (owner, 2026-07-19)

- **Top-level `spec.prerequisites`**, NOT under `spec.cluster` (that
  block is provider provisioning; prerequisites are pack delivery).
  Reuses `config.PackRef` verbatim → `ref` + `values` for free.
- Applied **in list order** — no depgraph participation; bootstrap stays
  deliberately dumb. Each: `pack.Fetch` → `RenderWith(values, "", gw)`
  → SSA with wait → `RecordInventory` → cube.lock entry → Pack record
  row with a `delivery: "ssa"` marker. Same rails as the engine pack.
- Prereqs are **CLI-owned forever** (GitOps-exempt like zot and the
  non-selfManaged engine). Posture: documentation, not kind
  restrictions — operator in control.
- Rejected alternative: `spec.packs[].delivery: "ssa"` — overloads a
  knob that means "how the engine sources the pack" with "the engine is
  not involved", and scatters bootstrap members through the list
  instead of an explicit phase boundary.

## 3. Why it is the honest generalization

The engine-as-pack spec's `[engine-pack]` step (fetch → render → CLI SSA
→ inventory → lock → record, pre-engine) IS a prerequisite in all but
name. This feature turns that single hardcoded step into a loop over
`[prerequisites…, engine pack]` — one code path, no second delivery
model. Implementing it before that plumbing exists means building it
twice; hence the hard dependency.

## 4. What it buys

1. Root-cause fix for the bootstrap CRD class: registry-route wait
   degrades to a backstop; engine-as-pack D5 relaxes (engine pack MAY
   carry gateway-kind objects — they apply and dangle until the gateway
   controller arrives).
2. Existing-cluster onboarding: baseline CRDs/namespaces/policies
   guaranteed present before any cube component touches the cluster —
   a segment currently closed to operators.
3. Uniform visibility: prereqs appear in `kubectl get packs` and
   cube.lock like everything else.

## 5. DUE DILIGENCE — must be resolved before ratification

1. **SSA field-manager ownership collision (the big one).** A
   `gateway-api-crds` prereq contains objects `packs/traefik` ALSO ships
   today. Two managers (CLI SSA + flux Kustomization) fighting over the
   same CRDs; prune trap: removing the traefik pack could GC CRDs the
   prereq owns. Adopting the canonical use case implies **removing the
   CRDs from the traefik pack** — a $PACKS breaking change (version
   bump, compat story for older CLIs that expect the pack to carry
   them). Investigate: exact flux/argocd behavior on adopting
   already-CLI-owned objects; prune semantics; migration sequencing.
2. **envoy-gateway**: its controller installs Gateway CRDs at runtime.
   Verify controller behavior when CRDs pre-exist (adopt/skip/fight?),
   and what its pack should ship once a CRD prereq exists.
3. **Capability-inference interplay** (engine-as-pack spec §8.2): the
   future provides/consumes depgraph analysis must treat
   prereq-provided GVKs as satisfied, or every HTTPRoute-bearing pack
   grows a phantom unresolvable dependency. The two features must be
   designed together even if built apart.
4. **`down` / removal semantics**: prereqs leave via the inventory
   cascade on `down`, but mid-life removal from the list — orphan
   detection? diff's orphanOnly handling? Decide and test.
5. **diff**: `desiredState` must render prereqs (warm cache) into the
   SSA dry-run kernel set, mirroring the engine pack.
6. **Bundles/airgap**: prereqs vendored like every pack
   (`vendorPacks`/`resolveBundleRefs`) — confirm no special-casing
   needed beyond list inclusion.
7. **Validation**: reject a ref appearing in both `prerequisites` and
   `packs` (one owner per pack); decide whether gateway/engine refs are
   similarly excluded. New typed config codes.
8. **Misuse posture**: document that prerequisites are GitOps-exempt;
   consider a `cube-idp doctor` note when workload-shaped kinds
   (Deployment/StatefulSet) appear in a prereq render — advisory, never
   a hard block.

## 6. Non-goals

- No depgraph/ordering among prerequisites (list order is the contract).
- No engine ownership or drift correction for prereqs — by design.
- Not a replacement for p6 `dependsOn` or capability inference: those
  order engine-delivered packs; this owns the segment before the engine.

## 7. Sequencing

engine-as-pack phase (PROPOSED spec) → THIS (own design pass resolving
§5, then plan) → capability inference (§8.2 there) designed aware of
both. The $PACKS CRD-ownership migration (§5.1/5.2) is the long pole and
may warrant its own wave in the plan.
