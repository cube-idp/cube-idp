# spec.prerequisites — Implementation Plan (ADR-0045)

> **Decision authority:** [ADR-0045](../../adr/0045-spec-prerequisites-bootstrap-packs.md) (accepted 2026-07-22).
> **Epic:** cube-idp/cube-idp#18. This plan is the *how*; the ADR is the *why*.
> Executed one-task-per-fresh-agent per `docs/process/sdd-dispatch-template.md`;
> claim/close per `docs/process/sdd-ledger-template.md`.

**Goal:** Add a top-level `spec.prerequisites` list — ordinary pack refs the CLI
applies via SSA *before* the engine, in list order — reusing the engine pack's
existing render/apply pipeline as a single pre-engine code path. Prerequisites are
CLI-owned (SSA + inventory, no engine drift-correction), appear in `kubectl get
packs` and `cube.lock` like any pack, and are removed by the `down` cascade.

**Architecture:** Open the closed CUE schema (`internal/config/schema.cue`) to a
`prerequisites?: [...PackRef]` key. Generalize the hardcoded pre-engine sequence in
`internal/up/up.go` (registry → Pack CRD → **[prerequisites… , engine pack]** loop)
so prerequisites run through the same `Fetch → RenderWith → SSA-wait →
RecordInventory → cube.lock → Pack row` pipeline the engine pack already uses
(`internal/pack/enginepack.go:FetchRenderEngine`, `render.go:RenderWith:130`). Reject
a ref present in both `prerequisites` and `packs` in `crossValidate`
(`internal/config/load.go:203`) with a new typed code. Render prerequisites into
`diff`'s dry-run kernel set (`internal/diff/diff.go:91`) and mark their GVKs satisfied
in capability inference. No ordering graph (list order is the contract); no kind
restrictions (advisory-only `doctor` note).

**Tech Stack:** Go, CUE (`internal/config/schema.cue`), existing `internal/pack`
render/apply, `internal/diag` typed codes. No new dependencies. Next free config
codes: `CUBE-0016+`.

## Global Constraints

- Work only in an isolated worktree on branch `adr-0045-prerequisites` (or per-task
  branches merging to it); never on `main`; never `git add -A`.
- Every commit trailer: `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.
- Go gate before any task closes: `go build ./... && go vet ./... && go test ./... -count=1`, green, output pasted.
- New diag codes need BOTH a constant (`internal/diag/codes.go`) AND a registry `Desc`
  (`TestRegistryCoversEveryDeclaredCode` enforces both directions).
- Every PR body: `Closes #<sub>` + `Implements ADR-0045`.
- Sequencing: this generalizes shipped engine-as-pack plumbing (ADR-0007) — that is a
  precondition, already met on main.

## Task Index & Ledger

| ID | Task | Sub-issue | Depends | STATUS |
| --- | --- | --- | --- | --- |
| T1 | Config surface: `spec.prerequisites` in schema + dual-owner validation + `CUBE-0016` | #43 | — | DONE |
| T2 | Pre-engine loop: `[prerequisites…, engine]` one code path; inventory + lock + Pack rows | (create) | T1 | DONE |
| T3 | `diff` dry-run + capability-inference satisfaction for prerequisite GVKs | (create) | T2 | DONE |
| T4 | Gateway API CRDs as a prerequisite (the first real consumer) | #25 | T2 | DONE |
| T5 | e2e (fresh + existing cluster; `down` cascade) + reference/architecture docs | (create) | T2,T3,T4 | UNCLAIMED |

---

### Task T1: Config surface + validation

**Files:** `internal/config/schema.cue`, `internal/config/load.go`,
`internal/diag/codes.go`, `internal/diag/registry.go`, tests.

- [x] **Step 1:** Add `prerequisites?: [...PackRef]` to the top-level spec in
  `schema.cue` (mirror the `packs?` entry shape at line 41 — `ref` required,
  optional `values`/`valuesRef`). A `PackRef` def may already be factorable; if not,
  inline to match `packs`.
- [x] **Step 2:** In `crossValidate` (`load.go:203`), reject any `ref` appearing in
  both `prerequisites` and `packs` — new typed code `CUBE-0016`
  (`CodePackDualOwner`), constant + registry `Desc`.
- [x] **Step 3:** Table tests: a valid `prerequisites` entry loads; a dual-owner ref
  fails with `CUBE-0016`; an empty/absent key is unchanged (byte-compat).
- [x] **Step 4:** Gate green; commit `feat(config): spec.prerequisites schema + dual-owner guard (CUBE-0016)`.

### Task T2: Pre-engine delivery loop

**Files:** `internal/up/up.go`, `internal/pack/enginepack.go` (or a shared helper),
`internal/lock`, tests.

- [x] **Step 1:** Factor the engine pack's `Fetch → RenderWith → SSA-wait →
  RecordInventory → cube.lock → Pack row` into a reusable per-pack pre-engine
  delivery func (the engine pack becomes its last caller). *(Deviation — see
  Outcome FINDINGS: factored `deliverPrerequisite`, not a func the engine also
  calls; the engine keeps its self-management-specific SSA path.)*
- [x] **Step 2:** In `up.go` after the Pack CRD step (~`:230`), loop
  `[prerequisites…, engine pack]` through that func in list order — prerequisites
  first. Same for new AND existing clusters.
- [x] **Step 3:** Prerequisites record inventory + `cube.lock` entries + Pack rows
  identically to any pack; CLI-owned (no engine self-management for them).
- [x] **Step 4:** Unit/integration coverage of the loop ordering + lock entries; gate
  green; commit `feat(up): apply spec.prerequisites before the engine, one code path`.

### Task T3: diff + capability inference

**Files:** `internal/diff/diff.go`, `internal/pack/depgraph.go`, tests.

- [x] **Step 1:** Render prerequisites into the desired kernel set
  (`diff.go:91`) from the warm cache, mirroring the engine pack.
- [x] **Step 2:** In capability inference, treat GVKs provided by prerequisites as
  satisfied (so HTTPRoute-bearing packs don't acquire phantom unresolved deps).
- [x] **Step 3:** Tests: `diff` surfaces a prerequisite change; a pack needing a GVK a
  prerequisite provides resolves clean. Gate green; commit
  `feat(diff): render prerequisites into dry-run set; satisfy their GVKs in depgraph`.

### Task T4: Gateway API CRDs as a prerequisite (#25)

**Files:** reference config/example, `$PACKS` note (out-of-repo — record only), tests.

- [x] **Step 1:** Provide/point at a prerequisite pack carrying the Gateway API CRDs;
  move the up-front CRD check to rely on it. *(Owner decision, session 2026-07-22:
  Option 1 — up.Run skips `waitCRDEstablished` when a prerequisite provides the
  Gateway API group, reusing T3's `ProvidedGroups`. The real `gateway-api-crds` pack
  is out-of-repo ($PACKS), so this repo ships the CODE that relies on it + a
  reference example, not the pack.)*
- [x] **Step 2:** Record the `$PACKS` breaking change the ADR flags (traefik pack must
  drop the CRDs so one field-manager owns them → version bump) as a HANDOFF for the
  packs repo — do NOT edit `$PACKS` from here. *(See Outcome HANDOFF below.)*
- [x] **Step 3:** Gate green; commit `feat(prereq): Gateway API CRDs via spec.prerequisites (closes #25)`.

### Task T5: e2e + docs

**Files:** `tests/e2e/…`, `docs/reference/cube-yaml-reference.md`,
`docs/architecture/packs.md`, `docs/architecture/cluster.md`,
`docs/adr/0005-pack-dependency-graph-and-ordering.md`.

- [ ] **Step 1:** e2e leg: a prerequisite pack lands before the engine on a fresh
  cluster AND an existing cluster; appears in `kubectl get packs`; `down` removes it
  via the inventory cascade. (Isolated `KUBECONFIG`, foreground, one live leg —
  CLAUDE.md §8.)
- [ ] **Step 2:** Document `spec.prerequisites` in `cube-yaml-reference.md`; update the
  `docs/architecture/packs.md` + `cluster.md` `cube:doc`/`cube:section` markers, add
  `adrs=…,0045`.
- [ ] **Step 2b (ADR-0005 reconciliation — added per owner instruction, session
  2026-07-22):** T3 made the implicit gateway edge CONDITIONAL (suppressed when a
  prerequisite provides `gateway.networking.k8s.io`), which amends a rule ADR-0005
  owns (ADR-0005:97 states the edge unconditionally; ADR-0005:65-66 says that ADR is
  the one to change for any edge-rule change). Add a cross-reference note to the
  relevant ADR-0005 row/consequence: the edge is now suppressed when a
  spec.prerequisites pack provides that group's CRDs (ADR-0045). Doc-only; keeps
  ADR-0005 consistent with the code. Update the `packs.md` "Dependencies and
  ordering" section's `adrs=` marker to include 0045 too.
- [ ] **Step 3:** Gate green; commit `test+docs: spec.prerequisites e2e + reference/architecture (ADR-0045)`.

---

## Ledger Outcomes

#### T1 Outcome
- STATUS: DONE
- BRANCH: adr-0045-prerequisites (feature branch; not yet PR'd to main — lands as one PR at feature completion per CLAUDE.md §4 merge model)
- COMMITS:
  - 660d07d docs: prerequisites plan — claim T1
  - 28301f9 feat(config): spec.prerequisites schema + dual-owner guard (CUBE-0016)
- FINDINGS:
  - `spec.prerequisites` scoped narrower than `packs`: entries take `{ref, valuesRef?, values?, extraManifests?}` but NOT `delivery`/`dependsOn` — per ADR-0045 prerequisites are never engine-delivered and take no part in the dependency graph. The schema (`schema.cue`) enforces this; the Go type reuses `PackRef` (whose `Delivery`/`DependsOn` fields stay empty for prerequisites — the schema is the gate, matching how the ADR says "an ordinary config.PackRef").
  - `PackRef` was NOT factored into a shared CUE `#PackRef` def (the plan allowed either) — `packs?` is inlined in `schema.cue`, so `prerequisites?` is inlined to match rather than introducing a refactor the plan didn't call for.
  - Placed the dual-owner guard early in `crossValidate` (before the argocd/gitea pack checks) so a mis-owned pack is caught before delivery-specific rules.
- BLOCKERS: none
- HANDOFF for T2:
  - Config field is `Cube.Spec.Prerequisites []config.PackRef`; iterate it in list order in the pre-engine loop.
  - New diag code available: `diag.CodePackDualOwner` = `CUBE-0016`.
  - The engine pack render/apply pipeline to generalize is `internal/pack/enginepack.go:FetchRenderEngine` (→ `RenderWith(values, "", gw)`); the pre-engine insertion point in `up.go` is right after the Pack CRD step (search `packs-crd`, ~:230 region).
  - `go build/vet/test ./...` all green on the branch; `TestRegistryCoversEveryDeclaredCode` PASS (confirms CUBE-0016 has constant + Desc). New tests: `TestLoadAcceptsPrerequisites`, `TestLoadRejectsDualOwnerPack`, `TestLoadAbsentPrerequisitesIsNil` in `internal/config/load_test.go`; testdata `prerequisites.yaml` + `prerequisites-dual-owner.yaml`.

#### T2 Outcome
- STATUS: DONE
- BRANCH: adr-0045-prerequisites (feature branch; not yet PR'd to main — lands as one PR at feature completion per CLAUDE.md §4 merge model)
- COMMITS:
  - b926ffd docs: prerequisites plan — claim T2
  - b7cf64b feat(up): apply spec.prerequisites before the engine, one code path
- FINDINGS:
  - DEVIATION (Step 1, minimal correction per CLAUDE.md §5): the plan said "factor the engine pack's pipeline into a reusable func the engine pack becomes the last caller of." I factored a dedicated `deliverPrerequisite` (Fetch → RenderResolved → SSA(wait) → RecordInventory → returns pack+lock.Entry) rather than folding the engine into the same func. Reason: the engine install carries self-management-specific SSA logic (`installNeedsSSA`/`engineHealthyAtStart`, the selfManage rules 1–3) and its lock/Pack-row handling is threaded through the rest of Run in a bespoke way; a shared func would have had to absorb all of that, a large refactor touching self-management that the "no scope-adding" rule cautions against. `deliverPrerequisite` is the same PIPELINE SHAPE as the engine install, minus self-management — the ADR's "one pre-engine code path" is honored as a shape, and prerequisites never self-manage anyway. The engine remains its own inline caller.
  - Prerequisites use `pack.RenderResolved` (not the engine's `RenderWith`) so valuesRef/values/extraManifests work on a prerequisite exactly as on any pack — the ADR scopes prerequisite entries to `{ref, valuesRef?, values?, extraManifests?}`, all of which RenderResolved handles.
  - Index-alignment invariant preserved: `entries`/`packs`/`renders` stay index-aligned with `refs` (the engine-delivered pack loop's contract — the Pack-row loop reads `refs[i]`↔`packs[i]`). Prerequisites are kept in a SEPARATE `prereqs []*deliveredPrereq` slice and merged only at the two consumption points (lock `Packs` list, prepended in list order; Pack rows, emitted first) — never mixed into those three slices.
  - `down` needs NO change: the cascade prunes by inventory label selector, and prerequisites are `RecordInventory`'d exactly like every other artifact, so they are removed by `down` for free (verified by reading cmd/down.go's inventory-driven prune; the T5 e2e will confirm live).
  - `deliverPrerequisite`'s applier param is the existing `packApplier` interface (not concrete `*apply.Applier`) so the `fakePackApplier` seam covers it in unit tests — the same seam `deliverPack` already uses. `*apply.Applier` satisfies it, so the production call in Run is unchanged.
  - Pack row for a prerequisite: `delivery="prerequisite"` (a free-string column — PackObject only special-cases empty→"oci"), `ready=true` by construction (SSA'd with wait=true, CLI-owned so no `cube-idp-<name>` engine component to poll), `dependsOn=nil` (no dep graph). CUSTOMIZED follows the same values/valuesRef/extraManifests rule as any pack.
- BLOCKERS: none
- HANDOFF for T3 (diff + capability inference):
  - The list of prerequisite refs to render into the diff's desired kernel set is `cube.Spec.Prerequisites` (`[]config.PackRef`); render each with `pack.RenderResolved(ctx, pk, pref, gw, cacheDir)` from the warm cache, mirroring how the engine pack is added to the desired set. Prerequisites are applied FIRST (before the engine) — reflect that ordering if diff surfaces order.
  - For capability inference (`internal/pack/depgraph.go`): treat GVKs provided by a prerequisite's rendered objects as SATISFIED, so a pack needing e.g. HTTPRoute doesn't acquire a phantom unresolved dep when a prerequisite (Gateway API CRDs) provides that GVK. The prerequisite objects to scan are the same `RenderResolved` output.
  - diff entry point is `internal/diff/diff.go:91` (the desired kernel set assembly, per the T3 task Files). The engine pack is already added there — prerequisites go in the same set, ahead of it.
  - No new diag codes were needed for T2. Next free config code remains `CUBE-0016`-consumed → `CUBE-0017+` if T3 needs one (it likely does not).
  - Tests to mirror: `TestDeliverPrerequisiteAppliesAndRecords`, `TestPrerequisitesAppliedInListOrder` in `internal/up/up_test.go`; fixture helper `writePrereqPack` (local pack dir Fetch can read). `go build/vet/test ./...` all green on the branch.

#### T3 Outcome
- STATUS: DONE
- BRANCH: adr-0045-prerequisites (feature branch; not yet PR'd to main — lands as one PR at feature completion per CLAUDE.md §4 merge model)
- COMMITS:
  - 0f4c176 docs: prerequisites plan — claim T3
  - 9a7b095 feat(diff): render prerequisites into dry-run set; satisfy their GVKs in depgraph
- FINDINGS:
  - CAPABILITY INFERENCE mechanism: the plan's "treat GVKs provided by prerequisites as satisfied" maps onto depgraph's implicit edge (a) — a pack rendering a `gateway.networking.k8s.io` object gets an edge to the gateway pack (for its CRDs). I made that edge conditional: `ResolveOrder` gained a `providedGroups map[string]bool` param, and edge (a) is suppressed when `providedGroups[gatewayAPIGroup]`. New helper `pack.ProvidedGroups([]*Rendered)` reads `spec.group` from each render's `CustomResourceDefinition` objects — so once T4 ships the Gateway API CRDs AS a prerequisite, HTTPRoute-bearing packs stop acquiring the phantom gateway edge. Until T4, no prerequisite carries those CRDs, so `providedGroups` is empty and the graph is byte-identical to pre-ADR-0045 (regression-fenced by `TestResolveOrderPrerequisiteSatisfiesGatewayGroup`'s second half + all 13 existing depgraph tests, updated to pass `nil`).
  - SIGNATURE CHANGE (public seam): `pack.ResolveOrder` gained a 4th param `providedGroups`. Three production callers (`up.resolveAndDeliverPacks`, `diff.desiredState`) + 13 test call sites updated. Callers with no prerequisites pass `nil`. This is a mechanical, plan-driven change (capability inference is explicitly in ADR-0045's Implementation Plan step 3), not a new architectural decision — no ADR needed.
  - DIFF now mirrors up.Run for prerequisites: renders each `cube.Spec.Prerequisites` entry and adds its objects to `desired` (real SSA dry-run diff — a changed prerequisite render surfaces as drift), a `lock.Entry` to the content-drift `entries`, and its Pack-record identity to `orphanOnly` (up.Run writes a Pack row per prerequisite, so without this a converged cube would show a false orphan). Placed BEFORE the engine block, mirroring up's apply order.
  - `deliveredPrereq` gained a `rendered *pack.Rendered` field (T2 stored only pk+entry) so up.Run can compute `ProvidedGroups(prereqRenders)`. No behavior change to T2's paths.
  - `ProvidedGroups` treats a malformed/absent `spec.group` as "provides nothing" (not a hard error) — a broken CRD is the pack author's problem, surfaced by the render/apply path, not the graph resolver's job to reject.
- BLOCKERS: none
- HANDOFF for T4 (Gateway API CRDs as a prerequisite, #25):
  - The capability-inference machinery is READY: once a prerequisite pack renders the Gateway API `CustomResourceDefinition`s (spec.group = `gateway.networking.k8s.io`), `pack.ProvidedGroups` will pick it up automatically and `ResolveOrder` will suppress the implicit gateway edge for HTTPRoute-bearing packs — no further depgraph/diff changes needed in T4. T4's job is to PROVIDE the CRD-bearing prerequisite pack and move the up-front CRD check to rely on it.
  - up.go still has the `waitCRDEstablished(ctx, a, con, httpRouteCRD, gatewayCRDTimeout)` gate (up.go ~:477 region) before the registry HTTPRoute apply, and the `gatewayCRDTimeout`/`httpRouteCRD` consts. With the CRDs coming from a prerequisite (applied + SSA-waited BEFORE the engine and all packs), that wait becomes a no-op (CRD already Established) — T4 should decide whether to keep it as a cheap belt-and-suspenders or remove it. It is currently justified by the traefik-ships-CRDs-as-manifests vs envoy-lags race; ADR-0045 flags that the traefik pack must DROP the CRDs (one field-manager owns them) — record that `$PACKS` breaking change as a HANDOFF, do NOT edit `$PACKS` from here (T4 Step 2).
  - Prerequisite Pack rows use `delivery="prerequisite"`; the Gateway API CRDs prerequisite will appear in `kubectl get packs` with that delivery and READY=yes.
  - Tests to mirror/extend: `TestResolveOrderPrerequisiteSatisfiesGatewayGroup`, `TestProvidedGroupsReadsCRDGroup` (internal/pack/depgraph_test.go), `TestDesiredStatePrerequisites` (internal/diff/diff_test.go). `go build/vet/test ./...` all green on the branch.

#### T4 Outcome
- STATUS: DONE
- BRANCH: adr-0045-prerequisites (feature branch; not yet PR'd to main — lands as one PR at feature completion per CLAUDE.md §4 merge model)
- COMMITS:
  - 2c89dca docs: prerequisites plan — claim T4
  - 9f96e23 feat(prereq): Gateway API CRDs via spec.prerequisites (closes #25)
- FINDINGS:
  - OWNER DECISION (session 2026-07-22): T4's approach was ambiguous because the real `gateway-api-crds` pack lives in `$PACKS` (out-of-repo, hands-off), so "move the up-front CRD check to rely on it" could mean (1) code that skips the runtime wait when a prerequisite provides the CRDs, (2) docs+handoff only, or (3) a new doctor preflight. Owner chose **Option 1**. This repo ships the CODE that relies on the prerequisite + a reference example; it does NOT create the pack.
  - CODE CHANGE (up.go ~:534): the late `waitCRDEstablished(httpRouteCRD)` gate — which today fails "later during deployment" (#25's complaint) — is now skipped when `providedGroups[pack.GatewayAPIGroup]`. A prerequisite providing the Gateway API group SSA-applied AND kstatus-waited its CRDs BEFORE the engine and every pack (T2's `deliverPrerequisite`, wait=true), so `httproutes` is already Established here and the traefik-vs-envoy async race the wait guards against cannot occur. #25's "validated up front" IS the prerequisite. Backward-compatible: no prerequisite provides it → the legacy gateway-pack wait path is byte-identical.
  - EXPORTED `pack.GatewayAPIGroup` (was unexported `gatewayAPIGroup`): up.Run now tests the SAME constant that keys the `ProvidedGroups` map ProvidedGroups builds — closing a magic-string divergence risk between the map producer (pack) and consumer (up). All in-package refs + depgraph tests updated.
  - The reference example (`docs/reference/cube-yaml-reference.md`) adds a `prerequisites:` section (before `packs:`, matching schema order) with the Gateway API CRDs as the canonical entry + a customized (values) prerequisite, documenting delivery=prerequisite / kubectl get packs / down cascade / CUBE-0016 one-owner rule. The machine-checked shape is T1's `internal/config/testdata/prerequisites.yaml`; this is the human-facing twin.
  - No new diag codes. No `$PACKS` edits (per plan + CLAUDE.md §8). No `init`-scaffold change (making `init` emit a `gateway-api-crds` prerequisite by default would break every fresh install until $PACKS ships that pack — deliberately deferred; see HANDOFF).
- BLOCKERS: none in this repo. The live end-to-end proof (CRDs from a prerequisite, HTTPRoute applies with no wait) needs the out-of-repo pack and is deferred to T5's e2e once a `gateway-api-crds` pack (even a local fixture) is available.
- HANDOFF — **$PACKS breaking change (traefik CRD ownership), for the packs-repo owner:**
  - ADR-0045 + the archived design (`docs/archive/superpowers/specs/2026-07-19-cube-idp-prerequisites-packs-design.md` §5.1) flag: the `packs/traefik` gateway pack TODAY ships the Gateway API CRDs as static manifests (`packs/traefik/manifests/00-gateway-api-crds.yaml`). Once a `gateway-api-crds` PREREQUISITE owns those CRDs, TWO field managers (CLI SSA for the prerequisite + flux/argocd Kustomization for the traefik pack) fight over the same objects, and removing the traefik pack could GC CRDs the prerequisite owns (prune trap).
  - REQUIRED $PACKS work (NOT done here — out of this repo's scope):
    1. Create a `gateway-api-crds` pack that renders the Gateway API CRDs (standard upstream CRDs; spec.group `gateway.networking.k8s.io`). This is what makes `pack.ProvidedGroups` light up and up.Run skip the wait.
    2. REMOVE the Gateway API CRDs from `packs/traefik` (and any other gateway pack that ships them, e.g. verify envoy-gateway) so exactly one manager owns them → **version bump** on the traefik pack (breaking: older CLIs expect the pack to carry the CRDs).
    3. Compat story: an older CLI (no `spec.prerequisites`) + a new traefik pack (no CRDs) would have no CRD source → document the minimum CLI version, or keep a transitional CRD-bearing traefik release.
    4. envoy-gateway: its controller installs Gateway CRDs at runtime — verify adopt/skip/fight behavior when the CRDs pre-exist from a prerequisite (archived design §5.2).
  - HANDOFF for T5 (e2e + docs):
    - For the e2e leg, a LOCAL fixture pack rendering a Gateway API CRD (spec.group `gateway.networking.k8s.io`) is enough to exercise the skip path end-to-end without $PACKS — mirror `internal/pack/testdata/crds-chart` or the depgraph test's `crdRendered` shape.
    - The up.go skip branch emits `con.Step("gateway-crd", "Gateway API CRDs provided by a prerequisite …")` — the e2e can assert that line (and the ABSENCE of the "waiting for the Gateway API HTTPRoute CRD" progress) as the falsifiable proof the wait was skipped.
    - Tests landed: `TestGatewayAPICRDPrerequisiteProvidesGatewayGroup` (pins the exported-constant ↔ map-key contract the up.go branch relies on), alongside T3's `TestProvidedGroupsReadsCRDGroup` / `TestResolveOrderPrerequisiteSatisfiesGatewayGroup`.
    - T5 Step 2b (added this session): reconcile ADR-0005 — its implicit-edge rule is now conditional; add the ADR-0045 cross-reference. See T5 Files.
    - `go build/vet/test ./...` all green on the branch.

#### T5 Outcome
- STATUS: · BRANCH: · COMMITS: · FINDINGS: · BLOCKERS: · HANDOFF:
