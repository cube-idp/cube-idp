# cube-idp Phase 4 Implementation Plan — First Release (v0.1.0)

> **STATUS: DRAFT — READY FOR EXECUTION AFTER TASK 0.** Written 2026-07-15 against main @ `07d6471` with every seam verified against the live tree at authoring time (the Ground Truth section below records exactly what was found, file:line). Task 0 re-verifies that section against the then-current tree before any task runs — cheap, because the answers are pre-recorded; it only has to confirm or adjust.
>
> Throughout this document, `RECONCILE: …` marks a statement that depends on something not verifiable from the tree at authoring time and says exactly what to verify. That is the only allowed deferral form in this plan — there are no TBDs.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. **Task 0 is blocking: no other task may start before it is checked off.** If executed subagent-driven, every task goes through the Phase 3 gate: implementer report → spec+quality review → fix loop before merge. Agent worktrees may spawn stale — ALWAYS `git reset --hard main` first and verify a named recent file exists (e.g. `internal/ui/pipeline.go`) before writing anything. The execution ledger lives at `.superpowers/sdd/progress.md`; every task appends one line to it.

**Goal:** Ship cube-idp **v0.1.0** as a **private** GitHub Release: versioned multi-platform binaries (darwin/linux × arm64/amd64, checksummed), a generated changelog, install documentation — on the **unchanged `cube-idp.dev/v1alpha1` config schema** — with the entire Phase 3 review backlog burned down and known behavioral warts fixed **without breaking any existing cube.yaml** (spec §1, P4-D1..D4).

**Architecture:** No new architecture. Release engineering (R1, R10) is pure build/CI machinery around the existing single static binary; every other task extends a proven seam: bundle integrity (R2) hardens `internal/bundle`'s existing Manifest/Verify; the event-stream migration (R3) repeats Task 14b's Console-facade technique on five more commands; the diag sweep (R4) is catalog bookkeeping plus two allocations; plugin polish (R5) hardens `internal/plugin`'s existing trust/index paths; D15 kustomize substitution (R6) extends `internal/pack`'s existing byte-level `substitute()` to the third render path; gateway coherence (R7) is the ONE design-bearing task and its design is fixed in the spec (§5.7): a D11-style optional `pack.cue` `gatewayService:` block consumed by `up`'s CoreDNS step. Execution shape is a **sequential single lane, R1 first** (P4-D6) so every later merge is release-candidate-testable via the snapshot build.

**Tech Stack:** Go per `go.mod` (currently 1.26.2 — never hardcode; `go-version-file: go.mod` in CI), goreleaser v2 (NEW external tool, R1 — config + CI action only, not a Go dependency), and the existing stack: cobra, cuelang, fluxcd/pkg/ssa, oras-go v2 (the only production OCI library), helm v4, kind v0.32.0, k3d v5.8.3, go-git v5, smallstep/truststore, charm.land lipgloss/huh/bubbletea v2, client-go, `golang.org/x/mod/sumdb/dirhash` (already a direct dep — R2 reuses it). go-containerregistry stays **test-only**. **No new Go dependencies in this phase.**

**Spec:** `docs/superpowers/specs/2026-07-15-cube-idp-phase4-first-release-design.md` (P4-D1..D6, §5 tasks R1–R10) — source of truth. Parent: `docs/superpowers/specs/2026-07-13-cube-idp-architecture-design.md` (D1–D15). UX contracts: `docs/superpowers/specs/2026-07-14-cube-idp-ux-design.md` (R3 builds on its §3–§6, §11). Phase 3 record: `docs/superpowers/plans/2026-07-13-cube-idp-phase3-draft.md` (findings F1–F11 + final-review backlog) and `.superpowers/sdd/progress.md`.

## Global Constraints (every task inherits these — spec §4, restated verbatim in substance)

- Module `github.com/rafpe/cube-idp`, Go per `go.mod` (currently 1.26.x); never hardcode a Go version in CI (`go-version-file: go.mod`).
- Every user-facing failure is a typed `CUBE-xxxx` `diag.Error` with remediation; every code is a constant in `internal/diag/codes.go`; `TestNoCubeLiteralsOutsideCatalog` bans literals in non-test Go code. **Range `8xxx` is reserved for this phase** (release/bundle-integrity codes) — grep the catalog before allocating. This plan's code snippets show literals for readability; implementers MUST use the catalog constants.
- Plain-mode output is byte-stable and golden/e2e-pinned; new output is additive and routed through `internal/ui` (`Console`/`Printer`/event stream per the UX design `docs/superpowers/specs/2026-07-14-cube-idp-ux-design.md`). Any deliberate plain-output change must be named in the task body and its affected test updated in the same commit, never silently.
- TDD per step; `go build ./... && go vet ./... && go test ./... -short -count=1` green before every commit; conventional commits (`feat:`, `fix:`, `test:`, `build:`, `ci:`, `docs:`, `chore:`) ending:

  ```
  Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
  ```
- e2e: gated by `CUBE_IDP_E2E=1`; locally ALWAYS set `CUBE_IDP_E2E_GATEWAY_PORT=18443` (8443 is squatted on the dev machine by the user's `testowy` cluster); **never touch clusters named `testowy`, `airbyte*`, `xxx`, `envoy-dbg`**; test clusters use `e2e-`/`fix-` prefixes and must never leak.
- SSA field manager `cube-idp`; inventory merge semantics (`RecordInventory` unions); prune opt-out annotation `cube-idp.dev/prune: disabled` unchanged.
- **Sequential order R1→R10 is BINDING (P4-D6)** — no parallel streams, no reordering. R1 lands first so every subsequent merge is buildable into a release candidate by the snapshot CI job.
- Schema: `apiVersion: cube-idp.dev/v1alpha1` unchanged (P4-D3). No cube.yaml field is added, renamed, or removed. The ONE sanctioned format addition is the `pack.cue` `gatewayService:` block (R7b) — pack format, not cube.yaml.
- Non-goals (P4-D1, spec §3): no public-launch work, no schema freeze/`migrate`, no new providers/RPC/operator, no new packs.

## CUBE-code allocations (declared up front; each defined at its point of use)

- **R2 reuses `CUBE-7003` (`CodeVendorBundleCorrupt`) and `CUBE-7004` (`CodeVendorIncomplete`)** — no new codes: v1-bundle rejection and extraction-cap trips are 7003; every content-hash mismatch is 7004 naming the exact entry.
- **R4 allocates exactly two new codes** (verified free at authoring time — in-use 3xxx: 3001, 3003–3007; in-use 70xx: 7001–7005):
  - `CUBE-3008` `CodePokeIOFail` — Poke found the delivery source but could not read/update it (transient engine IO); un-overloads `CUBE-3007`, which stays "target missing" only.
  - `CUBE-7006` `CodeBundleImageLoadFail` — bundled image load into cluster nodes failed (kindp/k3dp `LoadImages` consume side); un-overloads `CUBE-7002`, which stays vendor-side (produce) only.
- **R4 renames the `CUBE-1003` constant** `CodeClusterSetupFailed` → `CodeClusterFieldsConflict` — the code VALUE stays `CUBE-1003` (user-facing strings unchanged); only the Go identifier and comment change to match its one real use (config cross-validation, `internal/config/load.go:99`).
- **R5 allocates `CUBE-7105`** `CodePluginNameInvalid` — plugin name fails the `^[a-z0-9][a-z0-9-]*$` charset guard on `plugin install`/`plugin trust` (71xx family; 7101–7104 in use).
- **R7b allocates NOTHING** — a malformed `gatewayService:` block reuses `CUBE-4003` (`CodePackCueInvalid`), exactly as the D14 `images:` list does (`internal/pack/pack.go:113-119` precedent).
- **8xxx: NO ALLOCATIONS.** Release engineering (R1/R10) lives entirely in build config, CI yaml, and docs — no Go error path is added, so no code is needed. The range stays reserved and empty; this is deliberate and this line is the record of that decision.

## File Structure (new/modified in Phase 4)

```
.goreleaser.yaml                        # R1: NEW — builds/archives/checksums/changelog
.github/workflows/release.yaml          # R1: NEW — tag-triggered goreleaser release
.github/workflows/ci.yaml               # R1: + release-snapshot job (every push/PR)
CHANGELOG.md                            # R1: NEW — curated v0.1.0 seed
cmd/version.go                          # R1: Commit/Date vars + extended output
internal/bundle/bundle.go               # R2: Manifest v2 (packHashes/imageHashes),
                                        #     Open v1-reject, Verify content hashes,
                                        #     extractTarGz size caps
internal/bundle/vendor.go               # R2: hash computation at vendor time
                                        # R3: Vendor signature Printer→Console
                                        # R8: isLocalRegistryHost copy deleted
internal/ui/pipeline.go                 # R3: RunPipelineStatic (no-live variant)
internal/ui/render/styled.go            # R3: NEW — styled static projection
cmd/vendor.go cmd/sync.go cmd/repo.go
cmd/plugin.go cmd/pack.go               # R3: RunPipeline/RunPipelineStatic wraps
internal/syncer/syncer.go               # R3: Deps.Steps emitter seam
                                        # R8: synthesized-pack temp-dir cleanup
docs/machine-readable-output.md         # R3: per-command stream coverage (same
                                        #     commits); R9: encode_error + re-verify
internal/diag/codes.go                  # R4: 3008, 7006, 1003 rename, header ranges
internal/diag/diag.go                   # R4: package-doc range list 6xxx/7xxx/8xxx
internal/diag/codes_test.go             # R4: backtick ban + exhaustiveness test
internal/config/load.go                 # R4: CUBE-1003 constant rename call site
internal/cluster/kindp/kind.go          # R4: CUBE-7002→7006 (2 sites)
internal/cluster/k3dp/k3d.go            # R4: CUBE-7002→7006 (1 site)
internal/engine/flux/poke.go            # R4: CUBE-3007→3008 (2 sites)
internal/engine/argocd/poke.go          # R4: CUBE-3007→3008 (2 sites)
internal/plugin/trust.go                # R5: canonical trust-store keys
internal/plugin/index.go                # R5: 60s-timeout HTTP client
cmd/plugin.go                           # R5: name charset guard (also R3 target)
internal/pack/kustomize.go              # R6: RenderDirFor(dir, gw) substitution
internal/pack/render.go                 # R6: kustomize branch passes gw
cmd/init.go                             # R7a: --gateway-pack; single gateway source
internal/pack/pack.go                   # R7b: GatewayService field + parsing
internal/up/up.go                       # R7b: CoreDNS target from pack declaration
                                        # R8: pr loop-var rename
packs/envoy-gateway/pack.cue            # R7b: gatewayService declaration
packs/envoy-gateway/chart.yaml          # R7b: KNOWN GAP comment removed
packs/envoy-gateway/manifests/10-gatewayclass.yaml  # R7b: envoyService.name
tests/e2e/phase3_test.go                # R7b: in-cluster CoreDNS curl assertion
internal/pack/source.go                 # R8: IsLocalRegistryHost exported (one home)
internal/oci/pushdir.go                 # R8: helper copy deleted; content-derived
                                        #     created annotation
cmd/repo.go                             # R8: deployRepo wrap dedup (also R3 target)
tests/e2e/e2e_test.go                   # R8: lint items (slices.Contains/FieldsSeq)
internal/engine/flux/uninstall_test.go  # R8: label-scoped flux-system list
cmd/trust_test.go                       # R8: positive consent-wording assertion
hack/inject-argocd-cmd-params.awk       # R8: positional-fragility header note
README.md                               # R1 install; R5 plugin note; R7a precedence;
                                        #     R9 full sweep
```

---

### Task 0: Reconciliation Gate (mandatory, blocking)

**Files:**
- Modify: **this plan file** — every divergence found below must be edited into the affected tasks before they run.

**Interfaces:**
- Consumes: the then-current tree at execution start.
- Produces: a reconciled Phase 4 plan. Nothing else. No product code is written in this task.

Work through every checkbox. For each: open the named files, compare against the Ground Truth section below, and if reality differs, **edit the affected tasks before proceeding**. Record a one-line note per item (verified / diverged→fixed) in the plan-update commit message.

- [x] **0.1 — Version surface.** `cmd/version.go` still has `var Version = "dev"` printed as `"cube-idp version %s\n"` and `cmd/version_test.go` pins `"cube-idp version dev"` via `strings.Contains` (G2). Affects R1.
- [x] **0.2 — Bundle package.** `internal/bundle/bundle.go`: `Manifest{FormatVersion, Platform, CreatedAt, LockDigest, Images}`, `currentFormatVersion = 1`, `Verify()` presence+size semantics with the "known gap, tracked for Phase 4" docstring, `extractTarGz` with no size caps, `safeJoin` guard; `internal/up/up.go` bundle step line reads `"bundle opened — lock digest OK, %d packs / %d images present"` (G4). Test fixtures `writeLockFixture`/`writeLockFixtureWithImage`/`pushTestImage` in `internal/bundle/bundle_test.go` (G5). Affects R2.
- [x] **0.3 — ui/event surface.** `Console{Start,Step,Progress,Note,Warn,Health,Access}` + `ConsoleProgress{Done,Stop}` in `internal/ui/console.go`; `RunPipeline(ctx, cmdName, out, fn)` in `internal/ui/pipeline.go` with the ModeJSON/ModeLive/ModeStyled+TTY/plain switch; `render.Plain`/`render.JSON`/`render.Live` in `internal/ui/render/`; JSON envelope `{"v":1,"ts":…,"type":…}` incl. `encode_error`; Modes + `Resolve` ladder in `internal/ui/ui.go` (G6). Affects R3.
- [x] **0.4 — R3 targets' current output.** The five commands' exact current output calls as recorded in G7 (cmd/vendor.go passes `c.OutOrStdout()` into `bundle.Vendor`; cmd/sync.go one-shot prints nothing itself; syncer prints three `▸ [sync]` lines; cmd/repo.go's `printRepoAccess` raw Fprintf block; cmd/plugin.go tabwriter table + two `✔ plugin …` lines; cmd/pack.go's `▸ [pack] pushed …` line). Grep for tests pinning these bytes (`grep -rn "delivered — engine reconciling\|is now trusted\|installed and trusted\|repo .* created" --include="*_test.go"`) and list them in R3's step 0 before migrating. Affects R3.
- [x] **0.5 — Diag catalog state.** `internal/diag/codes.go` matches G8's used-code list exactly (esp.: 3008, 7006, 7105 still FREE; `CUBE-1003` still carries the stale `(RECONCILE: Task 0 use unclear)` comment; `internal/config/load.go:99` is its only non-test use — `grep -rn CodeClusterSetupFailed --include="*.go" | grep -v _test`); `codes_test.go` scans only `"CUBE-` (double-quote) literals (G9). Affects R4.
- [x] **0.6 — Poke/LoadImages call sites.** `internal/engine/flux/poke.go:45,56` + `internal/engine/argocd/poke.go:40,51` wrap non-missing IO failures as `CodePokeTargetMissing`; `internal/engine/contract/contract.go:160` asserts 3007 for the undelivered-pack case only. `internal/cluster/kindp/kind.go:128,134` + `internal/cluster/k3dp/k3d.go:161` wrap load failures as `CodeVendorPullFail` (G10). Affects R4.
- [x] **0.7 — Plugin package.** `internal/plugin/trust.go` keys the store by the RAW discovered path (`m[path] = sum` in `Trust`, lookups in `isTrusted`/`EnsureTrusted`); `internal/plugin/index.go:225` uses `http.DefaultClient`; `plugin.Lookup(name)`/`List()`/`InstallDir()`/`pluginPrefix = "cube-idp-"` in discover.go; `cmd/root.go:88-103` `Execute` fallthrough inspects only `os.Args[1]` and skips flag-shaped args (G11). Affects R5.
- [x] **0.8 — Render paths.** `internal/pack/render.go:50` kustomize branch calls `RenderDir(p.Dir)` with NO gw; `internal/pack/kustomize.go` `RenderDir` parses `resMap.AsYaml()` via `apply.ParseMultiDoc`; `substitute(s, gw)` in expose.go handles `${GATEWAY_HOST}`/`${GATEWAY_FQDN}`/`${GATEWAY_PACK}` and is identity for zero gw; `internal/cnoe/loader.go:114` also calls `RenderDir` (must stay substitution-free) (G12). `TestRenderForSubstitutesGatewayHost` in render_test.go uses fixture `testdata/gw-sub-pack`. Affects R6.
- [x] **0.9 — Gateway seams.** `cmd/init.go` `--local` hardcodes `packs/traefik` into `Gateway.Ref` (line 80) BEFORE the wizard overlay; the wizard has no gateway-pack question; `config.Default` sets `Gateway{Pack: "traefik", Host: "cube-idp.localtest.me", Port: 8443}` (G13). `internal/up/up.go:444` `gatewayServiceFQDN(gw)` returns `<pack>.<pack>.svc.cluster.local`, consumed at up.go:341 by `trust.EnsureCoreDNSRewrite`; the gateway pack is `packs[0]` at that point. `packs/envoy-gateway/` facts per G14 (EnvoyProxy with UNSET `envoyService.name`, KNOWN GAP comments in chart.yaml + 10-gatewayclass.yaml, `images:` declared in pack.cue). `internal/pack/pack.go:113-119` is the `images:` parse pattern R7b mirrors. Affects R7.
- [x] **0.10 — R8 file:line list.** Re-verify each against HEAD: `internal/pack/source.go:159` `isLocalRegistryHost`; `internal/oci/pushdir.go:213` (copy) and `:118` (`time.Now()` created annotation); `internal/bundle/vendor.go:363` (copy); `internal/syncer/syncer.go:144` `os.MkdirTemp("", "cube-idp-sync-*")` never removed; `cmd/repo.go:145-164` `deployRepo` triple identical `diag.Wrap`; `internal/up/up.go:251` `for i, pr := range refs` shadows the `*ConsoleProgress` `pr`; `tests/e2e/e2e_test.go:348-364` `deleteLingeringCluster`'s `strings.Fields` loop; `internal/engine/flux/uninstall_test.go:117` unfiltered `client.InNamespace(fluxNS)` list; `cmd/trust_test.go:85` negative-only assertion; `hack/inject-argocd-cmd-params.awk` header (G15). Import-cycle fact for the helper home: `internal/oci` imports `internal/pack` (push.go's `*pack.Rendered`), so the ONE exported helper lives in `internal/pack` (G15a). Affects R8.
- [x] **0.11 — Docs state.** README headings per G16 (no `config schema`, `down --keep-cluster`, or `vendor --lock` docs; plugin section at ~line 368; `## Development` at ~506). `docs/machine-readable-output.md` lacks `encode_error` (code emits it at `internal/ui/render/json.go:31`) (G17). Sweep README claims against the real `--help` output at execution time. Affects R9.
- [x] **0.12 — Release preconditions.** `git remote -v` → `https://github.com/RafPe/cube-idp.git` (owner `RafPe`, private); Makefile `build` already uses `CGO_ENABLED=0`; `.github/workflows/` contains ci.yaml + release-packs.yaml only; no `.goreleaser.yaml`, no `CHANGELOG.md` (G18). `RECONCILE:` run `goreleaser --version` (or `go run github.com/goreleaser/goreleaser/v2@latest --version`) — R1's config is written for goreleaser v2 schema (`version: 2`); if only v1 is available, install v2 (brew or go run pinned) rather than downgrading the config. **RESOLVED (Task 0): goreleaser v2.17.0 installed via `brew install goreleaser`; `go run …@latest` is NOT viable here (v2.17.0 needs Go ≥1.26.4, local toolchain is 1.26.2 with `GOTOOLCHAIN=local`) — use the brew binary for all local goreleaser invocations.** Affects R1/R10.
- [x] **0.13 — e2e harness.** Helpers `build(t)`, `run(t, dir, bin, args...)`, `gatewayPort(t)`, `patchCube`, `assertGatewayTLS`, `initCube`, `guardDeleteCluster`, `cleanupCube` in `tests/e2e/` as G19 records; `TestEnvoyGatewaySmoke` at phase3_test.go:521 ends with `assertGatewayTLS` + `down`. The five arbiters: `TestK3dUpDown`, `TestVendorBundleOffline`, `TestSyncOneShot`, `TestRepoCreateDeploy`, `TestEnvoyGatewaySmoke`. Affects R7b/R10.
- [x] **0.14 — Commit the reconciled plan.**

```bash
git add docs/superpowers/plans/2026-07-15-cube-idp-phase4-first-release.md
git commit -m "docs: phase 4 Task 0 — reconciliation gate complete

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

### Ground Truth (pre-answered 2026-07-15 from the live tree at `07d6471` — Task 0 verifies, then trusts)

- **G1 — Module/toolchain:** `module github.com/rafpe/cube-idp`, `go 1.26.2`. Direct deps incl. `golang.org/x/mod v0.37.0` (dirhash — R2 needs no new dep) and `oras.land/oras-go/v2 v2.6.2`. GitHub owner is **`RafPe`** (remote `https://github.com/RafPe/cube-idp.git`), repo private (P4-D2). Dev machine: Docker 29.4.0 verified; local e2e port **18443**; protected clusters `testowy`, `airbyte*`, `xxx`, `envoy-dbg`.
- **G2 — Version surface:** `cmd/version.go:10` — `var Version = "dev"` with the comment naming the exact ldflags path `-X github.com/rafpe/cube-idp/cmd.Version=v0.1.0`; RunE prints `"cube-idp version %s\n"`. `cmd/version_test.go:17` asserts `strings.Contains(out, "cube-idp version dev")` — an EXTENDED format keeping that prefix stays green. There are no `Commit`/`Date` vars yet (spec §5.1's RECONCILE resolved: stamp `cmd.Version`, add `cmd.Commit`/`cmd.Date`).
- **G3 — Lock pin formats** (`internal/pack/pack.go:52-59`, `internal/lock/lock.go`): `Pack.Pinned` ∈ `"git+<sha>"` | `"oci:<digest>"` (i.e. `oci:sha256:…`) | `"dir:<dirhash>"` where dirhash is `dirhash.HashDir(dir, "", dirhash.Hash1)` → `h1:…` (source.go:94-101 `dirPin`). `lock.File{APIVersion, Kind, Engine{Type}, Packs []Entry{Ref, Name, Version, Resolved, RenderedHash, Images}}`; `lock.Read` → `(nil, nil)` when missing, CUBE-0003 when corrupt.
- **G4 — Bundle Manifest v1** (`internal/bundle/bundle.go`): `Manifest{FormatVersion int, Platform string, CreatedAt string, LockDigest string, Images map[string]string}` (json tags `formatVersion`/`platform`/`createdAt`/`lockDigest`/`images`); `currentFormatVersion = 1` (line 58); `Open` rejects any other version with CUBE-7003 `"bundle manifest formatVersion %d is not supported (want %d)"`. `Verify()` (lines 174-207): lockDigest content check + pack/image **presence-and-size only**, docstring explicitly names the Phase 4 gap. `extractTarGz` (370-416) has **no size caps**; `safeJoin` (422-431) guards traversal. Layout: `manifest.json`, `cube.lock`, `packs/<name>/`, `images/<n>.tar` (plain tar of a single-image OCI layout). Vendor stages under `stage/packs/<name>` via `copyTree` and computes `LockDigest = "sha256:"+hex(sha256(raw))` (vendor.go:102-108). `up`'s step line after Verify: `con.Step("bundle", "bundle opened — lock digest OK, %d packs / %d images present", …)` (up.go:100-101) — tempered in `949dca6`, to be restored by R2.
- **G5 — Bundle test fixtures** (`internal/bundle/bundle_test.go`): `TestMain` neutralizes `engineInstallImages`/`registryInstallImages`; `writeLockFixture(t) string` builds an in-process registry (`ocitest.LocalRegistry(t)`) + demo pack (`ocitest.WriteDemoPack(t)`), pushes it, writes a one-pack cube.lock; `writeLockFixtureWithImage(t, goos, goarch)` adds one `pushTestImage` image pinned in `Entry.Images`; existing tamper tests `TestVerifyDetectsTampering` (truncate pack.cue → 7004) and `TestVerifyDetectsMissingImageTar` (delete tar → 7004 naming ref). Current Vendor signature: `Vendor(ctx, lockPath, outPath, platform string, progress io.Writer)`.
- **G6 — ui/event API:** events (`internal/ui/event/event.go`): `RunStarted{Cmd,Cube}`, `StepStarted{Stage,Msg}`, `StepDone{Stage,Msg,Dur}`, `StepFailed{Stage,Err}`, `HealthTick{Components}`, `Note{Msg}`, `Warn{Msg}`, `Access{Packs,Hint}`, `Diagnosis{Err,Raw}`, `RunDone{OK,Dur}` — closed set via `event()` marker. `Console` (console.go): `Start(cmd, cube)`, `Step(stage, format, args...)` (→StepDone Dur:0), `Progress(stage, msg) *ConsoleProgress` (→StepStarted; `Done(format, args...)` →StepDone with Dur, `Stop()` →StepFailed{Err:nil}), `Note(format, args...)` (renderers add exactly ONE trailing newline — migrating a raw Fprintf drops its trailing `\n`), `Warn`, `Health` (change-filtered), `Access(packs, hint)`. `RunPipeline(ctx context.Context, cmdName string, out io.Writer, fn func(ctx, *Console) error) error` (pipeline.go:44): producer goroutine + renderer on calling goroutine; terminal order on failure StepFailed?→RunDone{OK:false}→Diagnosis (always last); renderer switch: ModeJSON→`render.JSON(out)`, ModeLive or (ModeStyled ∧ `IsTerminal(out)`)→`runLive`, else→`render.Plain(out)`. Plain projection (render/plain.go): StepDone→`"▸ [%s] %s\n"`, Note/Warn→`Fprintln`, Access→`"\nAccess\n"` block, ZERO bytes for RunStarted/StepStarted/StepFailed/HealthTick/Diagnosis/RunDone. JSON (render/json.go): envelope `{"v":1,"ts":RFC3339Nano,"type":…}`, types `run_started`/`step_started`/`step_done`/`step_failed`/`health_tick`/`note`/`warn`/`access`/`run_done`/`diagnosis` + `encode_error` (json.go:31, NOT yet documented). Modes (ui.go): `ModeStyled(0)/ModePlain(1)/ModeJSON(2)/ModeLive(3)`; `Resolve(Request)` ladder rungs 1–9; `SetMode`/`CurrentMode` (atomic); `NewFor(out)` per-writer downgrade; `Printer{Step,Section,Glyph,Warn,Progress,AccessSummary,Styled,Out}`; glyphs `✔`/`✗`/`⚠`. `cmd/up.go` is the migration exemplar: RunE = `ui.RunPipeline(c.Context(), "up", c.OutOrStdout(), func(ctx, con) error { return up.Run(…) })`.
- **G7 — R3 targets' CURRENT output (the byte contract):**
  - `vendor` (cmd/vendor.go → bundle.Vendor with `p := ui.NewFor(progress)`): `▸ [vendor] pack %s (%s)` per pack (vendor.go:130), `▸ [vendor] image %s` per image (:232), final `▸ [vendor] bundle written: %s (%s, %d packs, %d images)` (:117).
  - `sync` one-shot (cmd/sync.go:81 prints nothing itself; syncer.go via `ui.NewFor(deps.Out)`): `▸ [sync] %s@%s rendered (%d object(s))` (:86), `▸ [sync] pushed packs/%s:%s` (:103), `▸ [sync] %s@%s delivered — engine reconciling` (:121). `sync --watch` calls the same SyncOnce inside `syncer.Watch` — watch stays on its current loop (spec §5.3).
  - `repo create` (cmd/repo.go:177-185 `printRepoAccess`, raw Fprintf): `%s repo %s/%s created\n` (glyph ✔), `  clone:  %s\n`, `  push:   git push %s %s\n`, and when deployed `  deploy: engine syncs %s on branch %s (--deploy)\n`.
  - `plugin list` (cmd/plugin.go:40-53): empty → `ui.Warn` `"no plugins found — install a cube-idp-<name> binary on PATH"`; else tabwriter table with header `NAME\tPATH\tTRUSTED`. `plugin trust`: `%s plugin %q (%s) is now trusted\n` (glyph ✔). `plugin install`: `%s plugin %q installed and trusted\n`.
  - `pack push` (cmd/pack.go:41): `▸ [pack] pushed %s@%s`.
- **G8 — Diag catalog (complete used set at authoring time):** 0001–0008, 0101–0105, 1001, 1003, 1004, 1101, 1102, 1201–1205, 1301–1305, 2001–2007, 3001, 3003–3007, 4001–4015, 5001–5005, 6001–6006, 7001–7005, 7101–7104, 7201, 7202, 7301–7303. **Free and now claimed by this plan: 3008, 7006, 7105. 8xxx entirely unused (stays that way).** `CUBE-3006` is reserved-unused by design (annotated); `CUBE-1002` deliberately unallocated (Phase 3 Task 0). `diag.go:3-4` package doc names only ranges 0–5xxx (R4 extends). `CUBE-1003`'s comment still reads `// cluster provider setup failed (RECONCILE: Task 0 use unclear)`; its ONLY non-test use is `internal/config/load.go:99` (crossValidate: node fields with `provider: existing`).
- **G9 — Ban-test mechanics** (`internal/diag/codes_test.go`): `findCubeLiteralOffenders` walks the repo skipping dot-dirs/testdata/vendor/`_test.go`, exempts exactly `internal/diag/codes.go`, flags files containing `` `"CUBE-` `` (double-quote form ONLY — backtick raw strings escape it, the R4 hole); `TestCatalogWellFormed` regexps `Code = "(CUBE-[0-9]{4})"` for format+uniqueness. There is NO used⊆defined/defined⊆used exhaustiveness test yet. **Task 0 find:** codes_test.go also carries a third test, `TestCubeLiteralScanAnchorsToCanonicalPath` (codes_test.go:95-127), pinning the exemption to the exact repo-relative `internal/diag/codes.go` path — R4 Step 5's scan change must keep it green.
- **G10 — R4 call sites:** flux poke.go wraps Get-error (:45) and Update-error (:56) as `CodePokeTargetMissing`; argocd poke.go likewise (:40 Get-error, :51 Update-error); both packages' target-missing paths (flux :62, argocd :35) are the legitimate 3007 uses; contract.go:160 asserts 3007 for `Poke(ctx, a, "never-delivered")`. kindp kind.go: ListNodes failure (:128) and per-image load failure (:134) wrap `CodeVendorPullFail`; k3dp k3d.go single import failure (:161) likewise; remediations carry the post-`949dca6` transient wording ("transient containerd import failure — re-run `cube-idp up --bundle` (idempotent); …") which R4 KEEPS verbatim under the new code.
- **G11 — Plugin internals:** trust store `~/.config/cube-idp/trust.json`, map plugin-binary-path→sha256; `Trust(name, path)` stores `m[path] = sum` with the path AS DISCOVERED (relative-PATH entries yield cwd-dependent keys — the R5 bug); `isTrusted(path)`/`EnsureTrusted(name, path, interactive)` look up the same raw key. `index.go:225` `http.DefaultClient.Do(req)` — ctx-bound only, no timeout (cobra ctx un-deadlined). `fetchArchive` caps at 256 MiB and sha256-verifies. `discover.go`: `pluginPrefix = "cube-idp-"`, `InstallDir()`, `Lookup(name) (path, bool)`, `List() []Descriptor`. `cmd/root.go:88-103` Execute fallthrough: only `os.Args[1]` non-flag-shaped dispatches; `cube-idp --plain myplugin` therefore does NOT dispatch (the documented R5 limitation).
- **G12 — Render paths:** `RenderFor` (render.go:33): kustomization.yaml present → `RenderDir(p.Dir)` (NO gw — the D15 asymmetry, R6's target); manifests/ walk substitutes raw bytes pre-parse (:82); chart path substitutes via `renderHelm(dir, vals, gw)` (helm.go:77). `substitute(s, gw)` (expose.go:50-58): identity when `gw.Host == ""`; `${GATEWAY_HOST}`→`GatewayHostString(gw)` (host[:port], port omitted at 443), `${GATEWAY_FQDN}`→`gw.Host`, `${GATEWAY_PACK}`→`gw.Pack`. `RenderDir` also consumed by `internal/cnoe/loader.go:114` (zero-gw semantics must be preserved there). Exemplar test `TestRenderForSubstitutesGatewayHost` (render_test.go:18) with fixture `internal/pack/testdata/gw-sub-pack`, gw = `{Pack: "traefik", Host: "cube-idp.localtest.me", Port: 8443}`.
- **G13 — init/gateway config:** `config.Default(name)` → `Gateway{Pack: "traefik", Host: "cube-idp.localtest.me", Port: 8443}` (Ref empty; published OCI pack refs `oci://ghcr.io/rafpe/cube-idp/packs/{gitea,argocd}:0.1.0`). `GatewaySpec.PackRef()`: Ref wins, else `"packs/"+Pack`. cmd/init.go: `--local` sets `Gateway.Ref = <abs>/packs/traefik` HARDCODED (line 80) before `applyWizardToCube` runs (line 92); the wizard (`runInitWizard`) asks name/provider/engine/gateway-host/gateway-port/optional-packs — NO gateway-pack question; `initWizardResult{Provider, Context, GatewayHost, GatewayPort, Packs}`; `applyWizardToCube` never touches Gateway.Pack/Ref. `CUBE-0008` (`CodeGatewayPackMismatch`) backstop lives at `internal/up/up.go:458-465` (`verifyGatewayPackRef`).
- **G14 — Gateway coherence seams:** `gatewayServiceFQDN(gw)` (up.go:444-446) → `fmt.Sprintf("%s.%s.svc.cluster.local", gw.Pack, gw.Pack)`; consumed once, at up.go:341-342 by `trust.EnsureCoreDNSRewrite(ctx, a.Client(), gw.Host, gatewayServiceFQDN(gw), dnsTimeout)` — AFTER the pack loop, so `packs[0]` (the fetched gateway pack) is in scope. envoy pack: `pack.cue` declares `images: ["docker.io/envoyproxy/envoy:distroless-v1.33.0"]` and `#Values: {}`; `chart.yaml` (chart `oci://docker.io/envoyproxy/gateway-helm` v1.3.0, releaseName/namespace `envoy-gateway`) carries the KNOWN GAP paragraph; `manifests/10-gatewayclass.yaml`'s EnvoyProxy `cube-idp` deliberately leaves `envoyService.name` UNSET (F9 post-mortem comment), pins `type: NodePort`, `externalTrafficPolicy: Cluster`, and a StrategicMerge `patch` mapping port 8000→30080, 8443→30443; `manifests/20-gateway.yaml` Gateway `cube-idp` ns `envoy-gateway`, listeners 8000/HTTP + 8443/HTTPS with cert `cube-idp-gateway-tls`. The F9 hijack cause: the name `envoy-gateway` collided with the CONTROLLER's own xDS Service — a NON-colliding stable name is safe (spec §5.7b). traefik pack: no `gatewayService`, service lands at `traefik.traefik.svc.cluster.local` — the default path, zero migration. `Pack` struct fields today: `Name, Version, Dir, Pinned, Expose *Expose, Images []string`; the `images:` parse (pack.go:113-119) is the pattern for `gatewayService:`.
- **G15 — R8 verified facts:** `isLocalRegistryHost` ×3: `internal/pack/source.go:159` (unexported original), `internal/oci/pushdir.go:213` (copy, comment says so), `internal/bundle/vendor.go:363` (copy, comment says so). `internal/oci/pushdir.go:118`: `ocispec.AnnotationCreated: time.Now().UTC().Format(time.RFC3339)` — republish of identical content yields a new digest; round-trip test is `TestPushPackDirRoundTripsThroughFetch` (pushdir docs, line 14). `internal/syncer/syncer.go:144`: synthesized-pack `os.MkdirTemp("", "cube-idp-sync-*")` never removed (`loadOrSynthesize` returns `*pack.Pack` only). `cmd/repo.go:145-164`: `deployRepo` wraps three different failures with the IDENTICAL `diag.Wrap(err, diag.CodeRepoDeployFail, "created the repo but could not register the deploy source", remediation)`. `internal/up/up.go:251`: `for i, pr := range refs` — `pr` shadows the outer `*ui.ConsoleProgress` `pr` (:139/:202); the loop body already renamed the PACK var to `pk` for the same reason. `tests/e2e/e2e_test.go:348-364`: `deleteLingeringCluster` iterates `strings.Fields(string(out))` with a manual equality loop (→ `slices.Contains`; `RECONCILE:` run the project's linter at execution to catch the second flagged item in the 329-area — the authoring-time read found the Fields loop at :354; adjust to whatever the linter names). `internal/engine/flux/uninstall_test.go:117`: `a.Client().List(ctx, list, client.InNamespace(fluxNS))` — unfiltered (poke_test.go:59 documents the resulting cross-test coupling). `cmd/trust_test.go:85-100`: asserts only the ABSENCE of `cube-idp.localtest.me`; the generic fallback wording in cmd/trust.go:46 is `"your cube-idp gateway's HTTPS"`. `hack/inject-argocd-cmd-params.awk`: header explains WHAT it injects but not that its matching is positional/fragile across argocd version bumps.
- **G15a — Helper home (import-cycle check):** `internal/oci` imports `internal/pack` (`PushRendered(ctx, r *pack.Rendered, …)`), and `internal/bundle` imports both — so `internal/pack` is the ONE cycle-free home all three can share: export `IsLocalRegistryHost` from `internal/pack` (source.go), delete the two copies.
- **G16 — README state (560 lines):** headings: Quickstart(19), cube.yaml reference(53), k3d provider(96), Node-image cache(119), Pack format(159), Engines(223), HTTPS & trust(247), Day 2(272), Delivering your own work(296: sync 301, repo create 323), Air-gapped install(343), Plugins(368), Pack sources(404), Pack discoverability(433), Terminal output(469), Migrating from idpbuilder(488), Development(506). NOT documented: `config schema` (only referenced in error remediations), `down --keep-cluster` (flag exists, cmd/down.go:35), `vendor --lock` (flag exists, cmd/vendor.go:23). No install-from-release section (no releases existed). `RECONCILE:` the envoy in-cluster caveat's README location — authoring-time grep found the KNOWN GAP only in packs/envoy-gateway/chart.yaml and 10-gatewayclass.yaml, not as a README paragraph; R9 sweeps `grep -in "envoy" README.md` at execution and removes whatever caveat exists (or notes none did).
- **G17 — machine-readable-output.md (282 lines):** documents the envelope, ordering, 10 event types (`encode_error` MISSING), and the three document schemas; says the event stream covers "long-running commands: `up`, `down`" — R3 extends that table per-command in the SAME commits as each migration.
- **G18 — Release preconditions:** no `.goreleaser.yaml`, no `CHANGELOG.md`, workflows = {ci.yaml, release-packs.yaml}. ci.yaml: `unit` job (vet, test -short, gen-argocd check, test-apply, test-engines) + `e2e` matrix job; both use `go-version-file: go.mod`. Makefile: `build` (CGO_ENABLED=0), `test`, `envtest-assets`, `test-apply`, `test-engines`. Binary entrypoint: repo root (`go build -o cube-idp .`).
- **G19 — e2e harness:** `build(t)` compiles `../..` into a temp bin; `run(t, dir, bin, args...)` fatal-on-error returns combined output; `gatewayPort(t)` honors `CUBE_IDP_E2E_GATEWAY_PORT`; `initCube`/`patchCube` write/rewrite cube.yaml (patchCube takes `func(*config.Cube)`); `assertGatewayTLS(t, hostport)` polls the TLS probe; `guardDeleteCluster`/`cleanupCube` enforce no-leak; providers via `CUBE_IDP_E2E_PROVIDER`, engines via `CUBE_IDP_E2E_ENGINE`. `TestEnvoyGatewaySmoke` (phase3_test.go:521): init → patch gateway to envoy (Pack + local Ref) → up → status Ready → `assertGatewayTLS("gitea.cube-idp.localtest.me:"+port)` → down. phase3_test.go already imports client-go (`k8s.io/client-go/kubernetes`) — R7b's in-cluster pod probe can build a clientset from the provider kubeconfig the same way existing helpers do.
- **G20 — Progress-mode plumbing:** `cmd/root.go` PersistentPreRunE resolves `ui.SetMode(ui.Resolve(…))` once, `--progress=auto|plain|live|json` + `--plain` alias both live (stage B shipped); `CUBE_IDP_PROGRESS` honored. Commands not on the event stream keep plain as their machine contract under `--progress=json` (docs line 23-25) — R3 upgrades the five targets to real streams.

---

### Task R1: Release pipeline (FIRST — every later merge becomes release-candidate-testable)

**Reconcile checkpoint:** 0.1 (version surface), 0.12 (remote/goreleaser/Makefile/workflows).

**Files:**
- Create: `.goreleaser.yaml`, `.github/workflows/release.yaml`, `CHANGELOG.md`
- Modify: `cmd/version.go`, `cmd/version_test.go`, `.github/workflows/ci.yaml`, `README.md` (install section), `.gitignore` (`dist/`)

**Interfaces:**
- Consumes: `cmd.Version` (G2).
- Produces:

```go
package cmd

// Version, Commit and Date are stamped at release time via
// -ldflags "-X github.com/rafpe/cube-idp/cmd.Version=… -X github.com/rafpe/cube-idp/cmd.Commit=… -X github.com/rafpe/cube-idp/cmd.Date=…"
// (.goreleaser.yaml). Defaults describe a plain `go build`.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)
// version output (the release smoke asserts on this exact shape, R10):
//   cube-idp version <Version> (commit <Commit>, built <Date>)
```

- [ ] **Step 1: Failing test — version output carries commit and date**

Append to `cmd/version_test.go`:

```go
// TestVersionPrintsCommitAndDate pins the R1 stamped-version surface: the
// un-stamped defaults render exactly as below, and the leading
// "cube-idp version dev" prefix survives (the pre-R1 assertion keys on it).
func TestVersionPrintsCommitAndDate(t *testing.T) {
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"version"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "cube-idp version dev (commit none, built unknown)\n" {
		t.Fatalf("version output: %q", got)
	}
}
```

Run: `go test ./cmd/ -run TestVersionPrintsCommitAndDate -v` — FAIL (got `cube-idp version dev\n`).

- [ ] **Step 2: Implement the version surface**

Replace `cmd/version.go`'s var + print with the Interfaces block above:

```go
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the cube-idp version",
		RunE: func(c *cobra.Command, _ []string) error {
			fmt.Fprintf(c.OutOrStdout(), "cube-idp version %s (commit %s, built %s)\n", Version, Commit, Date)
			return nil
		},
	}
}
```

Run: `go test ./cmd/ -run 'TestVersion' -v` — both version tests PASS (the old one uses Contains on the surviving prefix). Full suite green. Commit:

```bash
git add -A && git commit -m "feat: version surface stamps commit and build date

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

- [ ] **Step 3: goreleaser config (full content — this IS the file)**

`.goreleaser.yaml`:

```yaml
# cube-idp release build (Phase 4 R1). goreleaser v2 schema.
# Local validation: goreleaser release --snapshot --clean (also run by the
# ci.yaml release-snapshot job on every push — cheap, no publish).
version: 2

project_name: cube-idp

builds:
  - id: cube-idp
    main: .
    binary: cube-idp
    env:
      - CGO_ENABLED=0   # static binaries (Makefile build parity)
    goos: [darwin, linux]
    goarch: [amd64, arm64]
    ldflags:
      - -s -w
      - -X github.com/rafpe/cube-idp/cmd.Version={{ .Version }}
      - -X github.com/rafpe/cube-idp/cmd.Commit={{ .ShortCommit }}
      - -X github.com/rafpe/cube-idp/cmd.Date={{ .Date }}

archives:
  - id: tarballs
    formats: [tar.gz]
    name_template: "{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}"
    files: [LICENSE*, README.md]

checksum:
  name_template: checksums.txt
  algorithm: sha256

snapshot:
  version_template: "{{ incpatch .Version }}-snapshot-{{ .ShortCommit }}"

changelog:
  use: git
  sort: asc
  groups:
    - title: Features
      regexp: '^feat(\(.*\))?:'
      order: 0
    - title: Fixes
      regexp: '^fix(\(.*\))?:'
      order: 1
    - title: Documentation
      regexp: '^docs(\(.*\))?:'
      order: 2
    - title: Other
      order: 999
  filters:
    exclude:
      - '^chore\(release\):'

release:
  github:
    owner: RafPe
    name: cube-idp
  draft: false
  prerelease: auto
```

`RECONCILE:` if `goreleaser check` (below) rejects a key, fix per its message — the v2 schema is authoritative over this snippet; keep the semantic content (4 targets, CGO off, the three ldflags, tar.gz+checksums, conventional-commit groups).

Add `dist/` to `.gitignore`.

- [ ] **Step 4: Local acceptance (the spec §5.1 acceptance criterion)**

```bash
goreleaser check   # or: go run github.com/goreleaser/goreleaser/v2@latest check
goreleaser release --snapshot --clean
ls dist/*.tar.gz dist/checksums.txt          # four archives + checksums
./dist/cube-idp_darwin_arm64*/cube-idp version   # macOS dev machine host build
```

Expected: four `cube-idp_<ver>_{darwin,linux}_{amd64,arm64}.tar.gz`, a `checksums.txt`, and the binary printing `cube-idp version 0.1.1-snapshot-<sha> (commit <sha>, built <date>)` — the stamped snapshot version, NOT `dev`. Commit:

```bash
git add -A && git commit -m "build: goreleaser config — 4-platform static builds, checksums, changelog

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

- [ ] **Step 5: Snapshot job in normal CI**

Append to `.github/workflows/ci.yaml` `jobs:` (alongside `unit`/`e2e`, same style — `go-version-file: go.mod`, never a hardcoded Go version):

```yaml
  release-snapshot:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with: {fetch-depth: 0}   # goreleaser reads tags + full history
      - uses: actions/setup-go@v5
        with: {go-version-file: go.mod}
      - uses: goreleaser/goreleaser-action@v6
        with:
          version: "~> v2"
          args: release --snapshot --clean
      - run: ./dist/cube-idp_linux_amd64_v1/cube-idp version   # smoke: stamped, runs
```

`RECONCILE:` the exact dist subdirectory name (`_v1` GOAMD64 suffix) — confirm against Step 4's local `dist/` layout and adjust the smoke path (a `find dist -name cube-idp -type f | head -1` invocation is an acceptable robust alternative).

- [ ] **Step 6: Tag-triggered release workflow**

`.github/workflows/release.yaml`:

```yaml
name: release
on:
  push:
    tags: ["v*"]
permissions:
  contents: write   # create the GitHub Release + upload assets (private repo)
jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with: {fetch-depth: 0}
      - uses: actions/setup-go@v5
        with: {go-version-file: go.mod}
      - uses: goreleaser/goreleaser-action@v6
        with:
          version: "~> v2"
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

This workflow is exercised for real only in R10 (the tag push is owner-gated). Commit:

```bash
git add -A && git commit -m "ci: snapshot build on every push; tag-triggered release workflow

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

- [ ] **Step 7: CHANGELOG seed + install docs**

`CHANGELOG.md` — curated headline features per phase (NOT raw commits; goreleaser appends generated notes to future releases, this file is the human record):

```markdown
# Changelog

## v0.1.0 (unreleased — cut by Phase 4 R10)

First release. Private distribution via GitHub Releases (`gh release download`).

### Phase 1 — MVP (2026-07-13)
- `cube-idp init | up | down | status | doctor | get secrets | diff | config`:
  one static binary drives kind cluster + Flux/Argo CD + zot registry +
  traefik gateway + gitea/argocd packs from a single `cube.yaml`
  (`cube-idp.dev/v1alpha1`).
- Typed CUBE-xxxx error model with remediations; byte-stable plain output.

### Phase 2 — Trust, sources, day-2 (2026-07-14)
- Local CA + OS trust store (`cube-idp trust`), HTTPS gateway (NodePort 30443),
  CoreDNS `*.<host>` in-cluster resolution, registry certs.d wiring (D12).
- Pack sources: OCI, bare-git grammar, go-getter refs; `cube.lock` pins
  (`oci:sha256:…`, `git+<sha>`, `dir:h1:…`); `upgrade --plan`; pack
  discoverability records (`kubectl get packs`, D11); cnoe-compat import.

### Phase 3 — Providers, air-gap, delivery (2026-07-14/15)
- k3d provider (D4/D10/D12) + shared provider contract suite.
- Air-gap: `cube-idp vendor [--platform]` → `up --bundle` (per-image OCI tars).
- Exec-plugins with sha256-pinned index (`plugin list|trust|install`).
- `sync [--watch]` (D7), `repo create [--deploy]`, pack catalog
  (backstage, cert-manager, external-secrets, turnkey envoy-gateway),
  `pack push --also-tag`.
- One-console UX: typed event stream, plain/live/JSON renderers,
  `--progress`, JSON documents for status/doctor/get secrets.

### Phase 4 — First release hardening (2026-07-15)
- Release pipeline (goreleaser, 4 platforms, checksums) + version stamping.
- Bundle integrity: content-hashed manifest v2, extraction caps.
- Event stream covers vendor/sync/repo/plugin/pack push (`--progress=json`).
- Diag taxonomy sweep; plugin trust/index hardening; D15 kustomize
  substitution; gateway pack/ref coherence + envoy in-cluster CoreDNS fix.
```

README: insert an `## Install` section right after the intro (before `## Quickstart`):

````markdown
## Install

Releases are private — authenticate `gh` to RafPe/cube-idp first.

```bash
gh release download v0.1.0 -R RafPe/cube-idp -p "cube-idp_*_$(uname -s | tr A-Z a-z)_$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/').tar.gz"
tar xzf cube-idp_*.tar.gz
shasum -a 256 -c <(gh release download v0.1.0 -R RafPe/cube-idp -p checksums.txt -O - | grep "$(uname -s | tr A-Z a-z)_$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')")
chmod +x cube-idp && mv cube-idp ~/bin/   # or anywhere on PATH
cube-idp version
```

`go install github.com/rafpe/cube-idp@v0.1.0` does NOT work while the repo is
private unless you set `GOPRIVATE=github.com/rafpe/cube-idp` and have git
auth to the repo; prefer `gh release download`.
````

Run: `go build ./... && go vet ./... && go test ./... -short -count=1` — green. Commit:

```bash
git add -A && git commit -m "docs: CHANGELOG v0.1.0 seed and private-release install instructions

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

- [ ] **Step 8: Ledger** — append one line to `.superpowers/sdd/progress.md` (`Task R1: complete (…)`) and include it in the last commit or a follow-up `chore:` commit.

---

### Task R2: Bundle integrity — Manifest formatVersion 2

**Reconcile checkpoint:** 0.2 (bundle package + test fixtures + up step line).

**Files:**
- Modify: `internal/bundle/bundle.go`, `internal/bundle/vendor.go`, `internal/bundle/bundle_test.go`, `internal/up/up.go` (step line)
- Grep first: `grep -rn "bundle opened" --include="*"` — update any test pinning the tempered line in the same commit as the wording change.

**Interfaces:**
- Consumes: `dirhash.HashDir` (`golang.org/x/mod/sumdb/dirhash`, already used by `internal/pack/source.go:95`); G4's Manifest/Vendor/Verify.
- Produces:

```go
// Manifest is manifest.json's schema, versioned via FormatVersion.
type Manifest struct {
	FormatVersion int               `json:"formatVersion"` // now 2
	Platform      string            `json:"platform"`
	CreatedAt     string            `json:"createdAt"`
	LockDigest    string            `json:"lockDigest"`
	// PackHashes: pack name -> dirhash.Hash1 ("h1:…") of the STAGED
	// packs/<name> tree — same algorithm and prefix as the lock's dir: pins.
	PackHashes map[string]string `json:"packHashes"`
	Images     map[string]string `json:"images"`
	// ImageHashes: ORIGINAL image ref -> "sha256:…" of the tar file bytes.
	ImageHashes map[string]string `json:"imageHashes"`
}

const currentFormatVersion = 2

// Extraction caps (test seam: package vars, NOT exported config):
var (
	maxBundleFileBytes  int64 = 4 << 30  // 4 GiB per tar entry
	maxBundleTotalBytes int64 = 16 << 30 // 16 GiB per bundle
)
```

- [ ] **Step 1: Failing tests — the four new guarantees**

Append to `internal/bundle/bundle_test.go` (fixtures per G5; literals allowed in tests):

```go
// TestOpenRejectsV1Bundle: a bundle whose manifest says formatVersion 1 is
// refused with CUBE-7003 and the format-upgraded remediation — bundles are
// ephemeral transport artifacts, no compatibility shim (spec §5.2).
func TestOpenRejectsV1Bundle(t *testing.T) {
	out := filepath.Join(t.TempDir(), "bundle.tar.gz")
	if err := Vendor(context.Background(), writeLockFixture(t), out, "", os.Stderr); err != nil {
		t.Fatal(err)
	}
	downgradeManifestVersion(t, out, 1) // helper below: rewrites formatVersion in-archive
	_, err := Open(out)
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-7003" {
		t.Fatalf("want CUBE-7003 for a v1 bundle, got %v", err)
	}
	if !strings.Contains(de.Remediation, "bundle format upgraded") {
		t.Fatalf("remediation must name the format upgrade, got %q", de.Remediation)
	}
}

// TestVerifyDetectsPackContentSwap: flip one byte in a pack file WITHOUT
// changing its size — presence+size verification (the pre-R2 state) cannot
// catch this; the dirhash comparison must, naming the pack.
func TestVerifyDetectsPackContentSwap(t *testing.T) {
	out := filepath.Join(t.TempDir(), "bundle.tar.gz")
	if err := Vendor(context.Background(), writeLockFixture(t), out, "", os.Stderr); err != nil {
		t.Fatal(err)
	}
	o, err := Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()
	flipOneByte(t, filepath.Join(o.Dir, "packs", "demo", "pack.cue"))
	err = o.Verify()
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-7004" || !strings.Contains(err.Error(), `pack "demo"`) {
		t.Fatalf("want CUBE-7004 naming pack demo, got %v", err)
	}
}

// TestVerifyDetectsImageContentSwap: same-size byte flip inside an image tar.
func TestVerifyDetectsImageContentSwap(t *testing.T) {
	lockPath, imgRef := writeLockFixtureWithImage(t, "linux", runtime.GOARCH)
	out := filepath.Join(t.TempDir(), "bundle.tar.gz")
	if err := Vendor(context.Background(), lockPath, out, "", os.Stderr); err != nil {
		t.Fatal(err)
	}
	o, err := Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()
	flipOneByte(t, o.ImageTars()[imgRef])
	err = o.Verify()
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-7004" || !strings.Contains(err.Error(), imgRef) {
		t.Fatalf("want CUBE-7004 naming %q, got %v", imgRef, err)
	}
}

// TestExtractCaps: with the test-seam limits shrunk, an over-limit entry and
// an over-limit total are both CUBE-7003 (Open wraps extractTarGz's error).
func TestExtractCaps(t *testing.T) {
	out := filepath.Join(t.TempDir(), "bundle.tar.gz")
	if err := Vendor(context.Background(), writeLockFixture(t), out, "", os.Stderr); err != nil {
		t.Fatal(err)
	}
	restoreFile, restoreTotal := maxBundleFileBytes, maxBundleTotalBytes
	defer func() { maxBundleFileBytes, maxBundleTotalBytes = restoreFile, restoreTotal }()

	maxBundleFileBytes = 8 // every real entry exceeds this
	_, err := Open(out)
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-7003" {
		t.Fatalf("per-file cap: want CUBE-7003, got %v", err)
	}

	maxBundleFileBytes = restoreFile
	maxBundleTotalBytes = 64
	_, err = Open(out)
	if !errors.As(err, &de) || de.Code != "CUBE-7003" {
		t.Fatalf("total cap: want CUBE-7003, got %v", err)
	}
}
```

Test helpers (same file): `flipOneByte(t, path)` reads the file, XORs the last byte with 0xFF, writes it back same-length; `downgradeManifestVersion(t, bundlePath, v)` extracts the tar.gz to a temp dir (reuse `extractTarGz`), rewrites `manifest.json`'s `formatVersion` to `v` (unmarshal into `map[string]any`, set, marshal), re-archives with `tarGzDir` over the original path. Write both fully.

Run: `go test ./internal/bundle/ -run 'TestOpenRejectsV1|TestVerifyDetects.*Swap|TestExtractCaps' -v` — FAIL (v1 is currently the ACCEPTED version; swaps pass Verify; no caps).

- [ ] **Step 2: Implement Manifest v2 + Verify + caps**

`internal/bundle/bundle.go`:
1. Manifest gains `PackHashes`/`ImageHashes` per the Interfaces block; `currentFormatVersion = 2`.
2. `Open`'s version check message/remediation become: summary `fmt.Sprintf("bundle manifest formatVersion %d is not supported (want %d)", m.FormatVersion, currentFormatVersion)`, remediation `"re-run `cube-idp vendor` — bundle format upgraded"` (CUBE-7003, `diag.CodeVendorBundleCorrupt`).
3. `Verify()` — REPLACE the presence+size loops with content verification and update the docstring to the honest strong claim ("Verify recomputes the content hash of every pack tree and image tar and compares against the manifest — a tampered, truncated, or swapped file cannot pass"):

```go
func (o *Opened) Verify() error {
	// (lockDigest check unchanged — it was already a content hash.)
	raw, err := os.ReadFile(filepath.Join(o.Dir, "cube.lock"))
	if err != nil { /* unchanged CUBE-7004 wrap */ }
	sum := sha256.Sum256(raw)
	if got := "sha256:" + hex.EncodeToString(sum[:]); got != o.Manifest.LockDigest { /* unchanged CUBE-7004 */ }

	for _, entry := range o.Lock.Packs {
		want, ok := o.Manifest.PackHashes[entry.Name]
		if !ok {
			return diag.New(diag.CodeVendorIncomplete,
				fmt.Sprintf("bundle manifest has no content hash for pack %q", entry.Name),
				"re-run `cube-idp vendor`")
		}
		got, err := dirhash.HashDir(filepath.Join(o.Dir, "packs", entry.Name), "", dirhash.Hash1)
		if err != nil || got != want {
			return diag.New(diag.CodeVendorIncomplete,
				fmt.Sprintf("bundle content mismatch for pack %q (packs/%s): bundle is corrupt or was tampered with", entry.Name, entry.Name),
				"re-run `cube-idp vendor`")
		}
	}
	for ref, rel := range o.Manifest.Images {
		want, ok := o.Manifest.ImageHashes[ref]
		if !ok {
			return diag.New(diag.CodeVendorIncomplete,
				fmt.Sprintf("bundle manifest has no content hash for image %q", ref), "re-run `cube-idp vendor`")
		}
		got, err := sha256File(filepath.Join(o.Dir, filepath.FromSlash(rel)))
		if err != nil || got != want {
			return diag.New(diag.CodeVendorIncomplete,
				fmt.Sprintf("bundle content mismatch for image %q (%s): bundle is corrupt or was tampered with", ref, rel),
				"re-run `cube-idp vendor`")
		}
	}
	return nil
}

// sha256File returns "sha256:<hex>" of path's contents (streamed, not
// slurped — image tars can be GiB-scale).
func sha256File(path string) (string, error) { /* os.Open + io.Copy into sha256.New() */ }
```

4. `extractTarGz` caps — add the two package vars from the Interfaces block and enforce inside the `tar.TypeReg` branch:

```go
var total int64 // declared before the loop
// … in case tar.TypeReg:
if hdr.Size > maxBundleFileBytes {
	return fmt.Errorf("bundle entry %q exceeds the per-file limit (%d > %d bytes)", hdr.Name, hdr.Size, maxBundleFileBytes)
}
total += hdr.Size
if total > maxBundleTotalBytes {
	return fmt.Errorf("bundle exceeds the total size limit (%d bytes)", maxBundleTotalBytes)
}
// io.Copy(out, tr) -> io.Copy(out, io.LimitReader(tr, maxBundleFileBytes+1))
// followed by a written-bytes check: n > maxBundleFileBytes -> same per-file error
// (belt-and-braces: hdr.Size can lie; the LimitReader is the real guard).
```

(These return plain errors; `Open`'s existing `extractTarGz` wrap makes them CUBE-7003 — exactly the spec's "CUBE-7003 wrap".)

`internal/bundle/vendor.go`:
5. `vendorPacks` returns `(map[string]string, error)` — after each `copyTree`, compute `dirhash.HashDir(filepath.Join(stage, "packs", entry.Name), "", dirhash.Hash1)` into the map keyed by `entry.Name` (any hash error → CUBE-7002 wrap "cannot hash staged pack %q").
6. `vendorImages` additionally returns `imageHashes map[string]string`: after each `tarDir`, `sha256File` the written tar, keyed by the ORIGINAL ref.
7. `Vendor` threads both into the Manifest literal (`PackHashes: packHashes, ImageHashes: imageHashes`).

`internal/up/up.go:100`:
8. Restore the strong claim now the code earns it: `con.Step("bundle", "bundle verified — content hashes OK, %d packs / %d images present", …)`. Grep + update any test pinning the old line in the same commit (deliberate plain-output change, named here).

- [ ] **Step 3: Run**

`go test ./internal/bundle/ -v` — all new tests PASS; `TestVendorThenOpenRoundTrip` and both pre-existing tamper tests still PASS (truncation now fails the CONTENT check — still 7004). Full suite: `go build ./... && go vet ./... && go test ./... -short -count=1`.

- [ ] **Step 4: Commit + ledger**

```bash
git add -A && git commit -m "feat: bundle manifest v2 — content-hash verification and extraction caps

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

Append `Task R2: complete (…)` to `.superpowers/sdd/progress.md`.

---

### Task R3: Event-stream migration — vendor, sync, repo create, plugin, pack push

**Reconcile checkpoint:** 0.3 (ui/event surface), 0.4 (the five commands' pinned bytes + the tests that pin them).

Completes the UX design's stage plan: `--progress=json` emits a real `"v":1` event stream on every command. Technique is exactly 14b's: Console facade, plain projection byte-identical to today's lines. **Live view: ONLY `vendor` gains the LiveRenderer step-tree** (long-running, per-pack/per-image progress); the other four are short static commands and must NOT pop a live view — they need a pipeline variant that projects styled-static on a TTY. `sync --watch` keeps its current loop (ratified deferral, spec §5.3).

**Files:**
- Create: `internal/ui/render/styled.go` (+ `styled_test.go`)
- Modify: `internal/ui/pipeline.go` (+ test), `cmd/vendor.go`, `internal/bundle/vendor.go` (+ its tests' call plumbing), `cmd/sync.go`, `internal/syncer/syncer.go` (+ tests), `cmd/repo.go`, `cmd/plugin.go`, `cmd/pack.go`, `docs/machine-readable-output.md` (same commits), per-command golden tests.

**Interfaces:**
- Consumes: G6's entire surface.
- Produces:

```go
// internal/ui/render/styled.go
// Styled returns the styled-static projection for request/response commands
// migrated onto the event stream (Phase 4 R3): the same content as Plain,
// rendered through the existing Printer styling — StepDone via Printer.Step
// (badge+dim), Note via Fprintln, Warn via Printer.Warn, Access via
// Printer.AccessSummary. Zero bytes for the same event set Plain ignores.
// It is ONLY ever constructed for a real TTY (RunPipelineStatic's switch),
// so it builds its Printer with ui-package styling enabled.
func Styled(w io.Writer) func(event.Event)

// internal/ui/pipeline.go
// RunPipelineStatic is RunPipeline for short, static commands (plugin, pack
// push, repo create, sync one-shot): identical lifecycle and terminal-event
// ordering, but a TTY under ModeStyled gets the Styled projection instead of
// the Live renderer — the live step-tree is reserved for long-running
// commands (vendor, up, down; UX spec §5.2 + Phase 4 spec §5.3).
// ModeLive (explicit user force) still runs the LiveRenderer; ModeJSON and
// plain behave exactly as RunPipeline.
func RunPipelineStatic(ctx context.Context, cmdName string, out io.Writer,
	fn func(ctx context.Context, con *Console) error) error

// internal/bundle — Vendor loses its io.Writer, gains the Console:
func Vendor(ctx context.Context, lockPath, outPath, platform string, con *ui.Console) error

// internal/syncer — Deps gains the emitter seam both Console and Printer satisfy:
type Stepper interface{ Step(stage, format string, args ...any) }
// Deps.Steps Stepper — nil defaults to ui.NewFor(Out) (watch path unchanged).
```

- [ ] **Step 0: Inventory the byte pins.** `grep -rn "delivered — engine reconciling\|pushed packs/\|is now trusted\|installed and trusted\|repo .*created\|bundle written\|pushed .*@sha256\|no plugins found" --include="*_test.go" .` — list every test asserting the G7 lines in the task ledger note. **Task 0 pre-run found:** `cmd/plugin_test.go:79` pins "no plugins found"; `cmd/plugin_test.go:96` pins only the weak substring "trusted"; `cmd/repo_test.go:91` pins the full repo created/clone/push block; `cmd/pack_test.go:118` pins `▸ [pack] pushed <ref>@sha256:` (add that pattern to the grep); NO `cmd/vendor_test.go` or `cmd/sync_test.go` exist. Re-run the grep at execution to confirm. These tests are the byte-identity arbiters: they must stay GREEN THROUGH the migration with only call-plumbing edits (e.g. `bundle.Vendor`'s new signature), never assertion edits — except the two failure-path deltas named in Step 3.

- [ ] **Step 1: `RunPipelineStatic` + `render.Styled` (TDD)**

Failing test first, `internal/ui/pipeline_test.go` (mirror the existing RunPipeline tests' recorded-slice pattern):

```go
// TestRunPipelineStaticNeverGoesLive: under ModeStyled with a non-TTY writer
// the projection is plain (byte-identical to render.Plain for the same
// events); under ModeJSON it is the JSON stream. (A true-TTY styled
// assertion is render/styled_test.go's job — pipeline_test can only prove
// the non-TTY and JSON legs plus that no live program ever starts.)
func TestRunPipelineStaticNeverGoesLive(t *testing.T) {
	defer SetMode(CurrentMode())
	SetMode(ModeStyled)
	var buf bytes.Buffer
	err := RunPipelineStatic(context.Background(), "pack", &buf,
		func(ctx context.Context, con *Console) error {
			con.Step("pack", "pushed oci://x/y:1@sha256:abc")
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "▸ [pack] pushed oci://x/y:1@sha256:abc\n" {
		t.Fatalf("plain projection: %q", got)
	}
}
```

`render/styled_test.go`: feed a recorded slice (StepDone, Note, Warn, Access, plus the zero-byte set) into `Styled(&buf)` and assert content equals the Plain projection modulo ANSI (strip escapes, compare) — content identical, presentation only (the ui.go Printer rule). Implement `Styled` as a thin switch delegating to a `ui`-styled printer; NOTE the import direction: `render` must not import `ui` (ui imports render) — so `Styled` REIMPLEMENTS the three style calls with the same lipgloss styles (copy the badge/dim/warn style definitions; they are five lines) rather than importing Printer. Implement `RunPipelineStatic` by factoring RunPipeline's producer/lifecycle body into an unexported `runPipeline(ctx, out, fn, pickRenderer)` helper — the two exported functions differ ONLY in the renderer switch (`ModeStyled && IsTerminal(out)` → `render.Styled(out)` instead of `runLive`). Run; commit `feat: RunPipelineStatic — event stream for static commands without a live view`.

- [ ] **Step 2: Migrate `vendor` (the one live-gaining command)**

1. Golden test first (`internal/bundle/vendor_test.go` addition or new file): run `Vendor` against `writeLockFixture` THROUGH `ui.RunPipeline` with `ModePlain` forced and assert stdout bytes are exactly today's three-line sequence (pack line, image lines if any, `bundle written:` line) — write the expected literal from G7. RED is produced by the signature change compiling against the old tests; adapt all existing `bundle.Vendor(ctx, lock, out, "", os.Stderr)` call sites (bundle_test.go, load-path tests) to construct a Console via a small test helper `testConsole(t, w io.Writer) *ui.Console` — `RECONCILE:` if ui exports no test constructor for Console, add one to internal/ui (`NewConsoleForTest(ch chan<- event.Event)`) or route the tests through `ui.RunPipeline` with ModePlain; prefer the latter (no new API).
2. `internal/bundle/vendor.go`: `p *ui.Printer` → `con *ui.Console`; per-pack and per-image lines become Progress/Done pairs so the live tree gets spinners:
   - `vendorPacks`: `pr := con.Progress("vendor", fmt.Sprintf("pack %s (%s)", entry.Name, entry.Resolved))` before the fetch; `pr.Done("pack %s (%s)", entry.Name, entry.Resolved)` after staging; `pr.Stop()` on every error return.
   - `vendorImages`: same shape with `"image %s"`.
   - The final `bundle written:` line stays `con.Step("vendor", …)`.
   Plain bytes: Done prints the identical `▸ [vendor] …` line content; ordering across packs/images unchanged. **Deliberate failure-path delta #1 (plain):** pre-R3 the `pack/image` line printed BEFORE a failing pull; now Stop prints nothing and the failure surfaces only as the Diagnosis block. Name this in the commit message; update any test asserting the pre-failure line.
3. `cmd/vendor.go`: RunE wraps `ui.RunPipeline(c.Context(), "vendor", c.OutOrStdout(), func(ctx, con) error { con.Start("vendor", ""); return bundle.Vendor(ctx, lockPath, out, platform, con) })`. (`RunStarted.Cube` is empty — vendor is a pure lock consumer with no cube.yaml; document the empty field in the docs table.)
4. JSON golden: `--progress=json` run over the fixture → one event per line, `run_started`/`step_started`/`step_done`/…/`run_done` — assert via ModeJSON + recorded output (mirror render/json_test.go's pattern).
5. `docs/machine-readable-output.md`: extend the §1 command table with `vendor` (stages: `vendor`) — SAME commit.
6. Full suite green (byte pins from Step 0 untouched). Commit `feat(vendor): typed event stream — live step-tree, --progress=json`.

- [ ] **Step 3: Migrate `sync` one-shot**

1. `internal/syncer/syncer.go`: add `Stepper` (Interfaces block) + `Deps.Steps Stepper`; `SyncOnce` uses `steps := deps.Steps; if steps == nil { steps = ui.NewFor(deps.Out) }` and replaces its three `printer.Step("sync", …)` calls with `steps.Step("sync", …)`. `*ui.Console.Step` and `*ui.Printer.Step` both satisfy Stepper as-is (G6 — identical signatures); compile-time asserts: `var _ Stepper = (*ui.Console)(nil)` etc. in a test.
2. `cmd/sync.go`: the non-watch branch wraps everything AFTER config.Load…requireClusterExists? NO — wrap the WHOLE RunE body in `ui.RunPipelineStatic(c.Context(), "sync", c.OutOrStdout(), fn)`: inside fn, `config.Load` failure returns before `con.Start` (the RunStarted-skip rule, G6); after Load succeeds `con.Start("sync", cube.Metadata.Name)`; `deps.Steps = con`. The `--watch` branch stays OUTSIDE the pipeline — an early `if watch { …current body… }` before the RunPipelineStatic call, byte-identical (its Printer routing was fixed in `949dca6` and is out of scope, spec §5.3).
3. Golden tests: plain projection of a recorded sync event slice byte-equal to today's three lines; a JSON-mode unit through the syncer's fake seams (`Deps.PushAddr` + `syncFn` seams exist, G7) if a full fake sync is cheap — otherwise golden on the recorded slice only (14b precedent: renderer goldens run on recorded slices, not live clusters).
4. Docs table: add `sync` (one-shot; note `--watch` keeps plain). Same commit: `feat(sync): one-shot on the event stream — --progress=json`.

- [ ] **Step 4: Migrate `repo create`**

1. `cmd/repo.go`: wrap RunE in `RunPipelineStatic("repo", …)`; `con.Start("repo", cube.Metadata.Name)` after Load. `printRepoAccess(out, p, …)` becomes `emitRepoAccess(con, gw, repoInfo, deploy)`:

```go
func emitRepoAccess(con *ui.Console, gw config.GatewaySpec, r *gitea.Repo, deployed bool) {
	clone := repoCloneURL(gw, r)
	con.Note("✔ repo %s/%s created", r.Owner, r.Name) // plain glyph literal — what Glyph(GlyphOK) rendered in plain mode
	con.Note("  clone:  %s", clone)
	con.Note("  push:   git push %s %s", clone, r.DefaultBranch)
	if deployed {
		con.Note("  deploy: engine syncs %s on branch %s (--deploy)", repoDeliverGitDefault, r.DefaultBranch)
	}
}
```

Byte proof: each old Fprintf ended `\n`; Note's projection adds exactly one — identical. Styled projection: Note is Fprintln in Styled too — **deliberate styled-mode delta:** the ✔ loses its green styling in styled mode (it was `p.Glyph`); accepted — content identical, and repo create's styled output was never pinned. Keep `printRepoAccess` deleted, `repoCloneURL` kept.
2. Golden: recorded slice → plain projection equals today's four lines exactly (write the literal block).
3. Docs table: add `repo create`. Commit `feat(repo): repo create on the event stream — --progress=json`.

- [ ] **Step 5: Migrate `plugin list|trust|install` and `pack push`**

1. `cmd/plugin.go`: each RunE wraps in `RunPipelineStatic("plugin", …)` with `con.Start("plugin", "")`:
   - list: empty → `con.Warn("no plugins found — install a cube-idp-<name> binary on PATH")`; table → render the tabwriter into a `bytes.Buffer` first, then `con.Note("%s", strings.TrimRight(buf.String(), "\n"))` (one Note, embedded newlines — sanctioned by the Note contract, G6).
   - trust: `con.Note("✔ plugin %q (%s) is now trusted", name, path)`.
   - install: `con.Note("✔ plugin %q installed and trusted", name)`.
2. `cmd/pack.go` push: wrap in `RunPipelineStatic("pack", …)`; `ui.NewFor(...).Step("pack", "pushed %s@%s", …)` → `con.Step("pack", "pushed %s@%s", ref, digest)` — identical plain bytes.
3. Goldens per command (recorded slices, plain byte-pins + JSON one-per-line).
4. Docs: add `plugin list|trust|install`, `pack push` rows; update the "Commands with no meaningful JSON form" paragraph (it can no longer name these). Commit `feat(plugin,pack): remaining commands on the event stream — --progress=json everywhere`.

- [ ] **Step 6: Whole-surface proof**

`go build ./... && go vet ./... && go test ./... -short -count=1` green; manual spot-checks: `./cube-idp plugin list --progress=json | jq -c type` (valid JSON lines), `./cube-idp vendor --progress=json` against a scratch lock, `./cube-idp vendor` on a TTY shows the live tree. Ledger line `Task R3: complete (…)`.

---

### Task R4: Diag taxonomy sweep

**Reconcile checkpoint:** 0.5 (catalog state, free numbers), 0.6 (Poke/LoadImages sites).

**Files:**
- Modify: `internal/diag/codes.go`, `internal/diag/diag.go`, `internal/diag/codes_test.go`, `internal/config/load.go`, `internal/cluster/kindp/kind.go`, `internal/cluster/k3dp/k3d.go`, `internal/engine/flux/poke.go`, `internal/engine/argocd/poke.go` (+ any tests asserting the old codes on the migrated paths — grep per step).

**Interfaces — Produces (the complete catalog delta):**

```go
// 1xxx (rename only — VALUE unchanged):
CodeClusterFieldsConflict Code = "CUBE-1003" // node-creation fields (extraPorts/mounts/providerConfig/kubernetesVersion) set with provider: existing (config cross-validation)
// 3xxx:
CodePokeIOFail Code = "CUBE-3008" // Poke found the delivery source but could not read/update it (transient engine IO — retry)
// 70xx:
CodeBundleImageLoadFail Code = "CUBE-7006" // bundled image load into cluster nodes failed (kind/k3d LoadImages, consume side)
```

- [ ] **Step 1: CUBE-1003 re-scope.** Rename `CodeClusterSetupFailed` → `CodeClusterFieldsConflict` (constant + comment per Interfaces; drop the stale `(RECONCILE: Task 0 use unclear)`), update the single call site `internal/config/load.go:99` and any test referencing the identifier (`grep -rn CodeClusterSetupFailed`). No output changes (value identical) — no test assertion moves. Run suite. Commit `chore(diag): re-scope CUBE-1003 to its real (config cross-validation) use`.

- [ ] **Step 2: CUBE-7006 load-side code (RED first).** Extend the existing LoadImages retry tests (kindp/k3dp have fake-seam tests per G10's F10 work — `grep -rn loadWithRetry\|importWithRetry --include="*_test.go" internal/cluster`) with assertions that a permanently-failing load surfaces `CUBE-7006`; RED against the current 7002. Then: add the constant; switch kind.go:128/:134 and k3d.go:161 wraps to `diag.CodeBundleImageLoadFail`, keeping summaries and the transient-aware remediation VERBATIM. Grep for tests asserting `CUBE-7002` on load paths and update. Also update `CodeVendorPullFail`'s comment to say "produce side (vendor); the consume-side load is CUBE-7006". Commit `feat(diag): CUBE-7006 — dedicated code for bundle image load failures`.

- [ ] **Step 3: CUBE-3008 Poke IO code (RED first).** In flux/argocd poke tests (envtest-gated; run via `make test-engines`), assert a non-NotFound Get/Update failure is `CUBE-3008` — the cheap RED without envtest: a unit asserting the WRAPPED code via the fake client if one exists, else extend `internal/engine/contract/contract.go` with a subtest that Pokes through a client rigged to fail Update (`RECONCILE:` check what fake/interceptor machinery contract.go already uses; `sigs.k8s.io/controller-runtime/pkg/client/interceptor` is available via controller-runtime if none). Implement: add the constant; flip flux poke.go:45/:56 and argocd poke.go:40/:51 to `diag.CodePokeIOFail`; the target-missing paths (flux :62, argocd :35) and contract.go:160's 3007 assertion stay untouched. Contract suite must pass for BOTH engines (D2): `make test-engines`. Commit `feat(diag): CUBE-3008 — Poke transient-IO failures un-overload CUBE-3007`.

- [ ] **Step 4: Header range list.** `internal/diag/diag.go` package doc: `… 4xxx pack, 5xxx registry, 6xxx trust/hostname, 7xxx plugins/sync/vendor-bundle/repo, 8xxx release/bundle-integrity (reserved, unallocated)`. Commit with Step 5.

- [ ] **Step 5: Backtick ban-test hole (RED first).** Add to codes_test.go:

```go
// TestBanCatchesBacktickLiterals: raw-string CUBE literals must be flagged
// too — the scan previously matched only "\"CUBE-".
func TestBanCatchesBacktickLiterals(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "internal", "diag", "codes.go"), "package diag\n")
	mustWriteFile(t, filepath.Join(root, "internal", "x", "x.go"),
		"package x\n\nconst oops = `CUBE-9999: raw`\n")
	offenders, err := findCubeLiteralOffenders(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(offenders) != 1 {
		t.Fatalf("backtick literal not flagged: %v", offenders)
	}
}
```

(`mustWriteFile` helper: MkdirAll + WriteFile, write it fully.) RED, then fix the scan in `findCubeLiteralOffenders` — replace the single `strings.Contains` with a regexp matching a CUBE literal opened by EITHER quote character (double quote or backtick — raw strings previously escaped the check):

```go
var cubeLiteralRe = regexp.MustCompile("[\"`]CUBE-")
// … in the walker, replacing the strings.Contains check:
if cubeLiteralRe.MatchString(string(raw)) {
	offenders = append(offenders, rel)
}
```

Run the REAL tree scan too: if any production file now trips (a backtick literal existed), fix it by using the catalog constant in the same commit. Commit `test(diag): ban-test catches backtick CUBE literals; header range list extended`.

- [ ] **Step 6: Exhaustiveness test (used⊆defined ∧ defined⊆(used∪reserved)).** New test in codes_test.go:

```go
// TestCatalogExhaustive: every Code constant defined in codes.go is either
// referenced by identifier somewhere in non-test Go code or carries a
// "reserved" marker in its trailing comment (CUBE-3006 precedent); and every
// diag.Code* identifier used in non-test code is defined in codes.go.
func TestCatalogExhaustive(t *testing.T) {
	// 1. Parse codes.go with go/parser: collect ident -> code value, and
	//    whether the constant's line comment contains "reserved".
	// 2. Walk the repo's non-test .go files (reuse findCubeLiteralOffenders'
	//    walker skips) and collect every "diag.Code…" / (within package diag)
	//    bare "Code…" identifier via a word-boundary regexp.
	// 3. defined - used - reserved  => t.Errorf("unused, unreserved code %s (%s) — use it or annotate `// reserved:`")
	// 4. used - defined            => t.Errorf("undefined code identifier %s")
}
```

Write it fully (the comment names the exact algorithm; ~60 lines with go/parser's ast.GenDecl walk). Expected initial run: it must PASS after Steps 1–3 — if it flags anything real (e.g. a constant orphaned by an earlier phase), fix the flag in this commit by adding the `// reserved:` annotation or migrating the caller, and record which in the ledger. Commit `test(diag): catalog exhaustiveness — used⊆defined ∧ defined⊆(used∪reserved)`.

- [ ] **Step 7: Run + ledger.** Full suite + `make test-engines` green. Ledger `Task R4: complete (…)`.

---

### Task R5: Plugin polish

**Reconcile checkpoint:** 0.7 (trust-store keys, http client, Execute fallthrough).

**Files:**
- Modify: `internal/plugin/trust.go` (+ plugin_test.go), `internal/plugin/index.go` (+ index_test.go), `cmd/plugin.go` (+ cmd tests), `internal/diag/codes.go` (CUBE-7105), `README.md` (plugin section note).

- [ ] **Step 1: Canonical trust-store keys (RED first).** Test in `internal/plugin/plugin_test.go`:

```go
// TestTrustKeyCanonicalization: recording trust through a symlinked or
// relative path and checking through the resolved absolute path (or vice
// versa) must agree — the store keys on Abs+EvalSymlinks canonical paths.
func TestTrustKeyCanonicalization(t *testing.T) {
	// store isolation — RESOLVED (Task 0): existing plugin tests inline this
	// exact triple (no shared helper); mirror it verbatim:
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	dir := t.TempDir()
	real := filepath.Join(dir, "cube-idp-demo")
	if err := os.WriteFile(real, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link-to-demo")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if err := Trust("demo", link); err != nil { // record via the symlink
		t.Fatal(err)
	}
	if !isTrusted(real) { // look up via the real path
		t.Fatal("trust recorded via a symlink must be visible via the canonical path")
	}
}
```

RED (keys differ). Implement one helper used by BOTH record and lookup:

```go
// canonicalPath resolves path to its absolute, symlink-free form — the ONE
// trust-store key shape. Canonicalization failure falls back to the raw
// path (fail-safe: worst case is a re-prompt, never a false trust).
func canonicalPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return abs
	}
	return resolved
}
```

Apply in `Trust` (`m[canonicalPath(path)] = sum`), `isTrusted`, and `EnsureTrusted` (lookup AND the post-consent store write). Existing raw-key entries in a user's trust.json simply re-prompt once (fail-safe, documented in the commit message). GREEN; commit `fix(plugin): canonical trust-store keys — Abs+EvalSymlinks on record and lookup`.

- [ ] **Step 2: HTTP timeout for archive downloads.** `internal/plugin/index.go`: replace `http.DefaultClient` with a package-level `var indexHTTPClient = &http.Client{Timeout: 60 * time.Second}` (var, so index_test.go can shrink it if a timeout test is added — optional; the mandatory change is the client). Test: assert `fetchArchive` against an httptest server that sleeps past a test-shrunk timeout returns CUBE-7102 (index_test.go already spins httptest servers per G11 — mirror its pattern). Commit `fix(plugin): 60s HTTP timeout on index archive downloads`.

- [ ] **Step 3: Name charset guard (RED first).** cmd-level test (cmd/plugin_test.go or wherever plugin cmd tests live — `grep -rn "plugin trust" --include="*_test.go" cmd/`):

```go
// TestPluginNameCharsetGuard: option- or path-shaped names are refused with
// CUBE-7105 before any lookup/clone/exec — closes the `../`-shaped-name
// path escape (self-inflicted only, still worth closing).
func TestPluginNameCharsetGuard(t *testing.T) {
	for _, bad := range []string{"../evil", "-flag", "UPPER", "sp ace", "dot.dot", ""} {
		for _, sub := range [][]string{{"plugin", "trust", bad}, {"plugin", "install", bad, "--index", "https://example.invalid/repo.git"}} {
			root := NewRootCmd()
			root.SetOut(io.Discard); root.SetErr(io.Discard)
			root.SetArgs(sub)
			err := root.Execute()
			var de *diag.Error
			if !errors.As(err, &de) || de.Code != "CUBE-7105" {
				t.Fatalf("%v: want CUBE-7105, got %v", sub, err)
			}
		}
	}
}
```

Implement: `CodePluginNameInvalid Code = "CUBE-7105"` in codes.go (71xx section); in cmd/plugin.go a shared guard called first in trust's and install's RunE:

```go
var pluginNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

func validatePluginName(name string) error {
	if !pluginNameRe.MatchString(name) {
		return diag.New(diag.CodePluginNameInvalid,
			fmt.Sprintf("invalid plugin name %q", name),
			"plugin names are lowercase letters, digits, and hyphens (cube-idp-<name> binaries)")
	}
	return nil
}
```

GREEN; commit `feat(plugin): CUBE-7105 name charset guard on trust/install`.

- [ ] **Step 4: Document the flag-before-name limitation.** README plugin section (~line 368): add the two-liner — "Global flags go AFTER the plugin name: `cube-idp myplugin --plain` dispatches to the plugin, but `cube-idp --plain myplugin` does not (the plugin fallthrough inspects only the first argument)." Commit `docs(plugin): flag-before-plugin-name dispatch limitation`; ledger `Task R5: complete (…)`.

---

### Task R6: D15 kustomize substitution

**Reconcile checkpoint:** 0.8 (render paths, cnoe call site, exemplar test/fixture).

**Files:**
- Create: `internal/pack/testdata/gw-sub-kustomize/{kustomization.yaml,pack.cue,cm.yaml}`
- Modify: `internal/pack/kustomize.go`, `internal/pack/render.go`, `internal/pack/render_test.go` (or kustomize_test.go — wherever kustomize render tests live; `grep -rn RenderDir --include="*_test.go" internal/pack`). **RESOLVED (Task 0): zero direct `RenderDir` references in internal/pack tests — the kustomize path is exercised only indirectly through render_test.go's `RenderFor`/`Render` when a fixture ships kustomization.yaml; put the new test in render_test.go.**

**Interfaces:**

```go
// RenderDirFor kustomize-builds dir and applies the D15 gateway substitution
// to the built YAML bytes BEFORE parsing — the same pre-parse byte-level
// substitute() the manifests/ walk and renderHelm already apply, closing the
// documented D15 asymmetry. A zero gw is the identity (byte-identical to the
// pre-R6 RenderDir output).
func RenderDirFor(dir string, gw config.GatewaySpec) ([]*unstructured.Unstructured, error)

// RenderDir is RenderDirFor with a zero GatewaySpec — cnoe's loader and any
// gateway-less caller keep exactly today's behavior.
func RenderDir(dir string) ([]*unstructured.Unstructured, error)
```

- [ ] **Step 1: Failing test (mirrors `TestRenderForSubstitutesGatewayHost` on a kustomize fixture).**

Fixture `internal/pack/testdata/gw-sub-kustomize/`: `pack.cue` (`name: "gw-sub-kustomize"\nversion: "0.0.1"`), `cm.yaml` (a ConfigMap `gwsub-kz` with `data: {host: "${GATEWAY_HOST}", fqdn: "${GATEWAY_FQDN}", ns: "${GATEWAY_PACK}"}`), `kustomization.yaml` (`resources: [cm.yaml]`). Test:

```go
func TestRenderForSubstitutesGatewayHostKustomize(t *testing.T) {
	p, err := Fetch(context.Background(), "testdata/gw-sub-kustomize", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	gw := config.GatewaySpec{Pack: "traefik", Host: "cube-idp.localtest.me", Port: 8443}
	r, err := p.RenderFor(nil, gw)
	if err != nil {
		t.Fatal(err)
	}
	cm := r.Objects[0]
	for field, want := range map[string]string{
		"host": "cube-idp.localtest.me:8443",
		"fqdn": "cube-idp.localtest.me",
		"ns":   "traefik",
	} {
		if got, _, _ := unstructured.NestedString(cm.Object, "data", field); got != want {
			t.Fatalf("kustomize %s substitution: got %q want %q", field, got, want)
		}
	}
	// Zero-gw identity: tokens pass through untouched (the cnoe/Render path).
	r0, err := p.Render(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, _, _ := unstructured.NestedString(r0.Objects[0].Object, "data", "host"); got != "${GATEWAY_HOST}" {
		t.Fatalf("zero-gw kustomize render must not substitute, got %q", got)
	}
}
```

RED (tokens pass through under gw too).

- [ ] **Step 2: Implement.** kustomize.go: rename the body into `RenderDirFor(dir, gw)`; between `resMap.AsYaml()` and `ParseMultiDoc` insert `y = []byte(substitute(string(y), gw))`; add the two-line `RenderDir` wrapper. render.go:50: `RenderDir(p.Dir)` → `RenderDirFor(p.Dir, gw)`. cnoe loader untouched (calls `RenderDir`). Run the FULL pack test package: existing kustomize tests (token-free packs) stay green untouched — the zero-gw and no-token paths are byte-identical (substitute is a no-op on both).

- [ ] **Step 3: Commit + ledger.**

```bash
git add -A && git commit -m "feat(pack): D15 gateway substitution on the kustomize render path

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

Ledger `Task R6: complete (…)`.

---

### Task R7: Gateway coherence (the one design-bearing task — design fixed in spec §5.7)

**Reconcile checkpoint:** 0.9 (init/config/up/envoy-pack seams), 0.13 (e2e harness).

Two halves, committed separately: **(a)** de-fang the pack/ref trap at its source (init), **(b)** close the envoy CoreDNS in-cluster gap via the `gatewayService` pack contract.

#### R7a — `init` writes exactly one coherent gateway source

**Files:** `cmd/init.go` (+ init tests — `grep -rn "newInitCmd\|applyWizardToCube" --include="*_test.go" cmd/`), `README.md` (precedence paragraph).

**Interfaces:**

```go
// New flag (default preserves today's behavior exactly):
//   --gateway-pack string   gateway implementation pack: traefik | envoy-gateway (default "traefik")
// Wizard: a huh Select "Gateway pack" with the same two options, defaulting
// to traefik, stored in initWizardResult.GatewayPack and applied by
// applyWizardToCube.
//
// Coherence rule (spec §5.7a): the written cube.yaml carries
//   published mode: gateway.pack: <chosen>            (Ref empty)
//   --local mode:   gateway.pack: <chosen> AND gateway.ref: <abs>/packs/<chosen>
// — both derived from the ONE choice, never a ref for pack A with name B.
// gateway.ref stays optional and valid (schema unchanged, P4-D3); CUBE-0008
// (up's verifyGatewayPackRef) remains the backstop.
```

- [ ] **Step 1: Failing tests.**

```go
// TestInitLocalGatewayRefFollowsPack: init --local + --gateway-pack
// envoy-gateway writes ref packs/envoy-gateway AND pack envoy-gateway —
// the F11 trap (ref traefik, pack envoy) can no longer be authored by init.
func TestInitLocalGatewayRefFollowsPack(t *testing.T) {
	t.Chdir(t.TempDir())
	root := NewRootCmd()
	root.SetOut(io.Discard)
	root.SetArgs([]string{"init", "--name", "dev", "--local", "/repo", "--gateway-pack", "envoy-gateway"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	cube, err := config.Load("cube.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if cube.Spec.Gateway.Pack != "envoy-gateway" || cube.Spec.Gateway.Ref != filepath.Join("/repo", "packs", "envoy-gateway") {
		t.Fatalf("gateway source incoherent: pack=%q ref=%q", cube.Spec.Gateway.Pack, cube.Spec.Gateway.Ref)
	}
}

// TestInitPublishedGatewayPackOnly: without --local, choosing a gateway pack
// sets pack only — ref stays empty (published mode writes ONE source).
func TestInitPublishedGatewayPackOnly(t *testing.T) {
	t.Chdir(t.TempDir())
	root := NewRootCmd()
	root.SetOut(io.Discard)
	root.SetArgs([]string{"init", "--name", "dev", "--gateway-pack", "envoy-gateway"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	cube, err := config.Load("cube.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if cube.Spec.Gateway.Pack != "envoy-gateway" || cube.Spec.Gateway.Ref != "" {
		t.Fatalf("published mode must write pack only: pack=%q ref=%q", cube.Spec.Gateway.Pack, cube.Spec.Gateway.Ref)
	}
}
```

(NOTE: `--local /repo` — the flag path need not exist; init only joins paths. **RESOLVED (Task 0): init never stats the --local path — only `filepath.Abs` — so the literal `/repo` is fine.**) RED: the flag doesn't exist.

- [ ] **Step 2: Implement.** cmd/init.go:
  1. `var gatewayPack string` + `c.Flags().StringVar(&gatewayPack, "gateway-pack", "traefik", "gateway implementation pack: traefik | envoy-gateway")`; validate against the two known packs (unknown → `CUBE-0007` `CodeBadFlagValue`, the existing enum-flag code).
  2. `initWizardResult` gains `GatewayPack string` (default `"traefik"`); the wizard's second group gains `huh.NewSelect[string]().Title("Gateway pack").Options(huh.NewOption("traefik", "traefik"), huh.NewOption("envoy-gateway", "envoy-gateway")).Value(&res.GatewayPack)`; `wizardApplicable` additionally returns false when `--gateway-pack` was Changed (flags win — same rule as name/engine).
  3. Ordering fix: move the `--local` ref assignment AFTER the wizard overlay and derive from the final choice:

```go
// after applyWizardToCube (wizard) / flag resolution:
cube.Spec.Gateway.Pack = chosenGatewayPack // flag, or wizard answer
if local != "" {
	cube.Spec.Gateway.Ref = filepath.Join(abs, "packs", chosenGatewayPack)
	// (spec.packs --local rewrites unchanged)
} // published mode: Ref stays ""
```

  4. GREEN; existing init tests (default profile) byte-identical — default is traefik, ref logic for traefik unchanged.
- [ ] **Step 3: README precedence paragraph** (gateway table area, ~line 82): "Precedence: when both `spec.gateway.ref` and `spec.gateway.pack` are set, the REF decides what is fetched; `up` verifies the ref'd pack.cue name equals `gateway.pack` and fails with CUBE-0008 on mismatch. `cube-idp init` always writes the two coherently (`--gateway-pack`)."
- [ ] **Step 4: Commit.**

```bash
git add -A && git commit -m "feat(init): --gateway-pack — one coherent gateway source in both modes

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

#### R7b — `gatewayService` pack contract closes the envoy CoreDNS gap

**Files:** `internal/pack/pack.go` + `expose.go` (type) + pack_test.go, `internal/up/up.go` + up tests, `packs/envoy-gateway/{pack.cue,chart.yaml,manifests/10-gatewayclass.yaml}`, `internal/pack/helm_test.go` (network-gated collision check — `grep -rn "envoy" internal/pack/*_test.go` for the F8 network render test to extend), `tests/e2e/phase3_test.go` (+ e2e helper).

**Interfaces:**

```go
// internal/pack (expose.go, next to Expose — the D11-style data contracts):
// GatewayService is the optional pack.cue declaration of a gateway pack's
// DATA-PLANE Service (spec §5.7b): the CoreDNS *.<host> rewrite target.
// Absent, `up` falls back to today's <pack>.<pack>.svc default (traefik:
// zero migration).
type GatewayService struct{ Name, Namespace string }

// Pack gains:
//   GatewayService *GatewayService // nil when pack.cue declares none

// pack.cue schema (CUE, validated like expose:):
//   gatewayService?: { name: string, namespace: string }
// malformed -> CUBE-4003 CodePackCueInvalid (the images: precedent).

// internal/up:
// gatewayServiceFQDN derives the CoreDNS rewrite target from the RESOLVED
// gateway pack: the declared gatewayService if present, else the
// <pack>.<pack>.svc.cluster.local convention.
func gatewayServiceFQDN(gw config.GatewaySpec, gwPack *pack.Pack) string
```

- [ ] **Step 1: Parsing (RED first).** internal/pack tests:

```go
func TestGatewayServiceParsing(t *testing.T) {
	dir := t.TempDir()
	writePackCue(t, dir, `name: "gwp"
version: "0.1.0"
gatewayService: {name: "cube-idp-gateway", namespace: "envoy-gateway"}
`)
	p, err := Fetch(context.Background(), dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if p.GatewayService == nil || p.GatewayService.Name != "cube-idp-gateway" || p.GatewayService.Namespace != "envoy-gateway" {
		t.Fatalf("gatewayService: %+v", p.GatewayService)
	}
}

func TestGatewayServiceOptional(t *testing.T) { // packs predating the field load as before
	dir := t.TempDir()
	writePackCue(t, dir, "name: \"plain\"\nversion: \"0.1.0\"\n")
	p, err := Fetch(context.Background(), dir, "")
	if err != nil || p.GatewayService != nil {
		t.Fatalf("want nil GatewayService, got %+v (err %v)", p.GatewayService, err)
	}
}

func TestGatewayServiceMalformed(t *testing.T) { // missing namespace -> CUBE-4003
	dir := t.TempDir()
	writePackCue(t, dir, "name: \"gwp\"\nversion: \"0.1.0\"\ngatewayService: {name: \"x\"}\n")
	_, err := Fetch(context.Background(), dir, "")
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-4003" {
		t.Fatalf("want CUBE-4003, got %v", err)
	}
}
```

(`writePackCue` helper: MkdirAll+WriteFile pack.cue — check pack_test.go for an existing equivalent first.) Implement in `loadMeta` after the `images:` block, mirroring `parseExpose`'s unify-against-schema pattern:

```go
const gatewayServiceSchemaCUE = `{ name: string, namespace: string }`

if gv := v.LookupPath(cue.ParsePath("gatewayService")); gv.Exists() {
	schema := ctx.CompileString(gatewayServiceSchemaCUE)
	unified := schema.Unify(gv)
	if err := unified.Validate(cue.Concrete(true)); err != nil {
		return nil, diag.Wrap(err, diag.CodePackCueInvalid,
			fmt.Sprintf("gatewayService: block in %s/pack.cue is invalid", dir),
			`gatewayService needs both name and namespace, e.g. gatewayService: {name: "cube-idp-gateway", namespace: "envoy-gateway"}`)
	}
	var gs GatewayService
	if err := unified.Decode(&gs); err != nil {
		return nil, diag.Wrap(err, diag.CodePackCueInvalid,
			fmt.Sprintf("gatewayService: block in %s/pack.cue is invalid", dir),
			"gatewayService.name and .namespace must be strings")
	}
	p.GatewayService = &gs
}
```

Commit `feat(pack): optional gatewayService data contract (D11-style)`.

- [ ] **Step 2: Derivation in `up` (RED first).** Unit test in internal/up (no cluster needed — pure function):

```go
func TestGatewayServiceFQDNDerivation(t *testing.T) {
	gw := config.GatewaySpec{Pack: "envoy-gateway"}
	declared := &pack.Pack{Name: "envoy-gateway",
		GatewayService: &pack.GatewayService{Name: "cube-idp-gateway", Namespace: "envoy-gateway"}}
	if got := gatewayServiceFQDN(gw, declared); got != "cube-idp-gateway.envoy-gateway.svc.cluster.local" {
		t.Fatalf("declared: %q", got)
	}
	plain := &pack.Pack{Name: "traefik"}
	if got := gatewayServiceFQDN(config.GatewaySpec{Pack: "traefik"}, plain); got != "traefik.traefik.svc.cluster.local" {
		t.Fatalf("default: %q", got)
	}
	if got := gatewayServiceFQDN(config.GatewaySpec{Pack: "traefik"}, nil); got != "traefik.traefik.svc.cluster.local" {
		t.Fatalf("nil pack must fall back to the default: %q", got)
	}
}
```

Implement:

```go
func gatewayServiceFQDN(gw config.GatewaySpec, gwPack *pack.Pack) string {
	if gwPack != nil && gwPack.GatewayService != nil {
		return fmt.Sprintf("%s.%s.svc.cluster.local", gwPack.GatewayService.Name, gwPack.GatewayService.Namespace)
	}
	return fmt.Sprintf("%s.%s.svc.cluster.local", gw.Pack, gw.Pack)
}
```

Call site (up.go:341): `trust.EnsureCoreDNSRewrite(ctx, a.Client(), cube.Spec.Gateway.Host, gatewayServiceFQDN(cube.Spec.Gateway, packs[0]), dnsTimeout)` — `packs[0]` is the gateway pack (prepended at :238; guard `len(packs) > 0`, which always holds there — assert with a comment, not a branch, unless the compiler disagrees). Update the function's doc comment (it currently claims the traefik hardcode). Commit `feat(up): CoreDNS rewrite target honors the pack's declared gatewayService`.

- [ ] **Step 3: envoy pack changes + collision check.**

`packs/envoy-gateway/manifests/10-gatewayclass.yaml` EnvoyProxy: ADD `name: cube-idp-gateway` under `envoyService` (alongside type/externalTrafficPolicy/patch) and REWRITE the long "deliberately UNSET" comment to the new truth: the F9 hijack was the COLLIDING name `envoy-gateway` (the controller's own xDS Service); `cube-idp-gateway` is stable and non-colliding; the StrategicMerge NodePort patch is name-agnostic. `packs/envoy-gateway/pack.cue`: append

```cue
// Data-plane Service (Phase 4 R7b): a stable, NON-colliding name for the
// generated Envoy proxy Service, declared so `up` points the CoreDNS
// *.<host> rewrite at the DATA PLANE instead of the controller Service.
gatewayService: {name: "cube-idp-gateway", namespace: "envoy-gateway"}
```

`packs/envoy-gateway/chart.yaml`: delete the KNOWN GAP paragraph (the gap is closed).

**Collision check (spec §7 risk):** extend the existing network-gated envoy render test (the F8 one that pins CRDs+certgen in the rendered stream — find via `grep -rn "envoy" internal/pack/*_test.go internal/up/*_test.go`) with: no rendered object is a `v1 Service` named `cube-idp-gateway` in namespace `envoy-gateway` (the name must be free for EG's generated Service), and the pack's parsed `GatewayService` matches the EnvoyProxy's `envoyService.name` (read the manifest object). **RESOLVED (Task 0): the network-gated render test is `TestStarterPacksRender` at `tests/packs_render_test.go:48`, gated by `testing.Short()` (skip message "helm renders hit the network") — extend it there under the same gate.** Commit `feat(packs/envoy-gateway): stable cube-idp-gateway data-plane service + gatewayService declaration`.

- [ ] **Step 4: e2e proof — the in-cluster CoreDNS assertion (the exact flow broken today).**

Extend `TestEnvoyGatewaySmoke` (phase3_test.go:521) between `assertGatewayTLS` and `down`:

```go
// In-cluster *.<host> must be served by the DATA PLANE via the CoreDNS
// rewrite (the F9/KNOWN-GAP flow): run a one-shot curl pod against
// https://gitea.<host> and require success. Pre-R7b this resolved to the
// envoy CONTROLLER Service and could never answer.
assertInClusterHTTP(t, provider, name, "https://gitea.cube-idp.localtest.me")
```

New helper in phase3_test.go (client-go is already imported; build the clientset from the provider kubeconfig the way existing helpers do — reuse their pattern):

```go
// assertInClusterHTTP creates a curlimages/curl pod in the default namespace
// running `curl -fskS -o /dev/null <url>` (-k: the pod does not trust the
// cube CA; DNS + data-plane reachability is what's under test), polls its
// phase to Succeeded within 2 minutes, and dumps logs on failure. The pod is
// deleted in t.Cleanup.
func assertInClusterHTTP(t *testing.T, provider, clusterName, url string)
// contract — RESOLVED (Task 0): build the clientset exactly like the existing
// clusterClientset(t, dir) helper (phase3_test.go:209): config.Load →
// cluster.New → Provider.Ensure (connect-only) → kubernetes.NewForConfig(conn.REST);
// pod spec: restartPolicy Never, one container
// image "curlimages/curl:8.10.1", command ["curl","-fskS","-o","/dev/null",url];
// poll clientset.CoreV1().Pods("default").Get every 3s until Succeeded
// (pass) or Failed/timeout (t.Fatalf with pod logs).
```

Local GREEN leg (real Docker, protected-cluster rules apply):

```bash
CUBE_IDP_E2E=1 CUBE_IDP_E2E_GATEWAY_PORT=18443 go test ./tests/e2e/ -run TestEnvoyGatewaySmoke -v -timeout 30m
```

Also run the traefik non-regression arbiter (`TestSyncOneShot`) once — the CoreDNS derivation change touches the default path's call site:

```bash
CUBE_IDP_E2E=1 CUBE_IDP_E2E_GATEWAY_PORT=18443 go test ./tests/e2e/ -run TestSyncOneShot -v -timeout 30m
```

- [ ] **Step 5: Commit + ledger.**

```bash
git add -A && git commit -m "test(e2e): envoy smoke proves in-cluster *.<host> reaches the data plane

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

Ledger `Task R7: complete (…)` recording both live-leg results.

---

### Task R8: Hygiene + test hardening

**Reconcile checkpoint:** 0.10 (the verified file:line list + G15a's cycle facts).

Each item: failing test where testable → fix → suite → one commit per coherent group (three commits below).

- [ ] **Step 1 (group 1: helper consolidation + determinism).**
  1. **`IsLocalRegistryHost` — one home.** Export from `internal/pack/source.go` (rename `isLocalRegistryHost` → `IsLocalRegistryHost`, docstring: "reports whether host (optionally host:port) is a loopback registry — the only case where plain HTTP is acceptable; the ONE shared definition (Phase 4 R8)"); table test in source_test.go:

```go
func TestIsLocalRegistryHost(t *testing.T) {
	for host, want := range map[string]bool{
		"127.0.0.1": true, "127.0.0.1:5000": true, "localhost": true,
		"localhost:30500": true, "ghcr.io": false, "ghcr.io:443": false,
		"127.0.0.1.evil.com": false, "": false,
	} {
		if got := IsLocalRegistryHost(host); got != want {
			t.Errorf("%q: got %v want %v", host, got, want)
		}
	}
}
```

  Delete the copies at `internal/oci/pushdir.go:209-219` and `internal/bundle/vendor.go:359-369`; both call sites import nothing new (`oci` and `bundle` already import `internal/pack` — G15a). Byte-equal behavior is the table test (all three copies were textually identical).
  2. **Content-derived created annotation (RED first).** Test in pushdir_test.go: push the same demo pack twice to the in-process registry and assert the two returned digests are EQUAL (extend/duplicate `TestPushPackDirRoundTripsThroughFetch`'s arrangement). RED (time.Now differs). Fix pushdir.go:118: `ocispec.AnnotationCreated: "1970-01-01T00:00:00Z"` with the comment "fixed epoch, NOT wall time: identical content must republish to an identical digest so the CI pack republish is a true no-op (Phase 4 R8; annotation consumers only need a valid RFC3339 value)". GREEN.
  3. Commit `fix(oci,pack,bundle): one IsLocalRegistryHost; deterministic pack-push digests`.

- [ ] **Step 2 (group 2: leaks + duplication + shadowing).**
  1. **Syncer temp-dir cleanup.** `loadOrSynthesize` returns `(*pack.Pack, func(), error)` — cleanup is `func(){}` for the real-pack path, `func(){ os.RemoveAll(tmp) }` for the synthesized path; `SyncOnce` does `p, cleanup, err := loadOrSynthesize(dir); if err != nil { return Result{}, err }; defer cleanup()` (after Render is when the dir stops being needed, but deferring to SyncOnce-exit is equivalent and simpler — the render happens within the call). Test: run `SyncOnce` far enough to render (use the existing syncer test fakes — `grep -rn loadOrSynthesize --include="*_test.go" internal/syncer`) or unit-test `loadOrSynthesize` directly: synthesize, call cleanup, assert the dir is gone.
  2. **`deployRepo` wrap dedup** (cmd/repo.go):

```go
func deployRepo(ctx context.Context, a *apply.Applier, eng engine.Engine, name string, repoInfo *gitea.Repo) error {
	wrap := func(err error) error {
		return diag.Wrap(err, diag.CodeRepoDeployFail, "created the repo but could not register the deploy source",
			fmt.Sprintf("re-run `cube-idp repo create %s --deploy` — repo creation is idempotent", name))
	}
	src := engine.GitSource{ /* unchanged */ }
	objs, err := eng.DeliverGit(ctx, name, src)
	if err != nil { return wrap(err) }
	if err := a.Apply(ctx, objs, false, repoDeployTimeout); err != nil { return wrap(err) }
	if err := a.RecordInventory(ctx, objs); err != nil { return wrap(err) }
	return nil
}
```

  3. **`pr` loop-var rename** (up.go:251): `for i, pr := range refs` → `for i, pref := range refs` (+ the three body uses `pref.Ref`/`pref.Values`); delete the now-moot half of the `pk` comment if it references the shadow.
  4. Commit `fix: syncer temp-dir cleanup, deployRepo wrap dedup, up loop-var shadowing`.

- [ ] **Step 3 (group 3: test hardening + notes).**
  1. **Trust-consent positive assertion** (cmd/trust_test.go:97): after the existing negative check, add `if !strings.Contains(out.String(), "your cube-idp gateway's HTTPS") { t.Fatalf("generic fallback wording missing:\n%s", out.String()) }`.
  2. **Flux uninstall test label scoping** (uninstall_test.go:117): add the cube label selector to the List — `a.Client().List(ctx, list, client.InNamespace(fluxNS), client.MatchingLabels{"cube-idp.dev/cube": <the test's cube name>})`; `RECONCILE:` read the test to get the exact cube name the Applier was built with and the exact label key constant (grep `cube-idp.dev/cube` — it is the Phase 1 cube label; use the Go constant if one is exported). Then remove whatever `t.Cleanup` cross-test papering Task 10a added for this coupling (grep poke_test.go:59's note) IF the scoping makes it redundant; keep it if it guards something else — record which in the commit.
  3. **e2e lint items** (e2e_test.go deleteLingeringCluster): replace the manual loop with `if slices.Contains(strings.Fields(string(out)), cubeName) { … }` (imports `slices`); apply `strings.FieldsSeq` where the linter names it. `RECONCILE:` run `go vet ./tests/...` + the repo's linter to enumerate the exact 329-area findings and fix precisely those.
  4. **awk header note** (hack/inject-argocd-cmd-params.awk, top of file): add `# FRAGILITY NOTE: this script matches the argocd-cmd-params-cm document positionally/textually; an argo-cd version bump can reorder or reformat install.yaml and silently break the injection — hack/gen-argocd-manifests.sh --check (CI) is the tripwire; re-verify this script on every bump.`
  5. Commit `test: consent-wording positive assertion, label-scoped flux uninstall list, e2e lint, awk fragility note`.

- [ ] **Step 4: Run + ledger.** Full suite + `make test-apply` (syncer/up/flux tests are envtest-gated there) green. Ledger `Task R8: complete (…)`.

---

### Task R9: Docs pass

**Reconcile checkpoint:** 0.11 (README/docs state); R1/R3/R5/R7 docs already landed in their tasks — this task is the sweep and the remainder.

**Files:** `README.md`, `docs/machine-readable-output.md`, `CHANGELOG.md`.

- [ ] **Step 1: Verify the actual missing-docs set against `--help`.** Build (`go build -o /tmp/cube-idp .`) and diff every command/flag against README: `/tmp/cube-idp --help`, and `--help` for each of the 17 commands (G16's registration list). Authoring-time gaps to confirm: `config schema` (document under the cube.yaml reference: "print the CUE schema cube.yaml validates against — every CUBE-0002 remediation points here"), `down --keep-cluster` (Day 2 section: "delete cube-idp resources but keep the cluster"), `vendor --lock` (Air-gapped section: non-default lock path). Add whatever else the sweep finds; prune stale claims (anything --help contradicts).
- [ ] **Step 2: `machine-readable-output.md` completeness.** Add the `encode_error` event type (envelope-level: `{"v":1,"type":"encode_error","error":"…"}` — note it carries NO `ts`, per json.go:31's literal, and document why it exists: a marshal failure must surface on-stream, never drop an event silently. `RECONCILE:` re-read json.go:31 post-R3 and document the EXACT emitted shape). Re-verify every field table against the post-R3 code (the doc's accuracy-because-written-against-code property is the point): each event struct in event.go ↔ its table; the per-command coverage table added by R3 is present and complete (up, down, vendor, sync, repo create, plugin ×3, pack push).
- [ ] **Step 3: Gateway/envoy sweep.** Confirm R7a's precedence paragraph reads correctly in context; `grep -in "envoy" README.md` and remove any remaining in-cluster caveat (per G16's RECONCILE — possibly none existed in README; the chart.yaml/manifest comments were removed in R7b). Confirm the R1 install section renders (backtick nesting) and CHANGELOG.md's v0.1.0 header is still accurate after R2–R8 (amend the Phase 4 bullet list if scope shifted).
- [ ] **Step 4: Commit + ledger.**

```bash
git add -A && git commit -m "docs: v0.1.0 documentation pass — command coverage, event-schema completeness, gateway precedence

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

Ledger `Task R9: complete (…)`.

---

### Task R10: Cut v0.1.0 (exit gate — contains this phase's ONLY push, owner-gated)

**Reconcile checkpoint:** everything above merged to main; 0.12 (release workflow present); 0.13 (arbiters).

- [ ] **Step 1: Full suite at HEAD.** `go build ./... && go vet ./... && go test ./... -short -count=1 && make test-apply && make test-engines` — ALL green.
- [ ] **Step 2: Full local e2e sweep — the five Phase 3 arbiters** (deferred-commands convention: run serially, real Docker, port 18443, protected-cluster rules absolute):

```bash
CUBE_IDP_E2E=1 CUBE_IDP_E2E_GATEWAY_PORT=18443 go test ./tests/e2e/ -run TestK3dUpDown -v -timeout 30m
CUBE_IDP_E2E=1 CUBE_IDP_E2E_GATEWAY_PORT=18443 go test ./tests/e2e/ -run TestVendorBundleOffline -v -timeout 30m
CUBE_IDP_E2E=1 CUBE_IDP_E2E_GATEWAY_PORT=18443 go test ./tests/e2e/ -run TestSyncOneShot -v -timeout 30m
CUBE_IDP_E2E=1 CUBE_IDP_E2E_GATEWAY_PORT=18443 go test ./tests/e2e/ -run TestRepoCreateDeploy -v -timeout 30m
CUBE_IDP_E2E=1 CUBE_IDP_E2E_GATEWAY_PORT=18443 go test ./tests/e2e/ -run TestEnvoyGatewaySmoke -v -timeout 30m
```

All five PASS, no leaked clusters (`kind get clusters` / `k3d cluster list` show only pre-existing protected ones). Record durations in the ledger.

- [ ] **Step 3: ⛔ STOP — OWNER GO-AHEAD REQUIRED.** **Do NOT tag. Do NOT push.** Tagging and pushing `v0.1.0` is this phase's only push and requires the owner's explicit confirmation AT THIS MOMENT, with the Step 1+2 evidence presented. The executing agent halts here and asks; "the plan said so" is not consent — only the owner's live answer is. If the owner declines or amends, record the decision in the ledger and stop the task.
- [ ] **Step 4 (after explicit owner go-ahead): tag + push.**

```bash
git tag -a v0.1.0 -m "cube-idp v0.1.0 — first (private) release"
git push origin main --follow-tags
```

Watch the `release` workflow (`gh run watch`); it must produce the GitHub Release with four `.tar.gz` assets + `checksums.txt`.

- [ ] **Step 5: Release smoke FROM THE ARTIFACT (not a local build).** In a clean temp dir:

```bash
cd "$(mktemp -d)"
gh release download v0.1.0 -R RafPe/cube-idp -p 'cube-idp_*_darwin_arm64.tar.gz' -p checksums.txt
grep darwin_arm64 checksums.txt | shasum -a 256 -c -
tar xzf cube-idp_*_darwin_arm64.tar.gz
./cube-idp version   # MUST print: cube-idp version 0.1.0 (commit <sha>, built <date>) — RECONCILE: v-prefix presence in goreleaser's {{.Version}}; assert the semver and commit are present and it is NOT "dev"
./cube-idp init --name smoke && CUBE_IDP_E2E_GATEWAY_PORT= ./cube-idp up && ./cube-idp status && ./cube-idp get secrets && ./cube-idp down
```

(the smoke cube uses default port 8443 only if free — on the dev machine EDIT cube.yaml's `gateway.port: 18443` after init, per the standing constraint; name the cluster `smoke`, which is neither protected nor leak-prone, and verify `down` removed it.)

- [ ] **Step 6: Close out.** If the smoke found anything: open a findings register section in this plan (F-prefixed, Phase 3 convention), fix-wave loop (TDD, review, merge), re-run the smoke — the release may be re-cut as v0.1.1 ONLY with a fresh owner go-ahead. Then: tick every checkbox in this plan, update the STATUS banner to EXECUTED, final ledger lines (`Task R10: complete (…)`, `PHASE 4 CLOSED — v0.1.0 released`), commit:

```bash
git add -A && git commit -m "docs: phase 4 closed — v0.1.0 released, plan and ledger finalized

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

(Pushing THAT commit is again owner-gated — ask.)

---

## Self-Review

**1. Spec §5 coverage — item → task:**

| Spec item | Plan task |
|---|---|
| §5.1 R1 goreleaser 4-platform builds, CGO off, tar.gz+checksums | R1 Steps 3–4 |
| §5.1 R1 version stamping (version/commit/date; RECONCILE'd to `cmd.Version` + new `cmd.Commit`/`cmd.Date`) | R1 Steps 1–2 (G2) |
| §5.1 R1 changelog (goreleaser groups + curated CHANGELOG.md seed) | R1 Steps 3, 7 |
| §5.1 R1 CI: release.yaml on v* + snapshot on every main push | R1 Steps 5–6 |
| §5.1 R1 private install docs (gh release download; GOPRIVATE note) | R1 Step 7 |
| §5.1 acceptance (local snapshot artifacts + stamped version) | R1 Step 4 |
| §5.2 R2 Manifest v2 packHashes(dirhash h1)/imageHashes(sha256), Vendor writes v2, Open rejects v1 w/ CUBE-7003 | R2 Steps 1–2 |
| §5.2 R2 Verify recomputes, CUBE-7004 names entry; honest docstring + up step line restored | R2 Step 2 (items 3, 8) |
| §5.2 R2 extraction caps (16 GiB/4 GiB, CUBE-7003 wrap, test seam) | R2 Steps 1–2 (item 4) |
| §5.2 R2 tamper/v1/cap tests | R2 Step 1 |
| §5.3 R3 five commands onto event stream, plain byte-identical | R3 Steps 1–5 (Step 0 pins) |
| §5.3 R3 live view only for vendor; sync --watch untouched | R3 Steps 1–3 (RunPipelineStatic) |
| §5.3 R3 JSON `"v":1` everywhere + docs in SAME commits | R3 Steps 2–5 |
| §5.3 R3 per-command golden event-slice tests | R3 Steps 2–5 |
| §5.4 R4 CUBE-1003 re-scope/rename, stale comment dropped | R4 Step 1 |
| §5.4 R4 dedicated load-side 7xxx code, two provider files migrated | R4 Step 2 (CUBE-7006) |
| §5.4 R4 CUBE-3007 Poke overload → transient-IO code | R4 Step 3 (CUBE-3008) |
| §5.4 R4 diag.go header range list 6xxx/7xxx/8xxx | R4 Step 4 |
| §5.4 R4 backtick ban-test hole | R4 Step 5 |
| §5.4 R4 exhaustiveness test (used⊆defined ∧ defined⊆used∪reserved) | R4 Step 6 |
| §5.5 R5 trust-key canonicalization (record AND lookup) | R5 Step 1 |
| §5.5 R5 60s HTTP timeout on index downloads | R5 Step 2 |
| §5.5 R5 name charset guard on install/trust | R5 Step 3 (CUBE-7105) |
| §5.5 R5 flag-before-plugin-name README note | R5 Step 4 |
| §5.6 R6 kustomize-path D15 substitution + mirrored test + byte-identical token-free packs | R6 Steps 1–3 |
| §5.7a R7 init writes one coherent gateway source (wizard + flags); README precedence; CUBE-0008 backstop unchanged | R7a Steps 1–3 |
| §5.7b R7 gatewayService pack.cue contract, Pack field, up derivation w/ traefik default | R7b Steps 1–2 |
| §5.7b R7 envoy pack: stable non-colliding envoyService.name + declaration; KNOWN GAP removed | R7b Step 3 |
| §5.7b R7 collision check (spec §7 risk) | R7b Step 3 (render test) |
| §5.7b R7 e2e in-cluster curl through gitea.<host> | R7b Step 4 |
| §5.8 R8 isLocalRegistryHost consolidation (cycle-free home) | R8 Step 1.1 (G15a) |
| §5.8 R8 content-derived created annotation + digest-stability test | R8 Step 1.2 |
| §5.8 R8 syncer temp-dir cleanup | R8 Step 2.1 |
| §5.8 R8 deployRepo wrap dedup | R8 Step 2.2 |
| §5.8 R8 pr loop-var shadowing rename | R8 Step 2.3 |
| §5.8 R8 trust-consent positive assertion; flux-system label-scoped list; e2e lint; awk note | R8 Step 3 |
| §5.9 R9 README missing-command sweep vs --help; install; precedence; envoy caveat; stale claims | R9 Steps 1, 3 (+R1/R5/R7 in-task) |
| §5.9 R9 machine-readable-output.md encode_error + field re-verify | R9 Step 2 |
| §5.9 R9 CHANGELOG curated | R1 Step 7 + R9 Step 3 |
| §5.10 R10 full suite + test-engines at HEAD | R10 Step 1 |
| §5.10 R10 five-arbiter local e2e sweep (port 18443) | R10 Step 2 |
| §5.10 R10 tag/push OWNER-GATED (spec §8.6) | R10 Step 3 (explicit STOP) — Step 4 only after go-ahead |
| §5.10 R10 downloaded-artifact smoke (init→up→status→get secrets→down; version prints v0.1.0+commit) | R10 Step 5 |
| §5.10 R10 ledger/plan close-out + fix-wave loop if smoke finds anything | R10 Step 6 |
| §6 testing strategy (RED documented before GREEN; e2e matrix unextended except R7b) | Global Constraints + per-task steps |
| §8.1–8.6 zero-context enablement | Task 0 + Ground Truth G1–G20 + Global Constraints + CUBE allocations + R10 gate |

**2. Placeholder scan:** no TBDs; no "similar to task N"; no bare "add error handling". Every code step is either complete code or a comment-contract naming the exact functions, codes, message strings, and edge cases (R2 Step 2's Verify body, R4 Step 6's parser algorithm, R7b Step 4's pod-probe contract, R3's per-command migrations with exact line inventories in G7). Remaining `RECONCILE:` markers (grep to confirm) and why each is legitimately unresolvable from the tree at authoring time: **(1)** goreleaser v2 availability/exact schema acceptance + the dist dir layout (external tool, not in the repo — Task 0.12/R1 Steps 3–5); **(2)** the exact test-isolation helper for `os.UserConfigDir` in plugin tests and Console test-construction in bundle tests (existing test-suite internals to mirror, named greps given — R5 Step 1, R3 Step 2); **(3)** the linter's exact e2e findings at e2e_test.go:329-area and the flux uninstall test's cube-name/label constant (R8 Steps 3.2–3.3); **(4)** the envoy network-render test's file/gate to extend (R7b Step 3); **(5)** README's envoy caveat presence (G16/R9 Step 3); **(6)** whether init stats the --local path (R7a Step 1); **(7)** goreleaser {{.Version}} v-prefix in the R10 smoke assertion. Each names exactly what to check and where.

**3. Type consistency:** `Manifest` v2 fields declared once (R2 Interfaces) and used identically in Vendor/Verify/tests; `RunPipelineStatic`/`render.Styled` declared once (R3 Interfaces) and consumed by four commands; `bundle.Vendor`'s new Console signature declared in R3 and threaded through cmd/vendor.go + all test call sites; `syncer.Stepper` satisfied by both `*ui.Console` and `*ui.Printer` because their `Step(string, string, ...any)` signatures are verifiedly identical (G6); `pack.GatewayService`/`Pack.GatewayService` declared once (R7b Interfaces), consumed by `gatewayServiceFQDN(gw, gwPack)` whose both-callers shape is given at its single call site; `pack.IsLocalRegistryHost` export direction respects the verified import graph (oci→pack, bundle→{oci,pack} — G15a); CUBE constants: every new identifier (`CodePokeIOFail`, `CodeBundleImageLoadFail`, `CodePluginNameInvalid`, `CodeClusterFieldsConflict`) is declared in the allocations section AND its task body with matching names.

**4. Open questions:** NONE requiring owner input before execution. The single owner interaction is BUILT INTO R10 Step 3 by design (the tag/push go-ahead, spec §8.6) — it is a gate, not an open question. Everything else was resolved from the live tree (Ground Truth) or is a named RECONCILE with its verification recipe.
