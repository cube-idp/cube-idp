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
| T2 | Pre-engine loop: `[prerequisites…, engine]` one code path; inventory + lock + Pack rows | (create) | T1 | UNCLAIMED |
| T3 | `diff` dry-run + capability-inference satisfaction for prerequisite GVKs | (create) | T2 | UNCLAIMED |
| T4 | Gateway API CRDs as a prerequisite (the first real consumer) | #25 | T2 | UNCLAIMED |
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

- [ ] **Step 1:** Factor the engine pack's `Fetch → RenderWith → SSA-wait →
  RecordInventory → cube.lock → Pack row` into a reusable per-pack pre-engine
  delivery func (the engine pack becomes its last caller).
- [ ] **Step 2:** In `up.go` after the Pack CRD step (~`:230`), loop
  `[prerequisites…, engine pack]` through that func in list order — prerequisites
  first. Same for new AND existing clusters.
- [ ] **Step 3:** Prerequisites record inventory + `cube.lock` entries + Pack rows
  identically to any pack; CLI-owned (no engine self-management for them).
- [ ] **Step 4:** Unit/integration coverage of the loop ordering + lock entries; gate
  green; commit `feat(up): apply spec.prerequisites before the engine, one code path`.

### Task T3: diff + capability inference

**Files:** `internal/diff/diff.go`, `internal/pack/depgraph.go`, tests.

- [ ] **Step 1:** Render prerequisites into the desired kernel set
  (`diff.go:91`) from the warm cache, mirroring the engine pack.
- [ ] **Step 2:** In capability inference, treat GVKs provided by prerequisites as
  satisfied (so HTTPRoute-bearing packs don't acquire phantom unresolved deps).
- [ ] **Step 3:** Tests: `diff` surfaces a prerequisite change; a pack needing a GVK a
  prerequisite provides resolves clean. Gate green; commit
  `feat(diff): render prerequisites into dry-run set; satisfy their GVKs in depgraph`.

### Task T4: Gateway API CRDs as a prerequisite (#25)

**Files:** reference config/example, `$PACKS` note (out-of-repo — record only), tests.

- [ ] **Step 1:** Provide/point at a prerequisite pack carrying the Gateway API CRDs;
  move the up-front CRD check to rely on it.
- [ ] **Step 2:** Record the `$PACKS` breaking change the ADR flags (traefik pack must
  drop the CRDs so one field-manager owns them → version bump) as a HANDOFF for the
  packs repo — do NOT edit `$PACKS` from here.
- [ ] **Step 3:** Gate green; commit `feat(prereq): Gateway API CRDs via spec.prerequisites (closes #25)`.

### Task T5: e2e + docs

**Files:** `tests/e2e/…`, `docs/reference/cube-yaml-reference.md`,
`docs/architecture/packs.md`, `docs/architecture/cluster.md`.

- [ ] **Step 1:** e2e leg: a prerequisite pack lands before the engine on a fresh
  cluster AND an existing cluster; appears in `kubectl get packs`; `down` removes it
  via the inventory cascade. (Isolated `KUBECONFIG`, foreground, one live leg —
  CLAUDE.md §8.)
- [ ] **Step 2:** Document `spec.prerequisites` in `cube-yaml-reference.md`; update the
  `docs/architecture/packs.md` + `cluster.md` `cube:doc`/`cube:section` markers, add
  `adrs=…,0045`.
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
- STATUS: · BRANCH: · COMMITS: · FINDINGS: · BLOCKERS: · HANDOFF:

#### T3 Outcome
- STATUS: · BRANCH: · COMMITS: · FINDINGS: · BLOCKERS: · HANDOFF:

#### T4 Outcome
- STATUS: · BRANCH: · COMMITS: · FINDINGS: · BLOCKERS: · HANDOFF:

#### T5 Outcome
- STATUS: · BRANCH: · COMMITS: · FINDINGS: · BLOCKERS: · HANDOFF:
