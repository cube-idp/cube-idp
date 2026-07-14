# cube-idp Phase 3 Implementation Plan

> **STATUS: RECONCILED — Task 0 EXECUTED 2026-07-14 against the live tree.** Ground-truth items 0.1–0.11 verified (drift found and fixed in task bodies: render-cluster's non-creating-provider error already exists as CUBE-0004 `CodeProviderMiss` — the CUBE-1002 allocation is dropped; `requireClusterExists` guards only `"kind"` and must learn `"k3d"` in Task 2; `cmd.Execute(ctx)` receives its signal context from `main.go`; argocd's install.yaml carries `imagePullPolicy: Always` on most containers — Task 7 must flip it; `isLocalRegistryHost` is unexported in `internal/pack`). All Owner Decisions applied to task bodies: Tasks 3/6/7 rewritten onto oras-go v2 with per-image tar bundles + `vendor --platform` (#2), pack `images:` supplement (#3), Task 10a engine-surface merge (#4), `CUBE_IDP_CA` (#5), turnkey envoy-gateway (#7), Task 3.5 = D15 substitution (#11), `pack push --also-tag latest` (#13), inventory merge-vs-replace pre-answered (#14, it MERGES), UX Tasks 14a–14c appended (#15), ghcr namespace swept to `ghcr.io/rafpe/cube-idp` (#1). Remaining `RECONCILE:` markers depend only on dependencies not yet in `go.mod` (k3d v5 API surface, envoy-gateway chart CRD schema) or on future-task outputs.
> Previous status note (kept for history): **DRAFT — PARTIALLY RECONCILED 2026-07-14 against the EXECUTED Phase 2** (commits `0522799..ccd8785`, all Phase 2 tasks complete incl. the UX addendum; execution record `.superpowers/sdd/progress.md`). This reconciliation pass fixed the CUBE-code collisions (3006→**3007**, 4011→**4015**, 1002 confirmed free), corrected the dependency posture (fluxcd/pkg/oci is GONE), pinned the gateway node port to **30443**, and added the "Phase 2 Ground Truth" section after Task 0 pre-answering checkpoints 0.1–0.11 plus a new **Task 0.5 (Phase 2 debt paydown)**. Task 0 remains blocking at execution start — it verifies the ground-truth section against the then-current tree and applies the recorded body rewrites (Task 6's lock-schema swap, Task 3's oras rewrite) before any task runs.
> **MANDATORY GATE — RECONCILE AFTER PHASE 2:** This draft was written before Phases 1 and 2 were implemented (Phase 2 exists only as its own pre-implementation draft, `2026-07-13-cube-idp-phase2-draft.md`, which will itself change during its reconciliation). Phase 3 builds on BOTH. Before executing ANY task, the consuming agent MUST complete the **"Task 0: Reconciliation Gate"** below and update this plan to match the actual post-Phase-2 codebase (including the final Phase 2 plan, which is itself a draft that will change during reconciliation). Executing a task whose reconcile checkpoint has not been verified is a plan violation.
>
> Throughout this document, `RECONCILE: …` marks a statement that depends on prior-phase implementation detail and says exactly what to verify. That is the only allowed deferral form in this plan — there are no TBDs.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. **Task 0 is blocking: no other task may start before it is checked off.**

**Goal:** Ship cube-idp Phase 3 (spec §6): the k3d cluster provider, air-gapped `vendor`/`up --bundle`, exec-plugin discovery with a sha256-pinned index, the pack catalog buildout with CI-published OCI packs, `sync --watch` live delivery (D7), and `repo create [--deploy]` — one command from empty Gitea repo to deployed.

**Architecture:** Everything here extends existing Phase 1/2 seams without new architecture: k3d is a third compiled-in `cluster.Provider` (D4/D8), vendor/bundle is a pure consumer of Phase 2's `cube.lock`, sync reuses the pack-render → OCI-push → `GitOpsEngine.Deliver` pipeline with fsnotify in front, exec plugins are tier-2 extensibility (spec §4.4 — PATH binaries, env-var contract, no RPC), and `repo create` composes the gitea pack's credentials surface with a new engine-native git delivery shape. Two deliberate interface extensions to `engine.Engine` (`Poke`, `DeliverGit`) must land in **both** engine implementations and the shared contract suite, or not at all (D2's "an abstraction with one implementation is a lie").

**Tech Stack (corrected 2026-07-14):** Go 1.26 (`go-version-file: go.mod` everywhere — never hardcode), `github.com/k3d-io/k3d/v5` (library), `github.com/fsnotify/fsnotify`, existing stack from EXECUTED Phases 1–2: cobra, cuelang, fluxcd/pkg/ssa, **oras-go v2 (the ONLY OCI library — Phase 2 Task 3.5 dropped `fluxcd/pkg/oci` AND `go-containerregistry` entirely)**, helm.sh/helm/v4 (v3 is gone), the RafPe go-getter fork (`replace github.com/hashicorp/go-getter => github.com/rafpe/go-getter v1.9.0`), go-git v5 (pin probing), smallstep/truststore, charmbracelet lipgloss+huh, client-go. `go-containerregistry` — DECIDED 2026-07-14 (Owner Decisions #2): production code is oras-go v2 ONLY; go-containerregistry is admitted as a **test-only** dependency (its in-process `registry.New()` test registry). Task 0 REWROTE Tasks 3/6/7 bodies onto oras-go v2 and switched the bundle image format to per-image tar archives with a `vendor --platform` flag.

**Spec:** `docs/superpowers/specs/2026-07-13-cube-idp-architecture-design.md` — source of truth. Phase 1 plan: `docs/superpowers/plans/2026-07-13-cube-idp-phase1-mvp.md`. Phase 2 plan (RESOLVED — EXECUTED, all tasks ticked): `docs/superpowers/plans/2026-07-13-cube-idp-phase2-draft.md`; its "Task 0 Findings" section and the ledger `.superpowers/sdd/progress.md` are the authoritative record of what actually landed.

## Owner Decisions (2026-07-14, spec-review session — BINDING; Task 0 applies the mechanical consequences)

All open questions in this plan's Self-Review, plus the review findings, were decided with the owner on 2026-07-14. The spec has been amended in lockstep (new decisions D13–D15, §3 goal re-scope, §4.1 CLI stance, §4.2 namespace). **Task 0 APPLIED every task-body consequence below on 2026-07-14** — this section remains the binding record.

1. **ghcr namespace: `ghcr.io/rafpe/cube-idp/packs/<name>`** (resolves Self-Review Q1). Applies identically to Task 5 (workflow + `config.Default`), Task 9 (`DefaultIndex` naming — moot for now per decision 8), Task 13 (README). Org migration later is a one-line ref change.
2. **OCI libraries: oras-go v2 in production code; go-containerregistry allowed in `_test.go` ONLY** (its in-process `registry.New()` test registry). Task 0 rewrites Tasks 3/6/7 bodies accordingly: `PushPackDir` and vendor pulls on oras-go v2 (`oras.Copy`), and the bundle stores **per-image tar archives** (keyed by ref) instead of one OCI layout — kind (`nodeutils.LoadImageArchive`) and k3d (`ImageImportIntoClusterMulti`) both consume tars, so the layout→archive conversion at load time disappears. `vendor` gains a `--platform` flag (default: host platform).
3. **Pack-declared runtime images (spec D14)**: `pack.cue` gains an optional `images:` list for operator-pulled images absent from rendered manifests; merged into the lock's per-pack image list (extend the LOCK package). Task 4's envoy-gateway pack declares its proxy image; Task 6 consumes. Closes the air-gap blind spot.
4. **Engine seam: merge the Task 10 and Task 12 interface extensions into ONE task** — a single "engine surface" task adds `Poke` + `DeliverGit` to `engine.Engine`, both implementations, and both contract-suite cases in one commit series; `sync` (10/11) and `repo create` (12) become pure consumers and can then proceed in parallel. Halves the passes over the most D2-sensitive code.
5. **`CUBE_IDP_REGISTRY` = the in-cluster zot URL** (plugins port-forward via `CUBE_IDP_KUBECONFIG` if needed). ADD `CUBE_IDP_CA` (path to the cube-idp CA cert) to the Task 8 env contract, and document in the README that zot is also reachable over the gateway at `https://registry.<gateway.host>` (the HTTPRoute already exists — `internal/registry/route.go`) for plugins running where that hostname resolves.
6. **`up --bundle` + `provider: existing` → reject with CUBE-7005** as drafted (resolves Self-Review Q3). Air-gapped-existing is a future plan amendment if demanded.
7. **envoy-gateway is fully turnkey** (resolves Self-Review Q4): the pack ships GatewayClass + a Gateway named `cube-idp` mirroring the traefik pack's listener/NodePort-30443 wiring, so `spec.gateway.pack: envoy-gateway` works end-to-end; Task 13 adds an e2e smoke for it.
8. **Plugin index: `--index` required, no default repo yet** (resolves Self-Review Q5). Ship the mechanism fully tested against local fixtures; create the real index repo when the first real plugin exists. Task 9's `DefaultIndex` constant is dropped in favor of the CUBE-7102 empty-check as drafted.
9. **`up` <60s (Task 0.5j) → re-scoped honestly**: spec §3 now reads "<60s warm"; warm-up is the tracked CI metric; document the `mounts:`-based node-image cache recipe. No pre-pull engineering in this phase.
10. **Interactive UX (Task 0.5k) → spec amendment D13, new workstream**: rich Bubble Tea-class experience is the DEFAULT for human/TTY runs; CI/non-TTY/`--plain` keeps byte-stable plain output (existing invariant and tests survive) plus a JSON event-stream option. **Gated on a dedicated research spike (`docs/superpowers/research/2026-07-14-cube-idp-ux-research.md`, in progress) → owner picks a proposal → design doc → implementation.** No UX implementation task may start before the owner's proposal choice.
11. **`${GATEWAY_HOST}` substitution extends to chart values and manifests (spec D15)**: small contained change in `internal/pack`, done before/with Task 4 so backstage's baseUrl derives from the configured gateway instead of hardcoding. *(Task 0 reconciliation added a second variable, `${GATEWAY_FQDN}` — bare host, no port — because Gateway API `hostnames:` fields cannot carry ports; technically required for D15 to work in HTTPRoutes. PENDING OWNER RATIFICATION as an expansion of this decision's text; flagged in the controller's next checkpoint summary.)*
12. **Execution mode: parallel streams** after Task 0/0.5 — A: providers (Tasks 1→2); B: catalog (3→3.5→4→5); C: air-gap (6→7, needs B's helpers + A for k3d image-load); D: plugins (8→9, independent); E: engine surface (Task 10a, merged Poke+DeliverGit per decision 4) then sync (10→11) ∥ repo create (12); F: UX (14a design doc → 14b stage A → 14c stage B). Task 13 closes. Streams A/B/D/E-surface/F-design can start concurrently.
13. **Task 5 double-push resolved** (recommended, unobjected): `pack push` gains `--also-tag latest` (one push, two tags); the sed/version-extraction re-push in the workflow is deleted.
14. **Sync inventory semantics — new reconcile checkpoint for the sync task**: verify whether `Applier.RecordInventory` merges or replaces the cube's inventory before shipping `SyncOnce`; if it replaces, sync would orphan `up`-applied entries and `down` would leak them. Must be merge (or made so) — named check, not an assumption.
15. **UX stream decisions (2026-07-14, after the research spike — `docs/superpowers/research/2026-07-14-cube-idp-ux-research.md`):**
    - **Proposal B, staged A→B** ("One console"): ship A first (typed event stream in `internal/ui/event`, PlainRenderer = today's byte-pinned output with all existing tests untouched, LiveRenderer = Bubble Tea v2 **inline mode** step-tree for `up`/`down`, JSONRenderer = JSON-lines events), then B's wave (rich static `status`/`doctor` renders, full huh v2 `init` wizard, JSON documents for `status`/`doctor`/`get secrets`). No resident/alt-screen views (C explicitly rejected). Diagnosis-last rule: the CUBE-xxxx panel always renders after the TUI releases the terminal.
    - **Flag surface**: BuildKit-style `--progress=auto|plain|live|json` single knob + `CUBE_IDP_PROGRESS` env policy; `--plain` kept permanently as an alias for `--progress=plain`.
    - **Charm v2 migration** (`github.com/charmbracelet/*` v1 → `charm.land/*` v2) lands in the SAME PR as the first live view — no separate mechanical migration.
    - Defaults applied without objection: `Resolve` hardened with `NO_COLOR`/`TERM=dumb` (may land before the TUI — it's a gap today); JSON event schema labeled **experimental** until the D5 v1 freeze; Access summary becomes a JSON data event and plain mode gains stable access lines (one deliberate test update); `sync --watch` gets B's single-pane rolling view when Phase 3 builds it.
    - Sequencing: the UX stream's first deliverable is its **design doc** (from the research §3 architecture); stage A implementation may then proceed in parallel with the other streams.

## Global Constraints (every task inherits these)

- Module path: `github.com/rafpe/cube-idp` (verified in `go.mod` 2026-07-14; go 1.26.2, oras-go v2 v2.6.2, helm v4, kind v0.32.0, go-getter → rafpe fork v1.9.0 replace all present).
- Single static binary; nothing runs on the developer machine after exit (spec §3). `sync --watch` is the one sanctioned long-running *foreground* mode — it is still not a daemon: Ctrl-C exits cleanly and leaves only in-cluster state.
- Config: `apiVersion: cube-idp.dev/v1alpha1`, `kind: Cube` (D5). Users author YAML; CUE is internal only.
- Every user-facing failure is a typed `CUBE-xxxx` `diag.Error` with remediation (spec §4.5); every wait has a hard deadline; no silent fallbacks.
- SSA field manager `cube-idp`; cube label `cube-idp.dev/cube: <name>`; prune opt-out `cube-idp.dev/prune: disabled`; system namespace `cube-idp-system`. **CLI-secret labels are DEPRECATED as of Phase 2 (D11):** the primary credentials surface is the Pack record's `expose.authSecretRef` (`kubectl get packs`); the labels are honored for one release only — new Phase 3 packs (Task 4) declare `expose:` blocks, not labels.
- **CUBE-code catalog + literal-ban (Phase 2 Task 0.5, enforced by test):** every code is a constant in `internal/diag/codes.go`; NO `"CUBE-` string literal may appear in non-test Go code — `TestNoCubeLiteralsOutsideCatalog` fails the build otherwise. This plan's code snippets show literals for readability; implementers MUST add catalog constants and use them.
- **Plain-output invariant (Phase 2 Task 13.8):** command output goes through `internal/ui` (`Printer.Step`, `Section`, `Glyph`, `Progress`); plain mode (non-TTY / `CI` / `--plain` persistent flag) is byte-stable and many tests assert on it. New Phase 3 commands (`pack push`, `vendor`, `plugin`, `sync`, `repo create`) adopt the same helpers from day one; `sync --watch`'s live output must degrade to plain line-per-event when piped.
- **CUBE code ranges:** `0xxx` preflight/config, `1xxx` cluster, `2xxx` apply, `3xxx` engine, `4xxx` pack, `5xxx` registry (Phase 1); `6xxx` **reserved for Phase 2 trust/hostname — do not allocate here**; `7xxx` **NEW in this phase: exec-plugins / sync / vendor-bundle**.
- **New CUBE codes introduced by this plan** (documented here per convention; each defined at its point of use):
  - ~~`CUBE-1002`~~ DROPPED by Task 0 (2026-07-14): `config render-cluster` on a provider that creates no cluster is ALREADY typed as **CUBE-0004 `CodeProviderMiss`** ("cluster provider config mismatch (e.g., render-cluster for non-kind)") and used in the shipped `cmd/config.go` — Task 2 reuses that constant; 1002 stays unallocated. In-use 1xxx codes verified: 1001 1003 1004 1101 1102 1201–1205. · `CUBE-1301` k3d merged-config conflict (D10) · `CUBE-1302` invalid k3d providerConfig · `CUBE-1303` k3d create failed / runtime unreachable · `CUBE-1304` k3d kubeconfig retrieval failed · `CUBE-1305` k3d delete failed
  - `CUBE-3007` engine does not support the requested delivery shape (git source) — defensive; both shipped engines must support it. (RENUMBERED by the 2026-07-14 reconciliation: this plan originally claimed 3006, but Phase 2 reserved CUBE-3006 for the argocd OCI-capability failure — constant `CodeEngineArgocdRegFail`, currently unused but reserved. 3005 is the PHASE 1 flux Uninstall prune-timeout, not an argocd code.)
  - `CUBE-4015` pack directory push failed (`pack push`). (RENUMBERED by the 2026-07-14 reconciliation: 4001–4014 are ALL claimed — 4001–4005/4012/4013 by Phase 1, 4006–4011/4014 by Phase 2 executed code: git+getter sources 4006/4007, kustomize 4008, cnoe 4009/4010, D11 expose 4011, extraction guards 4014.)
  - `CUBE-7001` `cube.lock` missing/unreadable (vendor needs it) · `CUBE-7002` vendor pull failed (artifact or image) · `CUBE-7003` bundle unreadable/corrupt · `CUBE-7004` bundle incomplete vs `cube.lock` (missing entry or digest mismatch) · `CUBE-7005` `--bundle` unsupported for this cluster provider configuration
  - `CUBE-7101` plugin not found · `CUBE-7102` plugin index fetch failed or sha256 mismatch · `CUBE-7103` plugin failed to execute · `CUBE-7104` plugin not trusted
  - `CUBE-7201` sync path is not renderable · `CUBE-7202` watch setup failed (fsnotify/inotify)
  - `CUBE-7301` gitea not available (pack absent or admin secret not found) · `CUBE-7302` Gitea API call failed · `CUBE-7303` `--deploy` source registration failed
- Conventional commits (`feat:`, `test:`, `chore:`); each task ends committed with `go build ./... && go test ./... -short` green.
- TDD: failing test → run → minimal implementation → run → commit, per step.
- New dependencies allowed in this phase: `github.com/k3d-io/k3d/v5`, `github.com/fsnotify/fsnotify`, `github.com/google/go-containerregistry` (**test-only** — `_test.go` files, per Owner Decisions #2; production OCI code stays pure oras-go v2). UX stream (Owner Decisions #10) may add charmbracelet deps per its approved design doc. Nothing else without a plan change.

## File Structure (new/modified in Phase 3)

```
internal/cluster/contracttest/      # Task 1: shared ClusterProvider contract suite (spec §5)
  contracttest.go
internal/cluster/k3dp/              # Task 2: k3d provider (D4) + D10 merge + D12 registries wiring
  k3d.go  merge.go  merge_test.go  testdata/
internal/oci/                       # Task 3: pack-directory push (oras-go v2, mirror of Fetch's pull)
  pushdir.go  pushdir_test.go
cmd/pack.go                         # Task 3: `cube-idp pack push [--also-tag latest]`
internal/pack/                      # Task 3.5 (D15): ${GATEWAY_HOST} over chart values + manifests
  (substitution in render path; shared helper with expose.go)
packs/backstage/  packs/cert-manager/  packs/external-secrets/  packs/envoy-gateway/
                                    # Task 4: catalog packs (data only; envoy-gateway is turnkey:
                                    #   GatewayClass + Gateway `cube-idp` + images: declaration)
.github/workflows/release-packs.yaml# Task 5: CI publishes packs to ghcr.io/rafpe/cube-idp
internal/lock/                      # Task 6 prep (D14): merge pack-declared images: into Entry.Images
internal/bundle/                    # Tasks 6–7: vendor + bundle model (per-image tar archives)
  bundle.go  vendor.go  load.go  bundle_test.go  testdata/
cmd/vendor.go                       # Task 6: `cube-idp vendor [--platform]`
internal/plugin/                    # Tasks 8–9: exec-plugin discovery, trust, index
  discover.go  exec.go  trust.go  index.go  plugin_test.go  index_test.go
cmd/plugin.go                       # Tasks 8–9: `cube-idp plugin list|trust|install`
internal/engine/                    # Task 10a: engine surface — Poke + DeliverGit in engine.go,
                                    #   flux/, argocd/, and contract/ (both cases, both engines)
internal/kube/portforward.go        # Task 10: generic port-forward (Task 12 reuses)
internal/syncer/                    # Tasks 10–11: `sync` one-shot + --watch
  syncer.go  watch.go  syncer_test.go
cmd/sync.go
internal/gitea/                     # Task 12: minimal Gitea API client
  client.go  client_test.go
cmd/repo.go                         # Task 12: `cube-idp repo create`
tests/e2e/                          # Task 13: matrix + new-surface e2e (+ envoy-gateway smoke)
.github/workflows/ci.yaml           # Task 13: {kind, k3d} × {flux, argocd} matrix
docs/superpowers/specs/…-cube-idp-ux-design.md  # Task 14a: UX design doc (from research §3)
internal/ui/event/                  # Task 14b: typed event stream + renderers (stage A)
```

Modified (paths verified against the real tree 2026-07-14): `internal/config/schema.cue` + `internal/config/types.go` docs (k3d enum), `internal/cluster/provider.go` (factory + `GatewayNodePort` const), `cmd/config.go` (render-cluster provider switch), `cmd/root.go` (`requireClusterExists` learns k3d; command registration; plugin fallthrough lives in `Execute(ctx)` — signal wiring stays in `main.go`), `internal/engine/engine.go` + `internal/engine/flux/` + `internal/engine/argocd/` + `internal/engine/contract/contract.go` (`Poke`, `DeliverGit` — Task 10a), `internal/up/up.go` (`--bundle` options; lock-entry image union for D14), `cmd/up.go`, `internal/registry/portforward.go` (delegates to generic forward), `internal/config/types.go` `Default` refs (ghcr namespace), `hack/gen-argocd-manifests.sh` (imagePullPolicy flip, Task 7), `README.md`.

---

### Task 0: Reconciliation Gate (mandatory, blocking)

**Files:**
- Modify: **this plan file** — every divergence found below must be edited into the affected tasks before they run.

**Interfaces:**
- Consumes: the real post-Phase-2 codebase and the final Phase 2 plan.
- Produces: a reconciled Phase 3 plan. Nothing else. No product code is written in this task.

Work through every checkbox. For each: open the named files, compare against what this plan assumes, and if reality differs, **edit the affected tasks below before proceeding**. Record a one-line note per item (verified / diverged→fixed) in the commit message of the plan update.

- [x] *(2026-07-14: VERIFIED — interface/factory exactly as ground truth; `kindp.RenderConfig` takes the 4th `CertsD` param; one extra drift found: `cmd/root.go`'s `requireClusterExists` guards only `"kind"` — Task 2 extends it to k3d.)* **0.1 — `cluster.Provider` signature and the `kube.Conn` leaf type.** Read `internal/cluster/provider.go` and `internal/kube/` (Phase 1 Task 4/5 planned `Ensure(ctx, name string, spec config.ClusterSpec) (*kube.Conn, error)`, `Delete`, `Exists`, `Kubeconfig`, `Diagnose(ctx, name) []diag.Finding`, factory `cluster.New(spec, gw)` with `CUBE-1001`, and moved `Conn` into `internal/kube`). Read `internal/cluster/kindp/kind.go` + `merge.go` for how kindp implements it — the k3d provider (Task 2) must mirror the exact same shape, including how `RenderConfig` is kept pure and how `Ensure` is made idempotent. Update the affected tasks below if reality differs.
- [x] *(2026-07-14: VERIFIED — `Fetch` dispatch, digest-keyed `pullOCI` cache, `Pinned`/`Expose` fields, `FetchTree`, `DefaultCacheDir` all as ground truth; `extractManifest` consumes BOTH flux-style tar.gz layers and oras per-file title layers, so Task 3's flux-shaped push round-trips; `pullOCI` accepts `@digest` refs (`repo.Reference.Reference`), so Task 6's digest pulls need no source.go change.)* **0.2 — Pack source resolver, including the Phase-2 git-ref source.** Read `internal/pack/source.go` (Phase 1: `Fetch(ctx, ref, cacheDir)` handling local dir + `oci://` via oras-go, `CUBE-4001` for unknown schemes) and whatever Phase 2 added for `github.com/org/repo//path@vX` git refs. Note the exact on-disk cache layout `pullOCI` produces and whether OCI pack artifacts are Flux-style gzipped tarball layers — Task 3's `pack push` must produce artifacts `Fetch` can consume, and Task 6's bundle stores what `Fetch` reads. Update the affected tasks below if reality differs.
- [x] *(2026-07-14: VERIFIED — `lock.File{APIVersion, Kind, Engine EngineLock{Type}, Packs []Entry{Ref, Name, Version, Resolved, RenderedHash, Images}}` + `PathFor/Write/Read/RenderedHash/ImagesFrom` exactly as ground truth; NOTE the lock carries NO cube name — Task 6's bundle `Manifest.Cube` field is dropped. Task 6's bodies rewritten onto the real package.)* **0.3 — `cube.lock` format (Phase 2).** Find the lockfile writer/reader (Phase 2 scope: "`cube.lock` + `upgrade --plan`"; spec §4.1: "pins recorded in cube.lock (digests + full image list)"). Task 6 assumes a Go type it can import (this plan proposes `lock.File` with per-pack `Ref`, `Digest`, `Images []string` plus engine/registry image pins) — replace Task 6's assumed schema with the real one, reuse the real package (do NOT redefine lock parsing in `internal/bundle`). Update the affected tasks below if reality differs.
- [x] *(2026-07-14: VERIFIED — `internal/engine/contract` with `Impl{Name, New}`/`Run(t, impl)`, envtest gate on `KUBEBUILDER_ASSETS`, factory at `internal/engine/factory`; Engine = Install/InstallManifests/Deliver/Health/Uninstall; both engines name deliveries `cube-idp-<pack>` (flux in ns `flux-system`, argocd Application in ns `argocd`). Poke/DeliverGit extensions consolidated into Task 10a per Owner Decisions #4.)* **0.4 — Engine contract-test suite location and shape.** Phase 2 built the shared `GitOpsEngine` contract suite (D2). Find it (likely `internal/engine/contracttest/` or similar), note how a suite run is wired for flux and argocd, and how it obtains an `apply.Applier`/envtest. Tasks 10 and 12 extend the `Engine` interface (`Poke`, `DeliverGit`) — those extensions MUST be added to this suite and pass for **both** engines. Also confirm the factory package (`internal/engine/factory` per Phase 1 Task 9's import-cycle note) and the exact `Engine` method set after Phase 2. Update the affected tasks below if reality differs.
- [x] *(2026-07-14: VERIFIED — `diag.Error/New/Wrap/Render`, `Finding{Code, Severity, Message, Remediation}`, `SeverityError/Warning/Info` constants; doctor ships `CheckRuntime/CheckPortFree/CheckDiskSpace/CheckInotify/CheckGitCLI` + `Render`; provider `Diagnose` findings surface in `cmd/doctor.go` automatically.)* **0.5 — Doctor / diag surface.** Read `internal/diag/` and the Phase 2 `doctor` command. Confirm `diag.Error{Code, Summary, Cause, Remediation}`, `diag.New/Wrap/Render`, `diag.Finding` are as Phase 1 planned, and note any Phase 2 additions (e.g. a preflight registry that Task 2's k3d provider and Task 8's plugin runner should feed findings into). Update the affected tasks below if reality differs.
- [x] *(2026-07-14: VERIFIED with one correction — `NewRootCmd()` + `root.AddCommand(newXCmd())` + persistent `--plain` via `PersistentPreRunE` as ground truth, BUT the signal wiring lives in `main.go` (`signal.NotifyContext` there), and `cmd.Execute(ctx context.Context) error` just calls `NewRootCmd().ExecuteContext(ctx)` — Task 8's fallthrough snippet rewritten to that real shape. `-f/--file` default `cube.yaml` confirmed.)* **0.6 — cmd/ registration pattern.** Read `cmd/root.go`: Phase 1 planned `NewRootCmd()` + `root.AddCommand(newXCmd())` per command file, `Execute`/`ExecuteContext` with `signal.NotifyContext`. Task 8 wraps `Execute` for plugin fallthrough — confirm the exact current shape. Also confirm the `-f/--file cube.yaml` flag convention. Update the affected tasks below if reality differs.
- [x] *(2026-07-14: VERIFIED — `module github.com/rafpe/cube-idp`, go 1.26.2.)* **0.7 — Module path.** `go.mod` must say `github.com/rafpe/cube-idp`; all code blocks below import that path. Update the affected tasks below if reality differs.
- [x] *(2026-07-14: VERIFIED — traefik chart pins `ports.websecure.nodePort: 30443` (host `gw.Port` → 30443 HTTPS) and `web.nodePort: 30080` (in-cluster only); Gateway `cube-idp` ns `traefik`, listeners 8000/HTTP + 8443/HTTPS with cert `cube-idp-gateway-tls` — the TLS secret is issued by `up` into ns `gw.Pack` (`internal/up/tls.go`), and CoreDNS rewrites to `<gw.Pack>.<gw.Pack>.svc.cluster.local` — both constrain Task 4's turnkey envoy pack (namespace + service-name parity, spelled out there).)* **0.8 — Gateway / port decisions.** Phase 1 Task 5 defined `gatewayContainerPort = 443` but Task 12's traefik-pack note switched the design to NodePort `30080` (`gatewayContainerPort` → 30080, traefik service `type: NodePort`, `nodePorts.web: 30080`); Phase 2 added `trust` + HTTPS. Read `internal/cluster/kindp/merge.go` and `packs/traefik/` to learn the FINAL host-port → node-port wiring and whether the gateway listener is HTTP or HTTPS now. Task 2 (k3d port mapping), Task 12 (printed clone URLs), and Task 13 (e2e assertions) all depend on it. Update the affected tasks below if reality differs.
- [x] *(2026-07-14: VERIFIED with one nuance — `PushRendered(ctx, r, registryAddr)` over the `pushRenderedTo(ctx, r, oras.Target)` seam and flux media types exactly as ground truth, but `push.go` sets `PlainHTTP = true` UNCONDITIONALLY (its only production target is the 127.0.0.1 zot tunnel); the host-gated `isLocalRegistryHost` lives UNEXPORTED in `internal/pack/source.go` (shared by `pullOCI` + `ResolveRemote`, strips `:port` so `127.0.0.1:<any>` matches). Task 3's `PushPackDir` — which pushes to real registries — adds its own copy of that gate in `internal/oci`.)* **0.9 — OCI artifact push wrapper.** Read `internal/oci/push.go` (Phase 1: `PushRendered(ctx, r *pack.Rendered, registryAddr string) (engine.ArtifactRef, error)` over `fluxcd/pkg/oci` client, plain-HTTP for `127.0.0.1`) and any Phase 2 changes (auth options?). Tasks 3, 5, 10, 11 reuse this wrapper — `sync --watch` pushes through exactly this path. Note the actual fluxcd/pkg/oci client option names for insecure/plain-HTTP and auth. Update the affected tasks below if reality differs.
- [x] *(2026-07-14: VERIFIED — Secret `gitea-admin-cube-idp` ns `gitea` with `cube-idp.dev/cli-secret` + `pack-name` labels, keys `username`/`password`; `expose:` block with `https://gitea.${GATEWAY_HOST}`, `impliedFields.username: gitea_admin`; chart 12.6.0 release `gitea` → Service **`gitea-http:3000`** (chart default, not overridden); HTTPRoute `gitea` backendRef `gitea-http:3000` — and it writes every schema-defaulted field EXPLICITLY (argocd SSA-diff rule) — Task 4's new HTTPRoutes must copy that style, done.)* **0.10 — Gitea pack service/credentials surface.** Read `packs/gitea/` as shipped: the admin Secret name/namespace/labels (Phase 1 planned `gitea-admin-cube-idp` in ns `gitea`, keys `username`/`password`, labels `cube-idp.dev/cli-secret` + `pack-name`), the HTTP Service name/port (planned `gitea-http:3000`), and the HTTPRoute hostname (`gitea.cube-idp.localtest.me`). Task 12's `repo create` consumes all three. Also check whether Phase 2's CoreDNS/trust work changed in-cluster or host-facing git URLs. Update the affected tasks below if reality differs.
- [x] *(2026-07-14: VERIFIED — no provider contract suite exists (Task 1 stands); `render-cluster` is kindp-only AND its non-kind error is the existing CUBE-0004 `CodeProviderMiss` (drift → Task 2 reuses it, CUBE-1002 dropped); `lock.ImagesFrom` + `registry.Manifests()` exist (Task 6 consumes); lock entries are assembled in `internal/up/up.go` (~line 190) — that is Decision #3's merge point; `ui` package + helm v4 confirmed.)* **0.11 — Phase 2 leftovers that collide with this plan.** Grep the Phase 2 plan and code for anything already covering: provider contract tests (Task 1 may partially exist), `config render-cluster` generalization, image-list extraction for the lockfile (Task 6 needs the image list — if Phase 2's lock already records images per pack, Task 6 only consumes; if not, Task 6 grows an image-extraction step and the lock schema must be extended IN THE LOCK PACKAGE, not in bundle). Update the affected tasks below if reality differs.
- [x] **0.12 — Commit the reconciled plan.** *(2026-07-14: committed as "docs: phase 3 Task 0 — reconciliation gate complete, owner decisions applied to task bodies".)*

```bash
git add docs/superpowers/plans/2026-07-13-cube-idp-phase3-draft.md
git commit -m "docs: reconcile phase 3 plan against post-phase-2 codebase"
```

### Phase 2 Ground Truth (pre-answered 2026-07-14, from the executed Phase 2 — Task 0 verifies these against the then-current tree and applies the recorded body rewrites)

- **0.1 (Provider seam):** interface exactly `Ensure(ctx, name string, spec config.ClusterSpec) (*kube.Conn, error)` / `Delete` / `Exists` / `Kubeconfig` / `Diagnose(ctx, name) []diag.Finding`; factory `cluster.New(spec, gw)`. **CHANGED by Phase 2 Task 10:** `kindp.RenderConfig(name, spec, gw, certsd kindp.CertsD)` — a fourth param `CertsD{Host, HostDir}` injects the containerd `certs.d` mount + `config_path` patch; `kindp.Ensure` builds it via `trust.Dir()` + `trust.EnsureCA` + `trust.WriteCertsD(dir, "registry."+gw.Host, "http://localhost:30500", ca.CertPath)`. **Task 2's k3d provider MUST implement the equivalent D12 wiring** (k3d-native: a `registries.yaml` entry mapping `registry.<host>` → the node-local zot NodePort 30500 with the cube-idp CA, or k3d's registry-config mount) — this requirement did not exist when this draft was written. `cmd/config.go`'s render-cluster passes a zero `CertsD{}` (stays file-free) — k3d's render path mirrors that.
- **0.2 (pack sources):** `Fetch` dispatches: local dir (pin `dir:<dirhash>` via `dirPin`), `oci://` (oras-go v2 `pullOCI` → digest-keyed cache, pin `oci:<digest>`), bare git grammar `host/org/repo[//sub]@rev` (pin-first `resolveGitPin` ls-remote, fetch via the **RafPe go-getter fork v1.9.0** — git getter shells out to the git CLI; doctor warns via CUBE-0105), explicit getter refs `git::`/`s3::`/`http(s)://` (pin `dir:<dirhash>`). ALL getter output passes `GuardTree` (symlinks stripped, CUBE-4014) with atomic fetch→guard→rename caching; `pack.FetchTree` fetches a plain git tree WITHOUT pack.cue (cnoe uses it); `pack.DefaultCacheDir()` is the cache root (CUBE-4013); `Pack` has `Pinned` and `Expose` fields. Task 3's push→Fetch round-trip target is `pullOCI`'s consumption format — read `internal/pack/source.go` for the exact layer handling (cube-idp's own untar: dirs+regular files only, symlinks dropped).
- **0.3 (cube.lock):** real schema recorded in Task 6's Consumes block above — `Entry.Resolved` (typed pin string), `RenderedHash`, per-pack `Images`; NO engine/registry pins (derive via `lock.ImagesFrom` over `eng.InstallManifests()` + `registry.Manifests()`); `lock.Read` returns `(nil, nil)` when missing, CUBE-0003 when corrupt.
- **0.4 (engine seam):** `Engine` = `Install / InstallManifests() / Deliver / Health / Uninstall` — **InstallManifests is an interface METHOD**. Contract suite: `internal/engine/contract`, `contract.Impl{Name string; New func() engine.Engine}`, `contract.Run(t, impl)`; envtest subtests skip without `KUBEBUILDER_ASSETS`, run via `make test-engines`. Factory `internal/engine/factory.New(typ)`. `Poke`/`DeliverGit` additions → both impls + new `contract.Run` subtests, byte-identical. NOTE: argocd's vendored `install.yaml` carries a hand-added `reposerver.oci.layer.media.types` key in `argocd-cmd-params-cm` and a `defaultNamespace` stamp — `hack/gen-argocd-manifests.sh` regeneration DROPS both (guard in Task 0.5 below).
- **0.5 (diag/doctor):** `diag` API as planned + `SeverityInfo`; catalog + literal-ban per Global Constraints. `internal/doctor` exists: `CheckRuntime/CheckPortFree(dial-probe)/CheckDiskSpace/CheckInotify/CheckGitCLI` + provider `Diagnose` + engine `Health` in `cmd/doctor.go`; k3d's `Diagnose` findings surface there automatically; the plugin runner (Task 8) can append checks.
- **0.6 (cmd):** `NewRootCmd()` + `newXCmd()` per file; `-f/--file` default `cube.yaml`; `ExecuteContext`. **Phase 2 added a persistent `--plain` flag whose `PersistentPreRunE` sets `ui.PlainFlag`** — Task 8's plugin-fallthrough wrapper around Execute must preserve that hook (and pass `--plain` through to plugins via the env contract if useful). `requireClusterExists` (CUBE-1004) guards read-only commands — `sync`, `repo create`, `vendor` follow it.
- **0.7:** module `github.com/rafpe/cube-idp` confirmed.
- **0.8 (gateway/ports):** FINAL: host `gateway.port` (default 8443) → node port **30443** = traefik `websecure` (HTTPS, CA-issued cert `cube-idp-gateway-tls`, D12); `web`/30080 exists in the chart but is not the host mapping. `${GATEWAY_HOST}` in `expose:` URLs expands to `host[:port]` (port omitted at 443) via `pack.ExposeURLs`/`PackObject`. e2e honors `CUBE_IDP_E2E_GATEWAY_PORT` (local squatter on 8443). Task 12's printed clone URLs are `https://gitea.<host>:<port>/...` (real TLS).
- **0.9 (OCI push):** `internal/oci` is pure oras-go v2: `PushRendered(ctx, r, registryAddr)` unchanged signature over a `pushRenderedTo(ctx, r, oras.Target)` seam; flux media types preserved (config `application/vnd.cncf.flux.config.v1+json`, single layer `application/vnd.cncf.flux.content.v1.tar+gzip` containing `all.yaml`); PlainHTTP ONLY for 127.0.0.1/localhost via `isLocalRegistryHost` (shared with `pullOCI` and `ResolveRemote`; Task 0 correction: the helper lives UNEXPORTED in `internal/pack/source.go`, and `push.go` itself sets PlainHTTP unconditionally since its only production target is the local tunnel — Task 3 adds its own gate in `internal/oci`). Tasks 3/5/10/11 build on these; the draft's former `fluxoci.*` snippets were rewritten onto oras-go v2 by Task 0.
- **0.10 (gitea surface):** admin Secret `gitea-admin-cube-idp` ns `gitea` (legacy labels still present, one release); the pack's `expose:` block declares `urls: ["https://gitea.${GATEWAY_HOST}"]`, `authSecretRef {gitea, gitea-admin-cube-idp}`, `impliedFields.username: gitea_admin` — Task 12 can read the Pack record or the Secret directly. In-cluster, CoreDNS rewrites `*.<host>` → the gateway Service, and kind nodes pull `registry.<host>` via certs.d. **Task 12 decision recorded:** engine git sources for `--deploy` should use the in-cluster HTTP Service URL (`http://gitea-http.gitea.svc...` — verify exact Service name/port in packs/gitea manifests) rather than the TLS gateway URL, so source-controller/argocd need no CA distribution; the PRINTED operator clone URL uses the https gateway form.
- **0.11 (collisions/leftovers):** provider contract suite does NOT exist (Task 1 stands as written); `config render-cluster` exists kindp-only; image extraction EXISTS (`lock.ImagesFrom` — Task 6 consumes, do not reimplement); `ui` package exists (Global Constraints bullet); helm v4 is the SDK (catalog packs' charts render through it — `chartutil` renamed to `common` for ParseKubeVersion, see internal/pack/helm.go). Phase 2's review backlog is Task 0.5 below.

### Task 0.5: Phase 2 debt paydown (from the executed Phase 2's review ledger — small, mechanical unless marked)

**Files:** as named per item. Each item = failing test (where testable) → fix → `go build ./... && go vet ./... && go test ./... -short` → one commit per coherent group.

- [ ] **(a) Guard the argocd manifest regen:** `hack/gen-argocd-manifests.sh` must itself inject the `reposerver.oci.layer.media.types` cmd-params-cm key and the Namespace prepend it currently relies on hand-edits for — regenerate and diff-verify the committed install.yaml is reproducible; add a CI-runnable check (`hack/gen-argocd-manifests.sh --check` or a test comparing script output to the committed file).
- [ ] **(b) `internal/diff` desiredState unit test:** table test pinning the desired-set assembly + identity-stub list against `up`'s applied set (the false-orphan regression net; today only the e2e covers it).
- [ ] **(c) trust command coverage:** tests for `trust --uninstall`, `--yes`, and down's revert path (seams exist: `trustInstall`/`trustUninstall`/`trustDir`).
- [ ] **(d) ban-test scope:** `internal/diag/codes_test.go` exempts any file NAMED codes.go — anchor the exemption to the exact path `internal/diag/codes.go`.
- [ ] **(e) kustomization stat granularity:** `internal/pack/render.go` treats any `os.Stat(kustomization.yaml)` error as absent — distinguish `fs.ErrNotExist`; surface other errors.
- [ ] **(f) getter cache hardening:** `sanitizeRef`/subdir key uses `_` as separator (theoretical `a/b` vs `a_b` collision) — use an unambiguous encoding; OPTIONAL: cross-process cache lockfile (only if Phase 3's `sync --watch` makes concurrent runs plausible).
- [ ] **(g) diff blind spot:** the CoreDNS rewrite is outside `diff`'s model — either add a check or narrow `internal/diff/diff.go`'s doc claim; same note for `cmd/config.go render-cluster` output (print that certs.d is injected at up-time).
- [ ] **(h) message polish:** `cmd/trust.go` consent prompt hardcodes `cube-idp.localtest.me` — use the configured `gateway.host` (load via `-f` or accept the generic wording deliberately).
- [ ] **(i) CUBE-3006 constant:** `internal/diag/codes.go` keeps `CodeEngineArgocdRegFail` reserved-unused — update its comment to say "reserved: argocd gitea-fallback capability check (spec §7), unbuilt by design"; Phase 3 allocates 3007 for the delivery-shape error.
- [ ] **(j) RESOLVED 2026-07-14 (Owner Decisions #9) — `up` wall time vs spec §3's <60s:** re-scoped honestly. Spec §3 now says "<60s warm"; remaining work here: make warm-up the tracked CI metric and write the `mounts:` node-image cache recipe into the README. No pre-pull engineering.
- [ ] **(k) RESOLVED 2026-07-14 (Owner Decisions #10, spec D13) — rich UX by default:** spec amended. Remaining work here: none in this task — the UX overhaul is its own workstream (research spike → owner picks a proposal → design doc → implementation), gated on the owner's proposal choice.

---

### Task 1: Shared ClusterProvider contract suite

**Reconcile checkpoint:** requires 0.1 (Provider signature, `kube.Conn`, factory — Phase 1), 0.5 (diag surface — Phase 1/2), 0.11 (does any provider contract test already exist from Phase 2?).

Spec §5: "one shared suite run against every `ClusterProvider` … implementation." Phase 1 shipped two providers with per-provider tests but no shared suite; the k3d provider (Task 2) is the second *cluster-creating* provider, which is the moment the contract must exist (same D2 logic as engines). The suite has two halves: a **pure half** (factory behavior, always runs) and a **live half** (real cluster lifecycle, gated by `CUBE_IDP_PROVIDER_E2E=1` because it needs a container runtime).

**Files:**
- Create: `internal/cluster/contracttest/contracttest.go`
- Test: `internal/cluster/kindp/contract_test.go` (kind is the first consumer; k3d joins in Task 2; `existing` gets only the pure half — it cannot create clusters)

**Interfaces:**
- Consumes: `cluster.Provider`, `kube.Conn`, `config.ClusterSpec`, `diag`.
- Produces:

```go
package contracttest

// Run exercises the full provider lifecycle contract against a real runtime.
// Callers gate it themselves is NOT allowed — Run self-gates on
// CUBE_IDP_PROVIDER_E2E=1 so every provider package can call it
// unconditionally from a normal test.
func Run(t *testing.T, p cluster.Provider, spec config.ClusterSpec)
```

- [ ] **Step 1: Write the suite (it IS the test — there is no implementation to fail against first; TDD here means the suite must fail against a deliberately broken fake before trusting it)**

`internal/cluster/contracttest/contracttest.go`:

```go
// Package contracttest is the shared ClusterProvider contract (spec §5).
// Every cluster-creating provider (kindp, k3dp, future Talos/vcluster) calls
// Run from its own test package. The contract is behavioral: idempotent
// Ensure, truthful Exists, non-empty Kubeconfig for a live cluster, clean
// Delete, and a Diagnose that never panics.
package contracttest

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/rafpe/cube-idp/internal/cluster"
	"github.com/rafpe/cube-idp/internal/config"
	"github.com/rafpe/cube-idp/internal/diag"
)

const gate = "CUBE_IDP_PROVIDER_E2E"

func Run(t *testing.T, p cluster.Provider, spec config.ClusterSpec) {
	t.Helper()
	if os.Getenv(gate) != "1" {
		t.Skipf("set %s=1 to run the live provider contract (needs a container runtime)", gate)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute) // deadline rule
	defer cancel()
	name := "contract-" + spec.Provider

	// Pre-state: the cluster must not exist.
	if ok, err := p.Exists(ctx, name); err != nil || ok {
		t.Fatalf("pre-state Exists = %v, %v (leftover cluster? delete it first)", ok, err)
	}

	// Ensure creates…
	conn, err := p.Ensure(ctx, name, spec)
	if err != nil {
		t.Fatalf("Ensure (create): %v", err)
	}
	t.Cleanup(func() { _ = p.Delete(context.Background(), name) }) // never leak clusters
	if conn == nil || len(conn.Kubeconfig) == 0 || conn.REST == nil {
		t.Fatalf("Ensure returned an unusable Conn: %+v", conn)
	}

	// …and is idempotent.
	if _, err := p.Ensure(ctx, name, spec); err != nil {
		t.Fatalf("Ensure (idempotent re-run): %v", err)
	}
	if ok, err := p.Exists(ctx, name); err != nil || !ok {
		t.Fatalf("Exists after Ensure = %v, %v", ok, err)
	}

	// Kubeconfig for a live cluster is retrievable independently of Ensure.
	kc, err := p.Kubeconfig(ctx, name)
	if err != nil || len(kc) == 0 {
		t.Fatalf("Kubeconfig: %v (len %d)", err, len(kc))
	}

	// Diagnose never panics and returns no error-severity findings on a
	// healthy cluster. (diag.SeverityError verified 2026-07-14 — import
	// github.com/rafpe/cube-idp/internal/diag.)
	for _, f := range p.Diagnose(ctx, name) {
		if f.Severity == diag.SeverityError {
			t.Fatalf("Diagnose reported an error on a healthy cluster: %+v", f)
		}
	}

	// Delete tears down; Exists goes false.
	if err := p.Delete(ctx, name); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if ok, _ := p.Exists(ctx, name); ok {
		t.Fatal("Exists still true after Delete")
	}
}
```

RESOLVED 2026-07-14: `diag.SeverityError Severity = "error"` exists (`internal/diag/diag.go`) — the snippet above already compares against the constant.

- [ ] **Step 2: Verify the suite catches a broken provider**

Temporarily (in a scratch `_test.go`, not committed) run `Run` against a stub whose `Ensure` returns `(&kube.Conn{}, nil)` and whose `Exists` always returns false, with `CUBE_IDP_PROVIDER_E2E=1`: the suite must FAIL at "unusable Conn". Delete the scratch file after confirming.

- [ ] **Step 3: Wire kind as the first consumer**

`internal/cluster/kindp/contract_test.go`:

```go
package kindp_test

import (
	"testing"

	"github.com/rafpe/cube-idp/internal/cluster/contracttest"
	"github.com/rafpe/cube-idp/internal/cluster/kindp"
	"github.com/rafpe/cube-idp/internal/config"
)

func TestKindProviderContract(t *testing.T) {
	gw := config.GatewaySpec{Pack: "traefik", Host: "cube-idp.localtest.me", Port: 18443} // non-default port: avoid colliding with a dev cluster
	contracttest.Run(t, kindp.New(gw), config.ClusterSpec{Provider: "kind", KubernetesVersion: "v1.33.1"})
}
```

RESOLVED 2026-07-14: `kindp.New(gw config.GatewaySpec) *Kind` confirmed; `KubernetesVersion` default is `"v1.33.1"` (filled by `config.Load` for cluster-creating providers — see `internal/config/schema.cue` comment).

- [ ] **Step 4: Run**

Run: `go test ./internal/cluster/... -short -v` — contract test SKIPs (gate unset), everything else PASSes.
Then locally once: `CUBE_IDP_PROVIDER_E2E=1 go test ./internal/cluster/kindp/ -run TestKindProviderContract -v -timeout 15m` — PASS against a real runtime.

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "test: shared ClusterProvider contract suite; kind is the first consumer"
```

---

### Task 2: k3d provider (D4) with D10 two-layer merge + `config render-cluster`

**Reconcile checkpoint:** requires 0.1 (Provider signature + how kindp implements it — Phase 1), 0.8 (final gateway host-port → node-port wiring, `gatewayContainerPort`/NodePort value — Phase 1 Task 12 note, possibly changed by Phase 2 trust/HTTPS), 0.6 (`cmd/config.go` render-cluster shape — Phase 1 Task 5), 0.5 (diag). Task 1 must be done (the new provider must pass the contract suite).

k3d wraps k3s in docker. Three k3d-specific injection requirements beyond what kind needed: (a) k3s **bundles its own traefik**, which collides with our gateway pack — the merge must inject `--disable=traefik` on the server; (b) registry mirrors use the **k3s `registries.yaml` schema**, not raw containerd patches; (c) **the D12 zot-mirror wiring** (added by Phase 2 Task 10, verified 2026-07-14 in `kindp/kind.go`): kindp injects a containerd certs.d mount mapping `registry.<gw.Host>` → `http://localhost:30500` (zot's NodePort, `registry.NodePort`) via `trust.Dir()`/`trust.EnsureCA`/`trust.WriteCertsD` — the k3d equivalent is a `registries.yaml` mirrors entry `registry.<gw.Host>` with endpoint `http://localhost:30500`, injected by `Ensure` (NOT by the pure `RenderConfig` when called from `cmd/config.go`, which mirrors kindp's zero-`CertsD{}` file-free render — model this as an optional injection parameter just like kindp's `CertsD`).

**Files:**
- Create: `internal/cluster/k3dp/merge.go`, `internal/cluster/k3dp/k3d.go`, `internal/cluster/k3dp/testdata/{merged-typed.yaml,user-k3d-config.yaml,merged-with-user.yaml}`, `internal/cluster/k3dp/contract_test.go`
- Modify: `internal/config/schema.cue` (provider enum + cross-validation), `internal/cluster/provider.go` (factory case), `cmd/config.go` (render-cluster provider switch)
- Test: `internal/cluster/k3dp/merge_test.go`, extend `internal/config/load_test.go`

**Interfaces:**
- Consumes: `config.ClusterSpec`, `config.GatewaySpec`, `kube.Conn`, `diag`, `contracttest.Run`.
- Produces:

```go
package k3dp
func New(gw config.GatewaySpec) *K3d                        // implements cluster.Provider
func RenderConfig(name string, spec config.ClusterSpec, gw config.GatewaySpec, zot ZotMirror) ([]byte, error)
// Pure D10 merge, mirror of kindp.RenderConfig(name, spec, gw, certsd) —
// including the 4th injection param: ZotMirror{Host string} is k3d's
// equivalent of kindp.CertsD (D12); zero value = no zot mirror injected
// (cmd/config.go render path), non-zero = Ensure-time wiring of
// registry.<gw.Host> -> http://localhost:30500 in the embedded
// registries.yaml. Output: a k3d SimpleConfig (k3d.io/v1alpha5) YAML
// document. Merge rules:
//   base    = user providerConfig (file path or inline YAML; a k3d SimpleConfig) if set, else empty SimpleConfig
//   inject  = gateway port mapping host gw.Port -> node port cluster.GatewayNodePort (30443) on server:0,
//             k3sExtraArgs --disable=traefik (server:0) — our gateway pack owns ingress,
//             registry mirrors/insecure as embedded k3s registries.yaml
//             (user spec.cluster.registry entries + the D12 zot mirror when set),
//             typed extraPorts -> ports entries, mounts -> volumes,
//             image rancher/k3s:<kubernetesVersion>-k3s1 from kubernetesVersion
//   conflict= user config maps gw.Port to a different node port, or sets a
//             different image than kubernetesVersion implies, or re-enables
//             traefik -> CUBE-1301; unreadable/invalid providerConfig -> CUBE-1302
```

- [ ] **Step 1: Extend the config schema with a failing test**

Add to `internal/config/load_test.go` (helpers verified 2026-07-14: `codeOf(t, err) diag.Code` exists and is the file's convention):

```go
func TestLoadAcceptsK3dProvider(t *testing.T) {
	c, err := Load("testdata/k3d.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if c.Spec.Cluster.Provider != "k3d" {
		t.Fatalf("provider: %q", c.Spec.Cluster.Provider)
	}
}
```

`internal/config/testdata/k3d.yaml`:

```yaml
apiVersion: cube-idp.dev/v1alpha1
kind: Cube
metadata:
  name: dev
spec:
  cluster:
    provider: k3d
```

Run: `go test ./internal/config/ -run TestLoadAcceptsK3dProvider -v` — FAIL (CUE rejects `k3d`).

- [ ] **Step 2: Widen the schema and factory**

In `internal/config/schema.cue`: `provider: *"kind" | "existing"` → `provider: *"kind" | "existing" | "k3d"`. RESOLVED 2026-07-14: the current disjunction is exactly `provider: *"kind" | "existing"` (schema.cue line 9), and `crossValidate` (`internal/config/load.go`) rejects node fields ONLY for `provider == "existing"` (CUBE-1003 `CodeClusterSetupFailed`) — k3d gets node fields for free, no crossValidate change needed. Also update `crossValidate`'s remediation text "switch to provider: kind" → "switch to provider: kind or k3d".

In `internal/cluster/provider.go` factory, add the case and fix the `CUBE-1001` remediation:

```go
	case "k3d":
		return k3dp.New(gw), nil
```

…and remediation text: `"use provider: kind, k3d, or existing"`.

Run: `go test ./internal/config/ ./internal/cluster/ -short -v` — config PASSes; cluster fails to build until k3dp exists (that is the next failing state, expected).

- [ ] **Step 3: Write the failing merge tests**

`internal/cluster/k3dp/merge_test.go` — same golden-file pattern as kindp (Phase 1 Task 5):

```go
package k3dp

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/rafpe/cube-idp/internal/config"
	"github.com/rafpe/cube-idp/internal/diag"
)

var gw = config.GatewaySpec{Pack: "traefik", Host: "cube-idp.localtest.me", Port: 8443}

func golden(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestRenderTypedFieldsOnly(t *testing.T) {
	spec := config.ClusterSpec{
		Provider:          "k3d",
		KubernetesVersion: "v1.33.1",
		ExtraPorts:        []config.PortMapping{{HostPort: 32222, NodePort: 32222}},
		Registry: config.RegistrySpec{
			Mirrors:  map[string]string{"docker.io": "https://mirror.corp.example"},
			Insecure: []string{"registry.corp.example:5000"},
		},
		Mounts: []config.Mount{{HostPath: "/tmp/images", NodePath: "/var/lib/images"}},
	}
	out, err := RenderConfig("dev", spec, gw, ZotMirror{})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != golden(t, "merged-typed.yaml") {
		t.Fatalf("golden mismatch:\n--- got ---\n%s\n--- want ---\n%s", out, golden(t, "merged-typed.yaml"))
	}
}

func TestRenderMergesUserProviderConfig(t *testing.T) {
	spec := config.ClusterSpec{
		Provider:          "k3d",
		KubernetesVersion: "v1.33.1",
		ProviderConfig:    filepath.Join("testdata", "user-k3d-config.yaml"),
	}
	out, err := RenderConfig("dev", spec, gw, ZotMirror{})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != golden(t, "merged-with-user.yaml") {
		t.Fatalf("golden mismatch:\n--- got ---\n%s", out)
	}
}

func TestRenderConflictOnGatewayPort(t *testing.T) {
	inline := `
apiVersion: k3d.io/v1alpha5
kind: Simple
ports:
  - port: "8443:9999"
    nodeFilters: ["server:0"]
`
	spec := config.ClusterSpec{Provider: "k3d", KubernetesVersion: "v1.33.1", ProviderConfig: inline}
	_, err := RenderConfig("dev", spec, gw, ZotMirror{})
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-1301" {
		t.Fatalf("want CUBE-1301 conflict, got %v", err)
	}
}

func TestRenderConflictOnImage(t *testing.T) {
	inline := `
apiVersion: k3d.io/v1alpha5
kind: Simple
image: rancher/k3s:v1.30.0-k3s1
`
	spec := config.ClusterSpec{Provider: "k3d", KubernetesVersion: "v1.33.1", ProviderConfig: inline}
	_, err := RenderConfig("dev", spec, gw, ZotMirror{})
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-1301" {
		t.Fatalf("want CUBE-1301 image conflict, got %v", err)
	}
}
```

`internal/cluster/k3dp/testdata/user-k3d-config.yaml`:

```yaml
apiVersion: k3d.io/v1alpha5
kind: Simple
options:
  k3d:
    wait: true
servers: 1
```

Golden fixtures: same protocol as kindp — generate on first run with a temporary `os.WriteFile`, then **human-review**: `merged-typed.yaml` must contain the gateway `port: "8443:<gatewayNodePort>"` mapping on `server:0`, `--disable=traefik`, the registries.yaml mirror + insecure entry, the `/tmp/images:/var/lib/images` volume, the `32222:32222` port, and `image: rancher/k3s:v1.33.1-k3s1`; `merged-with-user.yaml` must additionally preserve `options.k3d.wait: true`. Remove the write, commit fixtures. RESOLVED 2026-07-14: `<gatewayNodePort>` = **30443** (Phase 2 Task 9 — the host gateway port maps to traefik's `websecure` HTTPS NodePort; plain-HTTP `web` stays on 30080 but is NOT what the host port maps to) — assert 30443.

Run: `go test ./internal/cluster/k3dp/ -v` — FAIL (package does not exist).

- [ ] **Step 4: Implement merge.go**

```bash
go get github.com/k3d-io/k3d/v5@latest
```

`internal/cluster/k3dp/merge.go`:

```go
// Package k3dp implements the k3d ClusterProvider (D4, Phase 3) with the
// same D10 two-layer customization model as kindp: typed fields + a
// provider-native SimpleConfig escape hatch, explicit conflict errors,
// inspectable via `cube-idp config render-cluster`.
package k3dp

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
	v1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"

	"github.com/rafpe/cube-idp/internal/config"
	"github.com/rafpe/cube-idp/internal/diag"
)

// RESOLVED 2026-07-14: kindp's gatewayContainerPort is 30443 (Phase 2 Task 9
// — traefik websecure HTTPS NodePort; verified in packs/traefik/chart.yaml
// ports.websecure.nodePort). Keep the two providers' constants defined from
// ONE shared constant — add it to internal/cluster (cluster.GatewayNodePort)
// and change kindp to use it in this task.
const gatewayNodePort = 30443

// ZotMirror is k3d's CertsD-equivalent (D12): when Host is non-empty,
// RenderConfig adds a registries.yaml mirrors entry for it pointing at the
// node-local zot NodePort. Ensure passes ZotMirror{Host: "registry." + gw.Host};
// cmd/config.go passes the zero value (file-free render, mirror of kindp).
type ZotMirror struct{ Host string }

func RenderConfig(name string, spec config.ClusterSpec, gw config.GatewaySpec, zot ZotMirror) ([]byte, error) {
	cfg, err := loadUserConfig(spec.ProviderConfig)
	if err != nil {
		return nil, err
	}
	cfg.TypeMeta.APIVersion = "k3d.io/v1alpha5"
	cfg.TypeMeta.Kind = "Simple"
	cfg.ObjectMeta.Name = name
	if cfg.Servers == 0 {
		cfg.Servers = 1
	}

	// Node image from kubernetesVersion (conflict on mismatch, D10).
	image := "rancher/k3s:" + spec.KubernetesVersion + "-k3s1"
	if cfg.Image != "" && cfg.Image != image {
		return nil, diag.New("CUBE-1301",
			fmt.Sprintf("providerConfig sets image %q but spec.cluster.kubernetesVersion implies %q", cfg.Image, image),
			"remove image from providerConfig or align kubernetesVersion; inspect with `cube-idp config render-cluster`")
	}
	cfg.Image = image

	// Required injection 1: the gateway port mapping (host gw.Port -> node
	// gatewayNodePort on the first server).
	gwMapping := fmt.Sprintf("%d:%d", gw.Port, gatewayNodePort)
	for _, p := range cfg.Ports {
		host, node, ok := strings.Cut(p.Port, ":")
		if ok && host == fmt.Sprint(gw.Port) && node != fmt.Sprint(gatewayNodePort) {
			return nil, diag.New("CUBE-1301",
				fmt.Sprintf("providerConfig maps host port %s to %s, but cube-idp requires %s for the gateway", host, node, gwMapping),
				"remove that ports entry or change spec.gateway.port; inspect with `cube-idp config render-cluster`")
		}
	}
	if !hasHostPort(cfg.Ports, gw.Port) {
		cfg.Ports = append(cfg.Ports, v1alpha5.PortWithNodeFilters{
			Port: gwMapping, NodeFilters: []string{"server:0"},
		})
	}

	// Required injection 2: disable k3s's bundled traefik — the gateway pack
	// owns ingress (D3). Conflict if the user explicitly re-enables it.
	disable := v1alpha5.K3sArgWithNodeFilters{Arg: "--disable=traefik", NodeFilters: []string{"server:0"}}
	for _, a := range cfg.Options.K3sOptions.ExtraArgs {
		if strings.Contains(a.Arg, "traefik") && a.Arg != disable.Arg {
			return nil, diag.New("CUBE-1301",
				fmt.Sprintf("providerConfig sets k3s arg %q, but cube-idp requires --disable=traefik (the gateway pack provides ingress)", a.Arg),
				"remove the traefik-related k3s arg; the traefik gateway pack replaces the bundled one")
		}
	}
	if !slices.ContainsFunc(cfg.Options.K3sOptions.ExtraArgs, func(a v1alpha5.K3sArgWithNodeFilters) bool { return a.Arg == disable.Arg }) {
		cfg.Options.K3sOptions.ExtraArgs = append(cfg.Options.K3sOptions.ExtraArgs, disable)
	}

	// D10 layer-1 typed fields.
	for _, p := range spec.ExtraPorts {
		if hasHostPort(cfg.Ports, int(p.HostPort)) {
			return nil, diag.New("CUBE-1301",
				fmt.Sprintf("host port %d is mapped both in providerConfig and spec.cluster.extraPorts", p.HostPort),
				"keep exactly one of the two mappings")
		}
		cfg.Ports = append(cfg.Ports, v1alpha5.PortWithNodeFilters{
			Port: fmt.Sprintf("%d:%d", p.HostPort, p.NodePort), NodeFilters: []string{"server:0"},
		})
	}
	for _, m := range spec.Mounts {
		cfg.Volumes = append(cfg.Volumes, v1alpha5.VolumeWithNodeFilters{
			Volume: m.HostPath + ":" + m.NodePath, NodeFilters: []string{"server:0"},
		})
	}
	if reg := registriesYAML(spec.Registry, zot); reg != "" {
		if cfg.Registries.Config != "" {
			return nil, diag.New("CUBE-1301",
				"registry mirrors are set both in providerConfig (registries.config) and spec.cluster.registry",
				"keep exactly one of the two")
		}
		cfg.Registries.Config = reg
	}

	return yaml.Marshal(cfg)
}

func loadUserConfig(pc string) (*v1alpha5.SimpleConfig, error) {
	var cfg v1alpha5.SimpleConfig
	if pc == "" {
		return &cfg, nil
	}
	raw := []byte(pc)
	if !strings.Contains(pc, "\n") { // single line -> file path (same rule as kindp)
		b, err := os.ReadFile(pc)
		if err != nil {
			return nil, diag.Wrap(err, "CUBE-1302", fmt.Sprintf("cannot read providerConfig file %s", pc),
				"set spec.cluster.providerConfig to a readable k3d SimpleConfig file or an inline YAML document")
		}
		raw = b
	}
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, diag.Wrap(err, "CUBE-1302", "providerConfig is not a valid k3d SimpleConfig document",
			"see https://k3d.io/stable/usage/configfile/")
	}
	return &cfg, nil
}

func hasHostPort(ports []v1alpha5.PortWithNodeFilters, host int) bool {
	for _, p := range ports {
		if h, _, ok := strings.Cut(p.Port, ":"); ok && h == fmt.Sprint(host) {
			return true
		}
	}
	return false
}

// registriesYAML renders the k3s registries.yaml document (mirrors +
// insecure TLS skip + the D12 zot mirror when zot.Host is set: an entry
// zot.Host -> endpoint http://localhost:30500, i.e. registry.NodePort —
// plain HTTP on the node-local port, same as kindp's WriteCertsD wiring),
// sorted for golden determinism.
func registriesYAML(r config.RegistrySpec, zot ZotMirror) string {
	if len(r.Mirrors) == 0 && len(r.Insecure) == 0 && zot.Host == "" {
		return ""
	}
	type tlsCfg struct {
		TLS map[string]bool `yaml:"tls,omitempty"`
	}
	doc := struct {
		Mirrors map[string]struct {
			Endpoint []string `yaml:"endpoint"`
		} `yaml:"mirrors,omitempty"`
		Configs map[string]tlsCfg `yaml:"configs,omitempty"`
	}{}
	if len(r.Mirrors) > 0 {
		doc.Mirrors = map[string]struct {
			Endpoint []string `yaml:"endpoint"`
		}{}
		for _, host := range slices.Sorted(maps.Keys(r.Mirrors)) {
			doc.Mirrors[host] = struct {
				Endpoint []string `yaml:"endpoint"`
			}{Endpoint: []string{r.Mirrors[host]}}
		}
	}
	if len(r.Insecure) > 0 {
		doc.Configs = map[string]tlsCfg{}
		for _, host := range r.Insecure {
			doc.Configs[host] = tlsCfg{TLS: map[string]bool{"insecure_skip_verify": true}}
		}
	}
	out, _ := yaml.Marshal(doc)
	return string(out)
}
```

(Import `maps`. RECONCILE: the exact field names on `v1alpha5.SimpleConfig` — `TypeMeta`/`ObjectMeta` embedding, `Ports []PortWithNodeFilters{Port string, NodeFilters []string}`, `Volumes`, `Options.K3sOptions.ExtraArgs []K3sArgWithNodeFilters`, `Registries.Config string`, `Image`, `Servers` — against the installed k3d v5 version; the k3d CLI's own `config.SimpleConfig` handling in `k3d-io/k3d/cmd/cluster/clusterCreate.go` is the reference consumer. Adjust mechanically; the tests define the behavior.)

Also in this step: hoist the shared constant. Add to `internal/cluster/provider.go`:

```go
// GatewayNodePort is the node port every cluster-creating provider must map
// the host gateway port onto; the traefik pack's service pins the same value.
const GatewayNodePort = 30443 // RESOLVED 2026-07-14: traefik websecure HTTPS NodePort (Phase 2 Task 9)
```

and change `kindp` + `k3dp` to reference `cluster.GatewayNodePort` (delete the local constants). Run kindp's tests to prove the refactor is behavior-neutral.

- [ ] **Step 5: Implement k3d.go (provider around the merge)**

`internal/cluster/k3dp/k3d.go` — mirrors `kindp/kind.go` exactly in shape (kindp method set verified 2026-07-14: `New/Ensure/Exists/Delete/Kubeconfig/Diagnose` + the private `certsD()` injection helper — k3dp's equivalent builds the `ZotMirror`; note `trust.EnsureCA` must have run, which `up.Run` guarantees before `Ensure`, same as kindp). Contract, with the k3d library specifics spelled out:

```go
package k3dp

// K3d implements cluster.Provider over the k3d v5 library.
//
//   type K3d struct{ gw config.GatewaySpec }
//   func New(gw config.GatewaySpec) *K3d { return &K3d{gw: gw} }
//
// Ensure(ctx, name, spec):
//   1. Exists check (below); if present, skip creation (idempotent).
//   2. cfgYAML := RenderConfig(name, spec, k.gw, ZotMirror{Host: "registry." + k.gw.Host})
//   3. Unmarshal into v1alpha5.SimpleConfig, then transform to the runtime
//      cluster config: k3dconfig.TransformSimpleToClusterConfig(ctx,
//      runtimes.SelectedRuntime, simpleCfg, "") and validate with
//      k3dconfig.ValidateClusterConfig. RECONCILE: exact transform/validate
//      function names in github.com/k3d-io/k3d/v5/pkg/config for the pinned
//      version — the k3d CLI's clusterCreate.go is the reference consumer.
//   4. k3dclient.ClusterRun(ctx, runtimes.SelectedRuntime, clusterConfig)
//      -> on error: diag.Wrap(err, "CUBE-1303", "k3d cluster creation failed",
//         "check that the container runtime is running and has free resources")
//   5. Kubeconfig: k3dclient.KubeconfigGet(ctx, runtimes.SelectedRuntime,
//      &types.Cluster{Name: name}) returns *clientcmdapi.Config; serialize
//      with clientcmd.Write, build REST via clientcmd.RESTConfigFromKubeConfig.
//      Errors -> CUBE-1304. Conn.Context = "k3d-" + name.
//
// Exists: k3dclient.ClusterList(ctx, runtimes.SelectedRuntime), match by
//   name; list error -> diag.Wrap(err, "CUBE-1303", "cannot list k3d
//   clusters", "is the container runtime running?").
//
// Delete: k3dclient.ClusterDelete(ctx, runtimes.SelectedRuntime,
//   &types.Cluster{Name: name}, types.ClusterDeleteOpts{}) -> CUBE-1305 on error.
//
// Kubeconfig: same call as Ensure step 5, without creation.
//
// Diagnose: ClusterList error -> one diag.Finding{Code: "CUBE-1303",
//   Severity: diag.SeverityError, Message: "container runtime unreachable: "
//   + err, Remediation: "start Docker/Podman and retry"}; else nil.
```

Write the real implementation (~120 lines) following that contract. It must satisfy `var _ cluster.Provider = (*K3d)(nil)` — add that compile-time assertion at the top of the file.

- [ ] **Step 6: render-cluster for k3d + the k3d guard in requireClusterExists**

In `cmd/config.go`, replace the kind-only guard with a provider switch (current body verified 2026-07-14: it returns `diag.CodeProviderMiss` for non-kind and calls `kindp.RenderConfig(..., kindp.CertsD{})` — keep BOTH conventions):

```go
	switch cube.Spec.Cluster.Provider {
	case "kind":
		out, err = kindp.RenderConfig(cube.Metadata.Name, cube.Spec.Cluster, cube.Spec.Gateway, kindp.CertsD{})
	case "k3d":
		out, err = k3dp.RenderConfig(cube.Metadata.Name, cube.Spec.Cluster, cube.Spec.Gateway, k3dp.ZotMirror{})
	default:
		return diag.New(diag.CodeProviderMiss, // CUBE-0004 — the EXISTING code for this exact case (verified in internal/diag/codes.go; the drafted CUBE-1002 allocation is dropped)
			fmt.Sprintf("render-cluster applies to cluster-creating providers (kind, k3d), not %q", cube.Spec.Cluster.Provider),
			"provider: existing has no provider config to render")
	}
```

Also in this step (drift found by Task 0): `cmd/root.go`'s `requireClusterExists` guards side-effect-free commands with `if provider != "kind" { return nil }` — k3d's `Ensure` also CREATES missing clusters, so change the guard to cover both cluster-creating providers (`provider != "kind" && provider != "k3d"`), keeping the CUBE-1004 error text generic ("%s cluster %q does not exist").

- [ ] **Step 7: Contract test + full run**

`internal/cluster/k3dp/contract_test.go` — identical shape to Task 1 Step 3 with `k3dp.New(gw)` and `Provider: "k3d"`.

Run: `go test ./internal/... ./cmd/ -short -v && go build ./...` — PASS.
Then locally once: `CUBE_IDP_PROVIDER_E2E=1 go test ./internal/cluster/k3dp/ -run TestK3dProviderContract -v -timeout 15m` — PASS.

- [ ] **Step 8: Commit**

```bash
git add -A && git commit -m "feat: k3d cluster provider with D10 two-layer merge and render-cluster support"
```

---

### Task 3: `cube-idp pack push` — publish a pack directory as an OCI artifact

**Reconcile checkpoint:** RESOLVED 2026-07-14 (0.2 + 0.9 verified): `pullOCI`'s `extractManifest` consumes both flux-style tar.gz layers AND oras per-file layers, so a flux-shaped push (mirroring `internal/oci/push.go`'s media types) round-trips; `internal/oci` is pure oras-go v2; `isLocalRegistryHost` is unexported in `internal/pack/source.go` (strips `:port`, matches `127.0.0.1`/`localhost`) — this task adds the same 6-line gate to `internal/oci` since `PushPackDir`, unlike `PushRendered`, targets real registries. Bodies below are the Task-0 rewrite (Owner Decisions #2); `--also-tag latest` added per Owner Decisions #13.

Phase 1's `internal/oci.PushRendered` pushes *rendered* manifests for engine delivery. The catalog (Tasks 4–5) needs the symmetric operation for *pack sources*: push a pack directory (pack.cue + manifests/ + chart.yaml) such that `pack.Fetch(ctx, "oci://…", cache)` round-trips it. Round-trip is the whole contract, so the test is a push→pull loop against a throwaway local registry.

**Files:**
- Create: `internal/oci/pushdir.go`, `cmd/pack.go`
- Test: `internal/oci/pushdir_test.go`

**Interfaces:**
- Consumes: `pack.Fetch`, the existing `internal/oci` oras-go v2 push seam (`pushRenderedTo`/`isLocalRegistryHost` patterns — NOT fluxcd/pkg/oci, which Phase 2 removed), `diag`.
- Produces:

```go
package oci
// PushPackDir pushes the pack source directory at dir to ociRef
// (form: oci://host/repo:tag) as an artifact pack.Fetch can pull.
// alsoTags: extra tags applied to the same pushed manifest (one push, N
// tags — Owner Decisions #13; `pack push --also-tag latest` feeds this).
// Auth: ambient docker credential chain via oras-go v2's
// registry/remote/credentials docker store (docker login / GITHUB_TOKEN via
// docker/login-action in CI); PlainHTTP only for 127.0.0.1/localhost hosts.
// Failure -> CUBE-4015.
func PushPackDir(ctx context.Context, dir, ociRef string, alsoTags ...string) (digest string, err error)
```

CLI: `cube-idp pack push <dir> <oci-ref> [--also-tag <tag>]` — prints the pushed digest. Tag defaulting: if `<oci-ref>` has no `:tag`, use the pack's `version` from `pack.cue` (RESOLVED 2026-07-14: `pack.Fetch(ctx, dir, cacheDir)` on a local dir returns `*pack.Pack{Name, Version, Dir, Pinned}` and ignores `cacheDir` for local paths — reuse it rather than re-parsing CUE).

- [ ] **Step 1: Write the failing round-trip test**

`internal/oci/pushdir_test.go`:

```go
package oci

import (
	"context"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/registry"

	"github.com/rafpe/cube-idp/internal/pack"
)

// localRegistry starts an in-process OCI registry (go-containerregistry's
// test registry) and returns its host:port.
func localRegistry(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(registry.New())
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	return u.Host
}

func writeDemoPack(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	must := func(p, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	must(filepath.Join(dir, "pack.cue"), "name: \"demo\"\nversion: \"0.9.9\"\n")
	must(filepath.Join(dir, "manifests", "cm.yaml"),
		"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\n  namespace: default\n")
	return dir
}

func TestPushPackDirRoundTripsThroughFetch(t *testing.T) {
	host := localRegistry(t)
	dir := writeDemoPack(t)
	ref := "oci://" + host + "/packs/demo:0.9.9"

	digest, err := PushPackDir(context.Background(), dir, ref, "latest") // --also-tag path exercised in the same round-trip
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(digest, "sha256:") {
		t.Fatalf("digest: %q", digest)
	}

	p, err := pack.Fetch(context.Background(), ref, t.TempDir())
	if err != nil {
		t.Fatalf("Fetch after push: %v", err)
	}
	if p.Name != "demo" || p.Version != "0.9.9" {
		t.Fatalf("round-trip metadata: %+v", p)
	}
	r, err := p.Render(nil)
	if err != nil || len(r.Objects) != 1 {
		t.Fatalf("round-trip render: %v (%d objects)", err, len(r.Objects))
	}

	// --also-tag: the same digest must be fetchable via the extra tag.
	if p2, err := pack.Fetch(context.Background(), "oci://"+host+"/packs/demo:latest", t.TempDir()); err != nil || p2.Pinned != "oci:"+digest {
		t.Fatalf("also-tag fetch: %v (pinned %q, want oci:%s)", err, p2.Pinned, digest)
	}
}
```

(`httptest` registries serve plain HTTP on `127.0.0.1` — this exercises the same insecure-transport path the zot tunnel uses. RESOLVED 2026-07-14: `pack`'s `isLocalRegistryHost` strips the port before comparing, so `127.0.0.1:<randomport>` already matches; `PushPackDir` gets an identical helper in `internal/oci`.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/oci/ -run TestPushPackDirRoundTrips -v`
Expected: FAIL (`PushPackDir` undefined)

- [ ] **Step 3: Implement**

```bash
go get github.com/google/go-containerregistry@latest
```

`internal/oci/pushdir.go` (rewritten by Task 0 onto oras-go v2, mirroring the shipped `internal/oci/push.go` — flux media types, `oras.PackManifest`, `registry/remote`):

```go
package oci

import (
	"context"
	"fmt"
	"strings"

	oras "oras.land/oras-go/v2"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/credentials"

	"github.com/rafpe/cube-idp/internal/diag"
)

// PushPackDir publishes the pack source directory as an OCI artifact in the
// same shape PushRendered produces (verified 2026-07-14: pack.Fetch's
// extractManifest handles flux-style single tar.gz layers — the Step 1
// round-trip test is the arbiter):
//   1. layer := tar.gz of the pack DIRECTORY TREE (dirs + regular files
//      only, deterministic walk order; entry names relative to dir —
//      reuse buildArtifactLayer's tar/gzip mechanics generalized to a tree)
//      with media type fluxContentMediaType.
//   2. desc := oras.PackManifest(ctx, repo, PackManifestVersion1_0,
//      fluxConfigMediaType, ...) with the same three
//      org.opencontainers.image.* annotations push.go writes.
//   3. repo := remote.NewRepository(<ref sans oci://>); repo.Client =
//      &auth.Client{Credential: credentials.Credential(dockerStore)} using
//      credentials.NewStoreFromDocker (ambient docker login / CI
//      login-action); repo.PlainHTTP = isLocalRegistryHost(host) — the
//      helper is COPIED from internal/pack/source.go (unexported there):
//      strip ":port", match 127.0.0.1/localhost.
//   4. repo.Tag(ctx, desc, tag) for the ref's tag and every alsoTags entry
//      (one manifest push, N tags — Owner Decisions #13).
//   5. return desc.Digest.String().
func PushPackDir(ctx context.Context, dir, ociRef string, alsoTags ...string) (string, error) {
	if !strings.HasPrefix(ociRef, "oci://") {
		return "", diag.New("CUBE-4015", fmt.Sprintf("pack push target %q is not an oci:// reference", ociRef),
			"use the form oci://host/repo:tag")
	}
	digest, err := pushDir(ctx, dir, strings.TrimPrefix(ociRef, "oci://"), alsoTags) // per the contract above
	if err != nil {
		return "", diag.Wrap(err, "CUBE-4015", fmt.Sprintf("failed to push pack directory %s to %s", dir, ociRef),
			"check registry credentials (docker login) and that the tag is writable")
	}
	return digest, nil
}
```

`cmd/pack.go` — cobra plumbing only:

```go
package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rafpe/cube-idp/internal/oci"
	"github.com/rafpe/cube-idp/internal/pack"
)

func newPackCmd() *cobra.Command {
	packCmd := &cobra.Command{Use: "pack", Short: "Work with cube-idp packs"}
	var alsoTags []string
	push := &cobra.Command{
		Use:   "push <dir> <oci-ref>",
		Short: "Publish a pack directory as an OCI artifact",
		Args:  cobra.ExactArgs(2),
		RunE: func(c *cobra.Command, args []string) error {
			dir, ref := args[0], args[1]
			if !strings.Contains(ref[len("oci://"):], ":") { // no tag -> pack version
				p, err := pack.Fetch(context.Background(), dir, "")
				if err != nil {
					return err
				}
				ref = ref + ":" + p.Version
			}
			digest, err := oci.PushPackDir(c.Context(), dir, ref, alsoTags...)
			if err != nil {
				return err
			}
			fmt.Fprintf(c.OutOrStdout(), "pushed %s@%s\n", ref, digest)
			return nil
		},
	}
	push.Flags().StringSliceVar(&alsoTags, "also-tag", nil, "additional tags for the same pushed artifact (e.g. latest)")
	packCmd.AddCommand(push)
	return packCmd
}
```

Register `root.AddCommand(newPackCmd())` in `cmd/root.go`'s `NewRootCmd` (pattern verified 2026-07-14). RESOLVED 2026-07-14: `pack.Fetch` ignores `cacheDir` for local directory paths (the `default:` branch in `source.go` never touches it) — empty string is fine.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/oci/ ./cmd/ -short -v && go build ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat: pack push command — publish pack directories as OCI artifacts"
```

---

### Task 3.5: `${GATEWAY_HOST}` substitution over chart values and manifests (spec D15 — Owner Decisions #11)

**Reconcile checkpoint:** RESOLVED 2026-07-14 — how the substitution works today: `pack.ExposeURLs` (`internal/pack/expose.go`) does `strings.ReplaceAll(u, "${GATEWAY_HOST}", host)` where `host = gw.Host` or `gw.Host:gw.Port` when `gw.Port != 443`. Render is `func (p *Pack) Render(values map[string]any) (*Rendered, error)` — it has NO gateway input today, and the shipped gitea HTTPRoute hardcodes `gitea.cube-idp.localtest.me` (the pre-D15 state).

Small, contained: the same `${GATEWAY_HOST}` expansion the D11 `expose:` block gets is extended to (a) `chart.yaml` `values:` (string leaf values) and (b) `manifests/*.yaml` bytes, so packs like backstage can derive their baseUrl from the configured gateway instead of hardcoding. Sequenced before Task 4 (its packs rely on it).

**Files:**
- Modify: `internal/pack/expose.go` (extract the shared `gatewayHostString(gw)` + `substitute(s, gw)` helpers), `internal/pack/render.go` + `internal/pack/pack.go` (`RenderFor`), callers in `internal/up/up.go` (and `internal/diff` if it renders packs — check its call site).
- Test: `internal/pack/render_test.go` (or the existing render test file).

**Interfaces:**

```go
package pack
// RenderFor is Render plus D15 substitution: every string leaf in the
// chart values (after merging pack defaults + user values) and the raw
// manifest bytes get TWO replacements — ${GATEWAY_HOST} -> host[:port]
// (the SAME expansion ExposeURLs uses; shared helper; port omitted at 443)
// and ${GATEWAY_FQDN} -> the bare gw.Host (for Gateway API hostname
// fields, which cannot carry ports — see Task 4's backstage HTTPRoute).
// A zero gw (Host == "") performs no substitution.
// Render(values) remains and delegates to RenderFor(values, config.GatewaySpec{})
// so existing callers/tests are untouched.
func (p *Pack) RenderFor(values map[string]any, gw config.GatewaySpec) (*Rendered, error)
```

- [ ] **Step 1: Failing test** — a fixture pack whose `chart.yaml` values and one manifest both contain `${GATEWAY_HOST}`; `RenderFor(nil, gw{Host: "cube-idp.localtest.me", Port: 8443})` must render `cube-idp.localtest.me:8443` in both places; `Render(nil)` must leave the literal untouched. Run — FAIL.
- [ ] **Step 2: Implement** — extract the host-string + substitute helpers from `ExposeURLs` (behavior-neutral refactor, expose tests prove it), apply in the render path (values walk for string leaves; manifest bytes before YAML parse). Wire `up.Run` (which already has `cube.Spec.Gateway`) to call `RenderFor`. Run — PASS.
- [ ] **Step 3: Run full tests** — `go build ./... && go test ./... -short` (existing packs contain no `${GATEWAY_HOST}` in values/manifests except gitea's expose block, which is untouched — byte-stable outputs survive).
- [ ] **Step 4: Commit**

```bash
git add -A && git commit -m "feat: D15 — \${GATEWAY_HOST} substitution over chart values and manifests"
```

---

### Task 4: Catalog packs — backstage, cert-manager, external-secrets, envoy-gateway

**Reconcile checkpoint:** RESOLVED 2026-07-14 (0.8/0.10 verified): HTTPRoute parentRef is `{group: gateway.networking.k8s.io, kind: Gateway, name: cube-idp, namespace: traefik}` and the shipped gitea HTTPRoute writes EVERY schema-defaulted field explicitly (argocd SSA-diff rule — new routes must copy that style); canonical scheme is `https` via the websecure listener (NodePort 30443); the pack format is `pack.cue` + `manifests/` + `chart.yaml` with `values:`. Requires Task 3.5 (D15 substitution) for the `${GATEWAY_HOST}` uses below. Turnkey envoy-gateway per Owner Decisions #7; pack-declared `images:` per Owner Decisions #3 (the parsing lands in Task 6's prep step — declaring it here is forward-compatible data).

Data only — no Go. Same conventions as the Phase 1 starter packs: pinned chart versions, `#Values` schemas, HTTPRoutes through the `cube-idp` Gateway, `expose:` blocks where there are credentials/URLs (D11 — CLI-secret labels are deprecated).

**Files:**
- Create:
  - `packs/cert-manager/{pack.cue,chart.yaml}`
  - `packs/external-secrets/{pack.cue,chart.yaml}`
  - `packs/envoy-gateway/{pack.cue,chart.yaml,manifests/10-gatewayclass.yaml,manifests/20-gateway.yaml}`
  - `packs/backstage/{pack.cue,chart.yaml,manifests/20-httproute.yaml}`
- Test: extend `tests/packs_render_test.go`

**Pack contents:**

`packs/cert-manager/pack.cue`:

```cue
name:    "cert-manager"
version: "0.1.0"
#Values: {
	replicas: int & >0 | *1
}
```

`packs/cert-manager/chart.yaml` (CRDs via chart flag — no vendored CRD file needed):

```yaml
chart: cert-manager
repo: https://charts.jetstack.io
version: "1.16.3"           # pin; bump deliberately
releaseName: cert-manager
namespace: cert-manager
values:
  crds:
    enabled: true
```

`packs/external-secrets/pack.cue`: `name: "external-secrets"`, `version: "0.1.0"`, `#Values: {}`.

`packs/external-secrets/chart.yaml`:

```yaml
chart: external-secrets
repo: https://charts.external-secrets.io
version: "0.12.1"
releaseName: external-secrets
namespace: external-secrets
values:
  installCRDs: true
```

`packs/envoy-gateway/pack.cue` — declares the operator-pulled proxy image absent from its rendered manifests (spec D14, Owner Decisions #3; envoy-gateway's controller spawns Envoy proxy pods at Gateway-attach time, so the image never appears in `helm template` output — the exact ref must match the pinned chart's proxy default, verify with the chart's values during Step 2):

```cue
name:    "envoy-gateway"
version: "0.1.0"
#Values: {}
images: ["docker.io/envoyproxy/envoy:distroless-v1.33.0"] // RECONCILE (Task 6 prep consumes): pin to the proxy image the pinned gateway-helm chart actually defaults to
```

`packs/envoy-gateway/chart.yaml` (OCI chart — exercises the oci:// chart path in the helm wrapper). TURNKEY CONSTRAINTS (verified 2026-07-14 against the shipped trust/CoreDNS wiring): `up` issues the TLS secret `cube-idp-gateway-tls` into namespace `gw.Pack` (= `envoy-gateway`) and CoreDNS rewrites `*.<host>` to the Service `<gw.Pack>.<gw.Pack>.svc.cluster.local` (= `envoy-gateway.envoy-gateway.svc`), so the pack's namespace MUST be `envoy-gateway` (not the upstream default `envoy-gateway-system`) and the data-plane Service must be pinned to that name + NodePorts 30443/30080:

```yaml
chart: gateway-helm
repo: oci://docker.io/envoyproxy
version: "1.3.0"
releaseName: envoy-gateway
namespace: envoy-gateway
```

`packs/envoy-gateway/manifests/10-gatewayclass.yaml` (turnkey per Owner Decisions #7: the pack ships GatewayClass AND Gateway, so `spec.gateway.pack: envoy-gateway` works end-to-end):

```yaml
# envoy-gateway pack: a full cube-idp gateway implementation. Set
# spec.gateway.pack: envoy-gateway in cube.yaml — this pack provides the
# GatewayClass, the Gateway named cube-idp (20-gateway.yaml), and pins the
# data-plane service to the cube-idp NodePorts via the EnvoyProxy config.
apiVersion: gateway.networking.k8s.io/v1
kind: GatewayClass
metadata:
  name: envoy-gateway
spec:
  controllerName: gateway.envoyproxy.io/gatewayclass-controller
```

`packs/envoy-gateway/manifests/20-gateway.yaml` — mirrors the shipped `packs/traefik/manifests/10-gateway.yaml` EXACTLY (verified shape: Gateway `cube-idp`, listeners `web` 8000/HTTP + `websecure` 8443/HTTPS, `allowedRoutes: {namespaces: {from: All}}`, TLS `mode: Terminate` with `certificateRefs: [{group: "", kind: Secret, name: cube-idp-gateway-tls}]` — keep the schema-defaulted group/kind explicit, argocd SSA rule) with `gatewayClassName: envoy-gateway` and `namespace: envoy-gateway`. Plus the NodePort/service-name parity: attach an `EnvoyProxy` parametersRef (via the GatewayClass or Gateway, per the pinned chart's CRD) that sets the generated data-plane Service to `type: NodePort` with websecure→30443 / web→30080 and a stable name satisfying `envoy-gateway.envoy-gateway.svc.cluster.local`. RECONCILE (depends on the pinned envoy-gateway chart, not in the tree yet): the exact EnvoyProxy `spec.provider.kubernetes.envoyService` schema for name/type/nodePort overrides — Task 13's envoy e2e smoke is the arbiter; if the pinned version cannot pin the Service NAME, bump the chart pin to one that can before shipping this pack.

`packs/backstage/pack.cue`:

```cue
name:    "backstage"
version: "0.1.0"
#Values: {
	replicas: int & >0 | *1
}
```

`packs/backstage/chart.yaml`:

```yaml
chart: backstage
repo: https://backstage.github.io/charts
version: "2.4.0"
releaseName: backstage
namespace: backstage
values:
  backstage:
    extraEnvVars:
      # ${GATEWAY_HOST} expands via D15 (Task 3.5) to host[:port] — https
      # is the canonical scheme (websecure listener, Phase 2 D6/D12).
      - name: APP_CONFIG_app_baseUrl
        value: "https://backstage.${GATEWAY_HOST}"
      - name: APP_CONFIG_backend_baseUrl
        value: "https://backstage.${GATEWAY_HOST}"
```

(RESOLVED 2026-07-14: scheme is `https`; host/port derive from the configured gateway via D15 substitution instead of hardcoding — the reason Task 3.5 sequences before this task. Note the D15 hostname wildcard in the pack README: hostnames follow `spec.gateway.host` automatically.)

`packs/backstage/manifests/20-httproute.yaml` — every schema-defaulted field explicit, copying the shipped gitea route's style (argocd SSA-diff rule, verified 2026-07-14); hostname via D15:

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: backstage
  namespace: backstage
spec:
  parentRefs:
    - group: gateway.networking.k8s.io
      kind: Gateway
      name: cube-idp
      namespace: traefik
  hostnames: ["backstage.${GATEWAY_FQDN}"]
  rules:
    - matches:
        - path: {type: PathPrefix, value: /}
      backendRefs:
        - group: ""
          kind: Service
          name: backstage
          port: 7007
          weight: 1
```

(`${GATEWAY_FQDN}` — bare host, no port — because Gateway API hostnames cannot carry ports; `${GATEWAY_HOST}` keeps its host[:port] semantics for URLs, matching the gitea `expose:` block. Both defined in Task 3.5. Also add an `expose:` block to `packs/backstage/pack.cue`: `urls: ["https://backstage.${GATEWAY_HOST}"]`.)

- [ ] **Step 1: Extend the render smoke test (failing first)**

In `tests/packs_render_test.go`, extend the pack-dir list:

```go
	for _, dir := range []string{
		"../packs/traefik", "../packs/gitea", "../packs/argocd",
		"../packs/backstage", "../packs/cert-manager",
		"../packs/external-secrets", "../packs/envoy-gateway",
	} {
```

Run: `go test ./tests/ -run TestStarterPacksRender -v` — FAIL (dirs missing).

- [ ] **Step 2: Create the four pack directories exactly as above; run the test**

Run: `go test ./tests/ -run TestStarterPacksRender -v` (network required for helm pulls)
Expected: PASS — every pack renders ≥1 object. While here, eyeball each render for a sane namespace and (where routed) an HTTPRoute.

- [ ] **Step 3: Commit**

```bash
git add -A && git commit -m "feat: catalog packs — backstage, cert-manager, external-secrets, envoy-gateway"
```

---

### Task 5: CI publishes the catalog to ghcr — resolves the Phase 1 `--local` wrinkle

**Reconcile checkpoint:** requires Task 3 (`pack push --also-tag`), Task 4 (catalog). RESOLVED 2026-07-14: the namespace is **`ghcr.io/rafpe/cube-idp/packs/<name>`** (Owner Decisions #1); `config.Default` currently emits `oci://ghcr.io/cube-idp/packs/{gitea,argocd}:0.1.0` (`internal/config/types.go` ~line 125) — this task changes it; all `packs/*/pack.cue` versions are `0.1.0` (verified), matching the refs.

Phase 1 Task 13 noted: `init` writes `oci://ghcr.io/cube-idp/packs/...` refs that don't exist, so e2e used `init --local`. This task makes the published refs real: CI pushes every `packs/*` directory as an OCI artifact on every change to `main`, tagged with the pack's `version` from `pack.cue` plus a moving `latest`.

**Files:**
- Create: `.github/workflows/release-packs.yaml`
- Modify: `internal/config/types.go` (`Default` — only if the ghcr namespace decision changes the refs), `README.md` (published-pack refs section)
- Test: `internal/config/load_test.go` (`TestDefaultProfileIncludesGitea` pins the exact ref — update it in lockstep with `Default`)

- [ ] **Step 1: Workflow**

`.github/workflows/release-packs.yaml`:

```yaml
name: release-packs
on:
  push:
    branches: [main]
    paths: ["packs/**", ".github/workflows/release-packs.yaml"]
  workflow_dispatch:
permissions:
  contents: read
  packages: write
jobs:
  publish:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: {go-version-file: go.mod}   # never hardcode (Tech Stack rule)
      - uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}
      - name: build cube-idp
        run: go build -o ./cube-idp .
      - name: push all packs (version tag + latest, one push)
        run: |
          set -euo pipefail
          NS="ghcr.io/rafpe/cube-idp/packs"   # Owner Decisions #1
          for dir in packs/*/; do
            name="$(basename "$dir")"
            ./cube-idp pack push --also-tag latest "$dir" "oci://${NS}/${name}"   # tag = pack.cue version
            echo "published ${name} (+latest)"
          done
```

(The sed/version-extraction double-push from the original draft is DELETED per Owner Decisions #13 — `pack push --also-tag latest` applies both tags to one pushed manifest.)

- [ ] **Step 2: Align `config.Default` with the published refs**

Phase 1's `Default` emits `oci://ghcr.io/cube-idp/packs/{gitea,argocd}:0.1.0` (verified 2026-07-14, `internal/config/types.go` ~lines 125–126). Change both refs to `oci://ghcr.io/rafpe/cube-idp/packs/{gitea,argocd}:0.1.0` (pack.cue versions verified `0.1.0` — refs resolve once the workflow's first run publishes). Update `TestDefaultProfileIncludesGitea` alongside.

Keep `init --local` (it stays valuable for offline dev and hermetic e2e) but change its help text: `"use repo-local pack paths instead of published OCI refs"`.

- [ ] **Step 3: Verify end-to-end once, manually**

After the workflow's first green run on `main` (or a `workflow_dispatch`):

Run: `go run . init --name pubtest && go run . up` in a scratch dir (docker required)
Expected: `up` fetches gitea/argocd packs from ghcr (watch for the fetch step lines) and completes. Then `go run . down`.

- [ ] **Step 4: Commit**

```bash
git add -A && git commit -m "feat: CI publishes catalog packs to ghcr; init default refs resolve for real"
```

---

### Task 6: `cube-idp vendor` — build the air-gap bundle from `cube.lock`

**Reconcile checkpoint:** RESOLVED 2026-07-14 — 0.3 verified (real `lock.File` schema below), 0.2 verified (`pullOCI` accepts `@digest` refs; cache layout Fetch-compatible), 0.11 verified (the lock records per-pack `Images` via `lock.ImagesFrom` at `up` time — `internal/up/up.go` ~line 190 — and `registry.Manifests()` exists for the registry image derivation). Task 3's push/pull symmetry is reused for artifact download. Bundle format is **per-image tar archives** and `vendor` gains `--platform` (Owner Decisions #2); Step 0 below is the Decision #3 `images:` supplement (extend the LOCK package + pack.cue parsing in a preparatory commit).

Spec §4.1: "Pins recorded in `cube.lock` (digests + full image list) — feeds `cube-idp vendor` for air-gap." Vendor is a pure consumer: read lock → pull every pinned artifact and image → write one tarball. No cluster access, no config mutation.

**Files:**
- Create: `internal/bundle/bundle.go` (format + read/write), `internal/bundle/vendor.go`, `cmd/vendor.go`
- Test: `internal/bundle/bundle_test.go`

**Interfaces:**
- Consumes: the REAL Phase 2 lock package (verified 2026-07-14): `lock.File{APIVersion, Kind, Engine lock.EngineLock{Type}, Packs []lock.Entry{Ref, Name, Version, Resolved, RenderedHash, Images []string}}`, functions `lock.PathFor/Write/Read/RenderedHash/ImagesFrom`. There is NO `Digest` field — the pin string is `Entry.Resolved` with prefixes `oci:sha256:…` / `git+<sha>` / `dir:h1:…`; `oci:`-pinned packs re-pull by digest, git/dir-pinned packs re-fetch by `Ref` and assert `Pinned == Resolved`. There are NO engine/registry image pins in the lock — vendor derives them at build time via `lock.ImagesFrom(eng.InstallManifests())` + `lock.ImagesFrom(registry.Manifests())` (both verified to exist). NOTE: `lock.Read` returns `(nil, nil)` for a MISSING file — `Vendor` maps that to CUBE-7001 itself. The lock has NO cube name, so the bundle manifest carries none. Also consumes `pack.Fetch` and oras-go v2 (`content/oci` per-image stores, `oras.Copy` with `WithTargetPlatform`), `diag`.
- Produces:

```go
package bundle

// Layout inside the tar.gz (format is versioned via manifest.json):
//   manifest.json     — {"formatVersion":1,"platform":"linux/amd64",
//                        "createdAt":RFC3339,"lockDigest":"sha256:…",
//                        "images":{"<original ref>":"images/<n>.tar", …}}
//   cube.lock         — verbatim copy of the lock the bundle was built from
//   packs/<name>/     — pack source dir at the locked pin (Fetch-compatible)
//   images/<n>.tar    — ONE tar per locked image (Owner Decisions #2): a
//                       single-image OCI layout, tarred; the original ref is
//                       recorded in manifest.json's images map AND as the
//                       layout index's org.opencontainers.image.ref.name
//                       annotation. containerd consumes OCI-layout tars
//                       natively, which is what kind (LoadImageArchive) and
//                       k3d (ImageImportIntoClusterMulti) hand it.
//                       NOT YET PROVEN LIVE (Task 0 review finding): that
//                       acceptance is plausible-but-unverified until Task
//                       13's bundle e2e exercises it. FALLBACK if either
//                       importer rejects the OCI-layout tar: convert to
//                       docker-archive at load time inside internal/bundle
//                       (oras-go content walk + archive/tar — NOT by
//                       promoting go-containerregistry out of test-only;
//                       if that proves impractical, it is a plan change).
type Manifest struct {
	FormatVersion int               `json:"formatVersion"`
	Platform      string            `json:"platform"`  // GOOS/GOARCH the images were pulled for
	CreatedAt     string            `json:"createdAt"`
	LockDigest    string            `json:"lockDigest"` // sha256 of the embedded cube.lock bytes
	Images        map[string]string `json:"images"`     // ref -> tar path inside the bundle
}

func Vendor(ctx context.Context, lockPath, outPath, platform string, progress io.Writer) error
	// platform "os/arch"; "" = host (runtime.GOOS+"/"+runtime.GOARCH) — the
	// `vendor --platform` flag. CUBE-7001 lock missing/unreadable; CUBE-7002
	// any pull failure (names the artifact/image that failed — no
	// partial-success silence: a bundle is either complete or an error)
func Open(bundlePath string) (*Opened, error)      // extracts to a temp dir, verifies manifest -> CUBE-7003
type Opened struct {
	Dir      string        // extraction root
	Manifest Manifest
	Lock     *lock.File    // parsed embedded lock (the real Phase 2 type)
}
func (o *Opened) PackDir(name string) (string, error)   // CUBE-7004 if absent
func (o *Opened) ImageTars() map[string]string          // ref -> absolute tar path (from Manifest.Images)
func (o *Opened) Verify() error                         // every lock pack + image present -> else CUBE-7004
func (o *Opened) Close()                                // removes the temp dir
```

- [ ] **Step 0 (preparatory commit — Owner Decisions #3, spec D14): pack-declared runtime `images:` merged into the lock**

The air-gap blind spot: operator-style packs (envoy-gateway's proxy, others later) pull images that never appear in rendered manifests, so `lock.ImagesFrom(rendered.Objects)` misses them. Fix at the source, in three small pieces, each TDD'd:

1. `internal/pack`: `pack.cue` gains an optional `images: [...string]` list — parse into a new `Pack.Images []string` field in `loadMeta` (invalid type → the existing CUBE-4003 pack.cue error). Test: fixture pack with `images:`, assert `p.Images`; fixture with `images: 42`, assert CUBE-4003.
2. `internal/up/up.go` lock assembly (~line 190): `Images: lock.ImagesFrom(rendered.Objects)` becomes the sorted union of that and `p.Images`. Test: extend the existing lock-assembly coverage (or add a focused unit) proving a declared image lands in the entry.
3. `internal/lock`: no schema change needed (`Entry.Images` already exists) — document the merged semantics in the `Entry.Images` field comment.

Commit: `git commit -m "feat: D14 — pack-declared runtime images merged into cube.lock entries"`. Task 4's envoy-gateway pack declares its proxy image; this task's vendor consumes `Entry.Images` unchanged.

- [ ] **Step 1: Write the failing tests (round-trip on a synthetic lock, no network)**

`internal/bundle/bundle_test.go`:

```go
package bundle

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/rafpe/cube-idp/internal/diag"
	"github.com/rafpe/cube-idp/internal/lock"
)

// writeLockFixture builds the lock with the REAL lock package (verified
// schema — no hand YAML): push the demo pack to the in-process registry
// (reuse Task 3's helpers — export localRegistry/writeDemoPack from a
// shared internal/oci/ocitest package in this step rather than
// copy-pasting), then:
//   lf := &lock.File{APIVersion: "cube-idp.dev/v1alpha1", Kind: "CubeLock",
//       Engine: lock.EngineLock{Type: "flux"},
//       Packs: []lock.Entry{{Ref: "oci://" + host + "/packs/demo:0.9.9",
//           Name: "demo", Version: "0.9.9",
//           Resolved: "oci:" + digest, Images: nil}}}
//   lock.Write(path, lf)

func TestVendorThenOpenRoundTrip(t *testing.T) {
	// Arrange: writeLockFixture per the helper contract above.
	// Act ("" platform = host):
	out := filepath.Join(t.TempDir(), "bundle.tar.gz")
	if err := Vendor(context.Background(), writeLockFixture(t), out, "", os.Stderr); err != nil {
		t.Fatal(err)
	}
	// Assert: open + verify + pack dir usable.
	o, err := Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()
	if err := o.Verify(); err != nil {
		t.Fatal(err)
	}
	dir, err := o.PackDir("demo")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "pack.cue")); err != nil {
		t.Fatalf("bundled pack dir is not a pack: %v", err)
	}
}

func TestVendorMissingLock(t *testing.T) {
	err := Vendor(context.Background(), "nope.lock", filepath.Join(t.TempDir(), "b.tgz"), "", os.Stderr)
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-7001" {
		t.Fatalf("want CUBE-7001, got %v", err)
	}
}

func TestOpenRejectsGarbage(t *testing.T) {
	p := filepath.Join(t.TempDir(), "garbage.tgz")
	os.WriteFile(p, []byte("not a tarball"), 0o644)
	_, err := Open(p)
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-7003" {
		t.Fatalf("want CUBE-7003, got %v", err)
	}
}

func TestVerifyDetectsTampering(t *testing.T) {
	// Build a valid bundle (as in the round-trip test), extract, truncate
	// packs/demo/pack.cue inside the extraction dir, and assert Verify()
	// returns CUBE-7004. Verify() also checks every Manifest.Images tar
	// exists on disk — delete one and assert CUBE-7004 names the ref.
}
```

(`writeLockFixture` is a test helper assembling the registry + lock via `lock.Write` per the contract comment above; write it fully in this step. The image-pull path gets its coverage in Step 3's test below.)

Run: `go test ./internal/bundle/ -v` — FAIL (package does not exist).

- [ ] **Step 2: Implement bundle.go + vendor.go**

`internal/bundle/vendor.go` core (pure oras-go v2 — go-containerregistry appears ONLY in `_test.go`, Owner Decisions #2):

```go
package bundle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/rafpe/cube-idp/internal/diag"
	"github.com/rafpe/cube-idp/internal/engine/factory"
	"github.com/rafpe/cube-idp/internal/lock"
	"github.com/rafpe/cube-idp/internal/pack"
	"github.com/rafpe/cube-idp/internal/registry"
)

func Vendor(ctx context.Context, lockPath, outPath, platform string, progress io.Writer) error {
	raw, err := os.ReadFile(lockPath)
	if err != nil {
		return diag.Wrap(err, "CUBE-7001", fmt.Sprintf("cannot read %s", lockPath),
			"run `cube-idp up` first — vendor bundles exactly what the lockfile pins")
	}
	lf, err := lock.Read(lockPath) // real entrypoint; (nil, nil) cannot happen here — raw read above already errored
	if err != nil {
		return diag.Wrap(err, "CUBE-7001", lockPath+" is not a valid cube.lock", "re-run `cube-idp up` to regenerate it")
	}
	if platform == "" {
		platform = runtime.GOOS + "/" + runtime.GOARCH
	}

	stage, err := os.MkdirTemp("", "cube-idp-vendor-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	os.WriteFile(filepath.Join(stage, "cube.lock"), raw, 0o644)

	// 1. Pack sources at their locked pins. Entry.Resolved prefixes decide:
	//    "oci:sha256:…" -> re-pull by digest (oci://<repo>@<digest> — beats
	//    tags, which move); "git+<sha>" / "dir:h1:…" -> Fetch(entry.Ref) and
	//    assert the returned Pinned equals Resolved (a moved pin is CUBE-7002
	//    "re-run `cube-idp up` to re-pin").
	for _, p := range lf.Packs {
		fmt.Fprintf(progress, "▸ [vendor] pack %s (%s)\n", p.Name, p.Resolved)
		ref := p.Ref
		if d, ok := strings.CutPrefix(p.Resolved, "oci:"); ok {
			ref = ociRefWithDigest(p.Ref, d) // oci://host/repo@sha256:… (pullOCI accepts digests — verified)
		}
		fetched, err := pack.Fetch(ctx, ref, filepath.Join(stage, ".cache"))
		if err != nil {
			return diag.Wrap(err, "CUBE-7002", fmt.Sprintf("cannot pull pack %q at its locked pin", p.Name),
				"check network/registry access; if the artifact was deleted upstream, re-run `cube-idp up` to re-pin")
		}
		if fetched.Pinned != p.Resolved {
			return diag.New("CUBE-7002", fmt.Sprintf("pack %q resolved to %s but cube.lock pins %s", p.Name, fetched.Pinned, p.Resolved),
				"the source moved since `up` — re-run `cube-idp up` to re-pin, then vendor again")
		}
		if err := copyTree(fetched.Dir, filepath.Join(stage, "packs", p.Name)); err != nil {
			return err
		}
	}

	// 2. Every locked image into its OWN tar (per-image OCI layout, tarred).
	//    Image set = union of per-pack Entry.Images (incl. D14 pack-declared
	//    images) + engine install images + registry images, derived exactly
	//    like `up` does:
	//      eng, _ := factory.New(lf.Engine.Type); em, _ := eng.InstallManifests()
	//      rm, _ := registry.Manifests()
	//      imgs := union(entries..., lock.ImagesFrom(em), lock.ImagesFrom(rm))
	//    For each img: oras.Copy(remote.Repository -> content/oci store in a
	//    fresh dir) with oras.CopyOptions + WithTargetPlatform(parsed
	//    platform); tag the copied desc with the ORIGINAL ref in the layout
	//    (ref.name annotation comes along), then tar the layout dir to
	//    images/<n>.tar and record Manifest.Images[img]. Pull failure ->
	//    CUBE-7002 naming the image; a multi-arch index missing the requested
	//    platform is the same CUBE-7002 (oras errors it).

	sum := sha256.Sum256(raw)
	writeJSON(filepath.Join(stage, "manifest.json"), Manifest{
		FormatVersion: 1, Platform: platform,
		CreatedAt:  time.Now().UTC().Format(time.RFC3339),
		LockDigest: "sha256:" + hex.EncodeToString(sum[:]),
		Images:     imageTarIndex, // built in step 2
	})

	// 3. Single tar.gz, atomically (write to tmp, rename).
	if err := tarGzDir(stage, outPath); err != nil {
		return diag.Wrap(err, "CUBE-7002", "cannot write bundle "+outPath, "check disk space and permissions")
	}
	fmt.Fprintf(progress, "✔ bundle written: %s (%s)\n", outPath, platform)
	return nil
}
```

Helpers to write fully in `bundle.go` (each is standard-library or oras mechanical code; write them, no stubs): `writeJSON`, `ociRefWithDigest` (strips `oci://`, splits `:tag`, appends `@sha256:…`, re-prefixes), `copyTree`, `tarGzDir`, `pullImageTar` (the per-image oras.Copy + tar recipe from step 2 — PlainHTTP for local hosts via the same gate Task 3 added to `internal/oci`), and the `Open`/`Verify`/`PackDir`/`ImageTars`/`Close` side: `Open` extracts with a path-traversal guard (reject entries containing `..`), parses `manifest.json` (any failure → `CUBE-7003` "bundle is unreadable or corrupt" / remediation "re-run `cube-idp vendor`"), checks `FormatVersion == 1`; `Verify` recomputes the lock digest, checks every `lf.Packs[i]` has `packs/<name>/pack.cue`, and every `Manifest.Images` tar exists, else `CUBE-7004` naming the missing entry.

(RESOLVED 2026-07-14: `pack.Fetch` already accepts `oci://host/repo@sha256:…` — `pullOCI` passes `repo.Reference.Reference` (tag OR digest) to `oras.Copy`. No source.go change needed.)

- [ ] **Step 3: Image-path test with the local registry**

Add to `bundle_test.go` a case where the lock's entry lists one image hosted on the in-process test registry (push a tiny image from the `_test.go` side with go-containerregistry — `crane.Push(random.Image(64, 1), …)` — test-only dependency per Owner Decisions #2), vendor it, open the bundle, and assert `ImageTars()` maps the original ref to an existing `.tar` whose contents include an `index.json` (it is an OCI layout tar). Add a second case pinning `--platform`: vendor with an explicit `platform` matching the pushed image's config and assert success (the mismatch path is covered by oras's own error → CUBE-7002 wrap, asserted with a bogus platform string).

- [ ] **Step 4: Command**

`cmd/vendor.go`:

```go
package cmd

import (
	"github.com/spf13/cobra"

	"github.com/rafpe/cube-idp/internal/bundle"
)

func newVendorCmd() *cobra.Command {
	var lockPath, out, platform string
	c := &cobra.Command{
		Use:   "vendor",
		Short: "Bundle every artifact and image pinned in cube.lock for air-gapped installs",
		RunE: func(c *cobra.Command, _ []string) error {
			return bundle.Vendor(c.Context(), lockPath, out, platform, c.OutOrStdout())
		},
	}
	c.Flags().StringVar(&lockPath, "lock", "cube.lock", "path to cube.lock")
	c.Flags().StringVarP(&out, "output", "o", "cube-bundle.tar.gz", "bundle output path")
	c.Flags().StringVar(&platform, "platform", "", "image platform os/arch (default: host platform)")
	return c
}
```

Register in `cmd/root.go`.

- [ ] **Step 5: Run tests**

Run: `go test ./internal/bundle/ -v && go build ./...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add -A && git commit -m "feat: vendor command — cube.lock-driven air-gap bundle (packs + images, verified)"
```

---

### Task 7: `up --bundle` — fully offline install

**Reconcile checkpoint:** requires Task 6. RESOLVED 2026-07-14: `up.Run(ctx, cfgPath string, out io.Writer) error` is UNCHANGED after Phase 2 (verified `internal/up/up.go` line 44) — this task introduces the Options struct and keeps one `Run`; kind is pinned at v0.32.0 and `sigs.k8s.io/kind/pkg/cluster/nodeutils.LoadImageArchive(node, reader)` exists (`kind load image-archive` is the reference consumer — it accepts OCI-layout tars via containerd import). RECONCILE (k3d only — the library enters go.mod in Task 2): exact `ImageImportIntoClusterMulti` signature for the pinned k3d v5.

Offline means: every pack source comes from the bundle, every image reaches the cluster nodes from the bundle, and any attempt to leave those rails is a typed error — never a silent network fallback.

**Files:**
- Create: `internal/bundle/load.go` (image loading into providers)
- Modify: `internal/up/up.go` (options struct + bundle wiring), `cmd/up.go` (`--bundle` flag), `internal/cluster/provider.go` (optional `ImageLoader` capability)
- Test: `internal/up/up_test.go` (pure resolution logic), e2e coverage in Task 13

**Interfaces:**
- Consumes: `bundle.Open/Verify/PackDir/ImageTars`, providers.
- Produces:

```go
package cluster
// ImageLoader is an optional capability of cluster-creating providers:
// load per-image tar archives (Task 6's bundle format — single-image OCI
// layout tars) directly into the cluster nodes' runtime. kindp and k3dp
// implement it; `existing` does not (see CUBE-7005 below).
type ImageLoader interface {
	// imageTars maps the original image ref -> tar path (bundle.Opened.ImageTars()).
	LoadImages(ctx context.Context, name string, imageTars map[string]string) error
}

package up
type Options struct {
	ConfigPath string
	Bundle     string    // path to a vendor bundle; "" = online mode
	Out        io.Writer
}
func Run(ctx context.Context, opts Options) error
// RESOLVED 2026-07-14: Phase 2 did NOT grow options — Run is still
// (ctx, cfgPath, out). This task introduces Options and keeps ONE Run,
// updating the existing callers (cmd/up.go, e2e-exercised paths).
```

- [ ] **Step 1: Failing test for bundle-mode ref resolution (pure)**

The core offline rule is a pure function: given the cube's pack refs and an opened bundle, every `oci://`/git ref must resolve to a bundle dir or fail loudly. Factor it as `up.resolveBundleRefs`:

`internal/up/up_test.go`:

```go
package up

import (
	"errors"
	"testing"

	"github.com/rafpe/cube-idp/internal/config"
	"github.com/rafpe/cube-idp/internal/diag"
)

func TestResolveBundleRefs(t *testing.T) {
	inBundle := map[string]string{"gitea": "/tmp/x/packs/gitea"} // pack name -> dir
	refs := []config.PackRef{{Ref: "oci://ghcr.io/rafpe/cube-idp/packs/gitea:0.1.0"}}
	resolved, err := resolveBundleRefs(refs, func(name string) (string, bool) {
		d, ok := inBundle[name]
		return d, ok
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolved[0].Ref != "/tmp/x/packs/gitea" {
		t.Fatalf("resolved: %+v", resolved)
	}

	_, err = resolveBundleRefs([]config.PackRef{{Ref: "oci://ghcr.io/rafpe/cube-idp/packs/absent:1.0.0"}}, func(string) (string, bool) { return "", false })
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-7004" {
		t.Fatalf("want CUBE-7004 for a ref missing from the bundle, got %v", err)
	}
}
```

Pack-name extraction: RESOLVED 2026-07-14 — the real lock stores name↔ref pairs (`Entry.Ref` + `Entry.Name`, verified), so `resolveBundleRefs` matches each cube ref against `opened.Lock.Packs` by `Ref` equality and resolves to `PackDir(entry.Name)`; the last-path-segment string surgery survives only as the fallback for local-dir refs (which the lock records verbatim).

Run: `go test ./internal/up/ -run TestResolveBundleRefs -v` — FAIL.

- [ ] **Step 2: Implement bundle mode in up.Run**

Wire into the existing orchestration (spec §4.3 sequence), adding exactly three deviations when `opts.Bundle != ""`:

```go
	var opened *bundle.Opened
	if opts.Bundle != "" {
		var err error
		if opened, err = bundle.Open(opts.Bundle); err != nil {
			return err
		}
		defer opened.Close()
		if err := opened.Verify(); err != nil {
			return err
		}
		step(out, "bundle", "verified %s (lock %s)", opts.Bundle, opened.Manifest.LockDigest)
	}
```

Deviation 1 — after `prov` construction, refuse un-loadable topologies up front (fail fast, before any mutation):

```go
	if opened != nil {
		if _, ok := prov.(cluster.ImageLoader); !ok {
			return diag.New("CUBE-7005",
				fmt.Sprintf("--bundle needs a provider that can load images into nodes; %q cannot", cube.Spec.Cluster.Provider),
				"use provider: kind or k3d for air-gapped installs, or pre-load the images into a registry your existing cluster can reach and run `up` without --bundle")
		}
	}
```

Deviation 2 — after `Ensure`, before installing anything, load all images:

```go
	if opened != nil {
		step(out, "bundle", "loading images into cluster nodes")
		if err := prov.(cluster.ImageLoader).LoadImages(ctx, cube.Metadata.Name, opened.ImageTars()); err != nil {
			return err // LoadImages wraps with CUBE-7002 and names the failing image
		}
	}
```

Deviation 3 — before the pack loop, rewrite refs through the bundle:

```go
	if opened != nil {
		refs, err = resolveBundleRefs(refs, opened.PackDirLookup()) // add PackDirLookup() func(string)(string,bool) to bundle.Opened
		if err != nil {
			return err
		}
	}
```

Everything downstream is unchanged: local-dir fetch → render → push to zot (the zot push is in-cluster delivery, not internet) → Deliver. The engine + zot images were loaded in deviation 2, so their pods start without pulling. RESOLVED 2026-07-14 (drift found): flux's `install.yaml` is `IfNotPresent` and zot's manifest pins `v2.1.2` with no explicit policy (default `IfNotPresent`) — fine; but **argocd's `install.yaml` says `imagePullPolicy: Always` on most containers**, which would ignore node-loaded images. Fix in THIS task by flipping them in `hack/gen-argocd-manifests.sh` (the script must inject the change so regeneration keeps it — coordinate with Task 0.5(a)'s reproducibility guard), regenerate, and diff-verify.

- [ ] **Step 3: Implement LoadImages for kindp and k3dp**

The bundle already stores per-image tars (Task 6, Owner Decisions #2), and both loaders consume tars directly — the layout→archive conversion step from the original draft is GONE, and `internal/bundle/load.go` shrinks to iterating `imageTars` in sorted-ref order (deterministic progress output) and handing each path to the provider:

```go
// kindp.LoadImages: for each tar, stream it into every cluster node:
// nodes, _ := provider.ListNodes(name); for each node, open the tar and
// nodeutils.LoadImageArchive(node, reader) (verified present in the pinned
// sigs.k8s.io/kind v0.32.0; containerd import is EXPECTED to accept
// OCI-layout tars — `kind load image-archive` is the reference consumer, but
// this is unproven until Task 13's bundle e2e; on rejection apply the
// docker-archive load-time conversion fallback recorded in Task 6's layout
// contract).
//
// k3dp.LoadImages: k3dclient.ImageImportIntoClusterMulti(ctx,
// runtimes.SelectedRuntime, tarPaths, &types.Cluster{Name: name},
// types.ImageImportOpts{}) — one call, all tars. RECONCILE: exact signature
// for the k3d v5 version pinned in Task 2.
//
// Both wrap failures: diag.Wrap(err, "CUBE-7002",
//   fmt.Sprintf("cannot load image %s into cluster nodes", ref),
//   "verify the bundle with `cube-idp vendor` on a connected machine and retry")
```

Write both implementations fully per those recipes; add `var _ cluster.ImageLoader = (*Kind)(nil)` / `(*K3d)(nil)` assertions.

- [ ] **Step 4: Flag wiring**

`cmd/up.go`: add `c.Flags().StringVar(&bundlePath, "bundle", "", "install fully offline from a cube-idp vendor bundle")` and pass `up.Options{ConfigPath: file, Bundle: bundlePath, Out: c.OutOrStdout()}`.

- [ ] **Step 5: Run tests**

Run: `go test ./internal/up/ ./internal/bundle/ -short -v && go build ./...`
Expected: PASS (full offline proof lands in Task 13's e2e).

- [ ] **Step 6: Commit**

```bash
git add -A && git commit -m "feat: up --bundle — offline install from a vendor bundle (image node-loading, ref pinning)"
```

---

### Task 8: Exec-plugin discovery, env contract, trust warning

**Reconcile checkpoint:** RESOLVED 2026-07-14 (0.6/0.1/0.5 verified): `cmd.Execute(ctx context.Context) error` receives its signal context FROM `main.go` (`signal.NotifyContext(…, os.Interrupt, syscall.SIGTERM)` lives there) and simply calls `NewRootCmd().ExecuteContext(ctx)` — the fallthrough hook below fits that shape without touching signal wiring; the persistent `--plain`/`PersistentPreRunE` hook survives because the fallthrough only intercepts NON-built-in first args. Spec §4.4 tier 2 is the contract: `cube-idp-<name>` on PATH (krew model), env vars `CUBE_IDP_KUBECONFIG`, `CUBE_IDP_CUBE_NAME`, `CUBE_IDP_REGISTRY`, **`CUBE_IDP_CA`** (Owner Decisions #5 — path to the cube-idp CA cert, `trust.EnsureCA`'s CertPath), explicit trust warning on first run.

**Files:**
- Create: `internal/plugin/discover.go`, `internal/plugin/exec.go`, `internal/plugin/trust.go`, `cmd/plugin.go`
- Modify: `cmd/root.go` (or wherever `Execute` lives — fallthrough hook)
- Test: `internal/plugin/plugin_test.go`

**Interfaces:**
- Consumes: `config.Load` (best-effort), `cluster.New` + `Kubeconfig` (best-effort), `registry.InClusterURL`, `diag`.
- Produces:

```go
package plugin

// Lookup finds cube-idp-<name> on PATH or in InstallDir() (Task 9's install
// target). Returns the absolute path.
func Lookup(name string) (path string, found bool)
func InstallDir() string // ~/.local/share/cube-idp/plugins (XDG-derived; os.UserHomeDir fallback)
func List() []Descriptor // every discovered plugin: {Name, Path, Trusted bool}
type Descriptor struct{ Name, Path string; Trusted bool }

// Exec replaces cube-idp's process semantics with the plugin's: it inherits
// stdio, receives the env contract, and its exit code is propagated verbatim.
// Refuses untrusted plugins (CUBE-7104) unless the trust store approves or
// the user confirms interactively.
func Exec(ctx context.Context, path string, args []string, env Env) error
type Env struct{ Kubeconfig, CubeName, Registry, CA string } // "" entries are omitted from the environment; CA = CUBE_IDP_CA (Owner Decisions #5)

// Trust store: ~/.config/cube-idp/trust.json — map[plugin path]sha256.
// EnsureTrusted: known+matching sha -> nil. Unknown or CHANGED sha ->
// interactive confirm (stderr prompt, default no) when stdin is a TTY,
// else CUBE-7104 with remediation "run `cube-idp plugin trust <name>`".
// A changed hash is called out as such — an updated binary re-prompts.
func EnsureTrusted(name, path string, interactive bool) error
func Trust(name, path string) error // records the current sha256 unconditionally (used by `plugin trust` and Task 9's verified installs)
```

- [ ] **Step 1: Write the failing tests**

`internal/plugin/plugin_test.go`:

```go
package plugin

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/rafpe/cube-idp/internal/diag"
)

// fakePlugin writes an executable cube-idp-<name> into dir that dumps its
// env and args, exiting with the given code.
func fakePlugin(t *testing.T, dir, name string, exitCode int) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("plugin exec tests are unix-only")
	}
	p := filepath.Join(dir, "cube-idp-"+name)
	script := "#!/bin/sh\necho \"CUBE_IDP_CUBE_NAME=$CUBE_IDP_CUBE_NAME\"\nexit " +
		string(rune('0'+exitCode)) + "\n"
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLookupFindsPathBinaries(t *testing.T) {
	dir := t.TempDir()
	fakePlugin(t, dir, "hello", 0)
	t.Setenv("PATH", dir)
	if p, ok := Lookup("hello"); !ok || p != filepath.Join(dir, "cube-idp-hello") {
		t.Fatalf("lookup: %q %v", p, ok)
	}
	if _, ok := Lookup("absent"); ok {
		t.Fatal("found a plugin that does not exist")
	}
}

func TestExecRefusesUntrustedWhenNonInteractive(t *testing.T) {
	dir := t.TempDir()
	p := fakePlugin(t, dir, "hello", 0)
	t.Setenv("HOME", t.TempDir()) // empty trust store
	t.Setenv("XDG_CONFIG_HOME", "")
	err := Exec(context.Background(), p, nil, Env{CubeName: "dev"})
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-7104" {
		t.Fatalf("want CUBE-7104, got %v", err)
	}
}

func TestExecRunsTrustedPluginAndPropagatesExit(t *testing.T) {
	dir := t.TempDir()
	p := fakePlugin(t, dir, "boom", 3)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	if err := Trust("boom", p); err != nil {
		t.Fatal(err)
	}
	err := Exec(context.Background(), p, nil, Env{CubeName: "dev"})
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 3 {
		t.Fatalf("want ExitError code 3, got %v", err)
	}
}

func TestTrustDetectsChangedBinary(t *testing.T) {
	dir := t.TempDir()
	p := fakePlugin(t, dir, "mutant", 0)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	if err := Trust("mutant", p); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0o755) // binary changed after trust
	err := EnsureTrusted("mutant", p, false)
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-7104" {
		t.Fatalf("changed binary must re-require trust: got %v", err)
	}
}
```

Run: `go test ./internal/plugin/ -v` — FAIL (package does not exist).

- [ ] **Step 2: Implement discover.go / trust.go / exec.go**

`discover.go` — `Lookup` walks `$PATH` entries + `InstallDir()` with `exec.LookPath` semantics (must be executable); `List` globs both for the `cube-idp-*` prefix. `trust.go` — trust file at `os.UserConfigDir()/cube-idp/trust.json`; `sha256File(path)`; `EnsureTrusted` per the contract above, with the interactive prompt written to stderr:

```
! plugin "hello" (/usr/local/bin/cube-idp-hello) is not trusted yet.
  cube-idp plugins run with your full user permissions.
  sha256: 3b1f…
  Run it and remember this hash? [y/N]
```

`exec.go`:

```go
func Exec(ctx context.Context, path string, args []string, env Env) error {
	name := strings.TrimPrefix(filepath.Base(path), "cube-idp-")
	interactive := term.IsTerminal(int(os.Stdin.Fd())) // golang.org/x/term v0.45.0 — verified DIRECT require in go.mod
	if err := EnsureTrusted(name, path, interactive); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = os.Environ()
	for k, v := range map[string]string{
		"CUBE_IDP_KUBECONFIG": env.Kubeconfig,
		"CUBE_IDP_CUBE_NAME":  env.CubeName,
		"CUBE_IDP_REGISTRY":   env.Registry,
		"CUBE_IDP_CA":         env.CA,
	} {
		if v != "" {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return err // plugin's own failure: propagate the exit code, do NOT wrap — its output is its diagnosis
		}
		return diag.Wrap(err, "CUBE-7103", fmt.Sprintf("plugin %q failed to execute", name),
			"check that the plugin binary is executable and built for this platform")
	}
	return nil
}
```

- [ ] **Step 3: Root-command fallthrough + env assembly**

In `cmd/root.go`'s `Execute` (real shape verified 2026-07-14 — signal wiring stays in `main.go`, which passes the SIGINT/SIGTERM-cancelable ctx in):

```go
func Execute(ctx context.Context) error {
	root := NewRootCmd()
	if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-") {
		if _, _, err := root.Find(os.Args[1:]); err != nil { // not a built-in command
			if path, ok := plugin.Lookup(os.Args[1]); ok {
				return plugin.Exec(ctx, path, os.Args[2:], pluginEnv(ctx))
			}
			return diag.New("CUBE-7101",
				fmt.Sprintf("unknown command %q and no cube-idp-%s plugin found on PATH", os.Args[1], os.Args[1]),
				"run `cube-idp plugin list` to see discovered plugins, or `cube-idp --help` for built-in commands")
		}
	}
	return root.ExecuteContext(ctx)
}
```

`pluginEnv(ctx)` (same file) is **best-effort by design** — plugins must run even with no cube.yaml/cluster around: `CubeName` from `config.Load("cube.yaml")` if it loads (else empty); `Kubeconfig` = path to a `0600` temp file containing `provider.Kubeconfig(ctx, name)` if the provider resolves and the cluster exists (else empty); `Registry` = `registry.InClusterURL` when a kubeconfig was resolvable (the plugin reaches it via its own port-forward — document this in the README plugin section), else empty; `CA` = the cube-idp CA cert path when it exists on disk (`trust.Dir()` + the CA cert filename — read-only check, do NOT create a CA as a side effect of running a plugin), else empty (Owner Decisions #5). Empty entries are omitted (see `Env`); a plugin that requires them must error itself. No cube-idp error is raised for missing env — that would break cluster-independent plugins. README (Task 13) also documents that zot is reachable from the HOST at `https://registry.<gateway.host>` (the HTTPRoute already exists — `internal/registry/route.go` `GatewayRoute`, verified) for plugins running where that hostname resolves, with `CUBE_IDP_CA` as the trust anchor.

`cmd/plugin.go` — `plugin list` (table: NAME / PATH / TRUSTED via `plugin.List()`) and `plugin trust <name>` (`Lookup` → `Trust`, printing the recorded sha256; `CUBE-7101` if not found). Register in root.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/plugin/ ./cmd/ -short -v && go build ./...`
Expected: PASS. Manual check: `go build -o /tmp/cube-idp . && PATH=/tmp/fakeplug:$PATH /tmp/cube-idp hello` prompts for trust, runs, and `cube-idp nosuch` renders CUBE-7101.

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat: exec-plugin discovery, env contract, and first-run trust store (spec 4.4 tier 2)"
```

---

### Task 9: Plugin index — sha256-pinned install

**Reconcile checkpoint:** requires Task 8 (`InstallDir`, `Trust`, `Lookup`), 0.5 (diag). No prior-phase code beyond that — the index is new surface.

Spec §4.4: "sha256-pinned git index." Design: an index is a git repository containing `plugins/<name>.yaml` descriptors; each descriptor pins per-platform archive URLs by sha256. The index is fetched with the system `git` (pinnable to a commit); archives over HTTPS; every byte verified before anything is executable on disk. Verified installs are auto-trusted (the sha was proven, which is exactly what the trust prompt would establish).

**Files:**
- Create: `internal/plugin/index.go`
- Modify: `cmd/plugin.go` (add `plugin install`)
- Test: `internal/plugin/index_test.go`

**Interfaces:**
- Consumes: Task 8's `InstallDir`, `Trust`; `diag`.
- Produces:

```go
package plugin

// NO DefaultIndex constant (RESOLVED 2026-07-14, Owner Decisions #8):
// `--index` is required until a first real plugin exists; `plugin install`
// without it fails with the CUBE-7102 empty-check in Step 3. Never point a
// default at a repo that does not exist.

type IndexEntry struct { // plugins/<name>.yaml
	Name             string     `yaml:"name"`
	ShortDescription string     `yaml:"shortDescription"`
	Platforms        []Platform `yaml:"platforms"`
}
type Platform struct {
	OS     string `yaml:"os"`     // GOOS
	Arch   string `yaml:"arch"`   // GOARCH
	URL    string `yaml:"url"`    // .tar.gz containing the plugin binary
	SHA256 string `yaml:"sha256"` // hex digest of the archive
	Bin    string `yaml:"bin"`    // path of the binary inside the archive
}

// Install fetches indexURL (git; optional @<commit> suffix pins the index
// itself), reads plugins/<name>.yaml, downloads the current-platform
// archive, verifies sha256, extracts Bin into InstallDir() as
// cube-idp-<name> (0755), and records trust. Errors -> CUBE-7102
// (fetch/verify) or CUBE-7101 (name not in index).
func Install(ctx context.Context, indexURL, name string) error
```

- [ ] **Step 1: Write the failing tests (local git repo + httptest server — no network)**

`internal/plugin/index_test.go`:

```go
package plugin

import (
	"archive/tar"
	"compress/gzip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http/httptest"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/rafpe/cube-idp/internal/diag"
)

func tgzWithBin(t *testing.T, binName, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: binName, Mode: 0o755, Size: int64(len(content))})
	tw.Write([]byte(content))
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func gitIndex(t *testing.T, entryYAML string) string {
	t.Helper()
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "plugins"), 0o755)
	os.WriteFile(filepath.Join(dir, "plugins", "hello.yaml"), []byte(entryYAML), 0o644)
	for _, args := range [][]string{
		{"init", "-q"}, {"add", "-A"},
		{"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "index"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func TestInstallVerifiesShaAndInstalls(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only")
	}
	archive := tgzWithBin(t, "cube-idp-hello", "#!/bin/sh\necho hi\n")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write(archive) }))
	t.Cleanup(srv.Close)
	sum := sha256.Sum256(archive)
	entry := fmt.Sprintf(
		"name: hello\nshortDescription: test\nplatforms:\n  - os: %s\n    arch: %s\n    url: %s/a.tgz\n    sha256: %s\n    bin: cube-idp-hello\n",
		runtime.GOOS, runtime.GOARCH, srv.URL, hex.EncodeToString(sum[:]))
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")

	if err := Install(context.Background(), gitIndex(t, entry), "hello"); err != nil {
		t.Fatal(err)
	}
	p, ok := Lookup("hello")
	if !ok {
		t.Fatal("installed plugin not discoverable")
	}
	if err := EnsureTrusted("hello", p, false); err != nil {
		t.Fatalf("verified install must be pre-trusted: %v", err)
	}
}

func TestInstallRejectsShaMismatch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only")
	}
	archive := tgzWithBin(t, "cube-idp-hello", "#!/bin/sh\necho evil\n")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write(archive) }))
	t.Cleanup(srv.Close)
	entry := fmt.Sprintf(
		"name: hello\nshortDescription: test\nplatforms:\n  - os: %s\n    arch: %s\n    url: %s/a.tgz\n    sha256: %s\n    bin: cube-idp-hello\n",
		runtime.GOOS, runtime.GOARCH, srv.URL, "deadbeef"+string(bytes.Repeat([]byte("0"), 56)))
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")

	err := Install(context.Background(), gitIndex(t, entry), "hello")
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-7102" {
		t.Fatalf("want CUBE-7102 on sha mismatch, got %v", err)
	}
	if _, ok := Lookup("hello"); ok {
		t.Fatal("a sha-mismatched plugin must never land in InstallDir")
	}
}

func TestInstallUnknownPlugin(t *testing.T) {
	err := Install(context.Background(), gitIndex(t, "name: hello\nplatforms: []\n"), "absent")
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-7101" {
		t.Fatalf("want CUBE-7101, got %v", err)
	}
}
```

Run: `go test ./internal/plugin/ -run TestInstall -v` — FAIL (`Install` undefined).

- [ ] **Step 2: Implement index.go**

Mechanics (write in full; every failure path shown gets the stated code):

1. Split optional `@<commit>` off `indexURL`. `git clone --depth 1 <url> <tmp>`; if a commit was pinned, `git -C <tmp> fetch -q origin <commit> && git -C <tmp> checkout -q <commit>`. `exec.LookPath("git")` failure or any git error → `CUBE-7102` "cannot fetch plugin index" / remediation "install git, check the index URL, or pass a different --index".
2. Read + YAML-decode `plugins/<name>.yaml`; missing file → `CUBE-7101` "plugin %q is not in index %s" / "run `git ls-tree` on the index or check the name".
3. Select the `Platform` matching `runtime.GOOS`/`runtime.GOARCH`; none → `CUBE-7102` "no %s/%s build of plugin %q in the index".
4. `http.Get` the URL into memory (plugins are small; enforce a 256 MiB cap with `io.LimitReader` → over-cap is `CUBE-7102`), compute sha256, compare case-insensitively to the pinned hex; mismatch → `CUBE-7102` `"sha256 mismatch for %s: index pins %s…, got %s…"` / remediation "the archive changed since the index pinned it — do not install; report it to the index maintainers".
5. Extract only the `Bin` entry from the tar.gz (path-traversal guard: reject `..` and absolute names), write to `InstallDir()/cube-idp-<name>` with `0o755` via temp-file + rename (atomic; never leave a half-written executable).
6. `Trust(name, installedPath)`.

- [ ] **Step 3: `plugin install` command**

In `cmd/plugin.go`:

```go
	var index string
	install := &cobra.Command{
		Use:   "install <name>",
		Short: "Install a plugin from a sha256-pinned git index",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if index == "" {
				return diag.New("CUBE-7102", "no plugin index configured",
					"pass --index <git-url>[@commit]; a default public index is planned but not yet published")
			}
			return plugin.Install(c.Context(), index, args[0])
		},
	}
	install.Flags().StringVar(&index, "index", "", "git URL of the plugin index (optionally @commit-pinned)")
```

(RESOLVED 2026-07-14, Owner Decisions #8: no default index repo yet — the empty-check ships as drafted; create the real index repo when the first real plugin exists, then revisit with a plan amendment.)

- [ ] **Step 4: Run tests**

Run: `go test ./internal/plugin/ -v && go build ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat: plugin install from sha256-pinned git index with auto-trust on verified installs"
```

---

### Task 10a: Engine surface — `Poke` + `DeliverGit` in one pass (Owner Decisions #4)

**Reconcile checkpoint:** RESOLVED 2026-07-14 (0.4 verified): `engine.Engine` = `Install / InstallManifests / Deliver / Health / Uninstall`; contract suite `internal/engine/contract` (`Impl{Name, New}`, `Run(t, impl)`, envtest subtests gated on `KUBEBUILDER_ASSETS` via `make test-engines`); factory `internal/engine/factory.New`. Delivery naming verified in BOTH engines: `"cube-idp-" + r.Name` — flux OCIRepository+Kustomization in ns `flux-system` (`internal/engine/flux/deliver.go`), argocd a single Application in ns `argocd` with `project: default`, `destination.server: https://kubernetes.default.svc`, `syncPolicy.automated {prune, selfHeal}` + `CreateNamespace=true, ServerSideApply=true` and the resources-finalizer (`internal/engine/argocd/deliver.go` — `DeliverGit` copies that scaffolding, changing only the source block).

D2 discipline: BOTH interface extensions land here, in one commit series, for BOTH engines, WITH their contract-suite cases — `sync` (Tasks 10/11) and `repo create` (Task 12) then consume a finished surface and can proceed in parallel. An interface method without contract coverage for both engines is a D2 violation.

**Files:**
- Modify: `internal/engine/engine.go` (+`Poke`, +`GitSource`, +`DeliverGit`), `internal/engine/flux/` (deliver.go or a new poke.go/delivergit.go), `internal/engine/argocd/` (same), `internal/engine/contract/contract.go`
- Test: flux/argocd `Poke` + `DeliverGit` unit tests, contract-suite extension

**Interfaces:**
- Consumes: `apply.Applier`, existing engine internals.
- Produces:

```go
package engine
// Poke asks the engine to reconcile the delivered pack now instead of on
// its poll interval. packName matches the name Deliver was called with.
// Implementations must be idempotent and cheap (an annotation patch).
// Poke works for BOTH delivery shapes: flux pokes the OCIRepository or
// GitRepository named cube-idp-<pack> (whichever exists), argocd always
// pokes the Application.
type GitSource struct {
	URL    string // in-cluster clone URL, e.g. http://gitea-http.gitea.svc.cluster.local:3000/<owner>/<repo>.git
	Branch string // default "main"
	Path   string // default "./"
}
// DeliverGit registers a continuously-synced git source with the engine
// (flux: GitRepository + Kustomization; argocd: Application with a git
// source). Same purity rule as Deliver: returns objects, caller applies.
Engine interface {
	…existing methods…
	Poke(ctx context.Context, a *apply.Applier, packName string) error
	DeliverGit(ctx context.Context, name string, src GitSource) ([]*unstructured.Unstructured, error)
}
```

- [ ] **Step 1: Failing shape tests — Poke (both engines)**

Flux (`internal/engine/flux/…_test.go` — place next to the existing Deliver tests):

```go
func TestPokePatchesOCIRepositoryAnnotation(t *testing.T) {
	// Against envtest (reuse the contract suite's startEnvtest scaffolding
	// pattern — do not build a second harness):
	// 1. Apply the OCIRepository CRD + a Deliver-shaped OCIRepository for
	//    pack "demo".
	// 2. f.Poke(ctx, applier, "demo")
	// 3. Get the OCIRepository; assert metadata.annotations
	//    ["reconcile.fluxcd.io/requestedAt"] parses as a recent RFC3339Nano
	//    timestamp (that is the value `flux reconcile` writes).
}
```

Argo CD equivalent asserts `argocd.argoproj.io/refresh: "normal"` on the pack's Application (name `cube-idp-demo`, ns `argocd` — verified naming). Run — FAIL (methods undefined).

- [ ] **Step 2: Failing shape tests — DeliverGit (both engines)**

Flux shape test (next to the Deliver tests):

```go
func TestDeliverGitShapesFluxObjects(t *testing.T) {
	f := New()
	objs, err := f.DeliverGit(context.Background(), "app",
		engine.GitSource{URL: "http://gitea-http.gitea.svc.cluster.local:3000/gitea_admin/app.git", Branch: "main", Path: "./"})
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 2 || objs[0].GetKind() != "GitRepository" || objs[1].GetKind() != "Kustomization" {
		t.Fatalf("want GitRepository+Kustomization, got %+v", kinds(objs))
	}
	// url, ref.branch, sourceRef.kind=GitRepository, prune=true — assert like
	// the Phase 1 Deliver test asserted OCIRepository fields.
}
```

Argo CD: one `Application` named `cube-idp-<name>` in ns `argocd` with `spec.source.repoURL/targetRevision/path` and the verified Deliver scaffolding otherwise unchanged. Run — FAIL.

- [ ] **Step 3: Implement (both engines)**

Flux `Poke` (~20 lines): `a.Client().Get` the OCIRepository `cube-idp-<packName>` in `flux-system` (naming verified — `"cube-idp-" + r.Name`), set annotation `reconcile.fluxcd.io/requestedAt: time.Now().Format(time.RFC3339Nano)`, `a.Client().Update`; if the OCIRepository is NotFound, try the GitRepository of the same name (git deliveries); both NotFound → `diag.New("CUBE-3007", fmt.Sprintf("pack %q has no delivery source to poke", packName), "run `cube-idp sync <dir>` or `cube-idp up` first — Poke only refreshes an existing delivery")`. Argo CD mirrors this on the Application with its refresh annotation (one shape covers both delivery kinds).

Flux `DeliverGit` mirrors `Deliver` with a `source.toolkit.fluxcd.io/v1` `GitRepository` (`spec.url`, `spec.ref.branch`, `interval: 30s`) + a `Kustomization` whose `sourceRef.kind` is `GitRepository` and `spec.path` is `src.Path`. Argo CD: copy the verified Application scaffolding from `deliver.go`, changing only the source block to `repoURL/targetRevision/path`.

- [ ] **Step 4: Contract-suite cases (both engines, same commit series)**

In `internal/engine/contract/contract.go` add: a `deliver_git_returns_addressable_objects` case (both engines produce applyable, kind/name/namespace-complete objects for the same `GitSource`, referencing the URL and branch) and a `poke_is_idempotent` envtest case (Deliver a fake pack, apply, `Poke` twice — no error both times; a `Poke` of an undelivered pack is CUBE-3007). Both engines' existing `contract_test.go` registrations pick the cases up automatically.

- [ ] **Step 5: Run tests**

Run: `go test ./internal/engine/... -short -v && go build ./... && make test-engines` (envtest legs)
Expected: PASS for BOTH engines.

- [ ] **Step 6: Commit**

```bash
git add -A && git commit -m "feat: engine surface — Poke and DeliverGit across both engines with contract coverage"
```

---

### Task 10: `cube-idp sync <dir>` (one-shot) + generic port-forward

**Reconcile checkpoint:** requires Task 10a (the engine surface — this task is a pure consumer) and 0.9 (verified: `oci.PushRendered` returns `ArtifactRef{Repo, Tag}` — NO digest; the digest-return extension happens here, Step 3). Generalizes Phase 1's `registry.PortForward` into `internal/kube` for reuse by Task 12. **Inventory semantics checkpoint (Owner Decisions #14) — PRE-ANSWERED 2026-07-14: `Applier.RecordInventory` MERGES (`object.ObjMetadataSet.Union` with the loaded existing set — verified in `internal/apply/inventory.go`), so `SyncOnce`'s RecordInventory cannot orphan `up`-applied entries. No code change needed; keep a regression assertion in this task's tests (sync after a recorded entry preserves it).**

D7: `cube-idp sync ./dir --watch` = fsnotify → OCI artifact push → engine reconciles. Engines poll their sources on an interval; for a live-feedback loop that interval is too slow — `Poke` (Task 10a) closes the gap.

**Files:**
- Create: `internal/kube/portforward.go`, `internal/syncer/syncer.go`, `cmd/sync.go`
- Modify: `internal/registry/portforward.go` (delegate), `internal/oci/push.go` (digest return)
- Test: `internal/syncer/syncer_test.go`

**Interfaces:**
- Consumes: `pack.Fetch/Render`, `oci.PushRendered`, `engine.Engine` (incl. Task 10a's `Poke`), `apply.Applier`, `registry`.
- Produces:

```go
package kube
// PortForward generalizes Phase 1's registry-only tunnel: forward a free
// local port to the first running pod matching selector in ns, targeting
// podPort. Returns "127.0.0.1:<port>" and a stop func. Failure -> the
// caller's domain code (it wraps), so this returns plain errors.
func PortForward(ctx context.Context, cfg *rest.Config, ns, selector string, podPort int) (string, func(), error)

package syncer
type Result struct{ Pack, Version, Digest string }
// SyncOnce renders dir as a pack, pushes it to the cube's zot via a
// port-forward tunnel, ensures engine delivery objects exist, and Pokes.
// Dirs without pack.cue are synthesized: name = filepath.Base(dir),
// version = "0.0.0-dev", manifests = every *.yaml in dir (CUBE-7201 if
// nothing renderable).
func SyncOnce(ctx context.Context, deps Deps, dir string) (Result, error)
type Deps struct { // assembled by cmd/sync.go from cube.yaml, injected for testability
	Applier  *apply.Applier
	Engine   engine.Engine
	REST     *rest.Config
	Out      io.Writer
	// PushAddr optionally overrides the zot tunnel (tests inject a local registry)
	PushAddr string
}
```

- [ ] **Step 1: Failing test — pack synthesis for bare manifest dirs**

`internal/syncer/syncer_test.go`:

```go
package syncer

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/rafpe/cube-idp/internal/diag"
)

func TestSynthesizePackFromBareDir(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "cm.yaml"),
		[]byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: x\n  namespace: default\n"), 0o644)
	p, err := loadOrSynthesize(dir)
	if err != nil {
		t.Fatal(err)
	}
	r, err := p.Render(nil)
	if err != nil || len(r.Objects) != 1 {
		t.Fatalf("render: %v (%d objects)", err, len(r.Objects))
	}
	if r.Name != filepath.Base(dir) || r.Version != "0.0.0-dev" {
		t.Fatalf("synthesized identity: %s@%s", r.Name, r.Version)
	}
}

func TestSyncRejectsEmptyDir(t *testing.T) {
	_, err := loadOrSynthesize(t.TempDir())
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-7201" {
		t.Fatalf("want CUBE-7201, got %v", err)
	}
}
```

RESOLVED 2026-07-14: `pack.Pack`'s fields ARE exported (`Name, Version, Dir, Pinned, Expose` — Phase 1 shape intact) and `Render` reads `manifests/` under `Dir` — synthesize by staging a temp dir: copy `*.yaml` into `<tmp>/manifests/` and return `&pack.Pack{Name: base, Version: "0.0.0-dev", Dir: tmp}`. (The exported-constructor alternative is not needed.)

Run: `go test ./internal/syncer/ -v` — FAIL.

- [ ] **Step 2: Implement kube.PortForward + the registry delegate**

`internal/kube/portforward.go`: move Phase 1's `registry.PortForward` body, parameterizing `ns`, `selector` (label selector string), `podPort`; `internal/registry/portforward.go` becomes a two-line delegate (`kube.PortForward(ctx, cfg, "cube-idp-system", "app=zot", 5000)`) wrapped with the existing `CUBE-5002` diag. Run the registry package's tests to prove neutrality.

- [ ] **Step 3: Implement SyncOnce (consuming Task 10a's Poke) + the PushRendered digest return**

`internal/syncer/syncer.go`:

```go
package syncer

// SyncOnce (D7, one iteration):
//   p := loadOrSynthesize(dir)                 -> CUBE-7201
//   rendered := p.Render(nil)                  -> pack's own CUBE-4xxx codes pass through
//   addr := deps.PushAddr; if "" { addr, stop = kube.PortForward(deps.REST, "cube-idp-system", "app=zot", 5000) wrapped as CUBE-5002; defer stop() }
//   ref := oci.PushRendered(ctx, rendered, addr)          -> CUBE-5003 passes through
//   objs := deps.Engine.Deliver(ctx, rendered, ref)
//   deps.Applier.Apply(ctx, objs, false, 2*time.Minute)   // idempotent SSA — safe every iteration
//   deps.Applier.RecordInventory(ctx, objs)               // sync'd packs are down-able; RecordInventory MERGES (verified — Owner Decisions #14)
//   deps.Engine.Poke(ctx, deps.Applier, rendered.Name)
//   return Result{rendered.Name, rendered.Version, digest}
// RESOLVED 2026-07-14: PushRendered returns only ArtifactRef{Repo, Tag}
// today — extend engine.ArtifactRef with a Digest field in THIS step
// (pushRenderedTo already holds the manifestDesc — expose desc.Digest),
// updating its existing callers (up, argocd/flux Deliver ignore it).
// Task 11's change-skip logic consumes it.
```

Write it in full (~60 lines) following that sequence; every step's error is already typed by the layer that produced it — SyncOnce adds no codes of its own beyond `CUBE-7201`.

`cmd/sync.go` — assembles `Deps` exactly like `status`/`down` connect (config → provider Ensure → Applier → engine factory), takes `<dir>` as `cobra.ExactArgs(1)`, prints `✔ synced <name>@<version> — engine reconciling`. The `--watch` flag lands in Task 11; declare it now, and until Task 11 make `--watch` return `diag.New("CUBE-7202", "watch mode lands in the next task of this plan", "run without --watch")` so the flag surface is stable — remove that stub in Task 11.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/syncer/ ./internal/oci/ ./internal/kube/ ./internal/registry/ -short -v && go build ./...`
Expected: PASS (Task 10a already proved the engine surface; this task's tests cover synthesis, port-forward neutrality, and the digest return).

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat: generic port-forward and one-shot sync command (D7 groundwork)"
```

---

### Task 11: `sync --watch` — fsnotify loop

**Reconcile checkpoint:** requires Task 10 (SyncOnce, digest-returning push) and Task 10a (Poke). Signal context RESOLVED 2026-07-14: `main.go`'s `signal.NotifyContext` (SIGINT/SIGTERM) flows through `Execute(ctx)`/`ExecuteContext` into every RunE — Ctrl-C cancellation reaches the watch loop for free.

Watch semantics (all decisions explicit, none deferred): recursive watch of the dir; 300 ms debounce (editors emit bursts); dotfiles/dirs and editor droppings (`*~`, `*.swp`, `.#*`, `4913`) ignored; new subdirectories are added to the watch on creation; a sync failure mid-watch is **rendered loudly and the watch continues** (documented behavior — the developer is mid-edit; killing the loop on a YAML typo would defeat the feature; this is not a silent fallback because every failure prints its full `diag.Render` block); unchanged renders (same digest) are skipped with a quiet note; Ctrl-C exits 0.

**Files:**
- Create: `internal/syncer/watch.go`
- Modify: `cmd/sync.go` (activate `--watch`)
- Test: `internal/syncer/watch_test.go`

**Interfaces:**
- Consumes: `SyncOnce`, fsnotify.
- Produces:

```go
package syncer
// Watch runs SyncOnce, then blocks: re-syncs on every debounced change
// under dir until ctx is cancelled. Returns nil on cancellation, an error
// only for unrecoverable setup failures (CUBE-7202).
func Watch(ctx context.Context, deps Deps, dir string, debounce time.Duration) error
```

- [ ] **Step 1: Write the failing test (fake sync fn — the loop is what's under test)**

Refactor seam: `Watch` calls an injectable `syncFn func(context.Context) error` field on `Deps` (defaulting to a `SyncOnce` closure) so the loop is testable without a cluster.

`internal/syncer/watch_test.go`:

```go
package syncer

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestWatchDebouncesAndResyncs(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "cm.yaml"), []byte("a: 1\n"), 0o644)

	var syncs atomic.Int32
	deps := Deps{Out: os.Stderr, syncFn: func(context.Context) error { syncs.Add(1); return nil }}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Watch(ctx, deps, dir, 50*time.Millisecond) }()

	waitFor(t, func() bool { return syncs.Load() == 1 }, "initial sync") // sync #1 on start
	// A burst of writes must coalesce into ONE debounced sync.
	for i := 0; i < 5; i++ {
		os.WriteFile(filepath.Join(dir, "cm.yaml"), []byte("a: 2\n"), 0o644)
		time.Sleep(5 * time.Millisecond)
	}
	waitFor(t, func() bool { return syncs.Load() == 2 }, "debounced sync")
	time.Sleep(150 * time.Millisecond)
	if got := syncs.Load(); got != 2 {
		t.Fatalf("burst produced %d syncs, want 2 total", got)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Watch must return nil on cancellation: %v", err)
	}
}

func TestWatchSurvivesSyncErrors(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "cm.yaml"), []byte("a: 1\n"), 0o644)
	calls := atomic.Int32{}
	deps := Deps{Out: os.Stderr, syncFn: func(context.Context) error {
		if calls.Add(1) == 2 {
			return context.DeadlineExceeded // any error: loop must keep going
		}
		return nil
	}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Watch(ctx, deps, dir, 30*time.Millisecond) }()
	waitFor(t, func() bool { return calls.Load() == 1 }, "initial")
	os.WriteFile(filepath.Join(dir, "cm.yaml"), []byte("a: 2\n"), 0o644) // -> failing sync #2
	waitFor(t, func() bool { return calls.Load() == 2 }, "failing sync")
	os.WriteFile(filepath.Join(dir, "cm.yaml"), []byte("a: 3\n"), 0o644) // -> sync #3 proves survival
	waitFor(t, func() bool { return calls.Load() == 3 }, "post-error sync")
	cancel()
	<-done
}

func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second) // every wait has a deadline, tests included
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}
```

Run: `go test ./internal/syncer/ -run TestWatch -v` — FAIL.

- [ ] **Step 2: Implement watch.go**

```bash
go get github.com/fsnotify/fsnotify@latest
```

```go
package syncer

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/rafpe/cube-idp/internal/diag"
)

func Watch(ctx context.Context, deps Deps, dir string, debounce time.Duration) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return diag.Wrap(err, "CUBE-7202", "cannot start the filesystem watcher",
			"on Linux, raise fs.inotify.max_user_watches (sysctl); then retry")
	}
	defer w.Close()
	addRecursive := func(root string) error {
		return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || !d.IsDir() || strings.HasPrefix(d.Name(), ".") && p != root {
				if d != nil && d.IsDir() && strings.HasPrefix(d.Name(), ".") && p != root {
					return filepath.SkipDir
				}
				return err
			}
			return w.Add(p)
		})
	}
	if err := addRecursive(dir); err != nil {
		return diag.Wrap(err, "CUBE-7202", "cannot watch "+dir, "check the directory exists and is readable")
	}

	syncOnce := deps.syncFn
	if syncOnce == nil {
		syncOnce = func(c context.Context) error { _, err := SyncOnce(c, deps, dir); return err }
	}
	runSync := func() {
		if err := syncOnce(ctx); err != nil {
			fmt.Fprintln(deps.Out, diag.Render(err)) // loud, non-fatal: developer is mid-edit
			fmt.Fprintln(deps.Out, "  (still watching — fix the file and save again)")
		}
	}

	runSync() // initial sync on start
	var timer *time.Timer
	fire := make(chan struct{}, 1)
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-w.Events:
			if !ok {
				return nil
			}
			if ignored(ev.Name) {
				continue
			}
			if ev.Op.Has(fsnotify.Create) {
				_ = addRecursive(ev.Name) // new subdirs join the watch; non-dirs error harmlessly
			}
			if timer != nil {
				timer.Stop()
			}
			timer = time.AfterFunc(debounce, func() {
				select {
				case fire <- struct{}{}:
				default:
				}
			})
		case <-fire:
			runSync()
		case err, ok := <-w.Errors:
			if !ok {
				return nil
			}
			fmt.Fprintln(deps.Out, diag.Render(diag.Wrap(err, "CUBE-7202", "filesystem watcher error", "if this repeats, restart `cube-idp sync --watch`")))
		}
	}
}

func ignored(path string) bool {
	base := filepath.Base(path)
	return strings.HasPrefix(base, ".") || strings.HasSuffix(base, "~") ||
		strings.HasSuffix(base, ".swp") || strings.HasPrefix(base, ".#") || base == "4913"
}
```

Digest-skip: inside the real `SyncOnce` closure path, keep the last pushed digest in `Deps` (small mutable field set by `SyncOnce`'s caller loop) and print `▸ [sync] no manifest changes — skipped push` when the new digest equals the last (uses `ArtifactRef.Digest`, the extension Task 10 Step 3 lands — RESOLVED, the extension stays).

- [ ] **Step 3: Activate `--watch` in cmd/sync.go**

Replace the Task 10 stub: `--watch` calls `syncer.Watch(c.Context(), deps, dir, 300*time.Millisecond)`. The command's help text documents D7's boundary: "Git-push-based deployment flows are provided by the gitea pack (`cube-idp repo create`), not by sync — sync pushes OCI artifacts directly."

- [ ] **Step 4: Run tests**

Run: `go test ./internal/syncer/ -v && go build ./...`
Expected: PASS. Manual smoke (cluster required): `cube-idp up`, `cube-idp sync ./somepack --watch`, edit a manifest, watch the engine pick it up in seconds (Poke) rather than minutes (interval).

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat: sync --watch — debounced fsnotify loop with loud non-fatal failures (D7)"
```

---

### Task 12: `cube-idp repo create <name> [--deploy]`

**Reconcile checkpoint:** RESOLVED 2026-07-14 (0.10/0.8 verified): admin Secret `gitea-admin-cube-idp` ns `gitea`, keys `username`/`password`, legacy `cube-idp.dev/cli-secret` + `pack-name` labels still present (one release); the HTTP Service is the chart-default **`gitea-http:3000`** (release name `gitea`, chart 12.6.0); printed operator URLs are `https://gitea.<gw.Host>:<gw.Port>` (real TLS via the gateway); the ENGINE-facing URL is the in-cluster `http://gitea-http.gitea.svc.cluster.local:3000/...` (recorded Task 12 decision — no CA distribution needed for source-controller/argocd). Requires Task 10a (`DeliverGit` — this task CONSUMES the finished engine surface) and Task 10 (`kube.PortForward`).

Spec §6 Phase 3: "creates a Gitea repo and registers an engine source pointing at it — one command from empty repo to deployed." One half remains here: a minimal Gitea REST client (create-repo only — not a git client); the `DeliverGit` engine surface landed in Task 10a.

**Files:**
- Create: `internal/gitea/client.go`, `cmd/repo.go`
- Test: `internal/gitea/client_test.go`

**Interfaces:**
- Consumes: `apply.Applier` (secret read + object apply), `kube.PortForward`, engine factory, `engine.GitSource`/`DeliverGit` (Task 10a).
- Produces:

```go
package gitea
type Client struct{ BaseURL, Username, Password string } // BaseURL = the port-forward tunnel
// EnsureRepo creates <name> for the admin user with auto_init (so the
// default branch exists for the engine to sync) and private=false (no pull
// secret needed in-cluster; local-dev posture, same as the pack's fixed
// admin password — documented in the command help). Idempotent: 409 ->
// fetch and return the existing repo. Other failures -> CUBE-7302.
func (c *Client) EnsureRepo(ctx context.Context, name string) (*Repo, error)
type Repo struct{ Owner, Name, CloneURL, DefaultBranch string }
```

- [ ] **Step 1: Failing tests — gitea client against httptest**

`internal/gitea/client_test.go`:

```go
package gitea

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rafpe/cube-idp/internal/diag"
)

func giteaFake(t *testing.T, existing bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, _ := r.BasicAuth()
		if u != "gitea_admin" || p != "pw" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/user/repos":
			if existing {
				w.WriteHeader(http.StatusConflict)
				return
			}
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]any{
				"name": "app", "default_branch": "main",
				"clone_url": "http://gitea/gitea_admin/app.git",
				"owner":     map[string]any{"login": "gitea_admin"},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repos/gitea_admin/app":
			json.NewEncoder(w).Encode(map[string]any{
				"name": "app", "default_branch": "main",
				"clone_url": "http://gitea/gitea_admin/app.git",
				"owner":     map[string]any{"login": "gitea_admin"},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestEnsureRepoCreates(t *testing.T) {
	srv := giteaFake(t, false)
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, Username: "gitea_admin", Password: "pw"}
	repo, err := c.EnsureRepo(context.Background(), "app")
	if err != nil {
		t.Fatal(err)
	}
	if repo.Owner != "gitea_admin" || repo.DefaultBranch != "main" {
		t.Fatalf("repo: %+v", repo)
	}
}

func TestEnsureRepoIdempotentOnConflict(t *testing.T) {
	srv := giteaFake(t, true)
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, Username: "gitea_admin", Password: "pw"}
	if _, err := c.EnsureRepo(context.Background(), "app"); err != nil {
		t.Fatalf("409 must resolve to the existing repo: %v", err)
	}
}

func TestEnsureRepoBadCredentials(t *testing.T) {
	srv := giteaFake(t, false)
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, Username: "gitea_admin", Password: "wrong"}
	_, err := c.EnsureRepo(context.Background(), "app")
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-7302" {
		t.Fatalf("want CUBE-7302, got %v", err)
	}
}
```

Run: `go test ./internal/gitea/ -v` — FAIL.

- [ ] **Step 2: Implement the gitea client**

`internal/gitea/client.go` (~90 lines): `EnsureRepo` POSTs `/api/v1/user/repos` with `{"name": name, "auto_init": true, "private": false, "default_branch": "main"}` and basic auth, 10 s request timeout (deadline rule); `201` → decode; `409` → GET `/api/v1/repos/<user>/<name>`; anything else (incl. 401) → `diag.New("CUBE-7302", fmt.Sprintf("Gitea API returned %s for %s", resp.Status, r.URL.Path), "check the gitea pod (`kubectl -n gitea get pods`) and credentials (`cube-idp get secrets -p gitea`)")`.

- [ ] **Step 3: The command**

`cmd/repo.go`:

```go
package cmd

// newRepoCmd: `repo create <name> [--deploy] [-f cube.yaml]`
// Sequence (connection boilerplate identical to the shipped `status`
// command's connect block — config.Load -> cluster.New -> requireClusterExists
// -> Ensure -> apply.New; copy it):
//  1. config.Load; requireClusterExists (CUBE-1004 guard — repo create must
//     never side-effect-create a cluster); provider Ensure; apply.New.
//  2. Read the gitea admin secret via a.Client(): RESOLVED per 0.10 — get
//     Secret "gitea-admin-cube-idp" in namespace "gitea" directly (name is
//     part of the pack's D11 expose contract; the legacy cli-secret labels
//     are deprecated), keys "username"/"password". Missing -> CUBE-7301:
//     "the gitea pack is not installed in this cube" / "add the gitea pack
//     to cube.yaml and re-run `cube-idp up`".
//  3. addr, stop := kube.PortForward(ctx, conn.REST, "gitea", "app.kubernetes.io/name=gitea", 3000)
//     wrapped -> CUBE-7301 on failure. (Chart-standard pod labels for the
//     gitea chart 12.6.0; the Service is gitea-http:3000 — verified. If the
//     label selector matches nothing at runtime, the e2e in Task 13 catches
//     it — adjust to the chart's rendered labels then.)
//  4. repo := (&gitea.Client{BaseURL: "http://" + addr, Username: u, Password: p}).EnsureRepo(ctx, name)
//  5. If --deploy:
//       eng := enginefactory.New(cube.Spec.Engine.Type)
//       objs := eng.DeliverGit(ctx, name, engine.GitSource{
//           URL: "http://gitea-http.gitea.svc.cluster.local:3000/" + repo.Owner + "/" + repo.Name + ".git",
//           Branch: repo.DefaultBranch, Path: "./"})   // in-cluster URL: the ENGINE clones, not the laptop (recorded 0.10 decision)
//       a.Apply(ctx, objs, false, 2*time.Minute); a.RecordInventory(ctx, objs)
//       any failure here -> wrap as CUBE-7303 "created the repo but could not
//       register the deploy source" / "re-run `cube-idp repo create <name>
//       --deploy` — repo creation is idempotent".
//  6. Print the access block (scheme/host RESOLVED per 0.8: https over the
//     gateway, port shown unless 443 — reuse pack's gatewayHostString helper
//     from Task 3.5):
//       ✔ repo <owner>/<name> created
//         clone:  https://gitea.<gw.Host>:<gw.Port>/<owner>/<name>.git
//         push:   git push <that-url> main
//         deploy: engine syncs ./ on branch <default-branch> (--deploy)   // only when --deploy
```

Write it in full following that sequence; register in root.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/gitea/ ./cmd/ -short -v && go build ./...`
Expected: PASS (the engine surface was proven in Task 10a).

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat: repo create — Gitea repo plus engine git source in one command"
```

---

### Task 13: E2E matrix, CI, README

**Reconcile checkpoint:** requires everything above. RESOLVED 2026-07-14: e2e helpers are `build(t *testing.T) string` (builds the binary into a temp dir) and `run(t *testing.T, dir, bin string, args ...string) string` (fatal on error, returns combined output) in `tests/e2e/e2e_test.go`; the harness honors `CUBE_IDP_E2E=1`, `CUBE_IDP_E2E_ENGINE` (default flux) and `CUBE_IDP_E2E_GATEWAY_PORT`; `.github/workflows/ci.yaml` already runs `make test-engines` plus an e2e job with `matrix.engine: [flux, argocd]` — this task widens it to the spec §5 {kind, k3d} × {flux, argocd} grid.

**Files:**
- Create: `tests/e2e/phase3_test.go`
- Modify: `.github/workflows/ci.yaml`, `README.md`

- [ ] **Step 1: E2E additions (gated by CUBE_IDP_E2E=1, reusing the Phase 1 harness helpers)**

`tests/e2e/phase3_test.go` — five scenarios (write them fully; helper signatures resolved above — `build(t)`, `run(t, dir, bin, args...)`):

```go
// 1. TestK3dUpDown: init --name e2e-k3d, edit cube.yaml provider to k3d
//    (sed-style file rewrite in the test), up, status shows all packs
//    Ready, down. Mirrors the Phase 1 kind loop — this is the provider
//    matrix's second leg.
//
// 2. TestVendorBundleOffline: up (online), vendor -o b.tgz, down, then
//    `up --bundle b.tgz`. Offline honesty check: the test asserts the up
//    output contains the "loading images into cluster nodes" step and that
//    NO "fetching oci://ghcr.io" lines appear (grep the -v output). A true
//    network-namespace cutoff is not feasible on shared runners; the
//    ref-resolution guarantee (CUBE-7004 on any non-bundle ref) plus these
//    output assertions are the CI-shaped proof, and that limitation is
//    stated in this comment on purpose.
//
// 3. TestSyncOneShot: up, write a bare dir with one ConfigMap, sync <dir>,
//    poll kubectl-style via the binary (`status`) until the synced pack
//    reports Ready, assert the ConfigMap exists in-cluster (client-go),
//    then `down` removes it (inventory covered sync'd packs — Task 10).
//
// 4. TestRepoCreateDeploy: up, repo create app --deploy, then push a
//    manifest to the new repo over the gateway URL using `git` CLI with the
//    admin credentials from `get secrets -p gitea`, and poll until the
//    pushed ConfigMap appears in-cluster. This is the "empty repo to
//    deployed" acceptance test, end to end.
//
// 5. TestEnvoyGatewaySmoke (Owner Decisions #7): init, set
//    spec.gateway.pack: envoy-gateway in cube.yaml, up, assert status shows
//    all packs Ready and `https://gitea.<host>:<port>` answers through the
//    envoy data plane (same probe the kind/traefik leg uses), down. Proves
//    the turnkey Gateway + NodePort-30443 parity wiring from Task 4.
```

- [ ] **Step 2: CI matrix + plugin/vendor units in CI**

`.github/workflows/ci.yaml` e2e job gains the provider dimension MERGED into the existing engine matrix (verified 2026-07-14: the shipped job already has `matrix.engine: [flux, argocd]` and passes `CUBE_IDP_E2E_ENGINE` — extend, do not overwrite):

```yaml
  e2e:
    runs-on: ubuntu-latest
    timeout-minutes: 40
    strategy:
      fail-fast: false
      matrix:
        provider: [kind, k3d]
        engine: [flux, argocd]   # spec §5: {kind, k3d} x {flux, argocd}
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: {go-version-file: go.mod}   # never hardcode (Tech Stack rule)
      - run: CUBE_IDP_E2E=1 CUBE_IDP_E2E_PROVIDER=${{ matrix.provider }} CUBE_IDP_E2E_ENGINE=${{ matrix.engine }} go test ./tests/e2e/ -v -timeout 35m
```

…and the e2e harness reads `CUBE_IDP_E2E_PROVIDER` to pick the provider it writes into cube.yaml (default kind). Unit job: no change needed — the new packages all run under `go test ./... -short` (registry/git/httptest fakes, no network).

- [ ] **Step 3: README**

Add sections (each a short paragraph + one example block): k3d provider (`provider: k3d`, render-cluster works for it), air-gap (`vendor [--platform]` → carry the tarball → `up --bundle`), plugins (naming convention, env contract table incl. `CUBE_IDP_CA`, trust model, `plugin install --index`; note that zot is also reachable from the host at `https://registry.<gateway.host>` with `CUBE_IDP_CA` as the trust anchor — Owner Decisions #5), `sync --watch` (D7, with the "git flows live in the gitea pack" boundary), `repo create` quickstart, published pack refs (`ghcr.io/rafpe/cube-idp/packs/...`), and the `mounts:`-based node-image cache recipe (Owner Decisions #9 / Task 0.5j — if 0.5j already wrote it, just cross-link).

- [ ] **Step 4: Full verification + commit**

Run: `go vet ./... && go test ./... -short && make test-apply && CUBE_IDP_E2E=1 go test ./tests/e2e/ -v -timeout 35m` (locally, docker required; then confirm both matrix legs green in CI).

```bash
git add -A && git commit -m "feat: phase 3 e2e (k3d, bundle, sync, repo create), CI provider matrix, docs"
```

---

### Task 14a: UX design doc (stream F deliverable #1 — Owner Decisions #10/#15)

**Reconcile checkpoint:** the research spike is DONE (`docs/superpowers/research/2026-07-14-cube-idp-ux-research.md`) and the owner picked **Proposal B, staged A→B** ("One console"). This task produces the design doc from the research's §3 architecture; stage A implementation (Task 14b) may then proceed in parallel with the other streams. NO UX implementation before this doc exists.

**Files:**
- Create: `docs/superpowers/specs/2026-07-XX-cube-idp-ux-design.md` (date it when written)

The doc translates research §3 ("one event stream, three renderers") + §4 Proposal B into implementable contracts, self-contained enough to brief Tasks 14b/14c from:

- **Event vocabulary** (`internal/ui/event`): `RunStarted{cmd, cube}`, `StepStarted{stage, msg}`, `StepDone{stage, msg, dur}`, `StepFailed{stage, *diag.Error}`, `HealthTick{[]ComponentState}`, `Note/Warn{msg}`, `Access{[]PackAccess, hint}`, `Diagnosis{*diag.Error}` (ALWAYS the last event on failure), `RunDone{ok, dur}` — stage names = today's badge names ("cluster","engine","pack","health","tls",…). Delivery: buffered `chan Event`; the producer never blocks on UI.
- **Renderer contracts**: PlainRenderer IS today's `ui.Printer` plain path (each event's plain projection is *defined as* today's bytes — every pinned test survives; Progress emits zero bytes until Done ⇒ plain ignores `StepStarted`/`HealthTick`); LiveRenderer = Bubble Tea v2 **inline mode** (tea.Println for done steps, managed bottom region for spinners + health table, exits leaving scrollback; no alt screen — Proposal C rejected); JSONRenderer = JSON lines, one event per line, `"v":1` version field, schema labeled **experimental** until the D5 v1 freeze.
- **Diagnosis-last rule**: on failure the live program stops the live region, prints in-flight state as final lines, EXITS, and only then renders the CUBE-xxxx panel — after the TUI releases the terminal. Plain: `diag.Render` unchanged. JSON: diagnosis as a first-class event.
- **Renderer selection** (single resolve, `cmd/root.go` PersistentPreRunE): `--progress=json` > `--progress=plain`/`--plain`/`CUBE_IDP_PROGRESS` > non-TTY > `CI` > `NO_COLOR`/`TERM=dumb` > live. Flag surface: BuildKit-style `--progress=auto|plain|live|json` + `CUBE_IDP_PROGRESS` env policy; `--plain` kept permanently as an alias for `--progress=plain`.
- **Lifecycle**: the bubbletea program lives strictly inside RunE; `tea.Quit` on `RunDone`/`Diagnosis`; no goroutine survives exit; `sync --watch` is a LiveRenderer model that also consumes fsnotify-driven events until Ctrl-C (B's single-pane rolling view).
- **Charm v2 policy**: `github.com/charmbracelet/*` v1 → `charm.land/*` v2 lands in the SAME PR as the first live view — no separate mechanical migration.
- **Staging**: stage A (Task 14b) vs stage B (Task 14c) scope split, including the one deliberate plain-output change (Access summary becomes a JSON data event + stable plain access lines — one test update, called out by name).

- [ ] **Step 1: Write the doc** per the bullets above; cross-link research §2 exemplars where they justify a choice (BuildKit progress knob, Terraform machine-readable UI, gh TTY discipline).
- [ ] **Step 2: Commit**

```bash
git add -A && git commit -m "docs: cube-idp UX design — one event stream, three renderers (proposal B, staged)"
```

---

### Task 14b: UX stage A — event stream, renderer facade, live up/down, JSON events (Owner Decisions #15)

**Reconcile checkpoint:** requires Task 14a (the design doc is the contract). Verified 2026-07-14: `internal/ui` is the single output seam (`Printer.Step/Section/Glyph/Progress`, `ui.PlainFlag` set once in `cmd/root.go` PersistentPreRunE); charmbracelet lipgloss v1.1.0 + huh v1.0.0 are the current deps (bubbletea v1.3.6 transitive). The Charm v2 migration (`charm.land/*`) lands in THIS task's live-view PR, per the recorded decision.

**Files:**
- Create: `internal/ui/event/` (event types + channel plumbing), `internal/ui/render/` (plain.go, live.go, json.go — or flat in `internal/ui`, per the design doc's layout)
- Modify: `internal/ui/ui.go` (Printer becomes a facade that constructs events), `cmd/root.go` (renderer resolve), `internal/up` call sites only if the facade cannot absorb them (goal: barely change)
- Test: golden-file renderer tests (recorded event slice → each renderer), existing `ui_test.go` + all plain-output e2e assertions UNTOUCHED and green

- [ ] **Step 1: Event types + PlainRenderer with failing goldens** — encode today's exact plain bytes as the plain projection of a recorded event stream; the existing pinned tests are the proof the facade is byte-neutral. TDD per event type.
- [ ] **Step 2: Printer facade** — `Printer.Step(...)` emits `StepDone`; plain mode output goes through PlainRenderer. Run the FULL test suite: zero output diffs allowed.
- [ ] **Step 3: JSONRenderer** — `--progress=json` skeleton behind the (not-yet-public) knob; JSON-lines `"v":1` events incl. `diagnosis`; goldens.
- [ ] **Step 4: LiveRenderer for `up`/`down`** — Bubble Tea v2 inline step-tree + health table per the design doc, Charm v2 migration in the same commits; diagnosis-last rule enforced by a test that feeds a failing event stream and asserts the CUBE panel renders after program exit.
- [ ] **Step 5: Run everything** — `go build ./... && go vet ./... && go test ./... -short`; manual: `up` on a TTY shows the live tree, `up | cat` is byte-identical to pre-task output, Ctrl-C leaves a clean terminal.
- [ ] **Step 6: Commit**

```bash
git add -A && git commit -m "feat: UX stage A — typed event stream, plain/json renderers, live up/down (charm v2)"
```

---

### Task 14c: UX stage B — rich status/doctor, init wizard, JSON documents, --progress knob (Owner Decisions #15)

**Reconcile checkpoint:** requires Task 14b. The `Resolve` hardening (`NO_COLOR`/`TERM=dumb` forcing plain) MAY land earlier — it is a gap today and has no dependency on the event stream; if it landed with 14b, tick that part here.

**Files:**
- Modify: `cmd/status.go`, `cmd/doctor.go`, `cmd/get.go` (rich static renders + `--output json` documents), `cmd/init.go` (full huh v2 wizard), `cmd/root.go` (`--progress=auto|plain|live|json` knob; `--plain` becomes a permanent alias for `--progress=plain`; `CUBE_IDP_PROGRESS` env), `internal/ui` (Resolve hardening)
- Test: renderer goldens per command; flag-resolution table test (the full precedence chain from the design doc)

- [ ] **Step 1: `--progress` knob + Resolve hardening** — table-driven precedence test (json > plain/alias/env > non-TTY > CI > NO_COLOR/TERM=dumb > live), then implement; `--plain` alias keeps every existing invocation working.
- [ ] **Step 2: Rich static `status`/`doctor`** — lipgloss-styled TTY renders; plain projections byte-stable; `--output json` DOCUMENT mode (gh-style final object — distinct from the event stream) for `status`/`doctor`/`get secrets`.
- [ ] **Step 3: `init` wizard** — full huh v2 form (provider, engine, gateway host/port, packs) writing the same cube.yaml `init` writes today; non-TTY/`--plain` falls back to current flag behavior unchanged.
- [ ] **Step 4: Access summary event** — the ONE deliberate plain-output change: Access summary becomes a JSON data event and plain mode gains stable access lines; update the single affected test deliberately and call it out in the commit message.
- [ ] **Step 5: Run everything + commit**

```bash
git add -A && git commit -m "feat: UX stage B — progress knob, rich status/doctor, init wizard, JSON documents"
```

---

## Self-Review

**1. Spec §6 Phase 3 coverage — item → task:**

| Spec §6 Phase 3 item | Task(s) |
|---|---|
| k3d provider (D4, D10 customization, D12 registries wiring, contract parity) | 1 (contract suite), 2 (provider + merge + render-cluster) |
| `${GATEWAY_HOST}`/`${GATEWAY_FQDN}` over chart values + manifests (spec D15) | 3.5 (before Task 4, which consumes it) |
| `vendor [--platform]` / `up --bundle` air-gap (driven by `cube.lock`, spec §4.1; per-image tars per Owner Decisions #2; D14 `images:` supplement per #3) | 6 (Step 0 lock/pack supplement + vendor + bundle format), 7 (offline up + image loading + argocd imagePullPolicy flip) |
| Exec-plugin discovery + index (spec §4.4 tier 2: PATH, env contract incl. `CUBE_IDP_CA`, sha256-pinned git index, first-run trust warning) | 8 (discovery/env/trust), 9 (index install) |
| Pack catalog buildout (backstage, cert-manager, external-secrets, turnkey envoy-gateway) | 4 (packs), 3 (`pack push --also-tag`), 5 (CI → ghcr.io/rafpe; closes Phase 1 Task 13's `--local` wrinkle — `config.Default`'s OCI refs become real) |
| Engine surface: `Poke` + `DeliverGit`, both engines, contract-covered (D2; Owner Decisions #4) | 10a (single task; 10/11/12 are pure consumers) |
| `sync --watch` (D7: fsnotify → OCI push → engine reconciles; git flow only via gitea pack) | 10 (one-shot + port-forward + digest return + inventory-merge check), 11 (--watch), boundary documented in 11/13 |
| `repo create <name> [--deploy]` (empty repo → deployed) | 12, acceptance-tested in 13 |
| Cross-cutting: spec §5 matrix ({kind, k3d} × {flux, argocd} e2e + envoy smoke), doctor-grade errors, deadlines | 13; CUBE codes throughout |
| Interactive UX stream (spec D13; Owner Decisions #10/#15 — Proposal B staged A→B) | 14a (design doc), 14b (stage A: events + renderers + live up/down), 14c (stage B: knob + rich static + wizard + JSON documents) |

**2. Placeholder scan (post-Task-0):** no TBDs; the 2026-07-14 reconciliation resolved every `RECONCILE:` answerable from the live tree in place. The remaining markers (grep to confirm) depend ONLY on artifacts not yet in the tree: the k3d v5 library surface (enters `go.mod` in Task 2 — SimpleConfig field names, transform/validate function names, ImageImport signature) and the pinned envoy-gateway chart (Task 4 — default proxy image ref, EnvoyProxy service-override schema). Comment-contract blocks (k3d.go, pushdir.go, LoadImages, repo.go) specify exact calls, error codes, and remediation strings, not "handle errors".

**3. Type consistency:** `cluster.Provider`/`kube.Conn` verified 2026-07-14 (checkpoint 0.1); `engine.Engine` extensions (`Poke`, `DeliverGit`) are declared ONCE in Task 10a's Interfaces block and used with those exact signatures in Tasks 10–13; `bundle.Opened` methods used in Task 7 (`PackDirLookup` added explicitly in Task 7 Step 2; `ImageTars` replaces the old `ImagesLayout`); `oci.PushPackDir` (Task 3) vs `oci.PushRendered` (Phase 1) are distinct and both referenced consistently; the `ArtifactRef.Digest` extension is declared in Task 10 Step 3 and consumed in Task 11.

**4. Known cross-task invariants (reconciled 2026-07-14):** the gateway node-port single-source constant (Task 2 hoists `cluster.GatewayNodePort = 30443`; kindp refactored in the same step); the ghcr namespace `ghcr.io/rafpe/cube-idp` is applied in Task 5 (workflow + `config.Default`), Task 7 (test refs), and Task 13 (README) — Task 9 has no default index at all (Owner Decisions #8); the engine contract suite gains `Poke` and `DeliverGit` cases in the SAME commit series that extends the interface (Task 10a) — an interface method without contract coverage for both engines is a D2 violation; the D15 substitution helpers (`${GATEWAY_HOST}` host[:port] vs `${GATEWAY_FQDN}` bare host) are defined once in Task 3.5 and consumed by Task 4's packs and Task 12's printed URLs.

**Open design questions — ALL RESOLVED 2026-07-14 with the owner; see the "Owner Decisions" section at the top of this plan:**

1. **ghcr namespace:** RESOLVED → `ghcr.io/rafpe/cube-idp/packs/...` (Owner Decisions #1).
2. **`CUBE_IDP_REGISTRY` semantics for plugins:** RESOLVED → in-cluster zot URL as drafted, plus a new `CUBE_IDP_CA` env var and README documentation of the `https://registry.<gateway.host>` gateway route (Owner Decisions #5).
3. **`up --bundle` on `provider: existing`:** RESOLVED → reject with `CUBE-7005` as drafted (Owner Decisions #6).
4. **envoy-gateway as a first-class `spec.gateway.pack` choice:** RESOLVED → fully turnkey: GatewayClass + Gateway with NodePort-30443 parity + e2e smoke (Owner Decisions #7).
5. **Default plugin index repo:** RESOLVED → out of scope; `--index` stays required until a first real plugin exists (Owner Decisions #8).



