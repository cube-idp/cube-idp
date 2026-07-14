# cube-idp Phase 2 Implementation Plan

> **STATUS: RECONCILED against commit `0522799` on `2026-07-13`.** Task 0 was executed (investigation report: `.superpowers/sdd/task-0-report.md`); its findings are recorded in the "Task 0 Findings" section below and folded into the affected tasks — most notably: CUBE-3005→**3006** and CUBE-4012→**4014** renumbers (both original codes are already claimed by Phase 1 code), `InstallManifests()` is a method ON the `Engine` interface (not a package-level accessor), traefik chart is pinned at **41.0.2** with a nested values shape, and CI pins Go via `go-version-file: go.mod`. A remaining `RECONCILE:` marker means "verify this detail at implementation time" — the blocking ones were resolved by Task 0.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. **Task 0 is blocking: no other task may start before it is checked off and this document has been amended with its findings.**

> **AMENDED 2026-07-13 (against spec commit `e3af013`):** the spec's Phase 2 bullet gained scope after this draft was written. This amendment folds it in: **D11** Pack discoverability CRD + `expose:` contract (new Task 12.5), **D12** TLS-from-first-boot ordering + mkcert CA reuse (amendments to Tasks 8/9), **go-getter pack sources** via the RafPe fork with OCI support (Task 4 reworked — supersedes the original hand-rolled go-git source), **fundamentals-review debt paydown** (new Tasks 0.5, 3.5, 13.5), and the **terminal UX pass** (new Task 13.8). Original task numbers are preserved; decimal-numbered tasks slot into execution order. **Document order = execution order.**

**Goal:** Ship cube-idp Phase 2 (spec §6): the Argo CD engine behind the same `GitOpsEngine` interface with a shared contract-test suite (D2), TLS from first boot (D12) + the opt-in `cube-idp trust` + canonical-hostname story with a real HTTPS gateway (D6), the `Pack` discoverability CRD + `expose:` contract (D11), `diff`, `doctor`, `cube.lock` + `upgrade --plan`, go-getter pack sources (git/http/s3/OCI), kustomize rendering, the cnoe-compat loader, the fundamentals-review debt paydown (oras-go consolidation, Helm v4 port, central CUBE-code catalog), and the terminal UX pass (lipgloss/huh/`--plain`).

**Architecture:** Everything extends the Phase 1 kernel without changing its shape: engines stay behind `internal/engine.Engine`, new delivery paths reuse `apply.Applier` + the zot registry, trust is a leaf package (`internal/trust`) with zero implicit side effects (OS trust-store changes only via the explicit `trust` command, D6), and every new command is a thin cobra shell over an `internal/` orchestrator, mirroring `internal/up`. The single D11 `Pack` CRD is an inert record type — written by `up`, inventory-tracked, watched by nobody — per the amended spec §3 non-goal.

**Tech Stack (additions over Phase 1):** `sigs.k8s.io/kustomize/api` (krusty), **`github.com/rafpe/go-getter` fork of hashicorp/go-getter v1 with an `oci://` getter (tag v1.9.0, consumed via a `replace` directive — see Task 4 Step 0)**, `github.com/go-git/go-git/v5` (demoted: only the `ls-remote` pin probe in `ResolveRemote`), `github.com/smallstep/truststore`, `golang.org/x/mod/sumdb/dirhash`, `golang.org/x/sys/unix` (doctor disk check), `github.com/charmbracelet/lipgloss` + `github.com/charmbracelet/huh` (Task 13.8). **Migrations:** `helm.sh/helm/v3` → `helm.sh/helm/v4` behind the existing wrapper (Task 13.5); `fluxcd/pkg/oci` dropped in favor of `oras.land/oras-go/v2` everywhere (Task 3.5). No new plugin protocols (D8), no *reconciled* CRDs (D11's inert `Pack` record is the one amended-spec exception), no daemon.

**Spec:** `docs/superpowers/specs/2026-07-13-cube-idp-architecture-design.md` — source of truth. Phase 1 plan: `docs/superpowers/plans/2026-07-13-cube-idp-phase1-mvp.md` — source of the interfaces this phase consumes (until Task 0 replaces it with the real code as the reference).

## Global Constraints (every task inherits these)

- Module path: `github.com/rafpe/cube-idp`. Conventional commits (`feat:`, `test:`, `chore:`); each task ends committed with `go build ./... && go test ./... -short` green.
- TDD: every behavioral step is failing test → run → implement → run → commit.
- Every user-facing failure is a typed `CUBE-xxxx` `diag.Error` with a copy-pasteable remediation (spec §4.5). Every wait has a hard deadline and ends in a rendered diagnosis — no infinite spinners.
- Existing code ranges (Phase 1): 0xxx preflight/config, 1xxx cluster, 2xxx apply, 3xxx engine, 4xxx pack, 5xxx registry. **New range owned by this phase: 6xxx = trust/hostname.** 7xxx is reserved for Phase 3 — do not use it.
- SSA field manager `cube-idp`; cube label `cube-idp.dev/cube: <name>`; prune opt-out annotation `cube-idp.dev/prune: disabled`; system namespace `cube-idp-system`; engine namespaces `flux-system` / `argocd`.
- Trust posture (D6): OS trust-store changes happen ONLY inside `cube-idp trust` after an explicit consent prompt, and are fully reverted by `cube-idp down`. `up` may generate a local CA and issue in-cluster certificates, but never touches the OS trust store.
- Packs stay data-only. The Argo CD *engine* is Go inside the binary (D2/D8); the `argocd` *pack* remains the UI-only option for flux users.

### CUBE codes introduced or changed in Phase 2

| Code | Range reused / new | Meaning |
|---|---|---|
| CUBE-3002 | changed | **Deleted.** `engine.type: argocd` now constructs the Argo CD engine (Task 2). |
| CUBE-3003 | reused, text generalized | embedded engine install manifests invalid (now flux **or** argocd) |
| CUBE-3006 | new (3xxx engine) | Argo CD OCI repo registration/capability failure (spec §7 risk). Renumbered by Task 0: CUBE-3005 is taken (flux Uninstall prune-timeout) |
| CUBE-0003 | new (0xxx config) | `cube.lock` unreadable or corrupt |
| CUBE-0005 | new (0xxx config) | `argocd` pack listed while `engine.type: argocd` (redundant install) |
| CUBE-0101 | new (0xxx preflight) | container runtime not found (doctor) |
| CUBE-0102 | new (0xxx preflight) | required host port already in use (doctor) |
| CUBE-0103 | new (0xxx preflight) | low disk space in the cube-idp cache dir (doctor, warning) |
| CUBE-0104 | new (0xxx preflight) | inotify limits too low (doctor, warning, linux-only) |
| CUBE-0105 | new (0xxx preflight) | git CLI missing while git-sourced packs configured (doctor, warning; added at execution — review split it out of CUBE-0101) |
| CUBE-2005 | new (2xxx apply) | server-side diff failed |
| CUBE-4006 | new (4xxx pack) | remote pack source fetch/resolution failed (go-getter or git ls-remote) |
| CUBE-4007 | new (4xxx pack) | remote pack ref not pinned (missing `@<rev>` / `:tag`) |
| CUBE-4008 | new (4xxx pack) | kustomize render failed |
| CUBE-4009 | new (4xxx pack) | cnoe-compat document invalid or unsupported |
| CUBE-4010 | new (4xxx pack) | `cnoe://` path unresolvable |
| CUBE-4011 | new (4xxx pack) | `expose:` block in pack.cue invalid (D11, Task 12.5) |
| CUBE-4014 | new (4xxx pack) | extraction guard tripped: path traversal or symlink in fetched pack (Task 4). Renumbered by Task 0: CUBE-4012 (pullOCI failures) and CUBE-4013 (cacheDir) are taken |
| CUBE-5004 | new (5xxx registry) | remote digest resolution failed (upgrade --plan) |
| CUBE-6001 | new (6xxx trust) | local CA creation/load failed |
| CUBE-6002 | new (6xxx trust) | OS trust-store install failed |
| CUBE-6003 | new (6xxx trust) | OS trust-store uninstall/revert failed |
| CUBE-6004 | new (6xxx trust) | CoreDNS rewrite patch failed or did not roll out |
| CUBE-6005 | new (6xxx trust) | server certificate issuance failed |
| CUBE-6006 | new (6xxx trust) | trust state file corrupt |

### `RECONCILE:` markers

Because this draft predates the Phase 1 code, steps that depend on details Phase 1 was allowed to resolve either way carry an explicit `RECONCILE:` marker stating exactly what to verify and how to adapt. That marker is the ONLY permitted form of deferral in this document; there are no TBDs. When Task 0 is executed, each marker must be either (a) resolved and rewritten as concrete instructions, or (b) confirmed as-is.

## File Structure

```
internal/diag/codes.go, codes_test.go         # CUBE-code sentinel catalog + literal-ban test (Task 0.5)
internal/engine/contract/contract.go          # shared GitOpsEngine contract suite (Task 1)
internal/engine/flux/contract_test.go         # flux runs the suite (Task 1)
internal/engine/argocd/argocd.go              # Argo CD engine: install/health/uninstall (Task 2)
internal/engine/argocd/deliver.go             # Application shaping for OCI source (Task 2)
internal/engine/argocd/manifests/install.yaml # generated by hack/gen-argocd-manifests.sh (Task 2)
internal/engine/argocd/manifests/repo-secret.yaml # zot OCI repo registration (Task 2)
internal/engine/argocd/argocd_test.go         # argocd-specific shape tests (Task 2)
internal/engine/argocd/contract_test.go       # argocd runs the shared suite (Task 2)
internal/engine/factory/factory.go            # MODIFY: wire argocd, drop CUBE-3002 (Task 2)
hack/gen-argocd-manifests.sh                  # (Task 2)
internal/pack/kustomize.go                    # kustomization.yaml rendering (Task 3)
internal/oci/*.go                             # REWORK: all OCI on oras-go v2, digest threaded out; drop fluxcd/pkg/oci (Task 3.5)
internal/pack/getter.go                       # go-getter PackSource: git/http/s3 refs via the RafPe fork (Task 4)
internal/pack/guards.go                       # extraction guards: path traversal + symlink skip (Task 4)
internal/pack/resolve.go                      # ResolveRemote pin probe — feeds Fetch pinning AND upgrade --plan (Tasks 4/7)
internal/lock/lock.go, images.go, lock_test.go # cube.lock model (Task 5)
internal/apply/diff.go                        # Applier.Diff over fluxcd/pkg/ssa (Task 6)
internal/diff/diff.go                         # diff orchestrator, mirrors internal/up (Task 6)
internal/upgrade/plan.go                      # upgrade --plan orchestrator (Task 7)
internal/trust/ca.go, state.go, trust_test.go # local CA + trust state (Task 8)
internal/trust/coredns.go, certsd.go          # canonical hostname wiring (Task 10)
internal/trust/store.go                       # smallstep/truststore wrapper (Task 11)
internal/registry/route.go                    # registry HTTPRoute via the gateway (Task 10)
internal/doctor/doctor.go, checks_linux.go, checks_other.go, doctor_test.go # (Task 12)
internal/cnoe/loader.go, translate.go, loader_test.go, testdata/ # cnoe-compat (Task 13)
internal/pack/expose.go, expose_test.go       # D11 expose: block parse + validation (Task 12.5)
internal/pack/manifests/pack-crd.yaml         # D11 inert Pack CRD, go:embed (Task 12.5)
internal/pack/discovery.go, discovery_test.go # D11 Pack object shaping for up (Task 12.5)
cmd/get.go                                    # MODIFY: get secrets pivots Pack -> authSecretRef (Task 12.5)
packs/{traefik,gitea,argocd}/pack.cue         # MODIFY: expose: blocks (Task 12.5)
internal/pack/helm.go                         # REWORK: helm v3 -> v4 SDK behind the same wrapper (Task 13.5)
internal/ui/ui.go, ui_test.go                 # lipgloss step/status renderer + --plain mode (Task 13.8)
cmd/root.go                                   # MODIFY: --plain persistent flag (Task 13.8)
cmd/diff.go, cmd/upgrade.go, cmd/trust.go, cmd/doctor.go, cmd/cnoe.go # new commands
cmd/init.go                                   # MODIFY: --engine flag (Task 2)
cmd/down.go                                   # MODIFY: trust revert + CoreDNS cleanup (Task 11)
internal/up/up.go                             # MODIFY: TLS secret, certs.d, CoreDNS, lock (Tasks 5/9/10)
internal/cluster/kindp/merge.go               # MODIFY: HTTPS port mapping + certs.d mount (Tasks 9/10)
internal/registry/manifests/zot.yaml          # MODIFY: NodePort for node-local pulls (Task 10)
packs/traefik/                                # MODIFY: websecure HTTPS listener + values (Task 9)
tests/e2e/e2e_test.go, .github/workflows/ci.yaml # MODIFY: engine matrix + new commands (Task 14)
```

---

### Task 0: Reconciliation Gate (mandatory, blocking)

**Files:**
- Modify: `docs/superpowers/plans/2026-07-13-cube-idp-phase2-draft.md` (this file — amend it with findings)

**Purpose:** Replace every assumption this draft inherited from the Phase 1 *plan* with facts from the Phase 1 *code*. Work through the checklist in order; every item ends with the same obligation: **update the affected tasks below if reality differs.** When done, delete the words "DO NOT EXECUTE AS-IS" from the status block, replace them with "RECONCILED against commit `<sha>` on `<date>`", and commit the amended plan before starting Task 1.

- [x] **0.1 Module path + layout.** Run `go list -m` at the repo root and `ls internal/ cmd/`. Expected: `github.com/rafpe/cube-idp` and the Phase 1 File Structure (diag, config, cluster, cluster/kindp, kube, apply, registry, pack, engine, engine/flux, engine/factory, oci, up). Update the affected tasks below if reality differs.
- [x] **0.2 diag API.** Run `go doc ./internal/diag`. Expected: `Code`, `Error{Code,Summary,Cause,Remediation}`, `New`, `Wrap`, `Render`, `Severity`, `Finding{Code,Severity,Message,Remediation}`. Tasks 6/8/12 construct `Finding` values and every task constructs `Error`s. Update the affected tasks below if reality differs.
- [x] **0.3 config types + validation.** Run `go doc ./internal/config` and read `schema.cue` + `crossValidate`. Expected: `Cube/Metadata/Spec/ClusterSpec/PortMapping/RegistrySpec/Mount/EngineSpec/GatewaySpec/PackRef`, `Load(path)`, `Default(name)`; CUE codes CUBE-0001/0002, cross-check CUBE-1003. Task 2 extends `crossValidate`; Tasks 5/6/7/9/12/13 read `GatewaySpec`/`PackRef`. Update the affected tasks below if reality differs.
- [x] **0.4 kube.Conn location.** Phase 1 Task 5 moved `Conn` into a leaf package `internal/kube` (fields `Kubeconfig []byte; Context string; REST *rest.Config`). Verify with `go doc ./internal/kube`. Tasks 6/9/10/12/13 use `conn.REST`. Update the affected tasks below if reality differs.
- [x] **0.5 cluster provider seam.** Run `go doc ./internal/cluster` and `go doc ./internal/cluster/kindp`. Expected: `Provider` interface (`Ensure/Delete/Exists/Kubeconfig/Diagnose`), factory `cluster.New(spec, gw)`, and `kindp.RenderConfig(name string, spec config.ClusterSpec, gw config.GatewaySpec) ([]byte, error)`. **Critical sub-check:** what is the final `gatewayContainerPort` constant? Phase 1 Task 12's note changed it from 443 to **30080** (Traefik exposed as NodePort 30080, plain HTTP). Tasks 9/10 change this mapping and `RenderConfig`'s signature. Update the affected tasks below if reality differs.
- [x] **0.6 apply.Applier surface + fluxcd/pkg/ssa version.** Run `go doc ./internal/apply` and `grep fluxcd/pkg/ssa go.mod`. Expected: `New(cfg *rest.Config, cubeName string)`, `Apply(ctx, objs, wait, timeout)`, `RecordInventory`, `LoadInventory`, `DeleteAll`, `Client()`, `ParseMultiDoc`, constants `FieldManager/CubeLabel/PruneAnnotation/SystemNamespace`. Note the exact `ssa.ResourceManager` method set of the pinned version — Task 6 needs its server-side diff entry point (`Diff`/`DryRunApply` naming drifted historically). Also note how Apply labels objects (inline loop vs helper) — Task 6 factors it out. Update the affected tasks below if reality differs.
- [x] **0.7 registry package.** Run `go doc ./internal/registry`. Expected: `InClusterURL = "zot.cube-idp-system.svc.cluster.local:5000"`, `Manifests()`, `Install(ctx, a, timeout)`, `PortForward(ctx, cfg) (string, func(), error)`. Task 10 modifies `manifests/zot.yaml` and adds `route.go`. Update the affected tasks below if reality differs.
- [x] **0.8 pack package.** Run `go doc ./internal/pack` and read `source.go`, `render.go`, `helm.go`. Expected: `Pack{Name,Version,Dir}`, `Rendered{Name,Version,Objects}`, `Fetch(ctx, ref, cacheDir)`, `(*Pack).Render(values)`, private `chartRef` (does it already carry a `Values map[string]any` field from Phase 1 Task 12's note?), CUBE-4001's exact message text ("git refs arrive in Phase 2" — Task 4 replaces it). Confirm how `pullOCI` stores artifacts (Task 5 needs the pulled digest). Update the affected tasks below if reality differs.
- [x] **0.9 engine seam.** Run `go doc ./internal/engine ./internal/engine/factory ./internal/engine/flux`. Expected: `Engine` interface exactly `Install(ctx, a *apply.Applier, timeout time.Duration) error; Deliver(ctx, r *pack.Rendered, src ArtifactRef) ([]*unstructured.Unstructured, error); Health(ctx, a *apply.Applier) ([]ComponentHealth, error); Uninstall(ctx, a *apply.Applier, timeout time.Duration) error`; `ArtifactRef{Repo,Tag}`; `ComponentHealth{Name,Ready,Message}`; factory in `internal/engine/factory` (vs the init()-registration alternative Phase 1 allowed); `flux.InstallManifests()`; the flux Deliver object names (`cube-idp-<pack>`) and how flux `Health` selects objects (cube label value vs label presence). Also record which `OCIRepository` apiVersion the generated flux manifests use. Tasks 1/2 depend on all of this. Update the affected tasks below if reality differs.
- [x] **0.10 oci push.** Run `go doc ./internal/oci` and `grep fluxcd/pkg/oci go.mod`. Expected: `PushRendered(ctx, r *pack.Rendered, registryAddr string) (engine.ArtifactRef, error)` and how plain-HTTP/insecure was enabled for the 127.0.0.1 tunnel. Tasks 5/13 reuse it. Update the affected tasks below if reality differs.
- [x] **0.11 Helm SDK pin.** Run `grep -E 'helm.sh/helm/v[34]' go.mod`. Phase 1 allowed pinning v4 **or** falling back to v3 behind the same wrapper. Record which one landed and what `renderHelm`'s exact signature is. Tasks 3/13 touch rendering; Task 13 exports a chart-render entry point. Update the affected tasks below if reality differs.
- [x] **0.12 cmd/ conventions.** Read `cmd/root.go` and one command file. Expected: `newXxxCmd() *cobra.Command` + `root.AddCommand(...)`, `-f/--file` defaulting to `cube.yaml`, `SilenceUsage/SilenceErrors`, context via `cmd.ExecuteContext`. **Critical sub-check:** how did Phase 1 Task 13 resolve the `init` pack-ref wrinkle — a `--local <repo-root>` flag, repo-local `./packs/...` defaults, or something else? Task 14's e2e and Task 2's `--engine` flag build on the real answer. Update the affected tasks below if reality differs.
- [x] **0.13 up orchestrator.** Read `internal/up/up.go`. Expected: `Run(ctx, cfgPath string, out io.Writer) error`, helpers `step`, `cacheDir()`, `waitHealthy` (CUBE-3004), inventory recording after registry/engine/deliver applies, gateway pack prepended to the pack list. Tasks 5/9/10 insert steps into this sequence — record the actual line structure. Update the affected tasks below if reality differs.
- [x] **0.14 traefik pack final shape.** Read `packs/traefik/`. Expected per Phase 1 Task 12 note: Gateway API CRDs vendored, `Gateway` with one HTTP listener (port 8000), chart values enabling `providers.kubernetesGateway` and NodePort 30080, HTTP-only. Record the exact chart `values:` mechanism and the traefik Service name the chart produces (Task 10's CoreDNS rewrite targets it). Update the affected tasks below if reality differs.
- [x] **0.15 actual CUBE codes in use.** Run `grep -rhoE 'CUBE-[0-9]{4}' --include='*.go' . | sort -u` and diff against the Phase 1 plan's set (0001-0002, 1001, 1003, 1101-1102, 1201-1205, 2001-2004, 3001-3004, 4001-4005, 5001-5003). The new-code table above must not collide with anything implementation added. Update the code table and affected tasks below if reality differs.
- [x] **0.16 e2e + CI harness.** Read `tests/e2e/e2e_test.go`, `.github/workflows/ci.yaml`, `Makefile`. Expected: `CUBE_IDP_E2E=1` gate, `build/run` helpers, unit + e2e jobs, `make test-apply` envtest wiring. Task 14 extends all three. Update the affected tasks below if reality differs.
- [x] **0.17 `get secrets` label convention.** Read `cmd/get.go`. Record: the exact label key/value the `cli-secret` convention uses, how packs' secrets are selected, and the output format. Task 12.5 pivots this command to Pack → `authSecretRef` → Secret with the label convention honored for one release — the deprecation text must name the real label. Update Task 12.5 below if reality differs.
- [x] **0.18 go-getter fork consumption.** The RafPe fork's `go.mod` declares `module github.com/hashicorp/go-getter` (v1 line); tag `v1.9.0` carries the OCI getter and is consumed via a plain `replace` directive (verified end-to-end 2026-07-13 — resolve, build, run; exact commands in Task 4 Step 0). Re-run the Task 4 Step 0 smoke check; if the fork has moved (newer tag, digest exposure or plain-HTTP added to `OCIGetter`), update Task 4 and — if the getter now returns digests — simplify Task 5's oci pinning. Also confirm the getter registration name is `"oci"` and the detector auto-matches only {azurecr.io, gcr.io, registry.gitlab.com, ECR, localhost:5000/127.0.0.1:5000}.
- [x] **0.19 helm wrapper surface.** Enumerate every `helm.sh/helm/v3` symbol `internal/pack/helm.go` imports (action config, chart loader, repo/registry download path, values merging). This is Task 13.5's port checklist. Also check the helm v4 SDK's release state at implementation time — if it is not yet stable enough, Task 13.5 is explicitly skippable (spec §7 risk: "can pin to v3 SDK if v4 breaks"); record the decision either way. Update Task 13.5 below if reality differs.
- [x] **0.20 Amend and commit.** Rewrite every task below whose assumptions drifted; resolve or confirm every `RECONCILE:` marker; update the status block; commit:

```bash
git add docs/superpowers/plans/2026-07-13-cube-idp-phase2-draft.md
git commit -m "docs: reconcile phase 2 plan against phase 1 implementation"
```

### Task 0 Findings (executed 2026-07-13 against commit `0522799`; full evidence: `.superpowers/sdd/task-0-report.md`)

Facts every implementer inherits (in addition to the amendments already folded into the tasks below):

- **0.2/0.6 (apply/diag):** `diag` API exactly as planned (plus `SeverityInfo`). `apply.New(cfg *rest.Config, cubeName string)`; `Apply` labels via an inline loop (Task 6 factors out `label()`) and applies with `rm.ApplyAllStaged(ctx, objs, ssa.DefaultApplyOptions())`; wait path uses `rm.WaitForSet`. `ssa.ResourceManager.Diff` exists on v0.77.0.
- **0.3 (config):** `GatewaySpec{Pack, Host, Port int, Ref string}` — `Ref` already shipped; `PackRef{Ref string, Values map[string]any}`; `Default(name)` already carries the D9 profile. `crossValidate` at `internal/config/load.go:93`.
- **0.8 (pack):** `Pack{Name, Version, Dir}` (no digest field). `Fetch` switch: `oci://` → `pullOCI` (returns dir only; digest computed for cache naming then DISCARDED — Task 5 threads it out); `://` → CUBE-4001 whose REMEDIATION says "(git refs arrive in Phase 2)" — Task 4 edits that one call site, leaving the other two CUBE-4001 sites alone. `pullOCI` failures are **CUBE-4012** (taken). `chartRef` already has `Values map[string]any`.
- **0.9 (engine):** `Engine` interface = Install / **InstallManifests()** / Deliver / Health / Uninstall. Flux Deliver names `cube-idp-<pack>`; `OCIRepository` is `source.toolkit.fluxcd.io/v1` with `spec.insecure: true`; Health selects `client.MatchingLabels{apply.CubeLabel: a.Cube()}` and has NO missing-CRD handling (argocd Health must, via `meta.IsNoMatchError` — see Task 2).
- **0.10 (oci):** `fluxcd/pkg/oci` v0.69.0, `oci.NewClient([]crane.Option{crane.Insecure})`, `LayerTypeTarball` → single layer `application/vnd.cncf.flux.content.v1.tar+gzip` containing `all.yaml`. Task 3.5's byte-compat target.
- **0.12 (cmd):** `NewRootCmd()` exists; `newXxxCmd()` + `AddCommand`; `-f/--file` default `cube.yaml`; `init` has `--name` + `--local <repo-root>` (writes `gateway.ref` + local pack paths) and refuses overwrite with CUBE-0006; `requireClusterExists` (CUBE-1004) guards read-only commands — `diff`/`doctor` should use it before `Ensure`.
- **0.13 (up):** step format is exactly `fmt.Fprintf(w, "▸ [%s] %s\n", stage, fmt.Sprintf(format, args...))` — Task 13.8's plain format. `cacheDir()` lives in `internal/up`, errors are **CUBE-4013** — Task 7 moves it to `pack.DefaultCacheDir()` keeping that code. Gateway prepend via `gatewayPackRef(gw)` (`gw.Ref` else `"packs/"+gw.Pack`) — Task 6 moves it to `config.GatewaySpec.PackRef()`. Per-pack `Apply` uses `wait=false`; health is one global `waitHealthy` (5m, poll 5s, zero components = not ready). Timeouts: cluster 3m, apply 2m.
- **0.14 (traefik):** chart **41.0.2**, `gateway.enabled: false` (pack's own `10-gateway.yaml` is the only Gateway), listener `web` port 8000, NodePort 30080, `service.spec.type: NodePort`. Service name inferred `traefik`/ns `traefik` (chart not vendored — e2e verifies).
- **0.15 (codes):** in use today: 0001 0002 0004 0006, 1001 1003 1004, 1101 1102, 1201–1205, 2001–2004 2006 2007, 3001–3005, 4001–4005 4012 4013, 5001–5003. Collisions resolved by renumbering: argocd capability failure → **CUBE-3006**, extraction guards → **CUBE-4014**. Task 0.5's catalog seeds from this list plus this plan's table.
- **0.16 (CI):** `go-version-file: go.mod` (currently go 1.26.2) — never reintroduce a hardcoded version. e2e uses `init --name e2e --local <repoRoot>`; a `deleteLingeringCluster` guard wraps the run.
- **0.17 (get secrets):** TWO labels: `cube-idp.dev/cli-secret: "true"` (selection) + `cube-idp.dev/pack-name: <pack>` (`--pack` filter); output is a tabwriter table `PACK NAMESPACE NAME DATA`. Task 12.5's deprecation notice must name both.
- **0.19 (helm):** v3.21.3 by deliberate API-shape choice (v4's `DryRunStrategy` vs v3's `DryRun/ClientOnly` bools — documented in helm.go). Symbols used: `action.Configuration/NewInstall`, `chartutil.ParseKubeVersion("v1.33.1")`, `registry.IsOCI/NewClient`, `LocateChart` + `cli.New()`, `loader.Load`, hand-rolled `mergeValues`. Task 13.5's port checklist.

---

### Task 0.5: Central CUBE-code sentinel catalog + literal-ban test (debt paydown)

**Reconcile checkpoint:** 0.2 (diag API — `Code` type exists), 0.15 (the full literal inventory this task migrates).

**Why first:** every later task in this phase constructs `diag.Error`s. Landing the catalog now means new code is written against it once, instead of being migrated after the fact. **Plan-wide note:** code snippets in the tasks below show literal `"CUBE-xxxx"` strings for readability — implementers MUST substitute the catalog constants; the literal-ban test enforces this mechanically.

**Files:**
- Create: `internal/diag/codes.go`
- Modify: every non-test `.go` file that constructs `diag.New`/`diag.Wrap` with a `"CUBE-` literal (mechanical sweep, inventory from checkpoint 0.15)
- Test: `internal/diag/codes_test.go`

**Interfaces:**
- Consumes: `diag.Code` (Phase 1).
- Produces (every subsequent task depends on this convention):

```go
package diag
// One exported constant per code, value format ^CUBE-[0-9]{4}$, grouped by
// range. Names follow Code<Area><Meaning>, e.g.:
const (
    CodeLockCorrupt      Code = "CUBE-0003" // cube.lock unreadable or corrupt
    CodeArgoPackRedundant Code = "CUBE-0005"
    // ... complete catalog seeded from checkpoint 0.15 + this plan's table
)
```

- [x] **Step 1: Write the failing literal-ban + format tests**

`internal/diag/codes_test.go`:

```go
package diag

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// repoRoot walks up from the package dir to the go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above " + dir)
		}
		dir = parent
	}
}

// TestNoCubeLiteralsOutsideCatalog is the debt-paydown enforcement: every
// CUBE code lives in codes.go and nowhere else in non-test Go code. Test
// files MAY use literals (asserting user-visible strings is the point of a
// golden test).
func TestNoCubeLiteralsOutsideCatalog(t *testing.T) {
	root := repoRoot(t)
	var offenders []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "testdata" || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") ||
			filepath.Base(path) == "codes.go" {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(raw), `"CUBE-`) {
			offenders = append(offenders, strings.TrimPrefix(path, root+string(os.PathSeparator)))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(offenders) > 0 {
		t.Fatalf("CUBE-code literals outside internal/diag/codes.go — use the catalog constants:\n  %s",
			strings.Join(offenders, "\n  "))
	}
}

// TestCatalogWellFormed parses codes.go and asserts format + uniqueness.
func TestCatalogWellFormed(t *testing.T) {
	raw, err := os.ReadFile("codes.go")
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`Code = "(CUBE-[0-9]{4})"`)
	seen := map[string]bool{}
	matches := re.FindAllStringSubmatch(string(raw), -1)
	if len(matches) == 0 {
		t.Fatal("catalog is empty — codes.go must define every CUBE code")
	}
	for _, m := range matches {
		if seen[m[1]] {
			t.Fatalf("duplicate code %s in the catalog", m[1])
		}
		seen[m[1]] = true
	}
}
```

- [x] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/diag/ -run 'TestNoCubeLiterals|TestCatalogWellFormed' -v`
Expected: FAIL — `TestCatalogWellFormed` (codes.go missing) and `TestNoCubeLiteralsOutsideCatalog` (Phase 1 literals everywhere).

- [x] **Step 3: Write the catalog and sweep the call sites**

Create `internal/diag/codes.go` with one constant per code from the checkpoint 0.15 inventory **plus** every code in this plan's table (constants for codes owned by future tasks are fine to add now — the table is frozen). Then mechanically replace each `"CUBE-xxxx"` literal in non-test code with its constant. No behavior change: constants have identical string values, so every existing test keeps passing unmodified.

- [x] **Step 4: Run the full suite**

Run: `go build ./... && go test ./... -short`
Expected: PASS — including both new tests and all untouched Phase 1 tests.

- [x] **Step 5: Commit**

```bash
git add -A && git commit -m "refactor: central CUBE-code catalog in internal/diag with literal-ban test"
```

---

### Task 1: Engine contract-test suite (spec §5, D2)

**Reconcile checkpoint:** 0.9 (Engine interface + `flux.InstallManifests` + flux Deliver/Health shape), 0.6 (Applier ctor for envtest), 0.7 (`registry.InClusterURL`), 0.16 (envtest Makefile pattern).

**Files:**
- Create: `internal/engine/contract/contract.go`
- Create: `internal/engine/flux/contract_test.go`
- Modify: `Makefile` (add `test-engines` target)

**Interfaces:**
- Consumes: `engine.Engine`, `engine.ArtifactRef`, `pack.Rendered`, `apply.New`, `registry.InClusterURL`.
- Produces (Task 2 depends on this exact surface):

```go
package contract
type Impl struct {
    Name string
    New  func() engine.Engine // Engine itself carries InstallManifests() — phase-1 interface method (Task 0 finding 0.9)
}
func Run(t *testing.T, impl Impl) // the one suite every engine must pass identically
```

The suite is the D2 honesty mechanism: *an abstraction with one implementation is a lie.* It asserts only interface-level behavior — nothing flux- or argo-shaped — so both engines run the byte-identical assertions.

- [x] **Step 1: Write the suite (it IS the failing test for Task 2, and a regression net for flux today)**

`internal/engine/contract/contract.go`:

```go
// Package contract is the shared GitOpsEngine conformance suite (spec §5).
// Every engine implementation registers itself via a small contract_test.go
// and must pass identical assertions — the mechanism that keeps D2 honest.
package contract

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/yaml"

	"github.com/rafpe/cube-idp/internal/apply"
	"github.com/rafpe/cube-idp/internal/engine"
	"github.com/rafpe/cube-idp/internal/pack"
	"github.com/rafpe/cube-idp/internal/registry"
)

type Impl struct {
	Name string
	New  func() engine.Engine // Engine carries InstallManifests() (interface method since phase 1 Task 10)
}

func Run(t *testing.T, impl Impl) {
	ctx := context.Background()
	demo := &pack.Rendered{Name: "demo", Version: "0.1.0"}
	demoRef := engine.ArtifactRef{Repo: "packs/demo", Tag: "0.1.0"}

	t.Run("deliver_returns_addressable_objects", func(t *testing.T) {
		objs, err := impl.New().Deliver(ctx, demo, demoRef)
		if err != nil {
			t.Fatal(err)
		}
		if len(objs) == 0 {
			t.Fatal("Deliver returned no objects")
		}
		for _, o := range objs {
			if o.GetKind() == "" || o.GetName() == "" || o.GetNamespace() == "" {
				t.Fatalf("delivery object missing kind/name/namespace: %v", o.Object)
			}
		}
	})

	t.Run("deliver_references_the_artifact", func(t *testing.T) {
		objs, err := impl.New().Deliver(ctx, demo, demoRef)
		if err != nil {
			t.Fatal(err)
		}
		blob := marshalAll(t, objs)
		wantURL := fmt.Sprintf("oci://%s/%s", registry.InClusterURL, demoRef.Repo)
		if !strings.Contains(blob, wantURL) {
			t.Fatalf("delivery objects never reference %q:\n%s", wantURL, blob)
		}
		if !strings.Contains(blob, demoRef.Tag) {
			t.Fatalf("delivery objects never reference tag %q:\n%s", demoRef.Tag, blob)
		}
	})

	t.Run("deliver_is_deterministic", func(t *testing.T) {
		a, err := impl.New().Deliver(ctx, demo, demoRef)
		if err != nil {
			t.Fatal(err)
		}
		b, err := impl.New().Deliver(ctx, demo, demoRef)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(a, b) {
			t.Fatal("two Deliver calls with identical input produced different objects")
		}
	})

	t.Run("deliver_names_are_distinct_per_pack", func(t *testing.T) {
		aObjs, _ := impl.New().Deliver(ctx, demo, demoRef)
		other := &pack.Rendered{Name: "other", Version: "0.1.0"}
		bObjs, _ := impl.New().Deliver(ctx, other, engine.ArtifactRef{Repo: "packs/other", Tag: "0.1.0"})
		names := map[string]bool{}
		for _, o := range aObjs {
			names[o.GetKind()+"/"+o.GetName()] = true
		}
		for _, o := range bObjs {
			if names[o.GetKind()+"/"+o.GetName()] {
				t.Fatalf("packs demo and other collide on %s/%s — down/prune cannot tell them apart", o.GetKind(), o.GetName())
			}
		}
	})

	t.Run("install_manifests_parse", func(t *testing.T) {
		objs, err := impl.New().InstallManifests()
		if err != nil {
			t.Fatal(err)
		}
		if len(objs) < 10 {
			t.Fatalf("install manifests look empty (%d objects) — regenerate them", len(objs))
		}
		hasNS := false
		for _, o := range objs {
			if o.GetKind() == "Namespace" {
				hasNS = true
			}
		}
		if !hasNS {
			t.Fatal("install manifests must carry their own Namespace (offline, self-contained install)")
		}
	})

	t.Run("install_health_uninstall_on_cluster", func(t *testing.T) {
		cfg := startEnvtest(t)
		a, err := apply.New(cfg, "contract-"+impl.Name)
		if err != nil {
			t.Fatal(err)
		}
		objs, err := impl.New().InstallManifests()
		if err != nil {
			t.Fatal(err)
		}
		// wait=false: envtest runs no controllers, Deployments never go Ready.
		// Readiness is asserted end-to-end in the CI engine matrix (Task 14).
		if err := a.Apply(ctx, objs, false, time.Minute); err != nil {
			t.Fatalf("install manifests must SSA-apply cleanly: %v", err)
		}
		if _, err := impl.New().Health(ctx, a); err != nil {
			t.Fatalf("Health must not error on a fresh, empty install: %v", err)
		}
		if err := impl.New().Uninstall(ctx, a, time.Minute); err != nil {
			t.Fatalf("Uninstall must not error: %v", err)
		}
	})
}

func marshalAll(t *testing.T, objs []*unstructured.Unstructured) string {
	t.Helper()
	var b strings.Builder
	for _, o := range objs {
		y, err := yaml.Marshal(o.Object)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(y)
		b.WriteString("---\n")
	}
	return b.String()
}

func startEnvtest(t *testing.T) *rest.Config {
	t.Helper()
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("KUBEBUILDER_ASSETS not set — run via `make test-engines`")
	}
	env := &envtest.Environment{}
	cfg, err := env.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = env.Stop() })
	return cfg
}
```

(Import `"k8s.io/client-go/rest"` for the helper's return type. RESOLVED by Task 0 finding 0.6: `apply.New(cfg *rest.Config, cubeName string)` — takes an envtest `*rest.Config` directly, exactly as the phase-1 apply tests do.)

- [x] **Step 2: Register flux against the suite**

`internal/engine/flux/contract_test.go`:

```go
package flux

import (
	"testing"

	"github.com/rafpe/cube-idp/internal/engine"
	"github.com/rafpe/cube-idp/internal/engine/contract"
)

func TestFluxContract(t *testing.T) {
	contract.Run(t, contract.Impl{
		Name: "flux",
		New:  func() engine.Engine { return New() },
	})
}
```

- [x] **Step 3: Add the Makefile target**

```make
test-engines:
	KUBEBUILDER_ASSETS=$$(go run sigs.k8s.io/controller-runtime/tools/setup-envtest@latest use 1.33 -p path) \
	go test ./internal/engine/... -v
```

- [x] **Step 4: Run the suite against flux**

Run: `make test-engines`
Expected: all `TestFluxContract` subtests PASS (the envtest subtest may Skip locally without assets — it must PASS in CI). If flux fails a subtest, the fix belongs in the flux engine, not in the suite — the suite is the contract.

- [x] **Step 5: Commit**

```bash
git add -A && git commit -m "test: shared GitOpsEngine contract suite, flux passing"
```

---

### Task 2: Argo CD engine implementation (D2)

**Reconcile checkpoint:** 0.9 (Engine interface, factory location, flux Deliver naming), 0.3 (`crossValidate`), 0.12 (`cmd/init.go` shape), 0.15 (CUBE-3002/3003 exact call sites), Task 1 merged.

**Files:**
- Create: `internal/engine/argocd/argocd.go`, `internal/engine/argocd/deliver.go`, `internal/engine/argocd/manifests/install.yaml` (generated), `internal/engine/argocd/manifests/repo-secret.yaml`, `hack/gen-argocd-manifests.sh`
- Modify: `internal/engine/factory/factory.go` (wire argocd, delete CUBE-3002), `internal/engine/engine_test.go` or `factory` tests (replace the CUBE-3002 expectation), `internal/config/load.go` (CUBE-0005 cross-check), `cmd/init.go` (`--engine` flag)
- Test: `internal/engine/argocd/argocd_test.go`, `internal/engine/argocd/contract_test.go`, `internal/config/load_test.go` (extend)

**Interfaces:**
- Consumes: `engine.Engine`, `apply.Applier`, `registry.InClusterURL`, `pack.Rendered`, `contract.Run`.
- Produces:

```go
package argocd
func New() *ArgoCD                                                        // implements engine.Engine
func (g *ArgoCD) InstallManifests() ([]*unstructured.Unstructured, error) // interface method (Task 0 finding 0.9): install.yaml + repo-secret.yaml
const Namespace = "argocd"
```

**Engine-specific requirement (spec §7 risk, documented here and in the package comment):** the Argo CD engine's primary Deliver path uses Argo CD's native OCI repository source pointing at the in-cluster zot registry. `RECONCILE:` verify during implementation, against the pinned Argo CD release, that an `Application` whose `spec.source.repoURL` is `oci://…` syncs a plain-manifest OCI artifact pushed by `oci.PushRendered` (Argo CD gained first-class OCI repository support in the 3.x line; confirm the exact minimum version and pin it in `hack/gen-argocd-manifests.sh`). **If it cannot** (artifact media-type mismatch, insecure-HTTP registry rejected, or feature gated off), the fallback per spec §7 is: the argocd engine declares a dependency on the **gitea pack** and `Deliver` pushes the rendered manifests to an in-cluster gitea repo (`cube-idp-delivery/<pack>`) instead of zot, emitting an `Application` with a git `repoURL` — documented as an engine-specific requirement, not a core component, and enforced at `up` time with `CUBE-3006` ("engine.type: argocd requires the gitea pack for delivery on this Argo CD version — add `oci://ghcr.io/cube-idp/packs/gitea` to spec.packs"). Do not build the fallback speculatively; build it only if the primary path fails verification, as its own inserted task.

- [x] **Step 1: Generate + pin the Argo CD install manifests**

`hack/gen-argocd-manifests.sh`:

```bash
#!/usr/bin/env bash
# Regenerates the embedded Argo CD install manifests (pre-rendered at build
# time: no external binaries at runtime, works offline — same posture as
# hack/gen-flux-manifests.sh).
set -euo pipefail
cd "$(dirname "$0")/.."
ARGOCD_VERSION="${ARGOCD_VERSION:-v3.1.0}"  # RECONCILE: pin the latest stable 3.x with OCI repo support at implementation time
OUT=internal/engine/argocd/manifests/install.yaml
{
  printf 'apiVersion: v1\nkind: Namespace\nmetadata:\n  name: argocd\n---\n'
  curl -fsSL "https://raw.githubusercontent.com/argoproj/argo-cd/${ARGOCD_VERSION}/manifests/install.yaml"
} > "$OUT"
echo "wrote $OUT (argo-cd ${ARGOCD_VERSION})"
```

Run it: `bash hack/gen-argocd-manifests.sh`.

`internal/engine/argocd/manifests/repo-secret.yaml` — registers zot as an OCI repository so Argo CD accepts the plain-HTTP in-cluster registry:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: cube-idp-zot-repo
  namespace: argocd
  labels:
    argocd.argoproj.io/secret-type: repository
stringData:
  name: cube-idp-zot
  type: oci
  url: oci://zot.cube-idp-system.svc.cluster.local:5000
  enableOCI: "true"
  insecure: "true"
```

(RECONCILE: verify the exact repository-secret field names (`type: oci` vs `enableOCI`, `insecure` vs `insecureOCIForceHttp`) against the pinned Argo CD version's declarative-setup docs, then fix this file and the Step 3 test together.)

- [x] **Step 2: Write the failing argocd-specific tests**

`internal/engine/argocd/argocd_test.go`:

```go
package argocd

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/rafpe/cube-idp/internal/engine"
	"github.com/rafpe/cube-idp/internal/pack"
)

func nestedString(o *unstructured.Unstructured, fields ...string) string {
	s, _, _ := unstructured.NestedString(o.Object, fields...)
	return s
}

func TestDeliverShapesApplication(t *testing.T) {
	g := New()
	r := &pack.Rendered{Name: "gitea", Version: "0.1.0"}
	objs, err := g.Deliver(context.Background(), r, engine.ArtifactRef{Repo: "packs/gitea", Tag: "0.1.0"})
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 1 {
		t.Fatalf("want exactly one Application, got %d objects", len(objs))
	}
	app := objs[0]
	if app.GetKind() != "Application" || app.GetNamespace() != Namespace {
		t.Fatalf("got %s in ns %s", app.GetKind(), app.GetNamespace())
	}
	if got := nestedString(app, "spec", "source", "repoURL"); got != "oci://zot.cube-idp-system.svc.cluster.local:5000/packs/gitea" {
		t.Fatalf("repoURL: %s", got)
	}
	if got := nestedString(app, "spec", "source", "targetRevision"); got != "0.1.0" {
		t.Fatalf("targetRevision: %s", got)
	}
	prune, _, _ := unstructured.NestedBool(app.Object, "spec", "syncPolicy", "automated", "prune")
	if !prune {
		t.Fatal("syncPolicy.automated.prune must be true (down/upgrade rely on it)")
	}
	if got := nestedString(app, "spec", "destination", "server"); got != "https://kubernetes.default.svc" {
		t.Fatalf("destination: %s", got)
	}
}

func TestInstallManifestsIncludeRepoSecret(t *testing.T) {
	objs, err := New().InstallManifests()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, o := range objs {
		if o.GetKind() == "Secret" && o.GetName() == "cube-idp-zot-repo" {
			found = true
		}
	}
	if !found {
		t.Fatal("install manifests must register the zot OCI repository (engine-specific requirement)")
	}
}
```

`internal/engine/argocd/contract_test.go`:

```go
package argocd

import (
	"testing"

	"github.com/rafpe/cube-idp/internal/engine"
	"github.com/rafpe/cube-idp/internal/engine/contract"
)

func TestArgoCDContract(t *testing.T) {
	contract.Run(t, contract.Impl{
		Name: "argocd",
		New:  func() engine.Engine { return New() },
	})
}
```

- [x] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/engine/argocd/ -v`
Expected: FAIL (package does not exist)

- [x] **Step 4: Implement the engine**

`internal/engine/argocd/argocd.go`:

```go
// Package argocd implements the GitOpsEngine over Argo CD (D2). Delivery
// shape: one Application per pack with an OCI repository source pointing at
// the in-cluster zot registry. ENGINE-SPECIFIC REQUIREMENT (spec §7): this
// engine needs an Argo CD version with OCI repository support; if that path
// proves insufficient the documented fallback is delivery via the gitea
// pack — see the Phase 2 plan, Task 2.
package argocd

import (
	"context"
	_ "embed"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/rafpe/cube-idp/internal/apply"
	"github.com/rafpe/cube-idp/internal/diag"
	"github.com/rafpe/cube-idp/internal/engine"
)

const Namespace = "argocd"

//go:embed manifests/install.yaml
var installYAML []byte

//go:embed manifests/repo-secret.yaml
var repoSecretYAML []byte

type ArgoCD struct{}

func New() *ArgoCD { return &ArgoCD{} }

func (g *ArgoCD) InstallManifests() ([]*unstructured.Unstructured, error) {
	objs, err := apply.ParseMultiDoc(installYAML)
	if err != nil {
		return nil, err
	}
	secretObjs, err := apply.ParseMultiDoc(repoSecretYAML)
	if err != nil {
		return nil, err
	}
	return append(objs, secretObjs...), nil
}

func (g *ArgoCD) Install(ctx context.Context, a *apply.Applier, timeout time.Duration) error {
	objs, err := g.InstallManifests()
	if err != nil {
		return diag.Wrap(err, "CUBE-3003", "embedded argocd manifests are invalid",
			"this is a cube-idp bug — regenerate with hack/gen-argocd-manifests.sh and report it")
	}
	return a.Apply(ctx, objs, true, timeout)
}

func (g *ArgoCD) Uninstall(ctx context.Context, a *apply.Applier, timeout time.Duration) error {
	// Same posture as flux: removal is inventory-driven by `down`; the engine
	// needs nothing beyond being present in the inventory.
	return nil
}

func (g *ArgoCD) Health(ctx context.Context, a *apply.Applier) ([]engine.ComponentHealth, error) {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "argoproj.io", Version: "v1alpha1", Kind: "ApplicationList"})
	if err := a.Client().List(ctx, list,
		client.InNamespace(Namespace), client.HasLabels{apply.CubeLabel}); err != nil {
		// No Applications CRD yet (fresh cluster) is not an error condition.
		return nil, client.IgnoreNotFound(err)
	}
	var out []engine.ComponentHealth
	for _, item := range list.Items {
		health, _, _ := unstructured.NestedString(item.Object, "status", "health", "status")
		sync, _, _ := unstructured.NestedString(item.Object, "status", "sync", "status")
		msg, _, _ := unstructured.NestedString(item.Object, "status", "operationState", "message")
		out = append(out, engine.ComponentHealth{
			Name:    item.GetName(),
			Ready:   health == "Healthy" && sync == "Synced",
			Message: sync + "/" + health + " " + msg,
		})
	}
	return out, nil
}
```

(RESOLVED by Task 0 finding 0.9: phase-1 flux `Health` does NOT handle the missing-CRD case — only flux's `listDelivered` (Uninstall path) uses `meta.IsNoMatchError`. The argocd engine must do better than flux here because the contract suite exercises Health right after an envtest install: replace the `client.IgnoreNotFound(err)` above with `if meta.IsNoMatchError(err) { return nil, nil }` (import `k8s.io/apimachinery/pkg/api/meta`) and otherwise wrap as a CUBE-3004 diag error, mirroring `listDelivered`'s treatment. Optionally apply the same one-line hardening to flux `Health` while in there — it is a real phase-1 gap the suite may expose.)

`internal/engine/argocd/deliver.go`:

```go
package argocd

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/rafpe/cube-idp/internal/engine"
	"github.com/rafpe/cube-idp/internal/pack"
	"github.com/rafpe/cube-idp/internal/registry"
)

func (g *ArgoCD) Deliver(ctx context.Context, r *pack.Rendered, src engine.ArtifactRef) ([]*unstructured.Unstructured, error) {
	name := "cube-idp-" + r.Name
	app := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "argoproj.io/v1alpha1",
		"kind":       "Application",
		"metadata": map[string]any{
			"name":       name,
			"namespace":  Namespace,
			"finalizers": []any{"resources-finalizer.argocd.argoproj.io"},
		},
		"spec": map[string]any{
			"project": "default",
			"source": map[string]any{
				"repoURL":        fmt.Sprintf("oci://%s/%s", registry.InClusterURL, src.Repo),
				"targetRevision": src.Tag,
				"path":           ".",
			},
			"destination": map[string]any{"server": "https://kubernetes.default.svc"},
			"syncPolicy": map[string]any{
				"automated":   map[string]any{"prune": true, "selfHeal": true},
				"syncOptions": []any{"CreateNamespace=true", "ServerSideApply=true"},
			},
		},
	}}
	return []*unstructured.Unstructured{app}, nil
}
```

- [x] **Step 5: Wire the factory (delete CUBE-3002) and the config cross-check**

In `internal/engine/factory/factory.go`, replace the `"argocd"` case:

```go
case "argocd":
	return argocd.New(), nil
```

(import `github.com/rafpe/cube-idp/internal/engine/argocd`). Delete the CUBE-3002 construction error entirely; update the factory test: `TestFactoryArgoCDPhase2` becomes

```go
func TestFactoryArgoCD(t *testing.T) {
	if _, err := New("argocd"); err != nil {
		t.Fatalf("argocd engine must construct in Phase 2 (D2): %v", err)
	}
}
```

In `internal/config/load.go` `crossValidate`, add (with a matching failing test in `load_test.go` first — fixture `testdata/argocd-engine-with-pack.yaml` setting `engine.type: argocd` and a pack ref containing `packs/argocd`):

```go
if c.Spec.Engine.Type == "argocd" {
	for _, p := range c.Spec.Packs {
		if strings.Contains(p.Ref, "packs/argocd") {
			return diag.New("CUBE-0005",
				"the argocd pack is redundant when engine.type is argocd (the engine installs Argo CD, UI included)",
				"remove the argocd pack from spec.packs")
		}
	}
}
```

- [x] **Step 6: Add `--engine` to init**

In `cmd/init.go`, add `c.Flags().StringVar(&engineType, "engine", "flux", "gitops engine: flux | argocd")` and set `cfg := config.Default(name); cfg.Spec.Engine.Type = engineType` before marshaling. When `engineType == "argocd"`, drop the argocd pack from `cfg.Spec.Packs` (it would trip CUBE-0005). RECONCILE: adapt to the exact phase-1 `newInitCmd` body, including the `--local` resolution from checkpoint 0.12.

- [x] **Step 7: Run everything**

Run: `make test-engines && go test ./internal/config/ ./cmd/ -v && go build ./...`
Expected: `TestArgoCDContract` and `TestFluxContract` both PASS with identical subtests; config and factory tests PASS.

- [x] **Step 8: Commit**

```bash
git add -A && git commit -m "feat: argocd GitOpsEngine with OCI delivery, passing the shared contract suite (D2)"
```

---

### Task 3: Kustomize overlay rendering in the pack engine

**Reconcile checkpoint:** 0.8 (`pack.Render` control flow in `render.go`, manifest-walk code), 0.11 (Helm pin — kustomize must not disturb the helm path), 0.15 (CUBE-4004 wording).

**Files:**
- Create: `internal/pack/kustomize.go`, `internal/pack/testdata/demo-kustomize/{pack.cue,kustomization.yaml,manifests/cm.yaml}`
- Modify: `internal/pack/render.go`
- Test: `internal/pack/pack_test.go` (extend)

**Interfaces:**
- Consumes: `apply.ParseMultiDoc`, `diag`.
- Produces (Task 13 reuses this):

```go
package pack
func RenderDir(dir string) ([]*unstructured.Unstructured, error)
// kustomize-builds dir (which must contain kustomization.yaml). CUBE-4008 on failure.
```

**Render precedence rule (document in the package comment):** if `kustomization.yaml` exists at the pack root, it is the *sole* source of raw manifests — `manifests/` is consumed through it (as `resources:`), never walked independently. Otherwise the Phase 1 behavior (walk `manifests/*.yaml`) is unchanged. `chart.yaml` helm rendering is orthogonal and appended in both cases.

- [x] **Step 1: Create fixtures**

`internal/pack/testdata/demo-kustomize/pack.cue`:

```cue
name:    "demo-kustomize"
version: "0.1.0"
```

`internal/pack/testdata/demo-kustomize/manifests/cm.yaml`:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: demo
  namespace: default
data:
  message: original
```

`internal/pack/testdata/demo-kustomize/kustomization.yaml`:

```yaml
resources:
  - manifests/cm.yaml
patches:
  - target:
      kind: ConfigMap
      name: demo
    patch: |-
      - op: replace
        path: /data/message
        value: patched
```

- [x] **Step 2: Write the failing tests**

Append to `internal/pack/pack_test.go`:

```go
func TestRenderKustomizeOverlay(t *testing.T) {
	p, err := Fetch(context.Background(), "testdata/demo-kustomize", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r, err := p.Render(nil)
	if err != nil {
		t.Fatal(err)
	}
	// exactly one object: kustomization governs manifests/, no double-count
	if len(r.Objects) != 1 {
		t.Fatalf("want 1 object (no double-render of manifests/), got %d", len(r.Objects))
	}
	msg, _, _ := unstructured.NestedString(r.Objects[0].Object, "data", "message")
	if msg != "patched" {
		t.Fatalf("kustomize patch not applied, message=%q", msg)
	}
}

func TestRenderKustomizeFailureIsTyped(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "pack.cue"), []byte("name: \"bad\"\nversion: \"0.1.0\"\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "kustomization.yaml"), []byte("resources: [does-not-exist.yaml]\n"), 0o644)
	p, err := Fetch(context.Background(), dir, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Render(nil)
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-4008" {
		t.Fatalf("want CUBE-4008, got %v", err)
	}
}
```

(Add imports `os`, `path/filepath`, `k8s.io/apimachinery/pkg/apis/meta/v1/unstructured` to the test file.)

- [x] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/pack/ -short -run TestRenderKustomize -v`
Expected: FAIL (RenderDir undefined / message=="original")

- [x] **Step 4: Implement**

```bash
go get sigs.k8s.io/kustomize/api@latest sigs.k8s.io/kustomize/kyaml@latest
```

`internal/pack/kustomize.go`:

```go
package pack

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/kustomize/api/krusty"
	"sigs.k8s.io/kustomize/kyaml/filesys"

	"github.com/rafpe/cube-idp/internal/apply"
	"github.com/rafpe/cube-idp/internal/diag"
)

// RenderDir kustomize-builds dir (which must contain kustomization.yaml) and
// returns the resulting objects. Exported because the cnoe-compat loader
// renders arbitrary directories through the same pipeline.
func RenderDir(dir string) ([]*unstructured.Unstructured, error) {
	k := krusty.MakeKustomizer(krusty.MakeDefaultOptions())
	resMap, err := k.Run(filesys.MakeFsOnDisk(), dir)
	if err != nil {
		return nil, diag.Wrap(err, "CUBE-4008",
			fmt.Sprintf("kustomize render failed for %s", dir),
			"check kustomization.yaml; try `kubectl kustomize` on the directory to reproduce")
	}
	y, err := resMap.AsYaml()
	if err != nil {
		return nil, diag.Wrap(err, "CUBE-4008",
			fmt.Sprintf("kustomize output for %s is not serializable", dir),
			"check kustomization.yaml for exotic transformer output")
	}
	return apply.ParseMultiDoc(y)
}
```

In `internal/pack/render.go`, replace the raw-manifest walk with the precedence rule (keep the existing walk code as the else-branch, unchanged):

```go
if _, statErr := os.Stat(filepath.Join(p.Dir, "kustomization.yaml")); statErr == nil {
	objs, err := RenderDir(p.Dir)
	if err != nil {
		return nil, err
	}
	r.Objects = append(r.Objects, objs...)
} else if entries, err := os.ReadDir(manifestsDir); err == nil {
	// ... Phase 1 raw-manifest walk, verbatim ...
}
```

- [x] **Step 5: Run tests**

Run: `go test ./internal/pack/ -short -v`
Expected: PASS, including all Phase 1 pack tests (precedence must not break the raw-manifest and helm paths)

- [x] **Step 6: Commit**

```bash
git add -A && git commit -m "feat: kustomize overlay rendering in the pack engine"
```

---

### Task 3.5: Consolidate all OCI on oras-go v2 — drop `fluxcd/pkg/oci` (debt paydown)

**Reconcile checkpoint:** 0.10 (**blocking**: exactly which `fluxcd/pkg/oci` entry points `oci.PushRendered` uses today, and how plain-HTTP was enabled for the 127.0.0.1 tunnel), 0.9 (which `OCIRepository` apiVersion/fields the flux Deliver objects set — media-type compatibility is judged against it).

**Why here:** Task 5 threads the pulled digest out of the (already-oras) pull path, and Task 4 adds a new source backend — both touch the OCI seams. Landing the consolidation first means those tasks build on one OCI library, not two.

**Files:**
- Modify: `internal/oci/*.go` (rewrite `PushRendered` on oras-go v2; introduce an `oras.Target` seam), `go.mod` (drop `github.com/fluxcd/pkg/oci`)
- Test: `internal/oci/push_test.go` (extend or add)

**Interfaces:**
- Consumes: `pack.Rendered`, `engine.ArtifactRef`.
- Produces: `oci.PushRendered(ctx, r *pack.Rendered, registryAddr string) (engine.ArtifactRef, error)` — **signature unchanged**; every caller (`up`, Task 13 cnoe import) is untouched.

**The compatibility landmine (this task's whole risk):** Phase 1's flux delivery works because `fluxcd/pkg/oci` pushes artifacts with Flux's media types (config `application/vnd.cncf.flux.config.v1+json`, layer `application/vnd.cncf.flux.content.v1.tar+gzip`), which source-controller consumes without a `layerSelector`. The rewrite must keep the pushed bytes engine-compatible. `RECONCILE:` capture the exact manifest (media types, annotations, layer archive format) Phase 1 pushes — `oras manifest fetch` against a local `up`'s zot — and reproduce it with plain oras-go v2 (`oras.PackManifest` + explicit media types + a tar.gz layer built with `archive/tar` + `compress/gzip`). The Task 14 engine-matrix e2e is the final arbiter for BOTH engines.

- [x] **Step 1: Write the failing media-type test**

Refactor `PushRendered` over an `oras.Target` seam so tests need no network: production passes the `remote.Repository` (plain-HTTP as today), tests pass `oras-go/v2/content/memory.New()`. Test sketch (adapt names to the phase-1 file layout):

```go
func TestPushRenderedKeepsFluxMediaTypes(t *testing.T) {
	r := &pack.Rendered{Name: "demo", Version: "0.1.0",
		Objects: []*unstructured.Unstructured{cmObj("demo", "default")}}
	store := memory.New()
	ref, err := pushRenderedTo(context.Background(), r, store) // the new seam
	if err != nil {
		t.Fatal(err)
	}
	if ref.Repo != "packs/demo" || ref.Tag != "0.1.0" {
		t.Fatalf("ArtifactRef drifted: %+v", ref)
	}
	desc, err := store.Resolve(context.Background(), "0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	manifest := fetchManifest(t, store, desc) // helper: read+unmarshal ocispec.Manifest
	// RECONCILE: pin these two constants to what phase-1 actually pushed.
	if manifest.Config.MediaType != "application/vnd.cncf.flux.config.v1+json" {
		t.Fatalf("config mediaType: %s", manifest.Config.MediaType)
	}
	if len(manifest.Layers) != 1 || manifest.Layers[0].MediaType != "application/vnd.cncf.flux.content.v1.tar+gzip" {
		t.Fatalf("layers: %+v", manifest.Layers)
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/oci/ -v`
Expected: FAIL (`pushRenderedTo` undefined)

- [x] **Step 3: Implement**

Rewrite `internal/oci` push: build one tar.gz layer containing the rendered multi-doc YAML (same entry name phase-1 produced — `RECONCILE:` check what source-controller unpacked, typically the manifest file at the archive root), push config + layer + manifest via `oras.PackManifest`/`store.Push` with the pinned media types, tag it, and keep `PushRendered(ctx, r, registryAddr)` delegating to the seam with a `remote.Repository{PlainHTTP: true}` target (reuse the phase-1 insecure setup per checkpoint 0.10).

- [x] **Step 4: Drop the dependency**

Run: `go mod tidy && grep -c fluxcd/pkg/oci go.mod`
Expected: `0` — the module is gone (`fluxcd/pkg/ssa` stays; only `pkg/oci` is retired).

- [x] **Step 5: Run everything, including one real flux round-trip**

Run: `go build ./... && go test ./... -short && make test-apply`
Expected: PASS. Then run the flux e2e locally (`CUBE_IDP_E2E=1 go test ./tests/e2e/ -v -timeout 25m`) — a pack delivered through the rewritten push must reach Ready. If source-controller rejects the artifact, the media-type reconcile in Step 1 was wrong — fix constants, not flux.

- [x] **Step 6: Commit**

```bash
git add -A && git commit -m "refactor: all OCI on oras-go v2, drop fluxcd/pkg/oci (debt paydown)"
```

---

### Task 4: go-getter pack sources — git/http/s3 refs behind the `PackSource` seam (spec §4.4, REWORKED by the 2026-07-13 amendment)

> **Supersedes the original hand-rolled go-git source.** Spec §4.4 (post-`e3af013`) decides: "Pack sources resolve via go-getter (file/http/git/s3/OCI in one addressing scheme) behind the `PackSource` seam, with cube-idp's own extraction guards (path traversal, symlink skipping) always applied to getter output." The fork in use is **`github.com/RafPe/go-getter` tag `v1.9.0`**, which adds an `oci://` getter on oras-go v2.

**Reconcile checkpoint:** 0.8 (`Fetch` switch in `source.go`, exact CUBE-4001 message being replaced, `Pack` struct fields), 0.15 (4xxx codes free), 0.18 (**blocking**: fork consumption mechanics + getter/detector inventory), Task 3.5 merged (one OCI library).

**Fork facts (verified 2026-07-13 at tag `v1.9.0`):**
- The fork's `go.mod` declares `module github.com/hashicorp/go-getter` (the **v1 API line**), so the import path stays `github.com/hashicorp/go-getter` and the dependency is consumed via a `replace` directive pointing at `github.com/rafpe/go-getter v1.9.0` (Step 0). Do not `go get github.com/rafpe/go-getter` directly (declared-path mismatch) — the replace is the mechanism.
- Its `OCIGetter` (`get_oci.go`) pulls via `oras.Copy` but **discards the returned descriptor (no digest out) and has no plain-HTTP option**. cube-idp needs both (digest → `cube.lock`, plain HTTP → the 127.0.0.1 zot tunnel). Therefore: **`oci://` pack refs keep the phase-1 oras-go v2 pull path**; go-getter owns git/http/s3 refs. Both live behind the same `Fetch` seam, so the spec's "one addressing scheme" holds at the CLI surface. If checkpoint 0.18 finds the fork has gained digest exposure + plain-HTTP, collapsing the oci branch onto go-getter becomes a follow-up task — do not do it speculatively here.
- Its `OCIDetector` auto-rewrites bare refs matching {azurecr.io, gcr.io, registry.gitlab.com, `*.dkr.ecr.*.amazonaws.com`, localhost:5000, 127.0.0.1:5000} to `oci://`. cube-idp does NOT enable ambient detectors (deterministic ref handling — an explicit scheme or the git-ref grammar is required), so this detector stays unused.
- The v1 `GitGetter` **shells out to the `git` CLI**. A missing git binary surfaces as CUBE-4006 with an "install git" remediation, and Task 12's doctor gains a warning-level check (Step 6 below).

**Files:**
- Create: `internal/pack/getter.go`, `internal/pack/guards.go`, `internal/pack/resolve.go` (git pin probe — Task 7 extends this file)
- Modify: `go.mod` (fork replace), `internal/pack/source.go` (route git/getter refs; update the CUBE-4001 remediation text), `internal/pack/pack.go` (add `Pinned` field)
- Test: `internal/pack/getter_test.go`, `internal/pack/guards_test.go`

**Interfaces:**
- Consumes: `loadMeta`, `diag`, `go-git` (pin probe only), `github.com/hashicorp/go-getter` (fork, via replace).
- Produces (Tasks 5/7/13 depend on these):

```go
type Pack struct{ Name, Version, Dir, Pinned string } // Pinned: "git+<sha>" | "oci:<digest>" | "dir:<dirhash>" (Task 5 fills the last two)
func isGitRef(ref string) bool     // host-with-dot + path, no scheme — github.com/org/repo//path@rev grammar
func isGetterRef(ref string) bool  // explicit go-getter forms: git::…, s3::…, http(s):// archive refs
// ref grammar (git): <host>/<org>/<repo>[//<subdir>]@<tag|branch|full-sha>  (spec §4.4)
// unpinned (no @rev) -> CUBE-4007; fetch/resolution failure -> CUBE-4006; guard trip -> CUBE-4014
func resolveGitPin(ctx context.Context, repoURL, rev string) (string, error) // "git+<full-sha>" via go-git ls-remote; shared with Task 7's ResolveRemote
func GuardTree(root string) (removedSymlinks []string, err error)            // extraction guards, applied to ALL getter output
```

- [x] **Step 0: Add the fork dependency (exact commands — verified consumption path)**

```bash
go mod edit -require=github.com/hashicorp/go-getter@v1.9.0 \
  -replace=github.com/hashicorp/go-getter=github.com/rafpe/go-getter@v1.9.0
go get github.com/go-git/go-git/v5@latest
go mod tidy
```

Expected: `go.mod` gains `replace github.com/hashicorp/go-getter => github.com/rafpe/go-getter v1.9.0` (verified end-to-end 2026-07-13 — resolve, build, and run all green). Smoke-check: `go build ./...` still green. If `go mod tidy` rejects the replace, checkpoint 0.18 was not honored — re-verify the fork state before proceeding.

- [x] **Step 1: Write the failing tests**

`internal/pack/getter_test.go`:

```go
package pack

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/rafpe/cube-idp/internal/diag"
)

// makeGitFixture creates a local repo with a pack under packs/demo, tagged
// v0.1.0, using the git CLI (the same binary go-getter's GitGetter shells
// out to — if it is absent, these tests skip exactly like the getter would
// fail loudly in production).
func makeGitFixture(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git CLI not on PATH")
	}
	dir := t.TempDir()
	packDir := filepath.Join(dir, "packs", "demo")
	if err := os.MkdirAll(filepath.Join(packDir, "manifests"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(packDir, "pack.cue"), []byte("name: \"demo\"\nversion: \"0.1.0\"\n"), 0o644)
	os.WriteFile(filepath.Join(packDir, "manifests", "cm.yaml"),
		[]byte("apiVersion: v1\nkind: ConfigMap\nmetadata: {name: demo, namespace: default}\n"), 0o644)
	for _, args := range [][]string{
		{"init", "-q"}, {"add", "."},
		{"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "init"},
		{"tag", "v0.1.0"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func TestIsGitRef(t *testing.T) {
	for ref, want := range map[string]bool{
		"github.com/org/repo//packs/foo@v1": true,
		"gitlab.corp.example/a/b@main":      true,
		"./packs/gitea":                     false,
		"packs/gitea":                       false,
		"oci://ghcr.io/org/pack:v1":         false,
		"git::https://example.com/repo":     false, // explicit getter form, not the bare grammar
		"/abs/path":                         false,
	} {
		if got := isGitRef(ref); got != want {
			t.Fatalf("isGitRef(%q) = %v, want %v", ref, got, want)
		}
	}
}

func TestIsGetterRef(t *testing.T) {
	for ref, want := range map[string]bool{
		"git::https://example.com/repo?ref=v1":   true,
		"s3::https://s3.amazonaws.com/b/pack":    true,
		"https://example.com/pack.tar.gz":        true,
		"oci://ghcr.io/org/pack:v1":              false, // stays on the oras path (digest + plain-HTTP)
		"github.com/org/repo//packs/foo@v1":      false, // bare git grammar, translated first
		"./packs/gitea":                          false,
	} {
		if got := isGetterRef(ref); got != want {
			t.Fatalf("isGetterRef(%q) = %v, want %v", ref, got, want)
		}
	}
}

func TestGitRefMustBePinned(t *testing.T) {
	_, err := Fetch(context.Background(), "github.com/org/repo//packs/foo", t.TempDir())
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-4007" {
		t.Fatalf("want CUBE-4007, got %v", err)
	}
}

func TestFetchGitByTag(t *testing.T) {
	fixture := makeGitFixture(t)
	restore := gitCloneURL
	gitCloneURL = func(repoPath string) string { return fixture }
	defer func() { gitCloneURL = restore }()

	p, err := Fetch(context.Background(), "example.com/org/repo//packs/demo@v0.1.0", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "demo" || p.Version != "0.1.0" {
		t.Fatalf("metadata: %+v", p)
	}
	if len(p.Pinned) < len("git+")+40 || p.Pinned[:4] != "git+" {
		t.Fatalf("Pinned must be git+<full-sha>, got %q", p.Pinned)
	}
}

func TestFetchGitUnknownRevision(t *testing.T) {
	fixture := makeGitFixture(t)
	restore := gitCloneURL
	gitCloneURL = func(repoPath string) string { return fixture }
	defer func() { gitCloneURL = restore }()

	_, err := Fetch(context.Background(), "example.com/org/repo//packs/demo@v9.9.9", t.TempDir())
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-4006" {
		t.Fatalf("want CUBE-4006, got %v", err)
	}
}
```

`internal/pack/guards_test.go`:

```go
package pack

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGuardTreeStripsSymlinks(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "ok.yaml"), []byte("a: 1"), 0o644)
	if err := os.Symlink("/etc/passwd", filepath.Join(root, "evil")); err != nil {
		t.Skip("symlinks unavailable on this platform")
	}
	removed, err := GuardTree(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 {
		t.Fatalf("want 1 removed symlink, got %v", removed)
	}
	if _, err := os.Lstat(filepath.Join(root, "evil")); !os.IsNotExist(err) {
		t.Fatal("symlink must be gone after GuardTree")
	}
	if _, err := os.Stat(filepath.Join(root, "ok.yaml")); err != nil {
		t.Fatal("regular files must survive GuardTree")
	}
}
```

- [x] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/pack/ -short -run 'TestIsGitRef|TestIsGetterRef|TestGitRef|TestFetchGit|TestGuardTree' -v`
Expected: FAIL (isGitRef/isGetterRef/GuardTree undefined)

- [x] **Step 3: Implement the guards and the pin probe**

`internal/pack/guards.go`:

```go
package pack

import (
	"os"
	"path/filepath"

	"github.com/rafpe/cube-idp/internal/diag"
)

// GuardTree applies cube-idp's extraction guards (spec §4.4) to a fetched
// pack tree: every symlink is removed (a pack is data-only — a symlink can
// point outside the tree or alias files during render), and any walk error
// aborts the fetch. Applied to ALL getter output, regardless of source.
func GuardTree(root string) ([]string, error) {
	var removed []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink == 0 {
			return nil
		}
		if err := os.Remove(path); err != nil {
			return diag.Wrap(err, "CUBE-4014",
				"cannot remove symlink "+path+" from the fetched pack",
				"the pack source contains symlinks cube-idp refuses to follow; re-publish the pack without them")
		}
		rel, _ := filepath.Rel(root, path)
		removed = append(removed, rel)
		return nil
	})
	if err != nil {
		if _, ok := err.(*diag.Error); ok {
			return nil, err
		}
		return nil, diag.Wrap(err, "CUBE-4014", "cannot scan the fetched pack tree", "check permissions under the cache dir")
	}
	return removed, nil
}
```

`internal/pack/resolve.go` (Task 7 extends this file with `ResolveRemote`):

```go
package pack

import (
	"context"
	"fmt"

	"github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/rafpe/cube-idp/internal/diag"
)

// gitCloneURL maps the repo path from a pack ref to a cloneable URL.
// Overridden in tests to point at a local fixture repository.
var gitCloneURL = func(repoPath string) string { return "https://" + repoPath }

// resolveGitPin returns "git+<full-sha>" for rev in the repo — the pin
// recorded in Pack.Pinned and cube.lock. go-git's in-memory ls-remote; no
// clone, no git CLI. A 40-char rev is its own pin.
func resolveGitPin(ctx context.Context, repoURL, rev string) (string, error) {
	if len(rev) == 40 {
		return "git+" + rev, nil
	}
	rem := git.NewRemote(memory.NewStorage(), &gitconfig.RemoteConfig{
		Name: "origin", URLs: []string{repoURL},
	})
	refs, err := rem.ListContext(ctx, &git.ListOptions{})
	if err != nil {
		return "", diag.Wrap(err, "CUBE-4006", fmt.Sprintf("cannot list refs of %s", repoURL),
			"check the repo path and your network")
	}
	for _, r := range refs {
		n := r.Name()
		if n.Short() == rev || n.String() == "refs/tags/"+rev || n.String() == "refs/heads/"+rev {
			return "git+" + r.Hash().String(), nil
		}
	}
	return "", diag.New("CUBE-4006", fmt.Sprintf("revision %q not found in %s", rev, repoURL),
		"use a tag, branch, or full commit SHA that exists in the repository")
}
```

- [x] **Step 4: Implement the getter source**

`internal/pack/getter.go`:

```go
package pack

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	getter "github.com/hashicorp/go-getter" // RafPe fork via replace (go.mod)

	"github.com/rafpe/cube-idp/internal/diag"
)

// isGitRef: no scheme, and the first path segment looks like a hostname
// (contains a dot) — distinguishes github.com/org/repo from ./dir and packs/x.
func isGitRef(ref string) bool {
	if strings.Contains(ref, "://") || strings.Contains(ref, "::") ||
		strings.HasPrefix(ref, "/") || strings.HasPrefix(ref, ".") {
		return false
	}
	first, rest, ok := strings.Cut(ref, "/")
	return ok && rest != "" && strings.Contains(first, ".")
}

// isGetterRef: explicitly-schemed go-getter forms. oci:// is EXCLUDED — it
// stays on the oras path (digest for cube.lock + plain-HTTP for the zot
// tunnel; the fork's OCIGetter exposes neither — see the task header).
func isGetterRef(ref string) bool {
	if strings.HasPrefix(ref, "oci://") {
		return false
	}
	return strings.Contains(ref, "::") ||
		strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://")
}

// sanitizeRef turns a ref into a filesystem-safe cache-dir segment.
func sanitizeRef(ref string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '/', ':', '?', '&', '=':
			return '_'
		}
		return r
	}, ref)
}

// fetchGetter fetches a pack via go-getter into cacheDir and applies the
// extraction guards. src is a ready go-getter URL (explicit form, or the
// translation fetchGit produced). subdir selection uses go-getter's native
// // syntax inside src.
// RECONCILE: verify the fork's v1 Client field set {Ctx, Src, Dst, Mode,
// Getters, Detectors} against the pinned commit; the intent is fixed —
// explicit getter map, NO ambient detectors, ClientModeDir.
func fetchGetter(ctx context.Context, src, dst string) error {
	client := &getter.Client{
		Ctx:  ctx,
		Src:  src,
		Dst:  dst,
		Mode: getter.ClientModeDir,
		Detectors: []getter.Detector{}, // deterministic: schemes are explicit
		Getters: map[string]getter.Getter{
			"git":   new(getter.GitGetter), // shells out to the git CLI
			"http":  new(getter.HttpGetter),
			"https": new(getter.HttpGetter),
			"s3":    new(getter.S3Getter),
			"file":  new(getter.FileGetter),
		},
	}
	if err := client.Get(); err != nil {
		return diag.Wrap(err, "CUBE-4006", fmt.Sprintf("cannot fetch pack source %q", src),
			"check the ref, your network, and that the git CLI is installed for git sources")
	}
	if _, err := GuardTree(dst); err != nil {
		_ = os.RemoveAll(dst)
		return err
	}
	return nil
}

// fetchGit resolves the bare git grammar <host>/<org>/<repo>[//subdir]@rev:
// pin first (ls-remote — fails fast, no clone on bad revs), then go-getter
// fetch of the subdir at that exact SHA.
func fetchGit(ctx context.Context, ref, cacheDir string) (*Pack, error) {
	base, rev, ok := strings.Cut(ref, "@")
	if !ok || rev == "" {
		return nil, diag.New("CUBE-4007",
			fmt.Sprintf("git pack ref %q is not pinned", ref),
			"append @<tag|branch|commit>, e.g. github.com/org/repo//packs/foo@v1.2.0")
	}
	repoPath, subdir, _ := strings.Cut(base, "//")
	repoURL := gitCloneURL(repoPath)

	pin, err := resolveGitPin(ctx, repoURL, rev)
	if err != nil {
		return nil, err
	}
	sha := strings.TrimPrefix(pin, "git+")

	dst := filepath.Join(cacheDir, "git", strings.ReplaceAll(repoPath, "/", "_")+"@"+sha)
	if _, statErr := os.Stat(dst); statErr != nil {
		src := fmt.Sprintf("git::%s?ref=%s", repoURL, sha)
		if subdir != "" {
			src = fmt.Sprintf("git::%s//%s?ref=%s", repoURL, subdir, sha)
		}
		if err := fetchGetter(ctx, src, dst); err != nil {
			_ = os.RemoveAll(dst)
			return nil, err
		}
	}
	p, err := loadMeta(dst)
	if err != nil {
		return nil, err
	}
	p.Pinned = pin
	return p, nil
}
```

In `internal/pack/source.go`, extend the `Fetch` switch **before** the unknown-scheme case (the `oci://` case above it is untouched — oras path, Task 3.5), and update the CUBE-4001 remediation (git refs are no longer "Phase 2"):

```go
case isGitRef(ref):
	return fetchGit(ctx, ref, cacheDir)
case isGetterRef(ref):
	dst := filepath.Join(cacheDir, "getter", sanitizeRef(ref))
	if err := fetchGetter(ctx, ref, dst); err != nil {
		return nil, err
	}
	p, err := loadMeta(dst)
	if err != nil {
		return nil, err
	}
	// http/s3 refs have no upstream pin protocol; Task 5's dirhash covers them.
	return p, nil
case strings.Contains(ref, "://"):
	return nil, diag.New("CUBE-4001", fmt.Sprintf("unsupported pack ref scheme in %q", ref),
		"use a local directory path, oci://host/repo:tag, github.com/org/repo//path@rev, or an explicit go-getter URL (git::…, s3::…, https://…)")
```

Add `Pinned string` to `Pack` in `internal/pack/pack.go` (document: `git+<sha>` for git sources; Task 5 fills `oci:<digest>` and `dir:<dirhash>`; http/s3 getter refs fall back to `dir:<dirhash>`).

- [x] **Step 5: Run tests**

Run: `go test ./internal/pack/ -short -v`
Expected: PASS (all new + all Phase 1 pack tests; `TestFetchGit*` skip if the git CLI is absent)

- [x] **Step 6: Note for Task 12 (doctor)**

Task 12's doctor gains one warning-level host check alongside `CheckRuntime`: if any `spec.packs` ref `isGitRef`/`git::`-schemed and the `git` CLI is not on PATH, emit CUBE-0101-adjacent warning text via the existing `CheckRuntime` pattern ("git sources configured but git CLI not found"). Implement it in Task 12, not here — this step exists so neither task forgets the seam.

- [x] **Step 7: Commit**

```bash
git add -A && git commit -m "feat: go-getter pack sources (RafPe fork) with extraction guards and ls-remote pinning"
```

---

### Task 5: cube.lock — digests, image lists, reproducible record

**Reconcile checkpoint:** 0.8 (`pullOCI` internals — where the pulled digest is knowable), 0.10 (`oci.PushRendered`), 0.13 (`up.Run` pack loop + `step` helper), Task 4 merged (`Pack.Pinned`).

**Files:**
- Create: `internal/lock/lock.go`, `internal/lock/images.go`
- Modify: `internal/pack/source.go` (fill `Pinned` for oci + local dirs), `internal/up/up.go` (write cube.lock)
- Test: `internal/lock/lock_test.go`

**Interfaces:**
- Consumes: `pack.Pack.Pinned`, `unstructured.Unstructured`, `diag`.
- Produces (Tasks 6/7 and Phase 3 `vendor` depend on these):

```go
package lock
type File struct {
	APIVersion string      `yaml:"apiVersion"` // "cube-idp.dev/v1alpha1"
	Kind       string      `yaml:"kind"`       // "CubeLock"
	Engine     EngineLock  `yaml:"engine"`
	Packs      []Entry     `yaml:"packs"`
}
type EngineLock struct{ Type string `yaml:"type"` }
type Entry struct {
	Ref          string   `yaml:"ref"`          // as written in cube.yaml
	Name         string   `yaml:"name"`
	Version      string   `yaml:"version"`
	Resolved     string   `yaml:"resolved"`     // pack.Pack.Pinned
	RenderedHash string   `yaml:"renderedHash"` // sha256 over canonical rendered YAML
	Images       []string `yaml:"images"`       // sorted, unique
}
func PathFor(cfgPath string) string                     // cube.lock next to cube.yaml
func Write(path string, f *File) error                  // deterministic output
func Read(path string) (*File, error)                   // missing -> (nil, nil); corrupt -> CUBE-0003
func RenderedHash(objs []*unstructured.Unstructured) (string, error)
func ImagesFrom(objs []*unstructured.Unstructured) []string
```

- [x] **Step 1: Write the failing tests**

`internal/lock/lock_test.go`:

```go
package lock

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/rafpe/cube-idp/internal/diag"
)

func deployment(image, initImage string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1", "kind": "Deployment",
		"metadata": map[string]any{"name": "d", "namespace": "n"},
		"spec": map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"initContainers": []any{map[string]any{"name": "i", "image": initImage}},
					"containers":     []any{map[string]any{"name": "c", "image": image}},
				},
			},
		},
	}}
}

func TestImagesFromWalksPodSpecs(t *testing.T) {
	objs := []*unstructured.Unstructured{
		deployment("nginx:1.27", "busybox:1.36"),
		deployment("nginx:1.27", "alpine:3.20"), // duplicate nginx must dedupe
	}
	got := ImagesFrom(objs)
	want := []string{"alpine:3.20", "busybox:1.36", "nginx:1.27"} // sorted, unique
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestRenderedHashDeterministic(t *testing.T) {
	objs := []*unstructured.Unstructured{deployment("nginx:1.27", "busybox:1.36")}
	h1, err := RenderedHash(objs)
	if err != nil {
		t.Fatal(err)
	}
	h2, _ := RenderedHash(objs)
	if h1 != h2 || len(h1) < len("sha256:")+64 {
		t.Fatalf("hash unstable or malformed: %q vs %q", h1, h2)
	}
	h3, _ := RenderedHash([]*unstructured.Unstructured{deployment("nginx:1.28", "busybox:1.36")})
	if h1 == h3 {
		t.Fatal("different content must hash differently")
	}
}

func TestWriteReadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cube.lock")
	f := &File{APIVersion: "cube-idp.dev/v1alpha1", Kind: "CubeLock",
		Engine: EngineLock{Type: "flux"},
		Packs: []Entry{{Ref: "./packs/gitea", Name: "gitea", Version: "0.1.0",
			Resolved: "dir:h1:abc", RenderedHash: "sha256:def", Images: []string{"gitea:1.22"}}}}
	if err := Write(path, f); err != nil {
		t.Fatal(err)
	}
	got, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, f) {
		t.Fatalf("round trip: %+v", got)
	}
}

func TestReadMissingIsNil(t *testing.T) {
	got, err := Read(filepath.Join(t.TempDir(), "cube.lock"))
	if err != nil || got != nil {
		t.Fatalf("missing lock must be (nil, nil), got %v %v", got, err)
	}
}

func TestReadCorrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cube.lock")
	os.WriteFile(path, []byte("{{{not yaml"), 0o644)
	_, err := Read(path)
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-0003" {
		t.Fatalf("want CUBE-0003, got %v", err)
	}
}
```

- [x] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/lock/ -v`
Expected: FAIL (package does not exist)

- [x] **Step 3: Implement**

`internal/lock/lock.go`:

```go
// Package lock reads and writes cube.lock: the reproducibility record of an
// `up` — resolved pack pins, rendered-content hashes, and the full image
// list (spec §4.1 pack engine; feeds Phase 3 `vendor` and `upgrade --plan`).
package lock

import (
	"fmt"
	"os"
	"path/filepath"

	"sigs.k8s.io/yaml"

	"github.com/rafpe/cube-idp/internal/diag"
)

type File struct {
	APIVersion string     `yaml:"apiVersion" json:"apiVersion"`
	Kind       string     `yaml:"kind" json:"kind"`
	Engine     EngineLock `yaml:"engine" json:"engine"`
	Packs      []Entry    `yaml:"packs" json:"packs"`
}

type EngineLock struct {
	Type string `yaml:"type" json:"type"`
}

type Entry struct {
	Ref          string   `yaml:"ref" json:"ref"`
	Name         string   `yaml:"name" json:"name"`
	Version      string   `yaml:"version" json:"version"`
	Resolved     string   `yaml:"resolved" json:"resolved"`
	RenderedHash string   `yaml:"renderedHash" json:"renderedHash"`
	Images       []string `yaml:"images" json:"images"`
}

func PathFor(cfgPath string) string {
	return filepath.Join(filepath.Dir(cfgPath), "cube.lock")
}

func Write(path string, f *File) error {
	out, err := yaml.Marshal(f) // sigs yaml marshals via JSON: keys sorted, deterministic
	if err != nil {
		return diag.Wrap(err, "CUBE-0003", "cannot serialize cube.lock", "this is a cube-idp bug — please report it")
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return diag.Wrap(err, "CUBE-0003", fmt.Sprintf("cannot write %s", path), "check directory permissions")
	}
	return nil
}

func Read(path string) (*File, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, diag.Wrap(err, "CUBE-0003", fmt.Sprintf("cannot read %s", path), "check file permissions")
	}
	var f File
	if err := yaml.Unmarshal(raw, &f); err != nil {
		return nil, diag.Wrap(err, "CUBE-0003", fmt.Sprintf("%s is corrupt", path),
			"delete it and re-run `cube-idp up` to regenerate")
	}
	return &f, nil
}
```

`internal/lock/images.go`:

```go
package lock

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

// ImagesFrom extracts every container image referenced by the objects,
// walking any containers/initContainers/ephemeralContainers list at any
// depth (covers Deployments, StatefulSets, DaemonSets, Jobs, CronJobs, Pods).
func ImagesFrom(objs []*unstructured.Unstructured) []string {
	set := map[string]struct{}{}
	for _, o := range objs {
		walkImages(o.Object, set)
	}
	out := make([]string, 0, len(set))
	for img := range set {
		out = append(out, img)
	}
	sort.Strings(out)
	return out
}

func walkImages(v any, set map[string]struct{}) {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			if k == "containers" || k == "initContainers" || k == "ephemeralContainers" {
				if list, ok := val.([]any); ok {
					for _, c := range list {
						if cm, ok := c.(map[string]any); ok {
							if img, ok := cm["image"].(string); ok && img != "" {
								set[img] = struct{}{}
							}
						}
					}
				}
			}
			walkImages(val, set)
		}
	case []any:
		for _, e := range t {
			walkImages(e, set)
		}
	}
}

// RenderedHash is a stable content hash of the rendered objects
// (sigs.k8s.io/yaml marshals via JSON with sorted keys).
func RenderedHash(objs []*unstructured.Unstructured) (string, error) {
	h := sha256.New()
	for _, o := range objs {
		b, err := yaml.Marshal(o.Object)
		if err != nil {
			return "", err
		}
		h.Write(b)
		h.Write([]byte("---\n"))
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}
```

- [x] **Step 4: Fill `Pinned` for oci and local-dir sources**

```bash
go get golang.org/x/mod@latest
```

In `internal/pack/source.go`:
- Local-dir case: after `loadMeta(abs)` succeeds, set `p.Pinned = "dir:" + h` where `h, err := dirhash.HashDir(abs, "", dirhash.Hash1)` (import `golang.org/x/mod/sumdb/dirhash`); on hash error return `diag.Wrap(err, "CUBE-4001", "cannot hash pack directory", "check file permissions under the pack directory")`. Extract this as `func dirPin(abs string) (string, error)` — the getter case below and Task 7's `ResolveRemote` reuse it.
- OCI case: set `p.Pinned = "oci:" + digest`. `RECONCILE:` `pullOCI` is pure oras-go v2 after Task 3.5; the pulled manifest digest is the `ocispec.Descriptor` returned by `oras.Copy` — thread it out of `pullOCI` (change its return to `(dir string, digest string, err error)`) and verify no other caller breaks.
- Getter case (`isGetterRef` http/s3 refs, Task 4): no upstream pin protocol exists — set `p.Pinned` via `dirPin(dst)` over the guarded fetched tree, same semantics as local dirs.
- Git case: nothing to do — Task 4's `fetchGit` already sets `Pinned = "git+<sha>"` via `resolveGitPin`.

Add a quick test to `internal/pack/pack_test.go`:

```go
func TestFetchLocalDirSetsPinned(t *testing.T) {
	p, err := Fetch(context.Background(), "testdata/demo", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Pinned) < 5 || p.Pinned[:4] != "dir:" {
		t.Fatalf("local packs must be pinned by dirhash, got %q", p.Pinned)
	}
}
```

- [x] **Step 5: Write cube.lock from `up`**

In `internal/up/up.go` (RECONCILE: splice into the actual phase-1 pack loop from checkpoint 0.13): declare `var entries []lock.Entry` before the loop; inside the loop after `oci.PushRendered` succeeds:

```go
rh, err := lock.RenderedHash(rendered.Objects)
if err != nil {
	return err
}
entries = append(entries, lock.Entry{
	Ref:          pr.Ref,
	Name:         rendered.Name,
	Version:      rendered.Version,
	Resolved:     p.Pinned,
	RenderedHash: rh,
	Images:       lock.ImagesFrom(rendered.Objects),
})
```

After the loop (before `waitHealthy`):

```go
lf := &lock.File{APIVersion: "cube-idp.dev/v1alpha1", Kind: "CubeLock",
	Engine: lock.EngineLock{Type: cube.Spec.Engine.Type}, Packs: entries}
if err := lock.Write(lock.PathFor(cfgPath), lf); err != nil {
	return err
}
step(out, "lock", "cube.lock written (%d packs)", len(entries))
```

- [x] **Step 6: Run tests**

Run: `go test ./internal/lock/ ./internal/pack/ -short -v && go build ./...`
Expected: PASS

- [x] **Step 7: Commit**

```bash
git add -A && git commit -m "feat: cube.lock with resolved pins, rendered hashes, and full image lists"
```

---

### Task 6: `cube-idp diff`

**Reconcile checkpoint:** 0.6 (**blocking**: the pinned `fluxcd/pkg/ssa` `ResourceManager` diff entry point and its ChangeSet action constants; how Apply labels objects), 0.9 (engine `Deliver` purity), 0.13 (`up.Run` structure to mirror), Task 5 merged (lock for pack drift).

**Files:**
- Create: `internal/apply/diff.go`, `internal/diff/diff.go`, `cmd/diff.go`
- Modify: `internal/apply/applier.go` (factor the labeling loop into `func (a *Applier) label(objs []*unstructured.Unstructured)`; `Apply` calls it — behavior unchanged)
- Test: `internal/apply/diff_test.go` (envtest)

**Interfaces:**
- Consumes: `Applier` internals (`rm`, `c`, `cube`), engine factory, `pack.Fetch/Render`, `lock.Read/RenderedHash`, `registry.Manifests`, engine `InstallManifests`.
- Produces:

```go
package apply
type Change struct {
	Ref    string // "<group>/<Kind>/<ns>/<name>"
	Action string // "created" | "configured" | "unchanged" (exact strings from ssa ChangeSet actions)
}
func (a *Applier) Diff(ctx context.Context, objs []*unstructured.Unstructured) ([]Change, error) // CUBE-2005

package diff
func Run(ctx context.Context, cfgPath string, out io.Writer) (changed bool, err error)
```

Exit contract (scripts rely on it, kubectl-diff convention): exit 0 = no changes, exit 1 = changes found; any error renders a CUBE diagnosis and exits 1 via the normal main.go path.

- [x] **Step 1: Write the failing envtest test**

`internal/apply/diff_test.go` (same envtest gate as the Phase 1 apply tests — reuses `testREST` from `testenv_test.go`):

```go
package apply

import (
	"context"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestDiffReportsCreatedConfiguredUnchanged(t *testing.T) {
	a, err := New(testREST, "diffcube")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	base := []*unstructured.Unstructured{ns("dt1"), cm("same", "dt1", nil), cm("drift", "dt1", nil)}
	if err := a.Apply(ctx, base, true, 30*time.Second); err != nil {
		t.Fatal(err)
	}

	changedCM := cm("drift", "dt1", nil)
	changedCM.Object["data"] = map[string]any{"k": "NEW"}
	desired := []*unstructured.Unstructured{
		ns("dt1"), cm("same", "dt1", nil), changedCM, cm("brandnew", "dt1", nil),
	}
	changes, err := a.Diff(ctx, desired)
	if err != nil {
		t.Fatal(err)
	}
	byRef := map[string]string{}
	for _, c := range changes {
		byRef[c.Ref] = c.Action
	}
	if byRef["/ConfigMap/dt1/brandnew"] != "created" {
		t.Fatalf("brandnew: %v", byRef)
	}
	if byRef["/ConfigMap/dt1/drift"] != "configured" {
		t.Fatalf("drift: %v", byRef)
	}
	if byRef["/ConfigMap/dt1/same"] != "unchanged" {
		t.Fatalf("same: %v", byRef)
	}
}
```

(RECONCILE: `cm`/`ns` helpers exist in the phase-1 `applier_test.go` — reuse them; the exact `Ref` format and action strings must be aligned with whatever the pinned ssa library's ChangeSetEntry exposes — fix the test and `Change` doc together, keeping the three-way created/configured/unchanged distinction.)

- [x] **Step 2: Run test to verify it fails**

Run: `make test-apply`
Expected: FAIL (Diff undefined)

- [x] **Step 3: Implement `Applier.Diff`**

`internal/apply/diff.go`:

```go
package apply

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/rafpe/cube-idp/internal/diag"
)

type Change struct {
	Ref    string
	Action string
}

// Diff server-side-dry-runs every object and reports what a real Apply
// would do. Objects are labeled exactly as Apply labels them, so the cube
// label never shows up as perpetual drift.
func (a *Applier) Diff(ctx context.Context, objs []*unstructured.Unstructured) ([]Change, error) {
	a.label(objs)
	out := make([]Change, 0, len(objs))
	for _, o := range objs {
		// Task 0 confirmed (finding 0.6): ssa v0.77.0's ResourceManager exposes
		//   Diff(ctx, object, opts) (*ChangeSetEntry, *unstructured.Unstructured, *unstructured.Unstructured, error)
		// entry.Action stringifies to created/configured/unchanged/skipped.
		entry, _, _, err := a.rm.Diff(ctx, o, ssa.DefaultDiffOptions())
		if err != nil {
			return nil, diag.Wrap(err, "CUBE-2005",
				"server-side diff failed for "+o.GetKind()+"/"+o.GetName(),
				"check cluster connectivity and RBAC (dry-run apply permission required)")
		}
		out = append(out, Change{Ref: entry.Subject, Action: string(entry.Action)})
	}
	return out, nil
}
```

(Also in this step: factor the labeling loop out of `Apply` into `func (a *Applier) label(objs []*unstructured.Unstructured)` and call it from both `Apply` and `Diff`. Run `make test-apply` after — the Phase 1 apply tests must stay green.)

- [x] **Step 4: Implement the orchestrator**

`internal/diff/diff.go`:

```go
// Package diff computes what a re-run of `up` would change, without mutating
// anything: kernel objects via SSA dry-run, pack content via cube.lock
// rendered hashes, orphans via the inventory.
package diff

import (
	"context"
	"fmt"
	"io"

	"github.com/rafpe/cube-idp/internal/apply"
	"github.com/rafpe/cube-idp/internal/cluster"
	"github.com/rafpe/cube-idp/internal/config"
	enginefactory "github.com/rafpe/cube-idp/internal/engine/factory"
	"github.com/rafpe/cube-idp/internal/lock"
	"github.com/rafpe/cube-idp/internal/pack"
	"github.com/rafpe/cube-idp/internal/registry"
)

func Run(ctx context.Context, cfgPath string, out io.Writer) (bool, error) {
	cube, err := config.Load(cfgPath)
	if err != nil {
		return false, err
	}
	prov, err := cluster.New(cube.Spec.Cluster, cube.Spec.Gateway)
	if err != nil {
		return false, err
	}
	exists, err := prov.Exists(ctx, cube.Metadata.Name)
	if err != nil {
		return false, err
	}
	if !exists {
		fmt.Fprintf(out, "cluster %q does not exist — `cube-idp up` would create everything\n", cube.Metadata.Name)
		return true, nil
	}
	conn, err := prov.Ensure(ctx, cube.Metadata.Name, cube.Spec.Cluster)
	if err != nil {
		return false, err
	}
	a, err := apply.New(conn.REST, cube.Metadata.Name)
	if err != nil {
		return false, err
	}
	eng, err := enginefactory.New(cube.Spec.Engine.Type)
	if err != nil {
		return false, err
	}

	// Desired kernel set: registry + engine install + per-pack delivery objects.
	// RECONCILE: mirror the exact assembly in the phase-1 up.Run (checkpoint
	// 0.13), including the gateway pack prepend and the engine InstallManifests
	// accessor per engine type; keep the two code paths structurally parallel.
	desired, packEntries, err := desiredState(ctx, cube, eng)
	if err != nil {
		return false, err
	}

	changed := false
	changes, err := a.Diff(ctx, desired)
	if err != nil {
		return false, err
	}
	fmt.Fprintln(out, "KERNEL OBJECTS")
	for _, c := range changes {
		if c.Action != "unchanged" {
			changed = true
		}
		fmt.Fprintf(out, "  %-11s %s\n", c.Action, c.Ref)
	}

	// Pack content drift: compare fresh rendered hashes against cube.lock.
	prev, err := lock.Read(lock.PathFor(cfgPath))
	if err != nil {
		return false, err
	}
	fmt.Fprintln(out, "PACK CONTENT")
	for _, e := range packEntries {
		old := lockEntryFor(prev, e.Name)
		switch {
		case old == nil:
			changed = true
			fmt.Fprintf(out, "  new         %s (no cube.lock entry — first delivery)\n", e.Name)
		case old.RenderedHash != e.RenderedHash:
			changed = true
			fmt.Fprintf(out, "  changed     %s (%s -> %s)\n", e.Name, short(old.RenderedHash), short(e.RenderedHash))
		default:
			fmt.Fprintf(out, "  unchanged   %s\n", e.Name)
		}
	}

	// Orphans: inventory entries no longer in the desired set.
	inv, err := a.LoadInventory(ctx)
	if err != nil {
		return false, err
	}
	orphans := orphanRefs(inv, desired)
	if len(orphans) > 0 {
		changed = true
		fmt.Fprintln(out, "ORPHANS (in inventory, no longer desired)")
		for _, ref := range orphans {
			fmt.Fprintf(out, "  orphaned    %s\n", ref)
		}
	}
	return changed, nil
}
```

Helpers in the same file (all real code, no cluster mutation):
- `desiredState(ctx, cube, eng)` — `registry.Manifests()` + `eng.InstallManifests()` (interface method, Task 0 finding 0.9 — no per-engine switch needed) + for each pack ref (gateway pack prepended exactly as `up` does — Task 0 finding 0.13: `up.gatewayPackRef(gw)` returns `gw.Ref` when set, else `"packs/" + gw.Pack`; move that helper to `config` as `func (g GatewaySpec) PackRef() string` and update `up` to use it, so `diff`/`upgrade` share one implementation): `pack.Fetch` → `Render(pr.Values)` → `eng.Deliver(ctx, rendered, engine.ArtifactRef{Repo: "packs/" + rendered.Name, Tag: rendered.Version})` (Deliver is pure — no push happens) → collect delivery objects into the desired set and a `lock.Entry{Name, RenderedHash}` into `packEntries`.
- `lockEntryFor(f *lock.File, name string) *lock.Entry` — nil-safe lookup.
- `short(h string) string` — first 12 hex chars after "sha256:".
- `orphanRefs(inv []object.ObjMetadata, desired []*unstructured.Unstructured) []string` — set-subtract by group/kind/ns/name strings.

`cmd/diff.go`:

```go
package cmd

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/rafpe/cube-idp/internal/diff"
)

func newDiffCmd() *cobra.Command {
	var file string
	c := &cobra.Command{
		Use:   "diff",
		Short: "Show what a re-run of `up` would change; exit 1 if anything would",
		RunE: func(c *cobra.Command, _ []string) error {
			changed, err := diff.Run(c.Context(), file, c.OutOrStdout())
			if err != nil {
				return err
			}
			if changed {
				os.Exit(1) // kubectl-diff convention; output is already flushed
			}
			return nil
		},
	}
	c.Flags().StringVarP(&file, "file", "f", "cube.yaml", "path to cube.yaml")
	return c
}
```

Register `newDiffCmd()` in `cmd/root.go`.

- [x] **Step 5: Run tests**

Run: `make test-apply && go build ./... && go test ./... -short`
Expected: PASS

- [x] **Step 6: Commit**

```bash
git add -A && git commit -m "feat: cube-idp diff — SSA dry-run + lock-hash pack drift + inventory orphans"
```

---

### Task 7: `cube-idp upgrade --plan`

**Reconcile checkpoint:** 0.8 (oras-go usage in `pullOCI` — `resolve.go` mirrors it), Task 3.5 merged (one OCI client setup to reuse), Task 4 merged (`gitCloneURL`, `isGitRef`, `isGetterRef`, `resolveGitPin`, `fetchGetter` — all in place), Task 5 merged (lock, `dirPin`), Task 6 merged (diff orchestrator reuse).

**Files:**
- Create: `internal/upgrade/plan.go`, `cmd/upgrade.go`
- Modify: `internal/pack/resolve.go` (extend with `ResolveRemote` — the file and its git pin probe were created in Task 4)
- Test: `internal/upgrade/plan_test.go`, `internal/pack/resolve_test.go`

**Interfaces:**
- Consumes: `lock.Read`, `pack` sources, `pack.resolveGitPin` (Task 4), `diff.Run`.
- Produces:

```go
package pack
func ResolveRemote(ctx context.Context, ref, cacheDir string) (string, error)
// Returns the CURRENT upstream pin for a ref without fetching content
// (except http/s3 getter refs, which have no pin protocol and are probed
// by fetch+dirhash):
//   oci://…:tag  -> "oci:<digest>"   (registry HEAD; CUBE-5004 on failure)
//   git ref      -> "git+<sha>"      (ls-remote via resolveGitPin; CUBE-4006 on failure)
//   http/s3 ref  -> "dir:<dirhash>"  (fetch to probe dir + dirPin)
//   local dir    -> "dir:<dirhash>"  (re-hash)

package upgrade
func Plan(ctx context.Context, cfgPath string, out io.Writer) (changed bool, err error)
```

Semantics (spec: *re-running `up` IS the upgrade command*): `upgrade` never mutates. `upgrade --plan` = pack-pin resolution against cube.lock + the Task 6 kernel diff. Running `upgrade` without `--plan` errors with the pointer to `up`.

- [x] **Step 1: Write the failing tests**

`internal/pack/resolve_test.go`:

```go
package pack

import (
	"context"
	"testing"
)

func TestResolveRemoteLocalDirMatchesFetch(t *testing.T) {
	p, err := Fetch(context.Background(), "testdata/demo", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pin, err := ResolveRemote(context.Background(), "testdata/demo", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if pin != p.Pinned {
		t.Fatalf("ResolveRemote %q != Fetch pin %q", pin, p.Pinned)
	}
}

func TestResolveRemoteGitTag(t *testing.T) {
	fixture := makeGitFixture(t) // from getter_test.go (Task 4)
	restore := gitCloneURL
	gitCloneURL = func(string) string { return fixture }
	defer func() { gitCloneURL = restore }()

	pin, err := ResolveRemote(context.Background(), "example.com/org/repo//packs/demo@v0.1.0", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(pin) < 4 || pin[:4] != "git+" {
		t.Fatalf("want git+<sha>, got %q", pin)
	}
}
```

`internal/upgrade/plan_test.go` (pure table logic — the network paths are covered by resolve tests and e2e):

```go
package upgrade

import (
	"strings"
	"testing"

	"github.com/rafpe/cube-idp/internal/lock"
)

func TestPlanRowClassification(t *testing.T) {
	locked := &lock.Entry{Ref: "./p", Name: "p", Resolved: "dir:h1:old"}
	if row := classify(locked, "dir:h1:old"); row.Change != "up to date" {
		t.Fatalf("same pin: %+v", row)
	}
	if row := classify(locked, "dir:h1:new"); row.Change != "update available" {
		t.Fatalf("moved pin: %+v", row)
	}
	if row := classify(nil, "dir:h1:new"); row.Change != "new (not in cube.lock)" {
		t.Fatalf("missing lock entry: %+v", row)
	}
}

func TestRenderTableAligns(t *testing.T) {
	out := renderTable([]Row{{Name: "gitea", Current: "oci:sha256:aaaa", Latest: "oci:sha256:bbbb", Change: "update available"}})
	if !strings.Contains(out, "gitea") || !strings.Contains(out, "update available") {
		t.Fatalf("table: %s", out)
	}
}
```

- [x] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/upgrade/ ./internal/pack/ -short -run 'TestResolveRemote|TestPlan|TestRenderTable' -v`
Expected: FAIL

- [x] **Step 3: Implement `ResolveRemote`**

Append to `internal/pack/resolve.go` (created in Task 4 — `resolveGitPin` and `gitCloneURL` already live there; add imports `path/filepath`, `strings`, `oras.land/oras-go/v2/registry/remote`):

```go
// ResolveRemote returns the current upstream pin for ref without pulling
// content (http/s3 getter refs excepted — no pin protocol, so they are
// probed by fetch+dirhash). It is upgrade --plan's probe: cube.lock's
// Resolved field records what we HAVE; this returns what we WOULD get.
func ResolveRemote(ctx context.Context, ref, cacheDir string) (string, error) {
	switch {
	case strings.HasPrefix(ref, "oci://"):
		name := strings.TrimPrefix(ref, "oci://")
		repoRef, tag, ok := strings.Cut(name, ":")
		if !ok {
			tag = "latest"
			repoRef = name
		}
		repo, err := remote.NewRepository(repoRef)
		if err != nil {
			return "", diag.Wrap(err, "CUBE-5004", fmt.Sprintf("bad OCI ref %q", ref), "use oci://host/repo:tag")
		}
		// RECONCILE: reuse the exact plain-HTTP/insecure client setup the
		// Task 3.5 oras consolidation landed on, so both paths trust the
		// same hosts.
		desc, err := repo.Resolve(ctx, tag)
		if err != nil {
			return "", diag.Wrap(err, "CUBE-5004",
				fmt.Sprintf("cannot resolve %s from the registry", ref),
				"check network access to the registry and that the tag exists")
		}
		return "oci:" + desc.Digest.String(), nil

	case isGitRef(ref):
		base, rev, ok := strings.Cut(ref, "@")
		if !ok || rev == "" {
			return "", diag.New("CUBE-4007", fmt.Sprintf("git pack ref %q is not pinned", ref),
				"append @<tag|branch|commit>")
		}
		repoPath, _, _ := strings.Cut(base, "//")
		return resolveGitPin(ctx, gitCloneURL(repoPath), rev) // Task 4's probe — one ls-remote implementation

	case isGetterRef(ref):
		// http/s3 sources have no cheap upstream pin: fetch to a probe dir
		// and dirhash it — identical semantics to what Fetch records.
		dst := filepath.Join(cacheDir, "probe", sanitizeRef(ref))
		if err := fetchGetter(ctx, ref, dst); err != nil {
			return "", err
		}
		return dirPin(dst)

	default:
		abs, err := filepath.Abs(ref)
		if err != nil {
			return "", diag.Wrap(err, "CUBE-4001", "bad pack path", "use a valid directory path")
		}
		return dirPin(abs) // Task 5's shared dirhash helper
	}
}
```

- [x] **Step 4: Implement the plan orchestrator + command**

`internal/upgrade/plan.go`:

```go
// Package upgrade implements `cube-idp upgrade --plan`: a non-mutating
// preview of what re-running `up` would change — pack pins vs cube.lock,
// plus the kernel object diff.
package upgrade

import (
	"context"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/rafpe/cube-idp/internal/config"
	"github.com/rafpe/cube-idp/internal/diag"
	"github.com/rafpe/cube-idp/internal/diff"
	"github.com/rafpe/cube-idp/internal/lock"
	"github.com/rafpe/cube-idp/internal/pack"
)

type Row struct {
	Name, Current, Latest, Change string
}

func Plan(ctx context.Context, cfgPath string, out io.Writer) (bool, error) {
	cube, err := config.Load(cfgPath)
	if err != nil {
		return false, err
	}
	lf, err := lock.Read(lock.PathFor(cfgPath))
	if err != nil {
		return false, err
	}
	if lf == nil {
		return false, diag.New("CUBE-0003", "no cube.lock found next to "+cfgPath,
			"run `cube-idp up` once to create it; upgrade --plan compares against it")
	}

	// Same ref list `up` uses: gateway pack first, then spec.packs.
	// Task 0 finding 0.13: honor gateway.ref (init --local writes it) via the
	// shared helper Task 6 moved to config.
	refs := append([]config.PackRef{{Ref: cube.Spec.Gateway.PackRef()}}, cube.Spec.Packs...)
	changed := false
	var rows []Row
	for _, pr := range refs {
		latest, err := pack.ResolveRemote(ctx, pr.Ref, cacheDirFor())
		if err != nil {
			return false, err
		}
		row := classify(lockEntryByRef(lf, pr.Ref), latest)
		row.Name = pr.Ref
		if row.Change != "up to date" {
			changed = true
		}
		rows = append(rows, row)
	}
	fmt.Fprint(out, renderTable(rows))

	fmt.Fprintln(out, "\nKernel + delivery object changes:")
	kernelChanged, err := diff.Run(ctx, cfgPath, out)
	if err != nil {
		return false, err
	}
	return changed || kernelChanged, nil
}

func classify(locked *lock.Entry, latest string) Row {
	switch {
	case locked == nil:
		return Row{Latest: latest, Change: "new (not in cube.lock)"}
	case locked.Resolved == latest:
		return Row{Current: locked.Resolved, Latest: latest, Change: "up to date"}
	default:
		return Row{Current: locked.Resolved, Latest: latest, Change: "update available"}
	}
}

func lockEntryByRef(f *lock.File, ref string) *lock.Entry {
	for i := range f.Packs {
		if f.Packs[i].Ref == ref {
			return &f.Packs[i]
		}
	}
	return nil
}

func renderTable(rows []Row) string {
	var b strings.Builder
	w := tabwriter.NewWriter(&b, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "PACK\tCURRENT\tLATEST\tCHANGE")
	for _, r := range rows {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", r.Name, shorten(r.Current), shorten(r.Latest), r.Change)
	}
	w.Flush()
	return b.String()
}

func shorten(pin string) string {
	if len(pin) > 24 {
		return pin[:24] + "…"
	}
	return pin
}
```

(`cacheDirFor()`: RECONCILE — reuse the phase-1 `up.cacheDir()` helper; if it is unexported in `internal/up`, move it to `internal/pack` as `DefaultCacheDir()` and update both call sites.)

`cmd/upgrade.go`:

```go
package cmd

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/rafpe/cube-idp/internal/upgrade"
)

func newUpgradeCmd() *cobra.Command {
	var file string
	var plan bool
	c := &cobra.Command{
		Use:   "upgrade --plan",
		Short: "Preview available pack updates and pending changes (apply them with `cube-idp up`)",
		RunE: func(c *cobra.Command, _ []string) error {
			if !plan {
				return fmt.Errorf("cube-idp has no separate apply step: re-running `cube-idp up` IS the upgrade.\nUse `cube-idp upgrade --plan` to preview what it would change")
			}
			changed, err := upgrade.Plan(c.Context(), file, c.OutOrStdout())
			if err != nil {
				return err
			}
			if changed {
				os.Exit(1)
			}
			return nil
		},
	}
	c.Flags().StringVarP(&file, "file", "f", "cube.yaml", "path to cube.yaml")
	c.Flags().BoolVar(&plan, "plan", false, "show the plan (required)")
	return c
}
```

(import `fmt`; register `newUpgradeCmd()` in `cmd/root.go`.)

- [x] **Step 5: Run tests**

Run: `go test ./internal/upgrade/ ./internal/pack/ -short -v && go build ./...`
Expected: PASS

- [x] **Step 6: Commit**

```bash
git add -A && git commit -m "feat: upgrade --plan — pack pin resolution against cube.lock plus kernel diff"
```

---

### Task 8: internal/trust — local CA, leaf certs, trust state (D6 groundwork + D12)

**Reconcile checkpoint:** 0.2 (diag), 0.15 (6xxx range free). This task is self-contained (crypto/x509 + filesystem only) — lowest reconcile risk in the phase.

**D12 amendment (2026-07-13):** this package is the "TLS from first boot" foundation — `up.Run` calls `EnsureCA` as its FIRST step, before cluster creation (wired in Task 9 Step 5). Two D12 behaviors land here: (a) `EnsureCA` stays strictly idempotent (already specified), and (b) **mkcert CA reuse** (Steps 3b/3c): if the user already has a mkcert root CA, cube-idp adopts it instead of generating its own, so browsers that trust mkcert show green locks with zero prompts.

**Files:**
- Create: `internal/trust/ca.go`, `internal/trust/state.go`
- Test: `internal/trust/trust_test.go`

**Interfaces:**
- Consumes: `diag`.
- Produces (Tasks 9/10/11 depend on these):

```go
package trust
func Dir() (string, error)                        // <os.UserConfigDir>/cube-idp, created 0700
type CA struct{ CertPath, KeyPath string; Cert *x509.Certificate; Key crypto.Signer }
func EnsureCA(dir string) (*CA, error)            // idempotent create-or-load; CUBE-6001
func (ca *CA) IssueServerCert(hosts []string, validity time.Duration) (certPEM, keyPEM []byte, err error) // CUBE-6005
func (ca *CA) LeafStillValid(certPEM []byte, hosts []string, margin time.Duration) bool
type State struct{ Installed bool `yaml:"installed"`; CACert string `yaml:"caCert"` }
func LoadState(dir string) (*State, error)        // missing -> zero value; corrupt -> CUBE-6006
func SaveState(dir string, s *State) error
```

- [x] **Step 1: Write the failing tests**

`internal/trust/trust_test.go`:

```go
package trust

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rafpe/cube-idp/internal/diag"
)

func TestEnsureCAIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	ca1, err := EnsureCA(dir)
	if err != nil {
		t.Fatal(err)
	}
	ca2, err := EnsureCA(dir)
	if err != nil {
		t.Fatal(err)
	}
	if ca1.Cert.SerialNumber.Cmp(ca2.Cert.SerialNumber) != 0 {
		t.Fatal("EnsureCA regenerated an existing CA — OS trust would silently break")
	}
	info, err := os.Stat(ca1.KeyPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("CA key must be 0600, got %v", info.Mode().Perm())
	}
	if !ca1.Cert.IsCA {
		t.Fatal("CA cert missing IsCA")
	}
}

func TestIssueServerCertVerifiesAgainstCA(t *testing.T) {
	ca, err := EnsureCA(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hosts := []string{"cube-idp.localtest.me", "*.cube-idp.localtest.me"}
	certPEM, keyPEM, err := ca.IssueServerCert(hosts, 365*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(keyPEM) == 0 {
		t.Fatal("no key produced")
	}
	block, _ := pem.Decode(certPEM)
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca.Cert)
	if _, err := leaf.Verify(x509.VerifyOptions{DNSName: "gitea.cube-idp.localtest.me", Roots: pool,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err != nil {
		t.Fatalf("leaf does not verify for a wildcard subdomain: %v", err)
	}
	if !ca.LeafStillValid(certPEM, hosts, 30*24*time.Hour) {
		t.Fatal("fresh leaf must count as valid")
	}
	if ca.LeafStillValid(certPEM, []string{"other.example.com"}, 30*24*time.Hour) {
		t.Fatal("leaf must not count as valid for hosts it does not cover")
	}
}

func TestStateRoundTripAndCorrupt(t *testing.T) {
	dir := t.TempDir()
	s, err := LoadState(dir)
	if err != nil || s.Installed {
		t.Fatalf("missing state must load as zero value: %+v %v", s, err)
	}
	if err := SaveState(dir, &State{Installed: true, CACert: "/x/ca.crt"}); err != nil {
		t.Fatal(err)
	}
	s, err = LoadState(dir)
	if err != nil || !s.Installed || s.CACert != "/x/ca.crt" {
		t.Fatalf("round trip: %+v %v", s, err)
	}
	os.WriteFile(filepath.Join(dir, "trust-state.yaml"), []byte("{{{"), 0o644)
	_, err = LoadState(dir)
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-6006" {
		t.Fatalf("want CUBE-6006, got %v", err)
	}
}
```

- [x] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/trust/ -v`
Expected: FAIL (package does not exist)

- [x] **Step 3: Implement**

`internal/trust/ca.go`:

```go
// Package trust implements cube-idp's D6 trust posture: a local CA (the
// mkcert mechanism), leaf certs for the gateway, canonical-hostname wiring,
// and — ONLY via the explicit `cube-idp trust` command — OS trust-store
// installation, fully reverted by `cube-idp down`. Nothing in this package
// touches the OS trust store implicitly.
package trust

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"time"

	"github.com/rafpe/cube-idp/internal/diag"
)

func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", diag.Wrap(err, "CUBE-6001", "cannot locate the user config directory", "set $HOME (or %AppData% on Windows)")
	}
	dir := filepath.Join(base, "cube-idp")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", diag.Wrap(err, "CUBE-6001", "cannot create "+dir, "check permissions on your config directory")
	}
	return dir, nil
}

type CA struct {
	CertPath, KeyPath string
	Cert              *x509.Certificate
	Key               crypto.Signer
}

func EnsureCA(dir string) (*CA, error) {
	certPath := filepath.Join(dir, "ca.crt")
	keyPath := filepath.Join(dir, "ca.key")
	if _, err := os.Stat(certPath); err == nil {
		return loadCA(certPath, keyPath)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, diag.Wrap(err, "CUBE-6001", "cannot generate the CA key", "retry; check system entropy")
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, diag.Wrap(err, "CUBE-6001", "cannot generate a CA serial", "retry")
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{Organization: []string{"cube-idp local CA"}, CommonName: "cube-idp local CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		MaxPathLenZero:        true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, key.Public(), key)
	if err != nil {
		return nil, diag.Wrap(err, "CUBE-6001", "cannot self-sign the CA", "retry")
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, diag.Wrap(err, "CUBE-6001", "cannot serialize the CA key", "retry")
	}
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		return nil, diag.Wrap(err, "CUBE-6001", "cannot write "+certPath, "check permissions")
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		return nil, diag.Wrap(err, "CUBE-6001", "cannot write "+keyPath, "check permissions")
	}
	return loadCA(certPath, keyPath)
}

func loadCA(certPath, keyPath string) (*CA, error) {
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, diag.Wrap(err, "CUBE-6001", "cannot read "+certPath, "delete the cube-idp config dir to regenerate the CA (browsers will need re-trusting)")
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, diag.Wrap(err, "CUBE-6001", "cannot read "+keyPath, "delete the cube-idp config dir to regenerate the CA (browsers will need re-trusting)")
	}
	certBlock, _ := pem.Decode(certPEM)
	keyBlock, _ := pem.Decode(keyPEM)
	if certBlock == nil || keyBlock == nil {
		return nil, diag.New("CUBE-6001", "CA files are not valid PEM", "delete the cube-idp config dir to regenerate the CA")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, diag.Wrap(err, "CUBE-6001", "CA certificate is corrupt", "delete the cube-idp config dir to regenerate the CA")
	}
	key, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, diag.Wrap(err, "CUBE-6001", "CA key is corrupt", "delete the cube-idp config dir to regenerate the CA")
	}
	return &CA{CertPath: certPath, KeyPath: keyPath, Cert: cert, Key: key}, nil
}

func (ca *CA) IssueServerCert(hosts []string, validity time.Duration) ([]byte, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, diag.Wrap(err, "CUBE-6005", "cannot generate a server key", "retry")
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, diag.Wrap(err, "CUBE-6005", "cannot generate a serial", "retry")
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{Organization: []string{"cube-idp"}, CommonName: hosts[0]},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(validity),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     hosts,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.Cert, key.Public(), ca.Key)
	if err != nil {
		return nil, nil, diag.Wrap(err, "CUBE-6005", "cannot sign the server certificate", "retry; if it persists, regenerate the CA")
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, diag.Wrap(err, "CUBE-6005", "cannot serialize the server key", "retry")
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM, nil
}

// LeafStillValid reports whether certPEM was signed by this CA, covers every
// host, and has at least margin left before expiry. `up` uses it to avoid
// re-issuing (and thus avoid perpetual diffs) on every run.
func (ca *CA) LeafStillValid(certPEM []byte, hosts []string, margin time.Duration) bool {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return false
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return false
	}
	if time.Now().Add(margin).After(leaf.NotAfter) {
		return false
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca.Cert)
	for _, h := range hosts {
		probe := h
		if len(h) > 1 && h[0] == '*' { // VerifyHostname rejects literal wildcards; probe a concrete name
			probe = "probe" + h[1:]
		}
		if _, err := leaf.Verify(x509.VerifyOptions{DNSName: probe, Roots: pool,
			KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err != nil {
			return false
		}
	}
	return true
}
```

`internal/trust/state.go`:

```go
package trust

import (
	"fmt"
	"os"
	"path/filepath"

	"sigs.k8s.io/yaml"

	"github.com/rafpe/cube-idp/internal/diag"
)

// State records host-machine side effects so `down` can revert them (D6).
type State struct {
	Installed bool   `yaml:"installed" json:"installed"` // CA present in OS trust stores
	CACert    string `yaml:"caCert" json:"caCert"`
}

func statePath(dir string) string { return filepath.Join(dir, "trust-state.yaml") }

func LoadState(dir string) (*State, error) {
	raw, err := os.ReadFile(statePath(dir))
	if os.IsNotExist(err) {
		return &State{}, nil
	}
	if err != nil {
		return nil, diag.Wrap(err, "CUBE-6006", "cannot read the trust state file", "check permissions on "+dir)
	}
	var s State
	if err := yaml.Unmarshal(raw, &s); err != nil {
		return nil, diag.Wrap(err, "CUBE-6006", fmt.Sprintf("%s is corrupt", statePath(dir)),
			"if you know the CA is trusted, run `cube-idp trust --uninstall` then `cube-idp trust`; otherwise delete the file")
	}
	return &s, nil
}

func SaveState(dir string, s *State) error {
	out, err := yaml.Marshal(s)
	if err != nil {
		return diag.Wrap(err, "CUBE-6006", "cannot serialize trust state", "this is a cube-idp bug — please report it")
	}
	if err := os.WriteFile(statePath(dir), out, 0o600); err != nil {
		return diag.Wrap(err, "CUBE-6006", "cannot write the trust state file", "check permissions on "+dir)
	}
	return nil
}
```

- [x] **Step 3b: Write the failing mkcert-reuse test (D12)**

Append to `internal/trust/trust_test.go`:

```go
func TestEnsureCAAdoptsMkcertRoot(t *testing.T) {
	// Build a fake mkcert CAROOT: generate a CA the normal way, then lay its
	// files out under mkcert's names.
	seed := t.TempDir()
	mk, err := EnsureCA(seed)
	if err != nil {
		t.Fatal(err)
	}
	caroot := t.TempDir()
	for src, dst := range map[string]string{
		mk.CertPath: filepath.Join(caroot, "rootCA.pem"),
		mk.KeyPath:  filepath.Join(caroot, "rootCA-key.pem"),
	} {
		raw, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		os.WriteFile(dst, raw, 0o600)
	}
	t.Setenv("CAROOT", caroot) // mkcert's own override env var

	dir := t.TempDir()
	ca, err := EnsureCA(dir)
	if err != nil {
		t.Fatal(err)
	}
	if ca.Cert.SerialNumber.Cmp(mk.Cert.SerialNumber) != 0 {
		t.Fatal("EnsureCA must adopt the mkcert root when one exists (D12)")
	}
	// adoption is a copy: the cube-idp dir is self-contained afterwards
	if _, err := os.Stat(filepath.Join(dir, "ca.crt")); err != nil {
		t.Fatal("adopted CA must be copied into the cube-idp dir")
	}
	// and a cube-idp CA, once present, wins over CAROOT (no flip-flop)
	again, err := EnsureCA(dir)
	if err != nil || again.Cert.SerialNumber.Cmp(ca.Cert.SerialNumber) != 0 {
		t.Fatalf("EnsureCA must stay stable once adopted: %v", err)
	}
}
```

Run: `go test ./internal/trust/ -run TestEnsureCAAdoptsMkcert -v` — Expected: FAIL (fresh CA generated instead).

- [x] **Step 3c: Implement mkcert detection (D12)**

In `internal/trust/ca.go`, extend `EnsureCA`: after the "cube-idp CA already exists" fast path and **before** generating a new one, probe mkcert:

```go
// mkcertCAROOT returns mkcert's CA directory: $CAROOT if set (mkcert's own
// override), else the per-OS default mkcert uses.
func mkcertCAROOT() string {
	if v := os.Getenv("CAROOT"); v != "" {
		return v
	}
	switch runtime.GOOS {
	case "darwin":
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "Library", "Application Support", "mkcert")
	case "windows":
		return filepath.Join(os.Getenv("LocalAppData"), "mkcert")
	default:
		if v := os.Getenv("XDG_DATA_HOME"); v != "" {
			return filepath.Join(v, "mkcert")
		}
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".local", "share", "mkcert")
	}
}

// adoptMkcertCA copies an existing mkcert root (cert+key) into dir so cube-idp
// issues leaves the user's browsers already trust (D12). Presence-based: we do
// NOT verify OS-store trust (no portable query API) — if the mkcert root turns
// out untrusted, `cube-idp trust` installs it exactly like a generated CA.
func adoptMkcertCA(dir string) (adopted bool) {
	caroot := mkcertCAROOT()
	certSrc := filepath.Join(caroot, "rootCA.pem")
	keySrc := filepath.Join(caroot, "rootCA-key.pem")
	cert, err1 := os.ReadFile(certSrc)
	key, err2 := os.ReadFile(keySrc)
	if err1 != nil || err2 != nil {
		return false
	}
	if os.WriteFile(filepath.Join(dir, "ca.crt"), cert, 0o644) != nil {
		return false
	}
	if os.WriteFile(filepath.Join(dir, "ca.key"), key, 0o600) != nil {
		return false
	}
	return true
}
```

and in `EnsureCA`, between the exists-check and the generate path:

```go
if adoptMkcertCA(dir) {
	return loadCA(certPath, keyPath)
}
```

**Key-format note (required for this step):** mkcert roots are RSA, and the key file is PKCS#8. Extend `loadCA`'s key parsing to a fallback chain — `x509.ParseECPrivateKey` → `x509.ParsePKCS8PrivateKey` → `x509.ParsePKCS1PrivateKey` — asserting the result to `crypto.Signer` (both `*ecdsa.PrivateKey` and `*rsa.PrivateKey` satisfy it; `IssueServerCert` already signs via `ca.Key`, so leaf issuance works unchanged). Fail with the existing CUBE-6001 "CA key is corrupt" if no parser accepts it.

Run: `go test ./internal/trust/ -v` — Expected: PASS, including all earlier Task 8 tests (idempotency etc. — the adopt path must not regress them; note `TestEnsureCAIsIdempotent` runs with no CAROOT set, so `t.Setenv("CAROOT", t.TempDir())` there if the developer machine has a real mkcert install — add that guard to ALL Task 8 tests that expect a generated CA).

- [x] **Step 4: Run tests**

Run: `go test ./internal/trust/ -v`
Expected: PASS

- [x] **Step 5: Commit**

```bash
git add -A && git commit -m "feat: local CA with mkcert adoption (D12), server cert issuance, trust state (D6 groundwork)"
```

---

### Task 9: HTTPS gateway — websecure listener, kind port mapping, TLS secret from `up` (D12)

**Reconcile checkpoint:** 0.5 (**blocking**: `kindp.RenderConfig` signature + final `gatewayContainerPort` = 30080 decision + golden fixtures), 0.14 (**blocking**: traefik pack chart values mechanism, entrypoint/NodePort names, traefik Service name), 0.13 (`up.Run` sequence), Task 8 merged.

**Files:**
- Modify: `packs/traefik/manifests/10-gateway.yaml` (websecure listener), `packs/traefik/chart.yaml` (websecure NodePort values), `internal/cluster/kindp/merge.go` (HTTPS port mapping), `internal/cluster/kindp/testdata/*` (regenerate goldens), `internal/up/up.go` (ensure TLS secret)
- Test: `internal/cluster/kindp/merge_test.go` (extend), `internal/up/tls_test.go`

**Phase 1 → Phase 2 behavior change (document in README):** Phase 1 mapped host `spec.gateway.port` (default 8443) to Traefik's plain-HTTP NodePort 30080 while printing an `https://` URL. Phase 2 makes that URL true: host `gateway.port` now maps to the websecure NodePort **30443** (TLS terminated by Traefik with a cube-idp CA-issued cert), and plain HTTP stays available in-cluster on the `web` listener. Existing kind clusters need `down`/`up` to pick up the new mapping — acceptable pre-1.0, called out in the release notes.

- [x] **Step 1: Extend the traefik pack**

`packs/traefik/manifests/10-gateway.yaml` — add the HTTPS listener alongside the existing web listener:

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: cube-idp
  namespace: traefik
spec:
  gatewayClassName: traefik
  listeners:
    - name: web
      port: 8000
      protocol: HTTP
      allowedRoutes:
        namespaces: {from: All}
    - name: websecure
      port: 8443
      protocol: HTTPS
      allowedRoutes:
        namespaces: {from: All}
      tls:
        mode: Terminate
        certificateRefs:
          - name: cube-idp-gateway-tls
```

(RECONCILE: the listener `port` must equal the Traefik entrypoint port the chart configures for `websecure`, and the listener names must match the chart's entrypoint names — read the phase-1 `packs/traefik/chart.yaml` values (checkpoint 0.14) and align; the two facts to preserve are: HTTPS terminates at Traefik with secret `cube-idp-gateway-tls`, and the websecure entrypoint is exposed as NodePort 30443.)

`packs/traefik/chart.yaml` — Task 0 finding 0.14 captured the real phase-1 file (chart **41.0.2**, `gateway.enabled: false`, nested `deployment.replicas`, `service.spec.type`). This is the FULL new file — the only change is adding the `websecure` port entry:

```yaml
chart: traefik
repo: https://traefik.github.io/charts
version: "41.0.2"          # app v3.7.6; pin; bump deliberately
releaseName: traefik
namespace: traefik
values:
  deployment:
    replicas: 1
  providers:
    kubernetesGateway:
      enabled: true
  gateway:
    enabled: false
  ports:
    web:
      port: 8000
      nodePort: 30080
    websecure:
      port: 8443
      nodePort: 30443
  service:
    spec:
      type: NodePort
```

- [x] **Step 2: Write the failing kind-merge test**

Append to `internal/cluster/kindp/merge_test.go`:

```go
func TestRenderMapsGatewayPortToWebsecure(t *testing.T) {
	spec := config.ClusterSpec{Provider: "kind", KubernetesVersion: "v1.33.1"}
	out, err := RenderConfig("dev", spec, gw)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "hostPort: 8443") || !strings.Contains(s, "containerPort: 30443") {
		t.Fatalf("gateway must map host %d to websecure NodePort 30443:\n%s", gw.Port, s)
	}
}
```

Run: `go test ./internal/cluster/kindp/ -run TestRenderMapsGatewayPort -v` — Expected: FAIL (still 30080).

- [x] **Step 3: Implement the mapping change**

In `internal/cluster/kindp/merge.go`, change the constant:

```go
const gatewayContainerPort = 30443 // Traefik websecure NodePort (HTTPS, Phase 2)
```

Regenerate + re-review the golden fixtures (`merged-typed.yaml`, `merged-with-user.yaml`) exactly as Phase 1 Task 5 Step 2 prescribed (temporary WriteFile, eyeball, remove). The CUBE-1201 conflict messages that mention the container port must keep rendering the right number automatically (they interpolate the constant — verify).

Run: `go test ./internal/cluster/kindp/ -v` — Expected: PASS.

- [x] **Step 4: Write the failing TLS-secret test**

`internal/up/tls_test.go`:

```go
package up

import (
	"testing"
	"time"

	"github.com/rafpe/cube-idp/internal/config"
	"github.com/rafpe/cube-idp/internal/trust"
)

func TestGatewayTLSSecretShape(t *testing.T) {
	ca, err := trust.EnsureCA(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	gw := config.GatewaySpec{Pack: "traefik", Host: "cube-idp.localtest.me", Port: 8443}
	objs, err := gatewayTLSObjects(ca, gw, 365*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 2 { // Namespace + Secret
		t.Fatalf("want ns+secret, got %d objects", len(objs))
	}
	sec := objs[1]
	if sec.GetKind() != "Secret" || sec.GetName() != "cube-idp-gateway-tls" || sec.GetNamespace() != "traefik" {
		t.Fatalf("secret identity: %s/%s/%s", sec.GetKind(), sec.GetNamespace(), sec.GetName())
	}
	typ, _, _ := unstructured.NestedString(sec.Object, "type")
	if typ != "kubernetes.io/tls" {
		t.Fatalf("type: %s", typ)
	}
	crt, _, _ := unstructured.NestedString(sec.Object, "stringData", "tls.crt")
	if crt == "" {
		t.Fatal("tls.crt empty")
	}
	if !ca.LeafStillValid([]byte(crt), []string{gw.Host, "*." + gw.Host}, 24*time.Hour) {
		t.Fatal("issued cert must cover the host and its wildcard")
	}
}
```

(import `k8s.io/apimachinery/pkg/apis/meta/v1/unstructured`.) Run: `go test ./internal/up/ -run TestGatewayTLS -v` — Expected: FAIL.

- [x] **Step 5: Implement `gatewayTLSObjects` + wire into `up.Run`**

Add to `internal/up/up.go` (new file section or `tls.go` in the same package):

```go
// gatewayTLSObjects builds the Namespace + kubernetes.io/tls Secret the
// gateway's websecure listener terminates with. The namespace equals the
// gateway pack name by pack convention (traefik pack -> ns "traefik").
// RECONCILE: confirm that convention against the phase-1 traefik pack
// (checkpoint 0.14) — if the pack's namespace differs from its name,
// derive it from the pack metadata instead.
func gatewayTLSObjects(ca *trust.CA, gw config.GatewaySpec, validity time.Duration) ([]*unstructured.Unstructured, error) {
	hosts := []string{gw.Host, "*." + gw.Host}
	certPEM, keyPEM, err := ca.IssueServerCert(hosts, validity)
	if err != nil {
		return nil, err
	}
	ns := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "Namespace",
		"metadata": map[string]any{"name": gw.Pack},
	}}
	sec := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "Secret",
		"metadata": map[string]any{"name": "cube-idp-gateway-tls", "namespace": gw.Pack},
		"type":     "kubernetes.io/tls",
		"stringData": map[string]any{
			"tls.crt": string(certPEM),
			"tls.key": string(keyPEM),
		},
	}}
	return []*unstructured.Unstructured{ns, sec}, nil
}

// ensureGatewayTLS is idempotent: it reuses a live secret whose cert still
// covers the hosts with >30 days left, so repeated `up` runs (and `diff`)
// see no churn.
func ensureGatewayTLS(ctx context.Context, a *apply.Applier, gw config.GatewaySpec) error {
	dir, err := trust.Dir()
	if err != nil {
		return err
	}
	ca, err := trust.EnsureCA(dir)
	if err != nil {
		return err
	}
	var existing corev1.Secret
	err = a.Client().Get(ctx, client.ObjectKey{Namespace: gw.Pack, Name: "cube-idp-gateway-tls"}, &existing)
	if err == nil && ca.LeafStillValid(existing.Data["tls.crt"], []string{gw.Host, "*." + gw.Host}, 30*24*time.Hour) {
		return nil
	}
	objs, err := gatewayTLSObjects(ca, gw, 365*24*time.Hour)
	if err != nil {
		return err
	}
	if err := a.Apply(ctx, objs, true, time.Minute); err != nil {
		return err
	}
	return a.RecordInventory(ctx, objs)
}
```

(imports: `corev1 "k8s.io/api/core/v1"`, `sigs.k8s.io/controller-runtime/pkg/client`.) In `up.Run`, call `ensureGatewayTLS(ctx, a, cube.Spec.Gateway)` **after** the engine install step and **before** the pack delivery loop (the gateway pack's listener references the secret; the secret must exist before Flux/Argo reconciles the Gateway), followed by `step(out, "tls", "gateway certificate ready (CA: run `cube-idp trust` to make browsers trust it)")`.

**D12 ordering (spec: "cert material is generated before cluster creation"):** additionally make `up.Run`'s FIRST step — before `ClusterProvider.Ensure` — a `trust.Dir()` + `trust.EnsureCA(dir)` call with `step(out, "ca", "local CA ready (%s)", ca.CertPath)`. This is what lets the kind provider mount the CA into containerd `certs.d` at cluster-create time (Task 10 builds on it) and guarantees the mkcert adoption (Task 8 Step 3c) happens before any cluster artifact references the trust root. `ensureGatewayTLS` keeps its own internal `EnsureCA` (idempotent load — by this point it always hits the fast path), so its signature stays as written above. Assert the ordering in a test: extend the Task 9 tls test file with a check that `up.Run`'s step sequence emits `"ca"` before the cluster step. RESOLVED at execution (controller adjudication): the test asserts on the output order of the `▸ [ca]` line using a fail-fast bogus-context config — acceptable because Task 13.8 pins the plain step format byte-for-byte, making the string-based assertion stable by design; the `[]stepFn` refactor was judged disproportionate.

- [x] **Step 6: Run tests**

Run: `go test ./internal/up/ ./internal/cluster/kindp/ -short -v && go build ./...`
Expected: PASS

- [x] **Step 7: Commit**

```bash
git add -A && git commit -m "feat: HTTPS gateway — websecure listener, 30443 mapping, CA-issued TLS secret from up"
```

---

### Task 10: Canonical hostname — CoreDNS rewrite, containerd certs.d, registry route

**Reconcile checkpoint:** 0.5 (`RenderConfig` — this task changes its signature; find BOTH call sites: `kind.go` Ensure and `cmd/config.go` render-cluster), 0.7 (zot manifests), 0.13 (`up.Run`), 0.14 (traefik Service name for the rewrite target), Tasks 8/9 merged.

**Files:**
- Create: `internal/trust/coredns.go`, `internal/trust/certsd.go`, `internal/registry/route.go`
- Modify: `internal/cluster/kindp/merge.go` + `kind.go` + `cmd/config.go` (certs.d parameter), `internal/registry/manifests/zot.yaml` (NodePort), `internal/up/up.go` (wire rewrite + route), `cmd/down.go` (remove rewrite on keep-cluster/existing down)
- Test: `internal/trust/coredns_test.go`, `internal/trust/certsd_test.go`, `internal/cluster/kindp/merge_test.go` (extend), `internal/registry/zot_test.go` (extend)

**Interfaces:**
- Consumes: `client.Client` (from `apply.Applier.Client()`), `trust.CA`, kindp merge.
- Produces:

```go
package trust
func EnsureCoreDNSRewrite(ctx context.Context, c client.Client, host, targetFQDN string, timeout time.Duration) error // CUBE-6004, idempotent
func RemoveCoreDNSRewrite(ctx context.Context, c client.Client, timeout time.Duration) error                          // CUBE-6004, idempotent
func WriteCertsD(dir, registryHost, endpoint, caCertPath string) error // hosts.toml + ca.crt into dir

package kindp
type CertsD struct{ Host, HostDir string } // zero value = no injection
func RenderConfig(name string, spec config.ClusterSpec, gw config.GatewaySpec, certsd CertsD) ([]byte, error)

package registry
const NodePort = 30500
func GatewayRoute(host string) *unstructured.Unstructured // HTTPRoute registry.<host> -> zot:5000
```

**What each piece is for (D6):**
- *CoreDNS rewrite*: inside the cluster, `*.cube-idp.localtest.me` resolves to 127.0.0.1 (localtest.me semantics) — useless. The rewrite sends any `*.<gateway.host>` query to the gateway Service, so in-cluster clients use the same canonical URLs as the browser. Host is overridable via `spec.gateway.host`.
- *Registry route + NodePort*: `registry.<gateway.host>` routes to zot through the gateway for host-side `docker push` (TLS via the CA once `cube-idp trust` ran); kind *nodes* pull through certs.d instead, because localtest.me on a node points at the node itself.
- *containerd certs.d*: maps image refs `registry.<gateway.host>/...` on kind nodes to a working endpoint, injected at cluster create via the kind provider.

- [x] **Step 1: Write the failing CoreDNS tests (fake client, pure string surgery)**

`internal/trust/coredns_test.go`:

```go
package trust

import (
	"context"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const baseCorefile = `.:53 {
    errors
    health
    ready
    kubernetes cluster.local in-addr.arpa ip6.arpa {
        pods insecure
    }
    forward . /etc/resolv.conf
    cache 30
}
`

func fakeClientWithCoreDNS(t *testing.T) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "coredns", Namespace: "kube-system"},
		Data:       map[string]string{"Corefile": baseCorefile},
	}
	dep := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "coredns", Namespace: "kube-system"}}
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm, dep).Build()
}

func getCorefile(t *testing.T, c client.Client) string {
	t.Helper()
	var cm corev1.ConfigMap
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: "kube-system", Name: "coredns"}, &cm); err != nil {
		t.Fatal(err)
	}
	return cm.Data["Corefile"]
}

func TestEnsureCoreDNSRewriteInsertsOnceAndReverts(t *testing.T) {
	c := fakeClientWithCoreDNS(t)
	ctx := context.Background()
	for i := 0; i < 2; i++ { // idempotent
		if err := EnsureCoreDNSRewrite(ctx, c, "cube-idp.localtest.me", "traefik.traefik.svc.cluster.local", time.Second); err != nil {
			t.Fatalf("ensure #%d: %v", i+1, err)
		}
	}
	cf := getCorefile(t, c)
	if strings.Count(cf, "cube-idp:rewrite:begin") != 1 {
		t.Fatalf("rewrite block must appear exactly once:\n%s", cf)
	}
	if !strings.Contains(cf, `(.*)\.cube-idp\.localtest\.me`) ||
		!strings.Contains(cf, "traefik.traefik.svc.cluster.local") {
		t.Fatalf("rewrite content wrong:\n%s", cf)
	}
	if !strings.Contains(cf, "kubernetes cluster.local") {
		t.Fatal("original Corefile content lost")
	}
	if err := RemoveCoreDNSRewrite(ctx, c, time.Second); err != nil {
		t.Fatal(err)
	}
	if got := getCorefile(t, c); got != baseCorefile {
		t.Fatalf("remove must restore the original Corefile:\n%s", got)
	}
	// removing again is a no-op
	if err := RemoveCoreDNSRewrite(ctx, c, time.Second); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureCoreDNSRewriteHostChange(t *testing.T) {
	c := fakeClientWithCoreDNS(t)
	ctx := context.Background()
	if err := EnsureCoreDNSRewrite(ctx, c, "old.example.com", "traefik.traefik.svc.cluster.local", time.Second); err != nil {
		t.Fatal(err)
	}
	if err := EnsureCoreDNSRewrite(ctx, c, "new.example.com", "traefik.traefik.svc.cluster.local", time.Second); err != nil {
		t.Fatal(err)
	}
	cf := getCorefile(t, c)
	if strings.Contains(cf, "old.example.com") || strings.Count(cf, "cube-idp:rewrite:begin") != 1 {
		t.Fatalf("host change must replace the block:\n%s", cf)
	}
}
```

- [x] **Step 2: Run tests to verify they fail, then implement coredns.go**

Run: `go test ./internal/trust/ -run TestEnsureCoreDNS -v` — Expected: FAIL.

`internal/trust/coredns.go`:

```go
package trust

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/rafpe/cube-idp/internal/diag"
)

const (
	rewriteBegin = "    # cube-idp:rewrite:begin"
	rewriteEnd   = "    # cube-idp:rewrite:end"
)

// EnsureCoreDNSRewrite makes *.<host> resolve to the gateway Service inside
// the cluster (D6 canonical hostname). Idempotent; replaces its own block on
// host change; restarts CoreDNS and waits (deadline-bound) for the rollout.
// RECONCILE: verify the `answer auto` rewrite option exists in the CoreDNS
// version shipped by the pinned kindest/node image (present since CoreDNS
// 1.8); if not, use the two-line name/answer regex form instead.
func EnsureCoreDNSRewrite(ctx context.Context, c client.Client, host, targetFQDN string, timeout time.Duration) error {
	block := fmt.Sprintf("%s\n    rewrite stop {\n        name regex (.*)\\.%s %s\n        answer auto\n    }\n%s",
		rewriteBegin, regexp.QuoteMeta(host), targetFQDN, rewriteEnd)
	return patchCorefile(ctx, c, timeout, func(corefile string) (string, error) {
		cleaned := removeManagedBlock(corefile)
		idx := strings.Index(cleaned, "\n    ready")
		if idx < 0 {
			idx = strings.Index(cleaned, "{") // fall back: right after the server block opens
			if idx < 0 {
				return "", diag.New("CUBE-6004", "CoreDNS Corefile has an unexpected shape",
					"inspect `kubectl -n kube-system get cm coredns -o yaml`; set spec.gateway.host to a name your DNS already resolves to skip the rewrite")
			}
		} else {
			idx += len("\n    ready")
		}
		return cleaned[:idx+1] + block + "\n" + cleaned[idx+1:], nil
	})
}

func RemoveCoreDNSRewrite(ctx context.Context, c client.Client, timeout time.Duration) error {
	return patchCorefile(ctx, c, timeout, func(corefile string) (string, error) {
		return removeManagedBlock(corefile), nil
	})
}

func removeManagedBlock(corefile string) string {
	b := strings.Index(corefile, rewriteBegin)
	e := strings.Index(corefile, rewriteEnd)
	if b < 0 || e < 0 {
		return corefile
	}
	return corefile[:b-1] + corefile[e+len(rewriteEnd)+1:] // -1/+1 swallow the surrounding newlines
}

func patchCorefile(ctx context.Context, c client.Client, timeout time.Duration, edit func(string) (string, error)) error {
	var cm corev1.ConfigMap
	if err := c.Get(ctx, client.ObjectKey{Namespace: "kube-system", Name: "coredns"}, &cm); err != nil {
		return diag.Wrap(err, "CUBE-6004", "cannot read the CoreDNS ConfigMap",
			"non-CoreDNS clusters are not supported for the canonical hostname; set spec.gateway.host to a resolvable name")
	}
	updated, err := edit(cm.Data["Corefile"])
	if err != nil {
		return err
	}
	if updated == cm.Data["Corefile"] {
		return nil // no change, no restart
	}
	cm.Data["Corefile"] = updated
	if err := c.Update(ctx, &cm); err != nil {
		return diag.Wrap(err, "CUBE-6004", "cannot update the CoreDNS ConfigMap", "check RBAC on kube-system")
	}
	// restart CoreDNS so the change takes effect, then wait (hard deadline)
	var dep appsv1.Deployment
	if err := c.Get(ctx, client.ObjectKey{Namespace: "kube-system", Name: "coredns"}, &dep); err != nil {
		return diag.Wrap(err, "CUBE-6004", "cannot find the CoreDNS Deployment", "check the cluster's DNS setup")
	}
	if dep.Spec.Template.Annotations == nil {
		dep.Spec.Template.Annotations = map[string]string{}
	}
	dep.Spec.Template.Annotations["cube-idp.dev/restartedAt"] = time.Now().Format(time.RFC3339)
	if err := c.Update(ctx, &dep); err != nil {
		return diag.Wrap(err, "CUBE-6004", "cannot restart CoreDNS", "check RBAC on kube-system")
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var d appsv1.Deployment
		if err := c.Get(ctx, client.ObjectKey{Namespace: "kube-system", Name: "coredns"}, &d); err == nil {
			if d.Status.ObservedGeneration >= d.Generation && d.Status.UpdatedReplicas == d.Status.Replicas && d.Status.UnavailableReplicas == 0 {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return diag.Wrap(ctx.Err(), "CUBE-6004", "interrupted while waiting for CoreDNS to roll out", "re-run the command")
		case <-time.After(2 * time.Second):
		}
	}
	return diag.New("CUBE-6004", "CoreDNS did not become ready after the rewrite within the deadline",
		"inspect `kubectl -n kube-system rollout status deploy/coredns`; the rewrite is applied and will work once CoreDNS recovers")
}
```

(Note for the fake-client tests: the fake client never updates Deployment status, so `TestEnsureCoreDNSRewrite*` pass a 1-second timeout and the fake Deployment has zero replicas — `UpdatedReplicas == Replicas == 0` satisfies the readiness predicate immediately. Keep that property or seed the fake with matching status.)

Run: `go test ./internal/trust/ -run TestEnsureCoreDNS -v` — Expected: PASS.

- [x] **Step 3: certs.d writer + kind merge injection (failing tests first)**

`internal/trust/certsd_test.go`:

```go
package trust

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteCertsD(t *testing.T) {
	dir := t.TempDir()
	caPath := filepath.Join(dir, "ca.crt")
	os.WriteFile(caPath, []byte("PEM"), 0o644)
	hostDir := filepath.Join(dir, "certsd")
	if err := WriteCertsD(hostDir, "registry.cube-idp.localtest.me", "http://localhost:30500", caPath); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(hostDir, "hosts.toml"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, `server = "https://registry.cube-idp.localtest.me"`) ||
		!strings.Contains(s, `[host."http://localhost:30500"]`) {
		t.Fatalf("hosts.toml:\n%s", s)
	}
	if _, err := os.Stat(filepath.Join(hostDir, "ca.crt")); err != nil {
		t.Fatal("ca.crt must be copied alongside hosts.toml")
	}
}
```

Append to `internal/cluster/kindp/merge_test.go`:

```go
func TestRenderConfigInjectsCertsD(t *testing.T) {
	spec := config.ClusterSpec{Provider: "kind", KubernetesVersion: "v1.33.1"}
	out, err := RenderConfig("dev", spec, gw, CertsD{Host: "registry.cube-idp.localtest.me", HostDir: "/tmp/certsd"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "/etc/containerd/certs.d/registry.cube-idp.localtest.me") {
		t.Fatalf("certs.d mount missing:\n%s", s)
	}
	if !strings.Contains(s, `config_path = "/etc/containerd/certs.d"`) {
		t.Fatalf("containerd config_path patch missing:\n%s", s)
	}
}
```

Run both — Expected: FAIL. Then implement:

`internal/trust/certsd.go`:

```go
package trust

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/rafpe/cube-idp/internal/diag"
)

// WriteCertsD prepares a containerd certs.d host directory that maps image
// refs on <registryHost> to <endpoint>. The kind provider bind-mounts dir to
// /etc/containerd/certs.d/<registryHost> on every node (D6).
//
// Endpoint choice: kind nodes cannot reach the gateway through localtest.me
// (it resolves to the node itself), so the default endpoint is the zot
// NodePort on the node's loopback: http://localhost:30500.
// RECONCILE: verify during e2e that a kind node's containerd can reach a
// NodePort via localhost (kube-proxy iptables + route_localnet). If it
// cannot, the fallback is: after cluster create, `up` rewrites hosts.toml
// (it is a live bind mount) replacing localhost with the node's InternalIP
// from `kubectl get nodes -o wide`.
func WriteCertsD(dir, registryHost, endpoint, caCertPath string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return diag.Wrap(err, "CUBE-6001", "cannot create the certs.d staging dir", "check permissions on the cube-idp config dir")
	}
	ca, err := os.ReadFile(caCertPath)
	if err != nil {
		return diag.Wrap(err, "CUBE-6001", "cannot read the CA certificate", "run any cube-idp command that ensures the CA first (`up`)")
	}
	if err := os.WriteFile(filepath.Join(dir, "ca.crt"), ca, 0o644); err != nil {
		return diag.Wrap(err, "CUBE-6001", "cannot stage ca.crt", "check permissions")
	}
	hosts := fmt.Sprintf(`server = "https://%s"

[host.%q]
  capabilities = ["pull", "resolve"]
  ca = "/etc/containerd/certs.d/%s/ca.crt"
`, registryHost, endpoint, registryHost)
	if err := os.WriteFile(filepath.Join(dir, "hosts.toml"), []byte(hosts), 0o644); err != nil {
		return diag.Wrap(err, "CUBE-6001", "cannot write hosts.toml", "check permissions")
	}
	return nil
}
```

In `internal/cluster/kindp/merge.go`, change the signature to `RenderConfig(name string, spec config.ClusterSpec, gw config.GatewaySpec, certsd CertsD) ([]byte, error)` with:

```go
type CertsD struct{ Host, HostDir string }
```

and, before the final `yaml.Marshal(cfg)`:

```go
if certsd.Host != "" {
	cfg.ContainerdConfigPatches = append(cfg.ContainerdConfigPatches,
		"[plugins.\"io.containerd.grpc.v1.cri\".registry]\n  config_path = \"/etc/containerd/certs.d\"")
	cp.ExtraMounts = append(cp.ExtraMounts, v1alpha4.Mount{
		HostPath: certsd.HostDir, ContainerPath: "/etc/containerd/certs.d/" + certsd.Host})
}
```

Update the two callers: `kind.go` `Ensure` (build the CertsD by calling `trust.Dir()` + `trust.EnsureCA` + `trust.WriteCertsD(filepath.Join(dir, "certsd", host), host, "http://localhost:30500", ca.CertPath)` where `host = "registry." + k.gw.Host` — the kind provider already holds the GatewaySpec) and `cmd/config.go` render-cluster (pass a zero `CertsD{}` — render-cluster stays pure and file-free). Update all existing merge tests for the new parameter (`CertsD{}` for old cases).

- [x] **Step 4: zot NodePort + gateway route (failing test first)**

Append to `internal/registry/zot_test.go`:

```go
func TestServiceExposesNodePort(t *testing.T) {
	objs, err := Manifests()
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range objs {
		if o.GetKind() != "Service" {
			continue
		}
		typ, _, _ := unstructured.NestedString(o.Object, "spec", "type")
		if typ != "NodePort" {
			t.Fatalf("zot Service must be NodePort for node-local pulls, got %q", typ)
		}
		ports, _, _ := unstructured.NestedSlice(o.Object, "spec", "ports")
		np, _, _ := unstructured.NestedInt64(ports[0].(map[string]any), "nodePort")
		if np != NodePort {
			t.Fatalf("nodePort: %d", np)
		}
	}
}

func TestGatewayRouteShape(t *testing.T) {
	r := GatewayRoute("cube-idp.localtest.me")
	if r.GetKind() != "HTTPRoute" || r.GetNamespace() != "cube-idp-system" {
		t.Fatalf("route identity: %s/%s", r.GetKind(), r.GetNamespace())
	}
	hostnames, _, _ := unstructured.NestedStringSlice(r.Object, "spec", "hostnames")
	if len(hostnames) != 1 || hostnames[0] != "registry.cube-idp.localtest.me" {
		t.Fatalf("hostnames: %v", hostnames)
	}
}
```

Modify `internal/registry/manifests/zot.yaml` Service:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: zot
  namespace: cube-idp-system
spec:
  type: NodePort
  selector: {app: zot}
  ports: [{port: 5000, targetPort: 5000, nodePort: 30500}]
```

`internal/registry/route.go`:

```go
package registry

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// NodePort is zot's node-local port: kind nodes pull "registry.<host>/..."
// images through it via containerd certs.d (Phase 2, D6).
const NodePort = 30500

// GatewayRoute exposes zot at registry.<host> through the gateway so the
// developer's docker/oras on the HOST can push with TLS + the cube-idp CA.
// Applied by `up` after the gateway pack is delivered (the HTTPRoute CRD
// arrives with the gateway pack's Gateway API CRDs).
func GatewayRoute(host string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "gateway.networking.k8s.io/v1",
		"kind":       "HTTPRoute",
		"metadata":   map[string]any{"name": "cube-idp-registry", "namespace": "cube-idp-system"},
		"spec": map[string]any{
			"parentRefs": []any{map[string]any{"name": "cube-idp", "namespace": "traefik"}},
			"hostnames":  []any{"registry." + host},
			"rules": []any{map[string]any{
				"backendRefs": []any{map[string]any{"name": "zot", "port": int64(5000)}},
			}},
		},
	}}
}
```

(RECONCILE: the `parentRefs` name/namespace must match the phase-1 Gateway (`cube-idp` in ns `traefik`, checkpoint 0.14). Cross-namespace backendRefs are same-namespace here — route lives in `cube-idp-system` next to zot, so the parentRef crosses namespaces, which the phase-1 Gateway allows via `allowedRoutes: {namespaces: {from: All}}`.)

- [x] **Step 5: Wire into `up` and `down`**

In `internal/up/up.go`, after the pack delivery loop and before `waitHealthy` (all deadline-bound):

```go
route := registry.GatewayRoute(cube.Spec.Gateway.Host)
if err := a.Apply(ctx, []*unstructured.Unstructured{route}, false, applyTimeout); err != nil {
	return err
}
if err := a.RecordInventory(ctx, []*unstructured.Unstructured{route}); err != nil {
	return err
}
if err := trust.EnsureCoreDNSRewrite(ctx, a.Client(), cube.Spec.Gateway.Host,
	gatewayServiceFQDN(cube.Spec.Gateway), 2*time.Minute); err != nil {
	return err
}
step(out, "dns", "*.%s resolves to the gateway in-cluster", cube.Spec.Gateway.Host)
```

with `func gatewayServiceFQDN(gw config.GatewaySpec) string { return fmt.Sprintf("%s.%s.svc.cluster.local", gw.Pack, gw.Pack) }` (RECONCILE: the Service name is the traefik chart's fullname — verify against the phase-1 chart values, checkpoint 0.14, and hardcode whatever the chart actually produces).

In `cmd/down.go`, on the `existing`/`--keep-cluster` path, call `trust.RemoveCoreDNSRewrite(c.Context(), a.Client(), 2*time.Minute)` before `a.DeleteAll(...)` (kind-delete path needs nothing — the cluster dies).

- [x] **Step 6: Run everything**

Run: `go test ./internal/trust/ ./internal/registry/ ./internal/cluster/kindp/ -short -v && go build ./... && go test ./... -short`
Expected: PASS

- [x] **Step 7: Commit**

```bash
git add -A && git commit -m "feat: canonical hostname — CoreDNS rewrite, containerd certs.d, registry gateway route (D6)"
```

---

### Task 11: `cube-idp trust` command + full revert on `down`

**Reconcile checkpoint:** 0.12 (cmd/ registration + `down` body), Task 8 merged (CA/state), smallstep/truststore API (see Step 3 marker).

**Files:**
- Create: `internal/trust/store.go`, `cmd/trust.go`
- Modify: `cmd/down.go` (revert), `cmd/root.go` (register)
- Test: `cmd/trust_test.go` (consent gating — the only unit-testable part; OS-store effects are verified manually and in a dedicated e2e step that runs only on developer machines, never CI)

**Interfaces:**
- Consumes: `trust.EnsureCA`, `trust.LoadState/SaveState`.
- Produces:

```go
package trust
func InstallOS(dir string) error   // EnsureCA + truststore install + state write; CUBE-6002
func UninstallOS(dir string) error // truststore uninstall + state clear; CUBE-6003; no-op if not installed
```

D6 contract: `up` NEVER calls these. Only `cube-idp trust` (after an explicit y/N prompt or `--yes`) calls `InstallOS`; `cube-idp trust --uninstall` and `cube-idp down` call `UninstallOS`.

- [x] **Step 1: Write the failing consent test**

`cmd/trust_test.go`:

```go
package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestTrustRequiresConsent(t *testing.T) {
	installed := false
	restore := trustInstall
	trustInstall = func(dir string) error { installed = true; return nil }
	defer func() { trustInstall = restore }()

	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader("n\n"))
	root.SetArgs([]string{"trust"})
	if err := root.Execute(); err != nil {
		t.Fatalf("declining consent must not be an error: %v", err)
	}
	if installed {
		t.Fatal("trust must not touch the OS store without consent (D6)")
	}
	if !strings.Contains(out.String(), "aborted") {
		t.Fatalf("expected an aborted notice, got:\n%s", out.String())
	}

	root = NewRootCmd()
	root.SetOut(&out)
	root.SetIn(strings.NewReader("y\n"))
	root.SetArgs([]string{"trust"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !installed {
		t.Fatal("trust must install after explicit consent")
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/ -run TestTrustRequiresConsent -v`
Expected: FAIL (trust command unknown)

- [x] **Step 3: Implement the store wrapper**

```bash
go get github.com/smallstep/truststore@latest
```

`internal/trust/store.go`:

```go
package trust

import (
	"github.com/smallstep/truststore"

	"github.com/rafpe/cube-idp/internal/diag"
)

// InstallOS installs the cube-idp CA into the OS trust stores (the mkcert
// mechanism, D6). Callers MUST have obtained explicit user consent first.
// RECONCILE: verify the pinned smallstep/truststore API — historically
// truststore.InstallFile(path, opts...) / truststore.UninstallFile(path,
// opts...) with options like truststore.WithFirefox(), truststore.WithJava().
// Enable the Firefox option when available; skip Java (not a cube-idp
// audience). Adjust the two call sites below mechanically.
func InstallOS(dir string) error {
	ca, err := EnsureCA(dir)
	if err != nil {
		return err
	}
	if err := truststore.InstallFile(ca.CertPath, truststore.WithFirefox()); err != nil {
		return diag.Wrap(err, "CUBE-6002", "installing the CA into the OS trust store failed",
			"you may be prompted for your password/sudo by the OS; re-run `cube-idp trust`. Manual alternative: import "+ca.CertPath+" into your trust store")
	}
	return SaveState(dir, &State{Installed: true, CACert: ca.CertPath})
}

// UninstallOS reverts InstallOS. Safe to call when nothing was installed.
func UninstallOS(dir string) error {
	st, err := LoadState(dir)
	if err != nil {
		return err
	}
	if !st.Installed {
		return nil
	}
	if err := truststore.UninstallFile(st.CACert, truststore.WithFirefox()); err != nil {
		return diag.Wrap(err, "CUBE-6003", "removing the CA from the OS trust store failed",
			"remove it manually from your OS trust store: "+st.CACert+", then delete the trust state file")
	}
	return SaveState(dir, &State{Installed: false})
}
```

- [x] **Step 4: Implement the command + down revert**

`cmd/trust.go`:

```go
package cmd

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rafpe/cube-idp/internal/trust"
)

// seams for tests — the OS trust store must never be touched by `go test`.
var (
	trustInstall   = trust.InstallOS
	trustUninstall = trust.UninstallOS
)

func newTrustCmd() *cobra.Command {
	var yes, uninstall bool
	c := &cobra.Command{
		Use:   "trust",
		Short: "Add (or remove, --uninstall) the cube-idp local CA to your OS trust stores — opt-in, fully reverted by `down` (D6)",
		RunE: func(c *cobra.Command, _ []string) error {
			dir, err := trust.Dir()
			if err != nil {
				return err
			}
			if uninstall {
				if err := trustUninstall(dir); err != nil {
					return err
				}
				fmt.Fprintln(c.OutOrStdout(), "cube-idp CA removed from OS trust stores")
				return nil
			}
			if !yes {
				fmt.Fprint(c.OutOrStdout(),
					"This adds the cube-idp local CA to your OS trust stores so browsers accept\n"+
						"https://*."+"cube-idp.localtest.me without warnings (mkcert mechanism).\n"+
						"It is fully removed by `cube-idp trust --uninstall` or `cube-idp down`.\nProceed? [y/N] ")
				line, _ := bufio.NewReader(c.InOrStdin()).ReadString('\n')
				if strings.ToLower(strings.TrimSpace(line)) != "y" {
					fmt.Fprintln(c.OutOrStdout(), "aborted — nothing was changed")
					return nil
				}
			}
			if err := trustInstall(dir); err != nil {
				return err
			}
			fmt.Fprintln(c.OutOrStdout(), "cube-idp CA is now trusted by this machine")
			return nil
		},
	}
	c.Flags().BoolVar(&yes, "yes", false, "skip the consent prompt")
	c.Flags().BoolVar(&uninstall, "uninstall", false, "remove the CA from the OS trust stores")
	return c
}
```

Register `newTrustCmd()` in `cmd/root.go`. In `cmd/down.go`, after the resource/cluster deletion succeeds (both paths), append:

```go
if dir, derr := trust.Dir(); derr == nil {
	st, serr := trust.LoadState(dir)
	if serr == nil && st.Installed {
		if err := trustUninstall(dir); err != nil {
			return err // CUBE-6003 with manual remediation
		}
		fmt.Fprintln(c.OutOrStdout(), "reverted: cube-idp CA removed from OS trust stores")
	}
}
```

- [x] **Step 5: Run tests + manual OS verification**

Run: `go test ./cmd/ ./internal/trust/ -v && go build ./...`
Expected: PASS. Then, on the developer machine only (not CI): `./cube-idp trust` → check the OS store (macOS: `security find-certificate -c "cube-idp local CA" /Library/Keychains/System.keychain`); `./cube-idp trust --uninstall` → gone. Record the result in the commit message.

- [x] **Step 6: Commit**

```bash
git add -A && git commit -m "feat: opt-in cube-idp trust command with full revert on down (D6)"
```

---

### Task 12: `cube-idp doctor` with fault-injection tests

**Reconcile checkpoint:** 0.2 (`diag.Finding` fields), 0.5 (provider `Diagnose`, kind runtime detection internals — see Step 3 marker), 0.9 (engine `Health`), 0.12 (cmd pattern), 0.15 (0xxx codes free).

**Files:**
- Create: `internal/doctor/doctor.go`, `internal/doctor/checks_linux.go`, `internal/doctor/checks_other.go`, `cmd/doctor.go`
- Test: `internal/doctor/doctor_test.go`

**Interfaces:**
- Consumes: `diag.Finding`, `cluster.Provider.Diagnose`, `engine.Health`, `config.Load`.
- Produces:

```go
package doctor
func CheckRuntime() *diag.Finding                                  // nil = ok; CUBE-0101
func CheckPortFree(port int, clusterExists bool) *diag.Finding     // CUBE-0102 (warning if cluster exists — the gateway legitimately holds it)
func CheckDiskSpace(dir string, minBytes uint64) *diag.Finding     // CUBE-0103 warning
func CheckInotify() []diag.Finding                                 // CUBE-0104 warnings; empty off-linux
func Render(out io.Writer, findings []diag.Finding) (hasErrors bool)
```

- [x] **Step 1: Write the failing fault-injection tests (spec §5 doctor tests)**

`internal/doctor/doctor_test.go`:

```go
package doctor

import (
	"fmt"
	"math"
	"net"
	"strings"
	"testing"

	"github.com/rafpe/cube-idp/internal/diag"
)

func TestPortSquatIsDetected(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	port := l.Addr().(*net.TCPAddr).Port

	f := CheckPortFree(port, false)
	if f == nil || f.Code != "CUBE-0102" || f.Severity != diag.SeverityError {
		t.Fatalf("squatted port must yield CUBE-0102 error, got %+v", f)
	}
	if !strings.Contains(f.Remediation, fmt.Sprint(port)) {
		t.Fatalf("remediation must name the port: %+v", f)
	}
	// when the cube is already up, the gateway holding the port is expected
	f = CheckPortFree(port, true)
	if f == nil || f.Severity != diag.SeverityWarning {
		t.Fatalf("existing cluster downgrades to warning, got %+v", f)
	}
}

func TestFreePortPasses(t *testing.T) {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	if f := CheckPortFree(port, false); f != nil {
		t.Fatalf("free port must pass, got %+v", f)
	}
}

func TestMissingRuntimeIsDetected(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // no docker/podman/nerdctl anywhere
	f := CheckRuntime()
	if f == nil || f.Code != "CUBE-0101" || f.Severity != diag.SeverityError {
		t.Fatalf("want CUBE-0101 error, got %+v", f)
	}
}

func TestLowDiskIsAWarning(t *testing.T) {
	f := CheckDiskSpace(t.TempDir(), math.MaxUint64)
	if f == nil || f.Code != "CUBE-0103" || f.Severity != diag.SeverityWarning {
		t.Fatalf("want CUBE-0103 warning, got %+v", f)
	}
	if f := CheckDiskSpace(t.TempDir(), 1); f != nil {
		t.Fatalf("1 byte of free disk must pass, got %+v", f)
	}
}

func TestRenderSeparatesErrorsFromWarnings(t *testing.T) {
	var b strings.Builder
	hasErrors := Render(&b, []diag.Finding{
		{Code: "CUBE-0103", Severity: diag.SeverityWarning, Message: "low disk", Remediation: "free space"},
	})
	if hasErrors {
		t.Fatal("warnings alone must not flag errors")
	}
	hasErrors = Render(&b, []diag.Finding{
		{Code: "CUBE-0101", Severity: diag.SeverityError, Message: "no runtime", Remediation: "install docker"},
	})
	if !hasErrors {
		t.Fatal("an error finding must flag errors (doctor exits 1)")
	}
}
```

- [x] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/doctor/ -v`
Expected: FAIL (package does not exist)

- [x] **Step 3: Implement the checks**

`internal/doctor/doctor.go`:

```go
// Package doctor implements cube-idp's preflight and health diagnosis
// (spec §4.1): runtime present, ports free, disk space, inotify limits,
// plus the providers' Diagnose and the engine's Health — every finding a
// typed CUBE code with a remediation.
package doctor

import (
	"fmt"
	"io"
	"net"
	"os/exec"
	"time"

	"github.com/rafpe/cube-idp/internal/diag"
)

// CheckRuntime looks for a container runtime CLI on PATH — the same set the
// kind provider auto-detects (docker, podman, nerdctl).
// RECONCILE: if the phase-1 kind provider exposes its DetectNodeProvider
// error directly (checkpoint 0.5), prefer calling that so doctor and Ensure
// agree byte-for-byte on what "runtime present" means.
func CheckRuntime() *diag.Finding {
	for _, bin := range []string{"docker", "podman", "nerdctl"} {
		if _, err := exec.LookPath(bin); err == nil {
			return nil
		}
	}
	return &diag.Finding{Code: "CUBE-0101", Severity: diag.SeverityError,
		Message:     "no container runtime found on PATH (docker, podman, or nerdctl)",
		Remediation: "install Docker Desktop, Podman, or nerdctl and ensure it is on PATH"}
}

// CheckPortFree probes the gateway host port. When the cluster already
// exists, the gateway itself legitimately holds the port — downgrade to a
// warning instead of lying about a conflict.
func CheckPortFree(port int, clusterExists bool) *diag.Finding {
	l, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err == nil {
		_ = l.Close()
		return nil
	}
	sev, msg := diag.SeverityError, fmt.Sprintf("port %d is already in use", port)
	if clusterExists {
		sev, msg = diag.SeverityWarning, fmt.Sprintf("port %d is in use (expected: the cube's gateway binds it)", port)
	}
	return &diag.Finding{Code: "CUBE-0102", Severity: sev, Message: msg,
		Remediation: fmt.Sprintf("if this is not cube-idp's gateway, stop whatever binds port %d or change spec.gateway.port", port)}
}

// Render prints findings and reports whether any is an error.
func Render(out io.Writer, findings []diag.Finding) bool {
	hasErrors := false
	for _, f := range findings {
		icon := "⚠"
		if f.Severity == diag.SeverityError {
			icon, hasErrors = "✗", true
		}
		fmt.Fprintf(out, "%s %s  %s\n    fix: %s\n", icon, f.Code, f.Message, f.Remediation)
	}
	if len(findings) == 0 {
		fmt.Fprintln(out, "✔ no problems found")
	}
	return hasErrors
}

// Deadline for the cluster-side portion of doctor (provider Diagnose +
// engine Health) — doctor must never hang on a dead apiserver.
const ClusterProbeTimeout = 15 * time.Second
```

`internal/doctor/checks_linux.go`:

```go
//go:build linux

package doctor

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/rafpe/cube-idp/internal/diag"
)

func CheckDiskSpace(dir string, minBytes uint64) *diag.Finding {
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return nil // cannot measure: stay silent rather than guess
	}
	free := st.Bavail * uint64(st.Bsize)
	if free >= minBytes {
		return nil
	}
	return &diag.Finding{Code: "CUBE-0103", Severity: diag.SeverityWarning,
		Message:     fmt.Sprintf("only %.1f GiB free at %s (kind images want ≥ %.0f GiB)", float64(free)/(1<<30), dir, float64(minBytes)/(1<<30)),
		Remediation: "free disk space or prune old images: `docker system prune`"}
}

func CheckInotify() []diag.Finding {
	var out []diag.Finding
	for path, min := range map[string]int64{
		"/proc/sys/fs/inotify/max_user_watches":   524288,
		"/proc/sys/fs/inotify/max_user_instances": 512,
	} {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		v, err := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
		if err != nil || v >= min {
			continue
		}
		out = append(out, diag.Finding{Code: "CUBE-0104", Severity: diag.SeverityWarning,
			Message:     fmt.Sprintf("%s = %d (kind clusters commonly need ≥ %d)", path, v, min),
			Remediation: fmt.Sprintf("sudo sysctl %s=%d", strings.TrimPrefix(strings.ReplaceAll(path, "/", "."), ".proc.sys."), min)})
	}
	return out
}
```

`internal/doctor/checks_other.go`:

```go
//go:build !linux

package doctor

import (
	"golang.org/x/sys/unix"

	"github.com/rafpe/cube-idp/internal/diag"
)

func CheckDiskSpace(dir string, minBytes uint64) *diag.Finding {
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return nil
	}
	free := uint64(st.Bavail) * uint64(st.Bsize)
	if free >= minBytes {
		return nil
	}
	return &diag.Finding{Code: "CUBE-0103", Severity: diag.SeverityWarning,
		Message:     "low disk space at " + dir,
		Remediation: "free disk space or prune old images: `docker system prune`"}
}

func CheckInotify() []diag.Finding { return nil } // linux-only concern
```

(RECONCILE: `unix.Statfs` exists on darwin and linux with slightly different field types — the two build-tagged files absorb that; if windows support is on the table, add a `checks_windows.go` using `golang.org/x/sys/windows.GetDiskFreeSpaceEx`. Keep the duplicated CUBE-0103 strings identical in both files.)

- [x] **Step 4: Implement the command**

`cmd/doctor.go`:

```go
package cmd

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/rafpe/cube-idp/internal/apply"
	"github.com/rafpe/cube-idp/internal/cluster"
	"github.com/rafpe/cube-idp/internal/config"
	"github.com/rafpe/cube-idp/internal/diag"
	"github.com/rafpe/cube-idp/internal/doctor"
	enginefactory "github.com/rafpe/cube-idp/internal/engine/factory"
	"github.com/rafpe/cube-idp/internal/trust"
)

func newDoctorCmd() *cobra.Command {
	var file string
	c := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose the host and (if reachable) the cluster; exit 1 on errors",
		RunE: func(c *cobra.Command, _ []string) error {
			out := c.OutOrStdout()
			var findings []diag.Finding

			cube, err := config.Load(file)
			if err != nil {
				// A broken config is itself a finding; host checks still run with defaults.
				var de *diag.Error
				if errors.As(err, &de) {
					findings = append(findings, diag.Finding{Code: de.Code, Severity: diag.SeverityError,
						Message: de.Summary, Remediation: de.Remediation})
				}
				cube = config.Default("dev")
			}

			clusterExists := false
			prov, provErr := cluster.New(cube.Spec.Cluster, cube.Spec.Gateway)
			if provErr == nil {
				clusterExists, _ = prov.Exists(c.Context(), cube.Metadata.Name)
			}

			if cube.Spec.Cluster.Provider == "kind" {
				if f := doctor.CheckRuntime(); f != nil {
					findings = append(findings, *f)
				}
			}
			if f := doctor.CheckPortFree(cube.Spec.Gateway.Port, clusterExists); f != nil {
				findings = append(findings, *f)
			}
			if dir, err := trust.Dir(); err == nil {
				if f := doctor.CheckDiskSpace(dir, 5<<30); f != nil {
					findings = append(findings, *f)
				}
			}
			findings = append(findings, doctor.CheckInotify()...)

			if provErr == nil {
				findings = append(findings, prov.Diagnose(c.Context(), cube.Metadata.Name)...)
			}

			if provErr == nil && clusterExists {
				ctx, cancel := context.WithTimeout(c.Context(), doctor.ClusterProbeTimeout)
				defer cancel()
				if conn, err := prov.Ensure(ctx, cube.Metadata.Name, cube.Spec.Cluster); err == nil {
					if a, err := apply.New(conn.REST, cube.Metadata.Name); err == nil {
						if eng, err := enginefactory.New(cube.Spec.Engine.Type); err == nil {
							if comps, err := eng.Health(ctx, a); err == nil {
								for _, comp := range comps {
									if !comp.Ready {
										findings = append(findings, diag.Finding{Code: "CUBE-3004",
											Severity: diag.SeverityError, Message: comp.Name + " not ready: " + comp.Message,
											Remediation: "re-run `cube-idp up`; inspect the component with kubectl"})
									}
								}
							}
						}
					}
				} else if de := new(diag.Error); errors.As(err, &de) {
					findings = append(findings, diag.Finding{Code: de.Code, Severity: diag.SeverityError,
						Message: de.Summary, Remediation: de.Remediation})
				}
			}

			if doctor.Render(out, findings) {
				os.Exit(1)
			}
			return nil
		},
	}
	c.Flags().StringVarP(&file, "file", "f", "cube.yaml", "path to cube.yaml")
	return c
}
```

(imports also `context`, `errors`. RECONCILE: the phase-1 kind provider's `Diagnose` may already cover the runtime check via `provider.List()` — if so, keep BOTH (fast PATH probe + real socket probe) but ensure they emit distinct messages, or drop `CheckRuntime` in favor of the provider finding if they fully overlap.) Register `newDoctorCmd()` in `cmd/root.go`. Also update the Phase 1 CUBE-1203 remediation that says "`cube-idp doctor` (Phase 2) will preflight this" — drop the parenthetical, the command exists now.

- [x] **Step 5: Run tests**

Run: `go test ./internal/doctor/ ./cmd/ -v && go build ./...`
Expected: PASS (including the broken-kubeconfig path already covered by the phase-1 `existing` provider tests feeding `Diagnose`)

- [x] **Step 6: Commit**

```bash
git add -A && git commit -m "feat: cube-idp doctor — host preflights, provider diagnose, engine health, fault-injection tests"
```

---

### Task 12.5: `Pack` discoverability CRD + `expose:` contract (D11 — NEW in the 2026-07-13 amendment)

**Reconcile checkpoint:** 0.3 (config `GatewaySpec` for host substitution), 0.8 (**blocking**: how `loadMeta` CUE-validates `pack.cue` — the `expose:` schema extension hooks in there), 0.13 (`up.Run` sequence — Pack objects are written after `waitHealthy`), 0.15 (CUBE-4011 free), 0.17 (**blocking**: the real `cli-secret` label convention `get secrets` uses today), 0.14 (gitea/argocd pack secret names for the `expose:` blocks).

**D11 contract (spec §2):** ONE inert, cluster-scoped CRD `packs.cube-idp.dev/v1alpha1` — written by `up`, removed by `down` (inventory-tracked like everything else), watched by nobody. `additionalPrinterColumns` make `kubectl get packs` render NAME / VERSION / URL / AUTH-SECRET / READY. The data comes from an optional `expose:` block in `pack.cue`. This becomes the standard delivery contract: a pack that exposes an endpoint or credential declares it in data, never in Go. `cube-idp get secrets` pivots to Pack → `authSecretRef` → Secret; the phase-1 label convention stays honored for one release, then is deprecated.

**Files:**
- Create: `internal/pack/manifests/pack-crd.yaml`, `internal/pack/expose.go`, `internal/pack/discovery.go`
- Modify: the phase-1 `pack.cue` CUE schema + `loadMeta` (accept + validate `expose:`), `internal/up/up.go` (apply CRD + write Pack objects), `cmd/get.go` (secrets pivot), `packs/gitea/pack.cue` + `packs/argocd/pack.cue` (`expose:` blocks; traefik demonstrates that the block is optional)
- Test: `internal/pack/expose_test.go`, `internal/pack/discovery_test.go`, `cmd/get_test.go` (extend)

**Interfaces:**
- Consumes: `Pack` (Task 4 shape), `apply.Applier`, `engine.ComponentHealth`, `config.GatewaySpec`.
- Produces:

```go
package pack
type SecretRef struct{ Namespace, Name string }
type Expose struct {
	URLs          []string          // may contain the literal token ${GATEWAY_HOST}
	AuthSecretRef *SecretRef        // nil = pack exposes no credential
	ImpliedFields map[string]string // e.g. {"username": "admin"} (ArgoCD's implicit login)
}
// Pack gains one field: Expose *Expose (nil when pack.cue has no expose: block)
func CRD() (*unstructured.Unstructured, error)  // the inert Pack CRD (go:embed)
func PackObject(p *Pack, gatewayHost string, ready bool) *unstructured.Unstructured
```

- [x] **Step 1: Write the failing expose-parse tests**

`internal/pack/expose_test.go`:

```go
package pack

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/rafpe/cube-idp/internal/diag"
)

func writePack(t *testing.T, cue string) string {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "pack.cue"), []byte(cue), 0o644)
	os.MkdirAll(filepath.Join(dir, "manifests"), 0o755)
	os.WriteFile(filepath.Join(dir, "manifests", "cm.yaml"),
		[]byte("apiVersion: v1\nkind: ConfigMap\nmetadata: {name: x, namespace: default}\n"), 0o644)
	return dir
}

func TestExposeParsed(t *testing.T) {
	dir := writePack(t, `name: "gitea"
version: "0.1.0"
expose: {
	urls: ["https://gitea.${GATEWAY_HOST}"]
	authSecretRef: {namespace: "gitea", name: "gitea-admin"}
	impliedFields: {username: "gitea_admin"}
}
`)
	p, err := Fetch(context.Background(), dir, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if p.Expose == nil || len(p.Expose.URLs) != 1 ||
		p.Expose.AuthSecretRef == nil || p.Expose.AuthSecretRef.Name != "gitea-admin" ||
		p.Expose.ImpliedFields["username"] != "gitea_admin" {
		t.Fatalf("expose not parsed: %+v", p.Expose)
	}
}

func TestExposeIsOptional(t *testing.T) {
	dir := writePack(t, "name: \"plain\"\nversion: \"0.1.0\"\n")
	p, err := Fetch(context.Background(), dir, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if p.Expose != nil {
		t.Fatalf("no expose block must mean nil, got %+v", p.Expose)
	}
}

func TestExposeInvalidIsTyped(t *testing.T) {
	// authSecretRef missing its name — the CUE schema must reject it
	dir := writePack(t, `name: "bad"
version: "0.1.0"
expose: {authSecretRef: {namespace: "x"}}
`)
	_, err := Fetch(context.Background(), dir, t.TempDir())
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-4011" {
		t.Fatalf("want CUBE-4011, got %v", err)
	}
}
```

- [x] **Step 2: Write the failing discovery-shape tests**

`internal/pack/discovery_test.go`:

```go
package pack

import (
	"testing"
)

func TestCRDParsesAndPrintsColumns(t *testing.T) {
	crd, err := CRD()
	if err != nil {
		t.Fatal(err)
	}
	if crd.GetKind() != "CustomResourceDefinition" || crd.GetName() != "packs.cube-idp.dev" {
		t.Fatalf("CRD identity: %s/%s", crd.GetKind(), crd.GetName())
	}
	scope, _, _ := unstructured.NestedString(crd.Object, "spec", "scope")
	if scope != "Cluster" {
		t.Fatalf("Pack must be cluster-scoped, got %q", scope)
	}
	vers, _, _ := unstructured.NestedSlice(crd.Object, "spec", "versions")
	cols, _, _ := unstructured.NestedSlice(vers[0].(map[string]any), "additionalPrinterColumns")
	if len(cols) < 4 { // VERSION, URL, AUTH-SECRET, READY (NAME is implicit)
		t.Fatalf("printer columns missing: %v", cols)
	}
}

func TestPackObjectShape(t *testing.T) {
	p := &Pack{Name: "gitea", Version: "0.1.0", Expose: &Expose{
		URLs:          []string{"https://gitea.${GATEWAY_HOST}"},
		AuthSecretRef: &SecretRef{Namespace: "gitea", Name: "gitea-admin"},
		ImpliedFields: map[string]string{"username": "gitea_admin"},
	}}
	o := PackObject(p, "cube-idp.localtest.me", true)
	if o.GetKind() != "Pack" || o.GetName() != "gitea" || o.GetNamespace() != "" {
		t.Fatalf("Pack object identity: %s %s/%s", o.GetKind(), o.GetNamespace(), o.GetName())
	}
	url, _, _ := unstructured.NestedString(o.Object, "spec", "url")
	if url != "https://gitea.cube-idp.localtest.me" {
		t.Fatalf("gateway host not substituted: %q", url)
	}
	sec, _, _ := unstructured.NestedString(o.Object, "spec", "authSecret")
	if sec != "gitea/gitea-admin" {
		t.Fatalf("authSecret column value: %q", sec)
	}
	ready, _, _ := unstructured.NestedBool(o.Object, "spec", "ready")
	if !ready {
		t.Fatal("ready must be carried into the record")
	}
}

func TestPackObjectWithoutExpose(t *testing.T) {
	o := PackObject(&Pack{Name: "plain", Version: "0.1.0"}, "h", false)
	if o.GetName() != "plain" {
		t.Fatal("packs without expose still get a record (VERSION/READY are useful alone)")
	}
	if _, found, _ := unstructured.NestedString(o.Object, "spec", "url"); found {
		t.Fatal("no expose -> no url field")
	}
}
```

(import `k8s.io/apimachinery/pkg/apis/meta/v1/unstructured` in both test files.)

- [x] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/pack/ -short -run 'TestExpose|TestCRD|TestPackObject' -v`
Expected: FAIL (Expose/CRD/PackObject undefined)

- [x] **Step 4: Implement**

`internal/pack/manifests/pack-crd.yaml`:

```yaml
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: packs.cube-idp.dev
  labels:
    app.kubernetes.io/part-of: cube-idp
spec:
  group: cube-idp.dev
  scope: Cluster
  names:
    kind: Pack
    listKind: PackList
    plural: packs
    singular: pack
  versions:
    - name: v1alpha1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          type: object
          properties:
            spec:
              type: object
              properties:
                version: {type: string}
                url: {type: string}
                urls: {type: array, items: {type: string}}
                authSecret: {type: string}
                authSecretRef:
                  type: object
                  properties:
                    namespace: {type: string}
                    name: {type: string}
                impliedFields:
                  type: object
                  additionalProperties: {type: string}
                ready: {type: boolean}
      additionalPrinterColumns:
        - {name: VERSION, type: string, jsonPath: .spec.version}
        - {name: URL, type: string, jsonPath: .spec.url}
        - {name: AUTH-SECRET, type: string, jsonPath: .spec.authSecret}
        - {name: READY, type: boolean, jsonPath: .spec.ready}
```

(No controller, no status subresource, no finalizers — an inert record type, per D11.)

`internal/pack/expose.go`:

```go
package pack

import (
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type SecretRef struct{ Namespace, Name string }

// Expose is the D11 contract: how a pack declares its endpoints and
// credentials — in data, never in Go. Parsed from pack.cue's optional
// expose: block; rendered by `up` into the pack's Pack record.
type Expose struct {
	URLs          []string
	AuthSecretRef *SecretRef
	ImpliedFields map[string]string
}

// PackObject builds the cluster-scoped Pack record `up` writes (and `down`,
// via the inventory, deletes). ${GATEWAY_HOST} in urls is replaced with the
// cube's spec.gateway.host — the one substitution the contract allows.
func PackObject(p *Pack, gatewayHost string, ready bool) *unstructured.Unstructured {
	spec := map[string]any{"version": p.Version, "ready": ready}
	if e := p.Expose; e != nil {
		if len(e.URLs) > 0 {
			urls := make([]any, 0, len(e.URLs))
			for _, u := range e.URLs {
				urls = append(urls, strings.ReplaceAll(u, "${GATEWAY_HOST}", gatewayHost))
			}
			spec["urls"] = urls
			spec["url"] = urls[0]
		}
		if r := e.AuthSecretRef; r != nil {
			spec["authSecretRef"] = map[string]any{"namespace": r.Namespace, "name": r.Name}
			spec["authSecret"] = r.Namespace + "/" + r.Name
		}
		if len(e.ImpliedFields) > 0 {
			f := map[string]any{}
			for k, v := range e.ImpliedFields {
				f[k] = v
			}
			spec["impliedFields"] = f
		}
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "cube-idp.dev/v1alpha1",
		"kind":       "Pack",
		"metadata":   map[string]any{"name": p.Name},
		"spec":       spec,
	}}
}
```

`internal/pack/discovery.go`:

```go
package pack

import (
	_ "embed"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/rafpe/cube-idp/internal/apply"
)

//go:embed manifests/pack-crd.yaml
var packCRDYAML []byte

// CRD returns the inert packs.cube-idp.dev CRD (D11): applied by `up`,
// inventory-tracked, deleted by `down`, reconciled by NOBODY.
func CRD() (*unstructured.Unstructured, error) {
	objs, err := apply.ParseMultiDoc(packCRDYAML)
	if err != nil {
		return nil, err
	}
	return objs[0], nil
}
```

CUE schema + parse: extend the phase-1 `pack.cue` schema with an optional `expose?: {urls?: [...string], authSecretRef?: {namespace: string, name: string}, impliedFields?: {[string]: string}}` and decode it into `Pack.Expose` inside `loadMeta`. `RECONCILE:` hook this into the exact phase-1 CUE validation mechanism (checkpoint 0.8); schema violations wrap as `diag.New("CUBE-4011", "expose: block in <dir>/pack.cue is invalid: <cue error>", "fix the expose block — see the pack authoring docs for the shape")`. Note: adding a field to the CUE schema must not reject packs written before it existed (`TestExposeIsOptional` guards this).

- [x] **Step 5: Wire into `up` (and therefore `down`)**

In `internal/up/up.go` (RECONCILE: splice per the real structure from checkpoint 0.13):
1. **Before the pack delivery loop** (right after the registry install): apply + inventory-record `pack.CRD()` with `wait=true` (kstatus waits for `Established`) — the CRD must exist before any Pack record is written.
2. **Retain each fetched `*pack.Pack`** alongside its `Rendered` in the loop (the loop currently drops it — keep a `[]*pack.Pack` in lockstep with the lock entries).
3. **After `waitHealthy`**: build `healthByName` from the engine's `[]ComponentHealth`, then for each pack apply + inventory-record `pack.PackObject(p, cube.Spec.Gateway.Host, healthByName["cube-idp-"+p.Name])` (RECONCILE: the health-name prefix is the flux/argocd Deliver object name from checkpoint 0.9 — use the real mapping). `step(out, "packs", "%d pack records written — try `kubectl get packs`", len(packs))`.

`down` needs NO change: Pack records and the CRD are in the inventory, so the existing reverse-order delete removes them (records before CRD — reverse of apply order; verify in the Task 14 e2e that `down` leaves neither).

- [x] **Step 6: Pivot `cube-idp get secrets` (label convention honored one release)**

In `cmd/get.go` (RECONCILE: adapt to the real phase-1 body from checkpoint 0.17):
1. Primary path: list `Pack` records, follow `spec.authSecretRef` to each Secret, merge `spec.impliedFields` into the rendered output (implied fields print alongside the secret's own keys — that is how ArgoCD's `username: admin` appears).
2. Fallback path: the existing label-convention lookup, prefixed with a deprecation notice: `"note: <pack> was found via the legacy cli-secret label; pack authors should declare expose.authSecretRef in pack.cue (label support ends next release)"`.
3. Extend `cmd/get_test.go` with a fake-client case: one Pack record + referenced Secret → output contains the secret data AND the implied field; one label-only Secret → output contains the deprecation notice.

- [x] **Step 7: Add `expose:` blocks to the shipped packs**

`packs/gitea/pack.cue` — append (RECONCILE: the real secret namespace/name and admin username from the phase-1 gitea pack, checkpoints 0.14/0.17):

```cue
expose: {
	urls: ["https://gitea.${GATEWAY_HOST}"]
	authSecretRef: {namespace: "gitea", name: "gitea-admin"}
	impliedFields: {username: "gitea_admin"}
}
```

`packs/argocd/pack.cue` — append (the UI pack; the argocd *engine* is not a pack and needs no record):

```cue
expose: {
	urls: ["https://argocd.${GATEWAY_HOST}"]
	authSecretRef: {namespace: "argocd", name: "argocd-initial-admin-secret"}
	impliedFields: {username: "admin"}
}
```

`packs/traefik/pack.cue` — deliberately unchanged: it demonstrates that `expose:` is optional (traefik IS the gateway; it exposes nothing through itself).

- [x] **Step 8: Run everything**

Run: `go test ./internal/pack/ ./cmd/ -short -v && go build ./... && go test ./... -short`
Expected: PASS (including the render tests over the modified gitea/argocd pack.cue files — the schema extension must not break their existing rendering)

- [x] **Step 9: Commit**

```bash
git add -A && git commit -m "feat: Pack discoverability CRD + expose contract, get secrets pivot (D11)"
```

---

### Task 13: cnoe-compat loader (spec §4.4, launch-critical)

**Reconcile checkpoint:** 0.8 (`chartRef` shape in `helm.go` — Step 4 exports it), 0.10 (`oci.PushRendered`), 0.13 (`up`'s connect sequence to mirror in the import command), Tasks 3/4 merged (`RenderDir`, git refs).

**Files:**
- Create: `internal/cnoe/loader.go`, `internal/cnoe/translate.go`, `internal/cnoe/testdata/{app.yaml,appset.yaml,manifests/deploy.yaml}`, `cmd/cnoe.go`
- Modify: `internal/pack/helm.go` (export the chart-render entry point)
- Test: `internal/cnoe/loader_test.go`

**Interfaces:**
- Consumes: `pack.RenderDir`, `pack.Fetch`, exported chart render, `oci.PushRendered`, engine factory, `apply.Applier`, `lock.RenderedHash`.
- Produces:

```go
package pack
type ChartRef struct{ Chart, Repo, Version, ReleaseName, Namespace string; Values map[string]any }
func RenderChart(ref ChartRef, values map[string]any) ([]*unstructured.Unstructured, error)
// thin export over the existing private renderHelm path — helm.go stays the only helm importer

package cnoe
type App struct {
	Name      string
	Namespace string      // Argo destination.namespace ("" = leave objects untouched)
	CnoeDir   string      // resolved absolute dir when repoURL is cnoe://
	GitRef    string      // translated cube git pack ref for remote git sources
	Helm      *pack.ChartRef
}
func Load(dir string) ([]App, error)                                   // CUBE-4009/4010
func (a *App) Render(ctx context.Context, cacheDir string) (*pack.Rendered, error)
```

Translation rules (spec §4.4: "ingests existing CNOE/idpbuilder Argo `Application`/`ApplicationSet` YAMLs and translates `cnoe://` paths into OCI pushes"):
1. Every `*.yaml`/`*.yml` directly in the import dir is scanned for `kind: Application` / `kind: ApplicationSet` (`apiVersion: argoproj.io/v1alpha1`); other documents are ignored.
2. `repoURL: cnoe://<rel>` → local dir relative to the YAML file (idpbuilder semantics), joined with `spec.source.path` when set and not `"."` → rendered locally → pushed to zot as an OCI artifact → delivered by the ACTIVE engine (flux or argocd — engine-neutral, unlike idpbuilder). Missing dir → CUBE-4010.
3. Remote git `repoURL` + pinned `targetRevision` → translated to a Task 4 git pack ref. `targetRevision` of `HEAD`/`""`/branch-tracking `*` → CUBE-4009 ("pin targetRevision to a tag or commit").
4. `spec.source.chart` set → helm chart render via `pack.RenderChart` (repoURL is the helm repo).
5. `ApplicationSet`: list generator only; each `generators[].list.elements` entry expands `{{key}}` placeholders in the template. Any other generator → CUBE-4009 naming the generator.
6. Rendered objects that are namespaced and have no namespace get `destination.namespace` (matching Argo's `CreateNamespace` behavior a Namespace object is prepended when `destination.namespace` is set). Cluster-scoped kinds (Namespace, ClusterRole, ClusterRoleBinding, CustomResourceDefinition, StorageClass, PriorityClass, IngressClass, GatewayClass, ValidatingWebhookConfiguration, MutatingWebhookConfiguration, PersistentVolume) are left untouched.
7. The artifact tag is a content hash (first 12 hex of `lock.RenderedHash`), so re-importing changed sources produces a new tag and the engine reconciles the change.

- [x] **Step 1: Create fixtures**

`internal/cnoe/testdata/app.yaml`:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: my-app
spec:
  destination:
    namespace: my-app
    server: https://kubernetes.default.svc
  source:
    repoURL: cnoe://manifests
    targetRevision: HEAD
    path: "."
  syncPolicy:
    automated: {}
```

`internal/cnoe/testdata/manifests/deploy.yaml`:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-app
spec:
  replicas: 1
  selector:
    matchLabels: {app: my-app}
  template:
    metadata:
      labels: {app: my-app}
    spec:
      containers:
        - name: app
          image: nginx:1.27
```

`internal/cnoe/testdata/appset.yaml`:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: envs
spec:
  generators:
    - list:
        elements:
          - env: dev
          - env: stage
  template:
    metadata:
      name: "web-{{env}}"
    spec:
      destination:
        namespace: "web-{{env}}"
        server: https://kubernetes.default.svc
      source:
        repoURL: cnoe://manifests
        targetRevision: HEAD
        path: "."
```

- [x] **Step 2: Write the failing tests**

`internal/cnoe/loader_test.go`:

```go
package cnoe

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/rafpe/cube-idp/internal/diag"
)

func TestLoadFindsAppsAndExpandsAppSets(t *testing.T) {
	apps, err := Load("testdata")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, a := range apps {
		names = append(names, a.Name)
	}
	sort.Strings(names)
	want := []string{"my-app", "web-dev", "web-stage"}
	if len(names) != 3 || names[0] != want[0] || names[1] != want[1] || names[2] != want[2] {
		t.Fatalf("apps: %v, want %v", names, want)
	}
}

func TestCnoePathResolvesRelativeToFile(t *testing.T) {
	apps, err := Load("testdata")
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range apps {
		if a.Name != "my-app" {
			continue
		}
		abs, _ := filepath.Abs(filepath.Join("testdata", "manifests"))
		if a.CnoeDir != abs {
			t.Fatalf("CnoeDir: %s, want %s", a.CnoeDir, abs)
		}
	}
}

func TestRenderSetsDestinationNamespaceAndHashTag(t *testing.T) {
	apps, _ := Load("testdata")
	var app *App
	for i := range apps {
		if apps[i].Name == "my-app" {
			app = &apps[i]
		}
	}
	r, err := app.Render(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// Namespace object prepended + Deployment
	if len(r.Objects) != 2 || r.Objects[0].GetKind() != "Namespace" || r.Objects[0].GetName() != "my-app" {
		t.Fatalf("objects: %d, first %s/%s", len(r.Objects), r.Objects[0].GetKind(), r.Objects[0].GetName())
	}
	if r.Objects[1].GetNamespace() != "my-app" {
		t.Fatalf("destination.namespace not applied: %q", r.Objects[1].GetNamespace())
	}
	if r.Name != "cnoe-my-app" || len(r.Version) != 12 {
		t.Fatalf("rendered identity: %s@%s (tag must be a 12-char content hash)", r.Name, r.Version)
	}
}

func TestUnpinnedRemoteGitIsRejected(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "app.yaml"), []byte(`
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata: {name: remote}
spec:
  destination: {namespace: remote, server: https://kubernetes.default.svc}
  source:
    repoURL: https://github.com/org/repo
    targetRevision: HEAD
    path: apps/remote
`), 0o644)
	_, err := Load(dir)
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-4009" {
		t.Fatalf("want CUBE-4009 (pin targetRevision), got %v", err)
	}
}

func TestMissingCnoeDirIsTyped(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "app.yaml"), []byte(`
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata: {name: broken}
spec:
  destination: {namespace: b, server: https://kubernetes.default.svc}
  source: {repoURL: "cnoe://nope", targetRevision: HEAD, path: "."}
`), 0o644)
	_, err := Load(dir)
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-4010" {
		t.Fatalf("want CUBE-4010, got %v", err)
	}
}
```

- [x] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/cnoe/ -v`
Expected: FAIL (package does not exist)

- [x] **Step 4: Export the chart renderer, implement the loader**

In `internal/pack/helm.go`: rename the private `chartRef` to exported `ChartRef` (fields `Chart, Repo, Version, ReleaseName, Namespace string; Values map[string]any` — RECONCILE: keep exactly the phase-1 field set from checkpoint 0.8, adding nothing) and add:

```go
// RenderChart renders a chart reference exactly as a pack's chart.yaml would
// be rendered. Exported for the cnoe-compat loader; helm.go remains the only
// file importing the Helm SDK.
func RenderChart(ref ChartRef, values map[string]any) ([]*unstructured.Unstructured, error) {
	return renderChartRef(ref, values) // the existing private implementation
}
```

`internal/cnoe/loader.go`:

```go
// Package cnoe ingests CNOE/idpbuilder Argo Application and ApplicationSet
// YAMLs and translates them into cube-idp deliveries: cnoe:// paths become
// local renders pushed to the in-cluster OCI registry, remote sources become
// cube pack refs — engine-neutral, so both flux and argocd cubes can absorb
// an existing idpbuilder setup (spec §4.4, launch-critical).
package cnoe

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/rafpe/cube-idp/internal/apply"
	"github.com/rafpe/cube-idp/internal/diag"
	"github.com/rafpe/cube-idp/internal/pack"
)

type App struct {
	Name      string
	Namespace string
	CnoeDir   string
	GitRef    string
	Helm      *pack.ChartRef
}

func Load(dir string) ([]App, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, diag.Wrap(err, "CUBE-4009", fmt.Sprintf("cannot read %s", dir), "pass a directory of Argo Application YAMLs")
	}
	var apps []App
	for _, e := range entries {
		if e.IsDir() || (filepath.Ext(e.Name()) != ".yaml" && filepath.Ext(e.Name()) != ".yml") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, diag.Wrap(err, "CUBE-4009", "cannot read "+path, "check file permissions")
		}
		docs, err := apply.ParseMultiDoc(raw)
		if err != nil {
			return nil, diag.Wrap(err, "CUBE-4009", path+" is not valid YAML", "fix the document")
		}
		for _, doc := range docs {
			switch doc.GetKind() {
			case "Application":
				app, err := translateApplication(doc, filepath.Dir(path))
				if err != nil {
					return nil, err
				}
				apps = append(apps, *app)
			case "ApplicationSet":
				expanded, err := expandApplicationSet(doc, filepath.Dir(path))
				if err != nil {
					return nil, err
				}
				apps = append(apps, expanded...)
			}
		}
	}
	return apps, nil
}

// Render produces the deliverable for one app; the tag is a 12-char content
// hash so re-imports of changed sources roll forward automatically.
func (a *App) Render(ctx context.Context, cacheDir string) (*pack.Rendered, error) {
	var objs []*unstructured.Unstructured
	var err error
	switch {
	case a.CnoeDir != "":
		objs, err = renderLocalDir(a.CnoeDir)
	case a.GitRef != "":
		var p *pack.Pack
		if p, err = pack.Fetch(ctx, a.GitRef, cacheDir); err == nil {
			var r *pack.Rendered
			if r, err = p.Render(nil); err == nil {
				objs = r.Objects
			}
		}
	case a.Helm != nil:
		objs, err = pack.RenderChart(*a.Helm, a.Helm.Values)
	}
	if err != nil {
		return nil, err
	}
	objs = applyDestinationNamespace(objs, a.Namespace)
	h, err := lock.RenderedHash(objs)
	if err != nil {
		return nil, err
	}
	return &pack.Rendered{Name: "cnoe-" + a.Name, Version: strings.TrimPrefix(h, "sha256:")[:12], Objects: objs}, nil
}

// renderLocalDir renders a cnoe:// directory: kustomization.yaml if present,
// otherwise every YAML document in the directory (recursively).
func renderLocalDir(dir string) ([]*unstructured.Unstructured, error) {
	if _, err := os.Stat(filepath.Join(dir, "kustomization.yaml")); err == nil {
		return pack.RenderDir(dir)
	}
	var objs []*unstructured.Unstructured
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || (filepath.Ext(path) != ".yaml" && filepath.Ext(path) != ".yml") {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return diag.Wrap(err, "CUBE-4009", "cannot read "+path, "check file permissions")
		}
		parsed, err := apply.ParseMultiDoc(raw)
		if err != nil {
			return diag.Wrap(err, "CUBE-4009", path+" is not valid YAML", "fix the manifest")
		}
		objs = append(objs, parsed...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(objs) == 0 {
		return nil, diag.New("CUBE-4010", "cnoe path "+dir+" contains no manifests",
			"point the Application's cnoe:// repoURL at a directory of Kubernetes YAML")
	}
	return objs, nil
}

var clusterScoped = map[string]bool{
	"Namespace": true, "ClusterRole": true, "ClusterRoleBinding": true,
	"CustomResourceDefinition": true, "StorageClass": true, "PriorityClass": true,
	"IngressClass": true, "GatewayClass": true, "PersistentVolume": true,
	"ValidatingWebhookConfiguration": true, "MutatingWebhookConfiguration": true,
}

func applyDestinationNamespace(objs []*unstructured.Unstructured, ns string) []*unstructured.Unstructured {
	if ns == "" {
		return objs
	}
	for _, o := range objs {
		if o.GetNamespace() == "" && !clusterScoped[o.GetKind()] {
			o.SetNamespace(ns)
		}
	}
	nsObj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "Namespace", "metadata": map[string]any{"name": ns}}}
	return append([]*unstructured.Unstructured{nsObj}, objs...)
}
```

(imports also `context` and `github.com/rafpe/cube-idp/internal/lock`.)

`internal/cnoe/translate.go`:

```go
package cnoe

import (
	"fmt"
	"path/filepath"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/rafpe/cube-idp/internal/diag"
	"github.com/rafpe/cube-idp/internal/pack"
)

func translateApplication(doc *unstructured.Unstructured, fileDir string) (*App, error) {
	name := doc.GetName()
	repoURL, _, _ := unstructured.NestedString(doc.Object, "spec", "source", "repoURL")
	rev, _, _ := unstructured.NestedString(doc.Object, "spec", "source", "targetRevision")
	srcPath, _, _ := unstructured.NestedString(doc.Object, "spec", "source", "path")
	chart, _, _ := unstructured.NestedString(doc.Object, "spec", "source", "chart")
	destNS, _, _ := unstructured.NestedString(doc.Object, "spec", "destination", "namespace")
	app := &App{Name: name, Namespace: destNS}

	switch {
	case strings.HasPrefix(repoURL, "cnoe://"):
		rel := strings.TrimPrefix(repoURL, "cnoe://")
		dir := filepath.Join(fileDir, filepath.FromSlash(rel))
		if srcPath != "" && srcPath != "." {
			dir = filepath.Join(dir, filepath.FromSlash(srcPath))
		}
		abs, err := filepath.Abs(dir)
		if err != nil {
			return nil, diag.Wrap(err, "CUBE-4010", "cannot resolve cnoe path for "+name, "check the repoURL")
		}
		if _, err := osStat(abs); err != nil {
			return nil, diag.New("CUBE-4010",
				fmt.Sprintf("application %q points at cnoe://%s but %s does not exist", name, rel, abs),
				"cnoe:// paths are relative to the Application YAML's directory — fix the path")
		}
		app.CnoeDir = abs

	case chart != "":
		app.Helm = &pack.ChartRef{Chart: chart, Repo: repoURL, Version: rev, ReleaseName: name, Namespace: destNS}

	case strings.HasPrefix(repoURL, "https://") || strings.HasPrefix(repoURL, "http://"):
		if rev == "" || rev == "HEAD" || strings.Contains(rev, "*") {
			return nil, diag.New("CUBE-4009",
				fmt.Sprintf("application %q tracks git revision %q, which cube-idp cannot pin", name, rev),
				"set spec.source.targetRevision to a tag or full commit SHA, then re-import")
		}
		host := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(repoURL, "https://"), "http://"), ".git")
		app.GitRef = host + "//" + srcPath + "@" + rev

	default:
		return nil, diag.New("CUBE-4009",
			fmt.Sprintf("application %q has an unsupported repoURL %q", name, repoURL),
			"supported: cnoe://<relative-dir>, https git repos with pinned revisions, and helm chart sources")
	}
	return app, nil
}

// expandApplicationSet supports the list generator (the only one idpbuilder
// setups commonly use); everything else is rejected loudly.
func expandApplicationSet(doc *unstructured.Unstructured, fileDir string) ([]App, error) {
	name := doc.GetName()
	gens, _, _ := unstructured.NestedSlice(doc.Object, "spec", "generators")
	tmpl, ok, _ := unstructured.NestedMap(doc.Object, "spec", "template")
	if !ok || len(gens) == 0 {
		return nil, diag.New("CUBE-4009", fmt.Sprintf("applicationset %q has no generators or template", name),
			"add a list generator and a template, or split it into plain Applications")
	}
	var apps []App
	for _, g := range gens {
		gm, _ := g.(map[string]any)
		listGen, ok := gm["list"].(map[string]any)
		if !ok {
			return nil, diag.New("CUBE-4009",
				fmt.Sprintf("applicationset %q uses a generator cube-idp does not support (only list generators)", name),
				"expand the ApplicationSet into plain Applications and re-import")
		}
		elements, _ := listGen["elements"].([]any)
		for _, el := range elements {
			vars, _ := el.(map[string]any)
			rendered := substitute(tmpl, vars)
			appDoc := &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "argoproj.io/v1alpha1", "kind": "Application",
				"metadata": rendered["metadata"], "spec": rendered["spec"],
			}}
			app, err := translateApplication(appDoc, fileDir)
			if err != nil {
				return nil, err
			}
			apps = append(apps, *app)
		}
	}
	return apps, nil
}

// substitute deep-copies v, replacing {{key}} placeholders in every string.
func substitute(v map[string]any, vars map[string]any) map[string]any {
	out := make(map[string]any, len(v))
	for k, val := range v {
		out[k] = substituteAny(val, vars)
	}
	return out
}

func substituteAny(v any, vars map[string]any) any {
	switch t := v.(type) {
	case string:
		s := t
		for k, val := range vars {
			s = strings.ReplaceAll(s, "{{"+k+"}}", fmt.Sprint(val))
		}
		return s
	case map[string]any:
		return substitute(t, vars)
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = substituteAny(e, vars)
		}
		return out
	default:
		return v
	}
}
```

(`osStat` = `os.Stat` — import `os` in translate.go and call it directly; the alias in the snippet exists only to keep the import list obvious.)

- [x] **Step 5: Implement the command**

`cmd/cnoe.go`:

```go
package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/rafpe/cube-idp/internal/apply"
	"github.com/rafpe/cube-idp/internal/cluster"
	"github.com/rafpe/cube-idp/internal/cnoe"
	"github.com/rafpe/cube-idp/internal/config"
	enginefactory "github.com/rafpe/cube-idp/internal/engine/factory"
	"github.com/rafpe/cube-idp/internal/oci"
	"github.com/rafpe/cube-idp/internal/registry"
)

func newCnoeCmd() *cobra.Command {
	var file string
	parent := &cobra.Command{Use: "cnoe", Short: "Compatibility tools for CNOE/idpbuilder setups"}

	imp := &cobra.Command{
		Use:   "import <dir>",
		Short: "Ingest idpbuilder Argo Application/ApplicationSet YAMLs into the running cube (cnoe:// paths become OCI pushes)",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			cube, err := config.Load(file)
			if err != nil {
				return err
			}
			apps, err := cnoe.Load(args[0])
			if err != nil {
				return err
			}
			prov, err := cluster.New(cube.Spec.Cluster, cube.Spec.Gateway)
			if err != nil {
				return err
			}
			conn, err := prov.Ensure(c.Context(), cube.Metadata.Name, cube.Spec.Cluster)
			if err != nil {
				return err
			}
			a, err := apply.New(conn.REST, cube.Metadata.Name)
			if err != nil {
				return err
			}
			eng, err := enginefactory.New(cube.Spec.Engine.Type)
			if err != nil {
				return err
			}
			tunnelAddr, stop, err := registry.PortForward(c.Context(), conn.REST)
			if err != nil {
				return err
			}
			defer stop()
			// RECONCILE: reuse the phase-1 cacheDir helper per checkpoint 0.13 /
			// Task 7's relocation of it (pack.DefaultCacheDir()).
			for _, app := range apps {
				rendered, err := app.Render(c.Context(), pack.DefaultCacheDir())
				if err != nil {
					return err
				}
				artifact, err := oci.PushRendered(c.Context(), rendered, tunnelAddr)
				if err != nil {
					return err
				}
				deliverObjs, err := eng.Deliver(c.Context(), rendered, artifact)
				if err != nil {
					return err
				}
				if err := a.Apply(c.Context(), deliverObjs, false, 2*time.Minute); err != nil {
					return err
				}
				if err := a.RecordInventory(c.Context(), deliverObjs); err != nil {
					return err
				}
				fmt.Fprintf(c.OutOrStdout(), "▸ [cnoe] %s imported as %s@%s\n", app.Name, rendered.Name, rendered.Version)
			}
			fmt.Fprintf(c.OutOrStdout(), "✔ %d application(s) imported — `cube-idp status` tracks their health\n", len(apps))
			return nil
		},
	}
	parent.PersistentFlags().StringVarP(&file, "file", "f", "cube.yaml", "path to cube.yaml")
	parent.AddCommand(imp)
	return parent
}
```

(import `github.com/rafpe/cube-idp/internal/pack`.) Register `newCnoeCmd()` in `cmd/root.go`.

- [x] **Step 6: Run tests**

Run: `go test ./internal/cnoe/ ./internal/pack/ -short -v && go build ./...`
Expected: PASS

- [x] **Step 7: Commit**

```bash
git add -A && git commit -m "feat: cnoe-compat loader — Application/ApplicationSet ingestion, cnoe:// to OCI pushes"
```

---

### Task 13.5: Port the helm wrapper to the v4 SDK (debt paydown — skippable per spec §7)

**Reconcile checkpoint:** 0.11 + 0.19 (**blocking**: the v3 symbol inventory `helm.go` actually uses, and the helm v4 SDK's release state at implementation time), Task 13 merged (`ChartRef`/`RenderChart` are now public API this port must NOT change).

**Skip clause (record the decision either way):** spec §7 accepts staying on v3 ("can pin to v3 SDK if v4 breaks"). If checkpoint 0.19 finds the v4 action/loader packages missing anything `helm.go` needs, mark this task skipped in this document with the blocking symbol named, and move on — do not force it.

**Files:**
- Modify: `internal/pack/helm.go` (imports + adapted call sites only — the wrapper surface `renderHelm`/`RenderChart`/`ChartRef` is frozen), `go.mod`
- Test: none new — the existing pack golden-file render tests ARE the port's acceptance suite

- [x] **Step 1: Freeze the baseline**

Run: `go test ./internal/pack/ -short -v 2>&1 | tee /tmp/helm-v3-baseline.txt`
Expected: PASS. This output (and the golden files themselves) define "unchanged behavior" for the port.

- [x] **Step 2: Swap the SDK**

```bash
go get helm.sh/helm/v4@latest
```

In `internal/pack/helm.go`, change every `helm.sh/helm/v3/...` import to `helm.sh/helm/v4/...` and adapt call sites mechanically per the checkpoint 0.19 inventory (the v4 SDK reorganized the action package; chart loading and the in-process template/install-dry-run path are the two seams to re-verify). `helm.go` remains the ONLY file importing the Helm SDK — that invariant is the whole point of the wrapper.

```bash
go mod tidy && grep -c 'helm.sh/helm/v3' go.mod
```

Expected: `0`.

- [x] **Step 3: Re-run the acceptance suite**

Run: `go test ./internal/pack/ ./internal/cnoe/ -short -v && go build ./...`
Expected: PASS with the golden files untouched. If a golden diff appears, inspect it: rendering differences between v3 and v4 must be understood and accepted deliberately (update the golden with a comment naming the v4 change), never waved through.

- [x] **Step 4: Commit**

```bash
git add -A && git commit -m "refactor: helm wrapper on the v4 SDK (debt paydown)"
```

---

### Task 13.8: Terminal UX pass — lipgloss status output, huh init wizard, `--plain` (NEW in the 2026-07-13 amendment)

**Reconcile checkpoint:** 0.12 (cmd conventions, `init` flag layout), 0.13 (**blocking**: the exact `step` helper signature every orchestrator uses — this task replaces its implementation).

**Design rule (keeps Task 14 stable):** the *current* phase-1 output format IS the plain format. Styled output is additive: `ui.New` picks styled only when stdout is a TTY AND `--plain` is absent AND `CI` is unset. Piped output (every e2e `run` helper, every CI log) therefore stays byte-compatible with today — no e2e assertion churn.

**Files:**
- Create: `internal/ui/ui.go`
- Modify: `internal/up/up.go` (+ every orchestrator using `step`: diff/upgrade/trust/doctor/cnoe/down), `cmd/root.go` (`--plain` persistent flag), `cmd/init.go` (huh wizard)
- Test: `internal/ui/ui_test.go`

**Interfaces:**
- Produces:

```go
package ui
func New(out io.Writer, plain bool) *Printer  // plain also auto-forced when out is not a TTY or $CI is set
type Printer struct{ /* unexported */ }
func (p *Printer) Step(name, format string, args ...any) // replaces the phase-1 step() body
```

- [x] **Step 1: Write the failing plain-format test**

`internal/ui/ui_test.go` — the plain format is pinned to the phase-1 `step` output byte-for-byte (`RECONCILE:` copy the real format string from checkpoint 0.13 into this test before running it):

```go
package ui

import (
	"bytes"
	"strings"
	"testing"
)

func TestPlainMatchesPhase1Format(t *testing.T) {
	var b bytes.Buffer
	p := New(&b, true)
	p.Step("tls", "gateway certificate ready")
	// RECONCILE: assert the EXACT phase-1 step() line, e.g. "▸ [tls] gateway certificate ready\n"
	if got := b.String(); !strings.Contains(got, "[tls] gateway certificate ready") {
		t.Fatalf("plain output drifted from the phase-1 format: %q", got)
	}
	if strings.Contains(b.String(), "\x1b[") {
		t.Fatal("plain mode must emit zero ANSI escapes")
	}
}

func TestNonTTYWriterForcesPlain(t *testing.T) {
	var b bytes.Buffer // a bytes.Buffer is never a TTY
	p := New(&b, false)
	p.Step("dns", "ready")
	if strings.Contains(b.String(), "\x1b[") {
		t.Fatal("non-TTY output must be plain even without --plain")
	}
}
```

- [x] **Step 2: Run tests to verify they fail, then implement**

Run: `go test ./internal/ui/ -v` — Expected: FAIL (package missing).

```bash
go get github.com/charmbracelet/lipgloss@latest github.com/charmbracelet/huh@latest
```

`internal/ui/ui.go`: `New` detects TTY (`term.IsTerminal` on the writer's Fd when it is an `*os.File`; any other writer → plain) and `os.Getenv("CI")`; plain mode reproduces the phase-1 `step` format verbatim; styled mode renders the same content with a lipgloss-styled name badge and dim message — content identical, only presentation differs. No Bubble Tea, no spinner loop: "a spinner is not a TUI" (spec §4.1).

- [x] **Step 3: Thread it through**

`cmd/root.go`: add `--plain` as a persistent flag; construct the `*ui.Printer` once and hand it to every orchestrator (RECONCILE: mirror how phase-1 threads `out io.Writer` — the Printer replaces the writer+`step` pair, or wraps the writer if orchestrators keep `io.Writer` signatures; choose whichever touches fewer signatures and note the choice here). Delete the phase-1 `step` helper; all orchestrators call `p.Step`.

`cmd/init.go`: when stdin+stdout are TTYs and neither `--name` nor `--engine` was passed, run a huh form (name input; engine select flux/argocd; "include gitea?" confirm — the D9 default profile pre-checked). Flags always win; non-TTY always skips the wizard (CI-safe). RECONCILE: huh's form API at the pinned version; keep the wizard to ONE form, three fields.

- [x] **Step 4: Run everything**

Run: `go test ./internal/ui/ ./cmd/ -v && go build ./... && go test ./... -short`
Expected: PASS — and specifically, every pre-existing cmd/e2e output assertion still passes untouched (the design rule above is the guarantee; if one breaks, the plain format drifted — fix ui.go, not the assertion).

- [x] **Step 5: Commit**

```bash
git add -A && git commit -m "feat: terminal UX pass — lipgloss status output, huh init wizard, --plain for CI"
```

---

### Task 14: E2E engine matrix, new-command coverage, CI, README

**Reconcile checkpoint:** 0.16 (e2e harness + CI shape), 0.12 (init flags incl. `--local` resolution), all previous tasks merged.

**Files:**
- Modify: `tests/e2e/e2e_test.go`, `.github/workflows/ci.yaml`, `README.md`, `Makefile` (ensure `test-engines` runs in CI)

- [x] **Step 1: Extend the e2e loop with the engine matrix and Phase 2 commands**

Rework `tests/e2e/e2e_test.go`'s main test (keep the `build`/`run` helpers; RECONCILE: keep the phase-1 `init` invocation incl. its `--local`-style flag exactly as checkpoint 0.12 found it — the `--engine` flag from Task 2 is appended to it):

```go
func TestUpStatusDown(t *testing.T) {
	if os.Getenv("CUBE_IDP_E2E") != "1" {
		t.Skip("set CUBE_IDP_E2E=1 to run")
	}
	eng := os.Getenv("CUBE_IDP_E2E_ENGINE")
	if eng == "" {
		eng = "flux"
	}
	bin := build(t)
	dir := t.TempDir()

	run(t, dir, bin, "init", "--name", "e2e", "--engine", eng)
	run(t, dir, bin, "doctor")               // preflights must pass on a clean runner
	run(t, dir, bin, "up")
	run(t, dir, bin, "up")                   // idempotency: re-run is the upgrade command

	// Phase 2: cube.lock written and well-formed
	lockRaw, err := os.ReadFile(dir + "/cube.lock")
	if err != nil || !strings.Contains(string(lockRaw), "renderedHash") {
		t.Fatalf("cube.lock missing or malformed: %v\n%s", err, lockRaw)
	}

	// Phase 2: a converged cube has no diff and no pending upgrades (exit 0)
	run(t, dir, bin, "diff")
	run(t, dir, bin, "upgrade", "--plan")

	out := run(t, dir, bin, "status")
	for _, comp := range []string{"traefik", "gitea"} {
		if !strings.Contains(out, comp) {
			t.Fatalf("status missing %s:\n%s", comp, out)
		}
	}
	if eng == "flux" { // argocd engine drops the redundant UI pack (CUBE-0005)
		if !strings.Contains(out, "argocd") {
			t.Fatalf("status missing argocd pack:\n%s", out)
		}
	}

	// Phase 2: HTTPS gateway — TLS handshake serves the cube-idp CA-issued cert
	assertGatewayTLS(t, "cube-idp.localtest.me:8443")

	// Phase 2 (D11): pack records are discoverable via plain kubectl
	packs := runKubectl(t, "get", "packs")
	for _, want := range []string{"gitea", "VERSION", "URL", "AUTH-SECRET", "READY"} {
		if !strings.Contains(packs, want) {
			t.Fatalf("kubectl get packs missing %q (D11 printer columns):\n%s", want, packs)
		}
	}

	// Phase 2: cnoe-compat import round-trips
	writeCnoeFixture(t, dir)
	run(t, dir, bin, "cnoe", "import", dir+"/cnoe-apps")

	// D9 + D11: the admin credential surfaces via the Pack -> authSecretRef
	// pivot, and gitea_admin arrives through expose.impliedFields
	secrets := run(t, dir, bin, "get", "secrets", "-p", "gitea")
	if !strings.Contains(secrets, "gitea_admin") {
		t.Fatalf("gitea admin secret not surfaced (D9/D11):\n%s", secrets)
	}
	run(t, dir, bin, "down")
}
```

Add the three helpers to the file:

```go
// runKubectl asserts against the cluster with plain kubectl (the D11 pitch
// is literally "kubectl get packs works"). GitHub runners ship kubectl;
// locally the test skips if it is absent. kind's Ensure wrote the context
// into the default kubeconfig.
func runKubectl(t *testing.T, args ...string) string {
	t.Helper()
	if _, err := exec.LookPath("kubectl"); err != nil {
		t.Skip("kubectl not on PATH")
	}
	out, err := exec.Command("kubectl", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("kubectl %v: %v\n%s", args, err, out)
	}
	return string(out)
}
```

(import `os/exec`.)

```go
// assertGatewayTLS dials the gateway and verifies the served cert chains to
// the cube-idp local CA and covers the wildcard host — the D6 story minus
// the OS trust store (never touched in CI).
func assertGatewayTLS(t *testing.T, addr string) {
	t.Helper()
	caPath := filepath.Join(mustUserConfigDir(t), "cube-idp", "ca.crt")
	caPEM, err := os.ReadFile(caPath)
	if err != nil {
		t.Fatalf("cube-idp CA missing after up: %v", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		t.Fatal("cannot parse cube-idp CA")
	}
	conn, err := tls.Dial("tcp", addr, &tls.Config{RootCAs: pool, ServerName: "gitea.cube-idp.localtest.me"})
	if err != nil {
		t.Fatalf("TLS handshake with the gateway failed: %v", err)
	}
	conn.Close()
}

func writeCnoeFixture(t *testing.T, dir string) {
	t.Helper()
	appDir := filepath.Join(dir, "cnoe-apps", "manifests")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(appDir, "cm.yaml"),
		[]byte("apiVersion: v1\nkind: ConfigMap\nmetadata: {name: cnoe-smoke}\ndata: {ok: \"true\"}\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "cnoe-apps", "app.yaml"), []byte(`apiVersion: argoproj.io/v1alpha1
kind: Application
metadata: {name: smoke}
spec:
  destination: {namespace: cnoe-smoke, server: https://kubernetes.default.svc}
  source: {repoURL: "cnoe://manifests", targetRevision: HEAD, path: "."}
`), 0o644)
}

func mustUserConfigDir(t *testing.T) string {
	t.Helper()
	d, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	return d
}
```

(imports: `crypto/tls`, `crypto/x509`, `path/filepath`.)

- [x] **Step 2: CI — engine matrix + contract suite**

`.github/workflows/ci.yaml` — extend the two jobs:

```yaml
jobs:
  unit:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: {go-version-file: go.mod}   # Task 0 finding 0.16: go.mod is the single source of truth (go 1.26.x)
      - run: go vet ./...
      - run: go test ./... -short
      - run: make test-apply
      - run: make test-engines   # contract suite incl. envtest subtests, both engines
  e2e:
    runs-on: ubuntu-latest
    timeout-minutes: 30
    strategy:
      fail-fast: false
      matrix:
        engine: [flux, argocd]   # spec §5: {kind} x {flux, argocd} x {up, diff, upgrade, down}
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: {go-version-file: go.mod}   # Task 0 finding 0.16: go.mod is the single source of truth (go 1.26.x)
      - run: CUBE_IDP_E2E=1 CUBE_IDP_E2E_ENGINE=${{ matrix.engine }} go test ./tests/e2e/ -v -timeout 25m
```

- [x] **Step 3: README additions**

Extend `README.md` with (each a short section, copy wording from the spec where it exists):
- `Engines`: `engine.type: flux | argocd`, both pass the same contract suite (D2); the argocd engine's OCI requirement (or its gitea fallback, per what Task 2 verification concluded).
- `HTTPS & trust`: `up` gives you real HTTPS from first boot with a local CA generated before the cluster exists (D12); an existing mkcert root is adopted automatically (green locks with zero prompts); `cube-idp trust` (opt-in, consent-prompted) makes browsers trust a generated CA; `down` reverts everything (D6). Note the Phase 1→2 port-mapping change (8443 now serves TLS).
- `Day 2`: `diff`, `upgrade --plan`, `doctor`, `cube.lock` semantics (what `resolved`/`renderedHash`/`images` mean, that the file should be committed).
- `Pack sources`: local dir, `oci://`, `github.com/org/repo//path@rev` (pinned), and explicit go-getter URLs (`git::…`, `s3::…`, `https://…` archives); note the git CLI requirement for git sources and that every fetched tree passes cube-idp's extraction guards.
- `Pack discoverability (D11)`: `kubectl get packs` shows every installed pack's version, URL, credential secret, and readiness — sourced from the pack's `expose:` block in `pack.cue`; document the block's shape and the `${GATEWAY_HOST}` substitution. `cube-idp get secrets` follows `expose.authSecretRef` (the `cli-secret` label is deprecated, one release of grace).
- `Terminal output`: styled status lines on a TTY; `--plain` (or piped output / `CI=true`) yields stable machine-readable lines; `cube-idp init` runs a short interactive wizard on a TTY when flags are absent.
- `Migrating from idpbuilder`: `cube-idp cnoe import ./your-packages` — what translates, what is rejected (unpinned revisions, non-list ApplicationSet generators) and why.

- [x] **Step 4: Full verification + commit**

Run locally:
```
go vet ./... && go test ./... -short && make test-apply && make test-engines
CUBE_IDP_E2E=1 CUBE_IDP_E2E_ENGINE=flux   go test ./tests/e2e/ -v -timeout 25m
CUBE_IDP_E2E=1 CUBE_IDP_E2E_ENGINE=argocd go test ./tests/e2e/ -v -timeout 25m
```
Expected: all green; both engine matrices prove up→up→diff→upgrade --plan→cnoe import→down and the gateway serves CA-issued TLS.

```bash
git add -A && git commit -m "feat: e2e engine matrix, Phase 2 command coverage, CI and README updates"
```

---

## Self-Review

### 1. Spec coverage — spec §6 Phase 2 items → tasks

| Spec §6 Phase 2 item | Tasks | Notes |
|---|---|---|
| Argo CD engine implementation + contract suite (D2) | 1, 2, 14 | Suite first (flux seeds it), argocd passes the identical suite, CI runs both engines e2e. CUBE-3002 removed. Spec §7 OCI-maturity risk handled as a verification step + documented gitea-pack fallback (engine-specific requirement, CUBE-3006), built only if the primary path fails. |
| TLS from first boot (D12) | 8, 9, 10 | CA ensured as `up.Run`'s FIRST step, before cluster creation (Task 9 Step 5 D12 ordering); mkcert root adoption with RSA/PKCS#8 key support (Task 8 Steps 3b/3c); CA mounted into containerd certs.d at create time (Task 10); OS trust store untouched until `cube-idp trust` (D6 unchanged). |
| `cube-idp trust` + CoreDNS canonical hostname (D6) | 8, 9, 10, 11 | CA/leaf/state (8), HTTPS gateway completing the Phase 1 deferral (9), CoreDNS rewrite (overridable via `gateway.host`) + containerd certs.d via the kind provider + registry route (10), opt-in consent-gated OS install with full revert on `down` (11). |
| `Pack` discoverability CRD + `expose:` contract (D11) | 12.5, 14 | One inert cluster-scoped CRD with printer columns (NAME/VERSION/URL/AUTH-SECRET/READY), `expose:` block in pack.cue (CUBE-4011), `${GATEWAY_HOST}` substitution, `get secrets` pivot with one-release label grace, gitea/argocd blocks shipped, `kubectl get packs` asserted in e2e. |
| `diff` | 6 | Applier/SSA dry-run + inventory orphans + lock-hash pack drift; kubectl-diff exit convention. |
| `doctor` with CUBE codes | 12 | Runtime/ports/disk/inotify preflights + provider `Diagnose` + engine `Health`; spec §5 fault-injection tests (port squat, missing runtime, broken kubeconfig via the provider path). Task 4 Step 6 adds the git-CLI warning seam. |
| `cube.lock` + `upgrade --plan` | 5, 7 | Digests + full image list + rendered hashes written by `up`; plan = remote pin re-resolution vs lock + kernel diff; feeds Phase 3 `vendor`. |
| go-getter pack sources (git/http/s3/OCI) | 4, 5, 7 | RafPe fork (v1.9.0 replace, verified consumption path), extraction guards (CUBE-4014) on ALL getter output, bare git grammar kept (`host/org/repo//path@rev`, CUBE-4006/4007), `oci://` stays on oras (digest + plain-HTTP — fork's getter exposes neither), pins via one shared `resolveGitPin`/`dirPin`. |
| cnoe-compat loader | 13 | Application + list-generator ApplicationSet, `cnoe://` → OCI pushes, engine-neutral delivery, typed rejections. |
| Kustomize overlays (Phase 1 deferral) | 3 | `kustomization.yaml` precedence rule; `RenderDir` reused by cnoe. |
| Debt paydown (fundamentals review) | 0.5, 3.5, 13.5 | CUBE-code catalog + literal-ban test FIRST so all new code uses it (0.5); all OCI on oras-go v2, `fluxcd/pkg/oci` dropped, flux media types preserved byte-compatibly (3.5); helm v3→v4 behind the frozen wrapper, golden tests as acceptance, explicitly skippable per spec §7 (13.5). |
| Terminal UX pass | 13.8 | lipgloss styled output on TTY only; plain format pinned byte-for-byte to the phase-1 `step` output so e2e/CI never churn; huh init wizard (TTY-only, flags win); `--plain` persistent flag. |

Phase-2-adjacent items deliberately NOT here (Phase 3 per spec §6): k3d provider, `vendor`/`--bundle`, exec plugins, `sync --watch`, `repo create`, pack catalog buildout.

### 2. Placeholder scan

No TBD/TODO/"handle later" items. All deferrals are explicit `RECONCILE:` markers, each naming exactly what to verify against the Phase 1 code and what to adjust — the load-bearing ones: apply-labeling factoring and ssa Diff entry point (Task 6), `pullOCI` digest threading (Task 5), Argo CD version/OCI capability + repo-secret fields + fresh-cluster Health semantics (Task 2), truststore API (Task 11), CoreDNS `answer auto` availability (Task 10), certs.d localhost-NodePort reachability with a bind-mount rewrite fallback (Task 10), traefik chart values/Service name/listener ports (Tasks 9/10), cacheDir helper location (Task 7/13), `init` flag layout (Tasks 2/14), gateway-namespace convention (Task 9). Task 0 requires resolving or confirming every one before execution.

Markers added by the 2026-07-13 amendment: fork `Client` field set + consumption re-check (Task 4 / checkpoint 0.18), flux artifact media types + archive entry layout (Task 3.5 — the e2e is the arbiter), the pack.cue CUE-validation hook for `expose:` (Task 12.5 / checkpoint 0.8), the Deliver-name → health-name mapping feeding Pack READY (Task 12.5 / checkpoint 0.9), mkcert key-format fallback chain (Task 8 Step 3c), the phase-1 `step` format string + writer-threading choice (Task 13.8 / checkpoint 0.13), the helm v4 symbol inventory and its skip decision (Task 13.5 / checkpoint 0.19), and the `up` step-ordering test seam (Task 9, D12 paragraph).

### 3. Type consistency

- `engine.Engine` methods used by Tasks 1/2/6/12/13 match the Phase 1 signature set (`Install/Deliver/Health/Uninstall` with `*apply.Applier` + timeouts); `ArtifactRef{Repo,Tag}` and `ComponentHealth{Name,Ready,Message}` used consistently.
- `pack.Pack` gains exactly one field (`Pinned`, Task 4) consumed by Tasks 5/7; `pack.Rendered{Name,Version,Objects}` unchanged and used by Tasks 1/2/6/13.
- `lock.Entry` fields written in Task 5's `up` integration are the same set read in Task 6 (`RenderedHash`) and Task 7 (`Resolved`, via `lockEntryByRef`).
- `trust.CA`/`State` produced in Task 8 are consumed by Tasks 9 (`EnsureCA`, `IssueServerCert`, `LeafStillValid`), 10 (`WriteCertsD` + `Dir`), 11 (`InstallOS/UninstallOS`, `LoadState`).
- `kindp.RenderConfig` signature changes once (Task 10 adds `CertsD`); Task 10 explicitly lists both call sites (`kind.go`, `cmd/config.go`) and the merge-test updates.
- `apply.Change` produced by Task 6 is consumed only inside `internal/diff`; `diff.Run`'s `(changed bool, err error)` shape is reused by `upgrade.Plan` (Task 7).
- `resolveGitPin`, `dirPin`, `fetchGetter`, `sanitizeRef` (Tasks 4/5) are single implementations consumed by `Fetch` AND Task 7's `ResolveRemote` — one ls-remote, one dirhash, everywhere.
- `oci.PushRendered(ctx, r, registryAddr) (engine.ArtifactRef, error)` is frozen across the Task 3.5 rewrite; `up` and Task 13's cnoe import compile untouched.
- `pack.Expose`/`SecretRef` (Task 12.5) are consumed only by `PackObject` and `cmd/get.go`; `Pack` gains exactly two fields across the whole phase (`Pinned` in Task 4, `Expose` in Task 12.5).
- `ui.Printer.Step(name, format, args...)` (Task 13.8) replaces the phase-1 `step` helper in every orchestrator with the same argument shape, so the sweep is mechanical.

### 4. Known risks carried forward

- **Argo CD OCI source maturity** (spec §7): explicitly a Task 2 verification gate with a designed fallback; the contract suite is deliberately delivery-mechanism-agnostic (it asserts the artifact is referenced, not how) so the fallback would still pass it after adjusting the `deliver_references_the_artifact` URL expectation — if the fallback activates, amend that subtest's expectation as part of the fallback task.
- **certs.d node-reachability** (Task 10): the localhost-NodePort assumption is the weakest guess in this plan; the fallback (rewriting the bind-mounted hosts.toml with the node IP after create) is designed but unbuilt. Verify early in Task 10's e2e pass.
- **Port-mapping migration**: Phase 2 changes the kind gateway mapping (30080→30443); existing Phase 1 clusters need `down`/`up`. Pre-1.0 acceptable; release-noted (Task 9/README).
- **go-getter fork maintenance** (Task 4): the dependency is a single-maintainer fork (tag v1.9.0), consumed via a `replace` because its go.mod declares the upstream module path. Upstream security fixes do not flow in automatically — checkpoint 0.18 re-verifies the pin every reconcile, and the extraction guards (CUBE-4014) exist precisely because getter output is not blindly trusted. If the fork gains digest exposure + plain-HTTP on its OCI getter, collapsing the `oci://` branch onto go-getter is a designed follow-up, not a speculative build.
- **git CLI runtime dependency** (Task 4): go-getter's git getter shells out to `git`. Scoped to git pack sources only (everything else stays binary-pure); surfaced as CUBE-4006 remediation + the Task 12 doctor warning; README-documented.
- **Flux media-type byte-compat** (Task 3.5): the oras rewrite must reproduce what `fluxcd/pkg/oci` pushed or flux stops syncing packs; the reconcile captures the real manifest and the engine-matrix e2e arbitrates.
- **Helm v4 SDK youth** (Task 13.5, unchanged from spec §7): the port is explicitly skippable with a recorded decision; the wrapper keeps the blast radius to one file.
