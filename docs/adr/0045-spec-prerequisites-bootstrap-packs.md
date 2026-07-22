# 0045 — `spec.prerequisites`: User-Declarable Bootstrap Packs Applied Before the Engine

Status: proposed
Date: 2026-07-22
Epic: cube-idp/cube-idp#18

## Context

Today the pre-engine bootstrap sequence is **hardcoded**: `up` applies zot →
the Pack CRD → the engine pack in a fixed order (`internal/up/up.go:176-270`),
and the config schema is closed to `cluster`/`engine`/`gateway`/`packs`/`spokes`
(`internal/config/schema.cue` — a `prerequisites:` key fails `CUBE-0002`). There
is no way for an operator to declare additional packs that must land *before* the
GitOps engine — e.g. the Gateway API CRDs (#25), a cluster-wide policy controller,
or a CNI add-on — even though the engine-as-pack work (ADR-0007) already built the
machinery to render-and-SSA a pack before the engine exists.

This decision generalizes that existing pre-engine plumbing into a declarable
`spec.prerequisites` list. A complete design was written during the p7 cycle and
validated against the code by the 2026-07-20 audit (21 recorded decisions); it is
archived at `docs/archive/superpowers/specs/2026-07-19-cube-idp-prerequisites-packs-design.md`
and is the source material for this ADR — non-authoritative history; this ADR is
the authority.

## Options

1. **Delivery mechanism**
   - (a) A new **top-level `spec.prerequisites`** list, each entry an ordinary
     `config.PackRef` (`ref` + `values`), applied by the CLI via SSA before the
     engine. **[RECOMMENDED — matches the archived design; keeps bootstrap
     explicit and separate from engine-delivered `spec.packs`.]**
   - (b) A `spec.packs[].delivery: "ssa"` marker reusing the existing packs list.
     **[rejected — overloads the engine-sourcing knob and scatters bootstrap
     members through the normal pack list instead of marking them explicitly.]**

2. **Ordering**
   - (a) **List order is the contract** — no dependency graph among prerequisites.
     **[RECOMMENDED — bootstrap is a short, operator-authored sequence; a graph is
     over-engineering. `dependsOn`/capability inference still order engine-delivered
     packs.]**
   - (b) Full `dependsOn` graph over prerequisites too. **[rejected — complexity
     without a demonstrated need at bootstrap time.]**

3. **Content restrictions**
   - (a) **No kind restrictions**; misuse addressed by docs + an advisory (never
     hard-blocking) `doctor` note when workload-shaped kinds (Deployment/StatefulSet)
     appear. **[RECOMMENDED — operator stays in control; bootstrap legitimately needs
     controllers.]**
   - (b) Whitelist CRDs/RBAC only. **[rejected — too restrictive; breaks real
     bootstrap use like a policy controller.]**

4. **Lifecycle & ownership**
   - Prerequisites are **CLI-owned for their whole lifetime** via SSA + inventory,
     exempt from engine drift-correction (same as zot and a non-selfManaged engine);
     removed by the inventory cascade on `down`. They appear in `kubectl get packs`
     and `cube.lock` like any other pack, and are vendored for bundles/airgap by the
     same path with no special-casing. **[follows from 1(a) — one delivery model.]**

## Decision

_Pending PR review — the merge of this PR is the acceptance. The recommendations
above are the proposed decision; the reviewer adjudicates each option group here._

Proposed, adopting the recommended options:

- **`spec.prerequisites`** is a new top-level list in the `Cube` resource (not
  nested under `spec.cluster` — that owns provider provisioning; prerequisites are
  pack delivery). Each entry is an ordinary `config.PackRef` (`ref` + `values`),
  reused verbatim.
- Prerequisites are **applied by the CLI, in list order, before the engine and
  everything the engine delivers**, for new and existing clusters alike. Delivery
  reuses **one single pre-engine code path** — a loop over `[prerequisites…, engine
  pack]` — running the same pipeline the engine pack already uses: `pack.Fetch` →
  `RenderWith(values, "", gw)` → SSA-with-wait → `RecordInventory` → `cube.lock`
  entry → Pack record row.
- **No ordering graph** among prerequisites; **no kind restrictions** (advisory-only
  `doctor` note for workload kinds). A ref appearing in both `prerequisites` and
  `packs` is a **config error** (new typed codes), enforcing one owner per pack.
- `diff`'s `desiredState` renders prerequisites from the warm cache into the SSA
  dry-run kernel set, mirroring the engine pack. Capability-inference treats GVKs
  provided by prerequisites as satisfied. `down`'s inventory cascade removes them.
- **Sequencing:** this must land **after** the engine-as-pack pre-engine plumbing it
  generalizes (already shipped, ADR-0007).

## Implementation Plan

- **Affected paths:** `internal/config/schema.cue` (open the `prerequisites:` key),
  `internal/config/load.go` (validation: dual-owner rejection, new `CUBE-00xx`
  codes), `internal/up/up.go:176-270` (the pre-engine loop), `internal/diff` (dry-run
  kernel set), `internal/pack` depgraph capability-satisfaction, `internal/bundle`
  (vendoring — likely no change), `docs/reference/cube-yaml-reference.md`,
  `docs/architecture/packs.md` + `cluster.md` (same-PR marker update).
- **Sub-issues (created at acceptance, each `Closes #<sub>` + `Implements ADR-0045`):**
  1. Config surface — open `spec.prerequisites` in the schema; dual-owner validation
     + typed codes.
  2. Pre-engine loop — one code path over `[prerequisites…, engine pack]`; inventory
     + lock + Pack rows.
  3. `diff` dry-run + capability-inference satisfaction for prerequisite GVKs.
  4. **Gateway API CRDs as a prerequisite (#25)** — the first real consumer; note the
     `$PACKS` traefik-CRD-ownership breaking change (version bump) called out in the
     archived design.
  5. e2e: a prerequisite pack lands before the engine on a fresh and an existing
     cluster; `down` cascade removes it; docs updated.
- **Follow-up / out of scope:** no dependency graph among prerequisites (list order
  is the contract, by decision).

## Verification

- [ ] `spec.prerequisites` accepted by the schema; a ref in both `prerequisites`
      and `packs` is rejected with a typed `CUBE-00xx` code
- [ ] A prerequisite pack is applied before the engine on a fresh cluster and on an
      existing cluster (e2e), appears in `kubectl get packs` and `cube.lock`
- [ ] `diff` shows prerequisite changes; capability inference does not flag phantom
      unresolved deps for HTTPRoute-bearing packs
- [ ] `down` removes prerequisites via the inventory cascade
- [ ] Gateway API CRD check moved into prerequisites (#25); docs updated
