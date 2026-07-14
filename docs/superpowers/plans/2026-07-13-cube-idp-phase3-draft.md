# cube-idp Phase 3 Implementation Plan

> **STATUS: DRAFT — PARTIALLY RECONCILED 2026-07-14 against the EXECUTED Phase 2** (commits `0522799..ccd8785`, all Phase 2 tasks complete incl. the UX addendum; execution record `.superpowers/sdd/progress.md`). This reconciliation pass fixed the CUBE-code collisions (3006→**3007**, 4011→**4015**, 1002 confirmed free), corrected the dependency posture (fluxcd/pkg/oci is GONE), pinned the gateway node port to **30443**, and added the "Phase 2 Ground Truth" section after Task 0 pre-answering checkpoints 0.1–0.11 plus a new **Task 0.5 (Phase 2 debt paydown)**. Task 0 remains blocking at execution start — it verifies the ground-truth section against the then-current tree and applies the recorded body rewrites (Task 6's lock-schema swap, Task 3's oras rewrite) before any task runs.
> **MANDATORY GATE — RECONCILE AFTER PHASE 2:** This draft was written before Phases 1 and 2 were implemented (Phase 2 exists only as its own pre-implementation draft, `2026-07-13-cube-idp-phase2-draft.md`, which will itself change during its reconciliation). Phase 3 builds on BOTH. Before executing ANY task, the consuming agent MUST complete the **"Task 0: Reconciliation Gate"** below and update this plan to match the actual post-Phase-2 codebase (including the final Phase 2 plan, which is itself a draft that will change during reconciliation). Executing a task whose reconcile checkpoint has not been verified is a plan violation.
>
> Throughout this document, `RECONCILE: …` marks a statement that depends on prior-phase implementation detail and says exactly what to verify. That is the only allowed deferral form in this plan — there are no TBDs.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. **Task 0 is blocking: no other task may start before it is checked off.**

**Goal:** Ship cube-idp Phase 3 (spec §6): the k3d cluster provider, air-gapped `vendor`/`up --bundle`, exec-plugin discovery with a sha256-pinned index, the pack catalog buildout with CI-published OCI packs, `sync --watch` live delivery (D7), and `repo create [--deploy]` — one command from empty Gitea repo to deployed.

**Architecture:** Everything here extends existing Phase 1/2 seams without new architecture: k3d is a third compiled-in `cluster.Provider` (D4/D8), vendor/bundle is a pure consumer of Phase 2's `cube.lock`, sync reuses the pack-render → OCI-push → `GitOpsEngine.Deliver` pipeline with fsnotify in front, exec plugins are tier-2 extensibility (spec §4.4 — PATH binaries, env-var contract, no RPC), and `repo create` composes the gitea pack's credentials surface with a new engine-native git delivery shape. Two deliberate interface extensions to `engine.Engine` (`Poke`, `DeliverGit`) must land in **both** engine implementations and the shared contract suite, or not at all (D2's "an abstraction with one implementation is a lie").

**Tech Stack (corrected 2026-07-14):** Go 1.26 (`go-version-file: go.mod` everywhere — never hardcode), `github.com/k3d-io/k3d/v5` (library), `github.com/fsnotify/fsnotify`, existing stack from EXECUTED Phases 1–2: cobra, cuelang, fluxcd/pkg/ssa, **oras-go v2 (the ONLY OCI library — Phase 2 Task 3.5 dropped `fluxcd/pkg/oci` AND `go-containerregistry` entirely)**, helm.sh/helm/v4 (v3 is gone), the RafPe go-getter fork (`replace github.com/hashicorp/go-getter => github.com/rafpe/go-getter v1.9.0`), go-git v5 (pin probing), smallstep/truststore, charmbracelet lipgloss+huh, client-go. `go-containerregistry` is therefore a NEW direct dependency if Tasks 3/6 keep it — Task 0 decision: prefer reworking the bundle layout onto oras-go v2's `content/oci` store (one OCI library, per the Phase 2 consolidation) and an in-process test registry via zot or oras memory targets; re-adding go-containerregistry is the fallback, not the default.

**Spec:** `docs/superpowers/specs/2026-07-13-cube-idp-architecture-design.md` — source of truth. Phase 1 plan: `docs/superpowers/plans/2026-07-13-cube-idp-phase1-mvp.md`. Phase 2 plan (RESOLVED — EXECUTED, all tasks ticked): `docs/superpowers/plans/2026-07-13-cube-idp-phase2-draft.md`; its "Task 0 Findings" section and the ledger `.superpowers/sdd/progress.md` are the authoritative record of what actually landed.

## Global Constraints (every task inherits these)

- Module path: `github.com/rafpe/cube-idp` (RECONCILE: confirm unchanged in `go.mod` after Phase 2).
- Single static binary; nothing runs on the developer machine after exit (spec §3). `sync --watch` is the one sanctioned long-running *foreground* mode — it is still not a daemon: Ctrl-C exits cleanly and leaves only in-cluster state.
- Config: `apiVersion: cube-idp.dev/v1alpha1`, `kind: Cube` (D5). Users author YAML; CUE is internal only.
- Every user-facing failure is a typed `CUBE-xxxx` `diag.Error` with remediation (spec §4.5); every wait has a hard deadline; no silent fallbacks.
- SSA field manager `cube-idp`; cube label `cube-idp.dev/cube: <name>`; prune opt-out `cube-idp.dev/prune: disabled`; system namespace `cube-idp-system`. **CLI-secret labels are DEPRECATED as of Phase 2 (D11):** the primary credentials surface is the Pack record's `expose.authSecretRef` (`kubectl get packs`); the labels are honored for one release only — new Phase 3 packs (Task 4) declare `expose:` blocks, not labels.
- **CUBE-code catalog + literal-ban (Phase 2 Task 0.5, enforced by test):** every code is a constant in `internal/diag/codes.go`; NO `"CUBE-` string literal may appear in non-test Go code — `TestNoCubeLiteralsOutsideCatalog` fails the build otherwise. This plan's code snippets show literals for readability; implementers MUST add catalog constants and use them.
- **Plain-output invariant (Phase 2 Task 13.8):** command output goes through `internal/ui` (`Printer.Step`, `Section`, `Glyph`, `Progress`); plain mode (non-TTY / `CI` / `--plain` persistent flag) is byte-stable and many tests assert on it. New Phase 3 commands (`pack push`, `vendor`, `plugin`, `sync`, `repo create`) adopt the same helpers from day one; `sync --watch`'s live output must degrade to plain line-per-event when piped.
- **CUBE code ranges:** `0xxx` preflight/config, `1xxx` cluster, `2xxx` apply, `3xxx` engine, `4xxx` pack, `5xxx` registry (Phase 1); `6xxx` **reserved for Phase 2 trust/hostname — do not allocate here**; `7xxx` **NEW in this phase: exec-plugins / sync / vendor-bundle**.
- **New CUBE codes introduced by this plan** (documented here per convention; each defined at its point of use):
  - `CUBE-1002` `config render-cluster` on a provider that creates no cluster (RESOLVED 2026-07-14: 1002 is free — in-use 1xxx codes after Phase 2 are 1001 1003 1004 1101 1102 1201–1205; keep 1002) · `CUBE-1301` k3d merged-config conflict (D10) · `CUBE-1302` invalid k3d providerConfig · `CUBE-1303` k3d create failed / runtime unreachable · `CUBE-1304` k3d kubeconfig retrieval failed · `CUBE-1305` k3d delete failed
  - `CUBE-3007` engine does not support the requested delivery shape (git source) — defensive; both shipped engines must support it. (RENUMBERED by the 2026-07-14 reconciliation: this plan originally claimed 3006, but Phase 2 reserved CUBE-3006 for the argocd OCI-capability failure — constant `CodeEngineArgocdRegFail`, currently unused but reserved. 3005 is the PHASE 1 flux Uninstall prune-timeout, not an argocd code.)
  - `CUBE-4015` pack directory push failed (`pack push`). (RENUMBERED by the 2026-07-14 reconciliation: 4001–4014 are ALL claimed — 4001–4005/4012/4013 by Phase 1, 4006–4011/4014 by Phase 2 executed code: git+getter sources 4006/4007, kustomize 4008, cnoe 4009/4010, D11 expose 4011, extraction guards 4014.)
  - `CUBE-7001` `cube.lock` missing/unreadable (vendor needs it) · `CUBE-7002` vendor pull failed (artifact or image) · `CUBE-7003` bundle unreadable/corrupt · `CUBE-7004` bundle incomplete vs `cube.lock` (missing entry or digest mismatch) · `CUBE-7005` `--bundle` unsupported for this cluster provider configuration
  - `CUBE-7101` plugin not found · `CUBE-7102` plugin index fetch failed or sha256 mismatch · `CUBE-7103` plugin failed to execute · `CUBE-7104` plugin not trusted
  - `CUBE-7201` sync path is not renderable · `CUBE-7202` watch setup failed (fsnotify/inotify)
  - `CUBE-7301` gitea not available (pack absent or admin secret not found) · `CUBE-7302` Gitea API call failed · `CUBE-7303` `--deploy` source registration failed
- Conventional commits (`feat:`, `test:`, `chore:`); each task ends committed with `go build ./... && go test ./... -short` green.
- TDD: failing test → run → minimal implementation → run → commit, per step.
- New dependencies allowed in this phase: `github.com/k3d-io/k3d/v5`, `github.com/fsnotify/fsnotify`, `github.com/google/go-containerregistry` (direct). Nothing else without a plan change.

## File Structure (new/modified in Phase 3)

```
internal/cluster/contracttest/      # Task 1: shared ClusterProvider contract suite (spec §5)
  contracttest.go
internal/cluster/k3dp/              # Task 2: k3d provider (D4) + D10 merge
  k3d.go  merge.go  merge_test.go  testdata/
internal/oci/                       # Task 3: pack-directory push (mirror of Fetch's pull)
  pushdir.go  pushdir_test.go
cmd/pack.go                         # Task 3: `cube-idp pack push`
packs/backstage/  packs/cert-manager/  packs/external-secrets/  packs/envoy-gateway/
                                    # Task 4: catalog packs (data only)
.github/workflows/release-packs.yaml# Task 5: CI publishes packs to ghcr
internal/bundle/                    # Tasks 6–7: vendor + bundle model
  bundle.go  vendor.go  load.go  bundle_test.go  testdata/
cmd/vendor.go                       # Task 6: `cube-idp vendor`
internal/plugin/                    # Tasks 8–9: exec-plugin discovery, trust, index
  discover.go  exec.go  trust.go  index.go  plugin_test.go  index_test.go
cmd/plugin.go                       # Tasks 8–9: `cube-idp plugin list|trust|install`
internal/kube/portforward.go        # Task 12 prerequisite (moved generic forward; see Task 10 note)
internal/syncer/                    # Tasks 10–11: `sync` one-shot + --watch
  syncer.go  watch.go  syncer_test.go
cmd/sync.go
internal/gitea/                     # Task 12: minimal Gitea API client
  client.go  client_test.go
cmd/repo.go                         # Task 12: `cube-idp repo create`
tests/e2e/                          # Task 13: matrix + new-surface e2e
.github/workflows/ci.yaml           # Task 13: {kind, k3d} matrix
```

Modified (RECONCILE each path against the real tree in Task 0): `internal/config/schema.cue` + `types` docs (k3d enum), `internal/cluster/provider.go` (factory), `cmd/config.go` (render-cluster for k3d), `internal/engine/engine.go` + both engine impls + engine contract suite (`Poke`, `DeliverGit`), `internal/up/up.go` (`--bundle` options), `cmd/up.go`, `cmd/root.go` / `cmd.Execute` (plugin fallthrough), `internal/registry/portforward.go` (delegates to generic forward), `internal/config` `Default` remediation text, `README.md`.

---

### Task 0: Reconciliation Gate (mandatory, blocking)

**Files:**
- Modify: **this plan file** — every divergence found below must be edited into the affected tasks before they run.

**Interfaces:**
- Consumes: the real post-Phase-2 codebase and the final Phase 2 plan.
- Produces: a reconciled Phase 3 plan. Nothing else. No product code is written in this task.

Work through every checkbox. For each: open the named files, compare against what this plan assumes, and if reality differs, **edit the affected tasks below before proceeding**. Record a one-line note per item (verified / diverged→fixed) in the commit message of the plan update.

- [ ] **0.1 — `cluster.Provider` signature and the `kube.Conn` leaf type.** Read `internal/cluster/provider.go` and `internal/kube/` (Phase 1 Task 4/5 planned `Ensure(ctx, name string, spec config.ClusterSpec) (*kube.Conn, error)`, `Delete`, `Exists`, `Kubeconfig`, `Diagnose(ctx, name) []diag.Finding`, factory `cluster.New(spec, gw)` with `CUBE-1001`, and moved `Conn` into `internal/kube`). Read `internal/cluster/kindp/kind.go` + `merge.go` for how kindp implements it — the k3d provider (Task 2) must mirror the exact same shape, including how `RenderConfig` is kept pure and how `Ensure` is made idempotent. Update the affected tasks below if reality differs.
- [ ] **0.2 — Pack source resolver, including the Phase-2 git-ref source.** Read `internal/pack/source.go` (Phase 1: `Fetch(ctx, ref, cacheDir)` handling local dir + `oci://` via oras-go, `CUBE-4001` for unknown schemes) and whatever Phase 2 added for `github.com/org/repo//path@vX` git refs. Note the exact on-disk cache layout `pullOCI` produces and whether OCI pack artifacts are Flux-style gzipped tarball layers — Task 3's `pack push` must produce artifacts `Fetch` can consume, and Task 6's bundle stores what `Fetch` reads. Update the affected tasks below if reality differs.
- [ ] **0.3 — `cube.lock` format (Phase 2).** Find the lockfile writer/reader (Phase 2 scope: "`cube.lock` + `upgrade --plan`"; spec §4.1: "pins recorded in cube.lock (digests + full image list)"). Task 6 assumes a Go type it can import (this plan proposes `lock.File` with per-pack `Ref`, `Digest`, `Images []string` plus engine/registry image pins) — replace Task 6's assumed schema with the real one, reuse the real package (do NOT redefine lock parsing in `internal/bundle`). Update the affected tasks below if reality differs.
- [ ] **0.4 — Engine contract-test suite location and shape.** Phase 2 built the shared `GitOpsEngine` contract suite (D2). Find it (likely `internal/engine/contracttest/` or similar), note how a suite run is wired for flux and argocd, and how it obtains an `apply.Applier`/envtest. Tasks 10 and 12 extend the `Engine` interface (`Poke`, `DeliverGit`) — those extensions MUST be added to this suite and pass for **both** engines. Also confirm the factory package (`internal/engine/factory` per Phase 1 Task 9's import-cycle note) and the exact `Engine` method set after Phase 2. Update the affected tasks below if reality differs.
- [ ] **0.5 — Doctor / diag surface.** Read `internal/diag/` and the Phase 2 `doctor` command. Confirm `diag.Error{Code, Summary, Cause, Remediation}`, `diag.New/Wrap/Render`, `diag.Finding` are as Phase 1 planned, and note any Phase 2 additions (e.g. a preflight registry that Task 2's k3d provider and Task 8's plugin runner should feed findings into). Update the affected tasks below if reality differs.
- [ ] **0.6 — cmd/ registration pattern.** Read `cmd/root.go`: Phase 1 planned `NewRootCmd()` + `root.AddCommand(newXCmd())` per command file, `Execute`/`ExecuteContext` with `signal.NotifyContext`. Task 8 wraps `Execute` for plugin fallthrough — confirm the exact current shape. Also confirm the `-f/--file cube.yaml` flag convention. Update the affected tasks below if reality differs.
- [ ] **0.7 — Module path.** `go.mod` must say `github.com/rafpe/cube-idp`; all code blocks below import that path. Update the affected tasks below if reality differs.
- [ ] **0.8 — Gateway / port decisions.** Phase 1 Task 5 defined `gatewayContainerPort = 443` but Task 12's traefik-pack note switched the design to NodePort `30080` (`gatewayContainerPort` → 30080, traefik service `type: NodePort`, `nodePorts.web: 30080`); Phase 2 added `trust` + HTTPS. Read `internal/cluster/kindp/merge.go` and `packs/traefik/` to learn the FINAL host-port → node-port wiring and whether the gateway listener is HTTP or HTTPS now. Task 2 (k3d port mapping), Task 12 (printed clone URLs), and Task 13 (e2e assertions) all depend on it. Update the affected tasks below if reality differs.
- [ ] **0.9 — OCI artifact push wrapper.** Read `internal/oci/push.go` (Phase 1: `PushRendered(ctx, r *pack.Rendered, registryAddr string) (engine.ArtifactRef, error)` over `fluxcd/pkg/oci` client, plain-HTTP for `127.0.0.1`) and any Phase 2 changes (auth options?). Tasks 3, 5, 10, 11 reuse this wrapper — `sync --watch` pushes through exactly this path. Note the actual fluxcd/pkg/oci client option names for insecure/plain-HTTP and auth. Update the affected tasks below if reality differs.
- [ ] **0.10 — Gitea pack service/credentials surface.** Read `packs/gitea/` as shipped: the admin Secret name/namespace/labels (Phase 1 planned `gitea-admin-cube-idp` in ns `gitea`, keys `username`/`password`, labels `cube-idp.dev/cli-secret` + `pack-name`), the HTTP Service name/port (planned `gitea-http:3000`), and the HTTPRoute hostname (`gitea.cube-idp.localtest.me`). Task 12's `repo create` consumes all three. Also check whether Phase 2's CoreDNS/trust work changed in-cluster or host-facing git URLs. Update the affected tasks below if reality differs.
- [ ] **0.11 — Phase 2 leftovers that collide with this plan.** Grep the Phase 2 plan and code for anything already covering: provider contract tests (Task 1 may partially exist), `config render-cluster` generalization, image-list extraction for the lockfile (Task 6 needs the image list — if Phase 2's lock already records images per pack, Task 6 only consumes; if not, Task 6 grows an image-extraction step and the lock schema must be extended IN THE LOCK PACKAGE, not in bundle). Update the affected tasks below if reality differs.
- [ ] **0.12 — Commit the reconciled plan.**

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
- **0.9 (OCI push):** `internal/oci` is pure oras-go v2: `PushRendered(ctx, r, registryAddr)` unchanged signature over a `pushRenderedTo(ctx, r, oras.Target)` seam; flux media types preserved (config `application/vnd.cncf.flux.config.v1+json`, single layer `application/vnd.cncf.flux.content.v1.tar+gzip` containing `all.yaml`); PlainHTTP ONLY for 127.0.0.1/localhost via `isLocalRegistryHost` (shared with `pullOCI` and `ResolveRemote`). Tasks 3/5/10/11 build on these; every `fluxoci.*` snippet in this draft is stale.
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
- [ ] **(j) INVESTIGATION — `up` wall time vs spec §3's <60s:** measure where the ~2m10s goes (image pulls dominate); options: pre-pull hints, `mounts:` image cache docs, or re-scope the spec goal honestly. Produces a decision + follow-up, not necessarily code.
- [ ] **(k) OWNER DECISION — full TUI:** operator wants richer live UX; spec §4.1 bans a persistent TUI app ("a spinner is not a TUI"). Present options (keep spinner-only / transient bubbletea view during up / spec amendment) — do not build without the owner's call.

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
	// healthy cluster.
	for _, f := range p.Diagnose(ctx, name) {
		if f.Severity == "error" {
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

RECONCILE: verify `diag.Finding.Severity` compares as the string `"error"` (Phase 1 defined `diag.SeverityError Severity = "error"`) — import and use the constant if it exists, then compare with `f.Severity == diag.SeverityError`.

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

RECONCILE: `kindp.New(gw)` constructor signature per 0.1; the `KubernetesVersion` default per the real `config` schema.

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

k3d wraps k3s in docker. Two k3d-specific injection requirements beyond what kind needed: (a) k3s **bundles its own traefik**, which collides with our gateway pack — the merge must inject `--disable=traefik` on the server; (b) registry mirrors use the **k3s `registries.yaml` schema**, not raw containerd patches.

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
func RenderConfig(name string, spec config.ClusterSpec, gw config.GatewaySpec) ([]byte, error)
// Pure D10 merge, mirror of kindp.RenderConfig. Output: a k3d SimpleConfig
// (k3d.io/v1alpha5) YAML document. Merge rules:
//   base    = user providerConfig (file path or inline YAML; a k3d SimpleConfig) if set, else empty SimpleConfig
//   inject  = gateway port mapping host gw.Port -> node port <gatewayNodePort> on server:0,
//             k3sExtraArgs --disable=traefik (server:0) — our gateway pack owns ingress,
//             registry mirrors/insecure as embedded k3s registries.yaml,
//             typed extraPorts -> ports entries, mounts -> volumes,
//             image rancher/k3s:<kubernetesVersion>-k3s1 from kubernetesVersion
//   conflict= user config maps gw.Port to a different node port, or sets a
//             different image than kubernetesVersion implies, or re-enables
//             traefik -> CUBE-1301; unreadable/invalid providerConfig -> CUBE-1302
```

- [ ] **Step 1: Extend the config schema with a failing test**

Add to `internal/config/load_test.go` (RECONCILE: match the real test file's helpers, e.g. `codeOf`):

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

In `internal/config/schema.cue`: `provider: *"kind" | "existing"` → `provider: *"kind" | "existing" | "k3d"`. RECONCILE: verify the exact current disjunction (Phase 2 may have touched it) and that the D10 cross-validation (`CUBE-1003` for `existing` + node fields) treats `k3d` like `kind` (node fields ARE valid for k3d — no change needed there, but read `crossValidate` to be sure).

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
	out, err := RenderConfig("dev", spec, gw)
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
	out, err := RenderConfig("dev", spec, gw)
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
	_, err := RenderConfig("dev", spec, gw)
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
	_, err := RenderConfig("dev", spec, gw)
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

func RenderConfig(name string, spec config.ClusterSpec, gw config.GatewaySpec) ([]byte, error) {
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
	if reg := registriesYAML(spec.Registry); reg != "" {
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
// insecure TLS skip), sorted for golden determinism.
func registriesYAML(r config.RegistrySpec) string {
	if len(r.Mirrors) == 0 && len(r.Insecure) == 0 {
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

`internal/cluster/k3dp/k3d.go` — mirrors `kindp/kind.go` exactly in shape (RECONCILE: match the real kindp method set from 0.1). Contract, with the k3d library specifics spelled out:

```go
package k3dp

// K3d implements cluster.Provider over the k3d v5 library.
//
//   type K3d struct{ gw config.GatewaySpec }
//   func New(gw config.GatewaySpec) *K3d { return &K3d{gw: gw} }
//
// Ensure(ctx, name, spec):
//   1. Exists check (below); if present, skip creation (idempotent).
//   2. cfgYAML := RenderConfig(name, spec, k.gw)
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

- [ ] **Step 6: render-cluster for k3d**

In `cmd/config.go`, replace the kind-only guard with a provider switch (RECONCILE: exact current body per 0.6):

```go
	switch cube.Spec.Cluster.Provider {
	case "kind":
		out, err = kindp.RenderConfig(cube.Metadata.Name, cube.Spec.Cluster, cube.Spec.Gateway)
	case "k3d":
		out, err = k3dp.RenderConfig(cube.Metadata.Name, cube.Spec.Cluster, cube.Spec.Gateway)
	default:
		return diag.New("CUBE-1002", // RESOLVED 2026-07-14: 1002 is free after Phase 2 — add the constant to internal/diag/codes.go (literal-ban applies)
			fmt.Sprintf("render-cluster applies to cluster-creating providers (kind, k3d), not %q", cube.Spec.Cluster.Provider),
			"provider: existing has no provider config to render")
	}
```

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

**Reconcile checkpoint:** requires 0.2 (what `pack.Fetch`'s `pullOCI` actually consumes — cache layout, media types; NOTE the Phase 2 ground truth: `internal/oci` is pure oras-go v2, `fluxcd/pkg/oci` no longer exists in the module) and 0.9 (the REAL push wrapper: `pushRenderedTo(ctx, r, oras.Target)` seam + `isLocalRegistryHost` PlainHTTP gating — this task's snippets referencing `fluxoci.NewClient` are STALE and must be rewritten onto oras-go v2 at Task 0, mirroring internal/oci/push.go's patterns).

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
// Auth: ambient docker credential chain (docker login / GITHUB_TOKEN via
// docker/login-action in CI); plain HTTP for 127.0.0.1 hosts.
// Failure -> CUBE-4015.
func PushPackDir(ctx context.Context, dir, ociRef string) (digest string, err error)
```

CLI: `cube-idp pack push <dir> <oci-ref>` — prints the pushed digest. Tag defaulting: if `<oci-ref>` has no `:tag`, use the pack's `version` from `pack.cue` (loaded via the pack package's metadata loader; RECONCILE: `pack.Fetch(ctx, dir, …)` on a local dir returns `*pack.Pack{Name, Version}` — reuse it rather than re-parsing CUE).

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

	digest, err := PushPackDir(context.Background(), dir, ref)
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
}
```

(`httptest` registries serve plain HTTP on `127.0.0.1` — this exercises the same insecure-transport path the zot tunnel uses. RECONCILE: `pack.Fetch`'s insecure-when-127.0.0.1 rule from Phase 1 Task 8 must also match `127.0.0.1:<randomport>`; if it matches only host `127.0.0.1` this already works, if it does something narrower, widen it here with a test.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/oci/ -run TestPushPackDirRoundTrips -v`
Expected: FAIL (`PushPackDir` undefined)

- [ ] **Step 3: Implement**

```bash
go get github.com/google/go-containerregistry@latest
```

`internal/oci/pushdir.go`:

```go
package oci

import (
	"context"
	"fmt"
	"strings"

	"github.com/rafpe/cube-idp/internal/diag"
)

// PushPackDir publishes the pack source directory as an OCI artifact.
// RECONCILE: it MUST produce the artifact shape pack.Fetch's pullOCI
// consumes (checkpoint 0.2). If pullOCI pulls Flux-style artifacts
// (gzipped tarball layer, media type
// application/vnd.cncf.flux.content.v1.tar+gzip).
// STALE SNIPPET (pre-Phase-2 reconciliation) — fluxcd/pkg/oci no longer
// exists in the module. Task 0 rewrites this onto oras-go v2, mirroring
// internal/oci/push.go: build the tar.gz layer of the pack DIRECTORY with
// archive/tar + compress/gzip, oras.PackManifest with the flux media types,
// push via registry/remote with isLocalRegistryHost PlainHTTP gating, tag,
// and return the resolved digest. The historical fluxoci sketch was:
//   c := fluxoci.NewClient(...); digest, err := c.Push(ctx, ref, dir, ...)
//
// If pullOCI instead expects a plain oras-go file store, push with oras-go
// (oras.Copy from an oci file store) to match. Either way the Step 1
// round-trip test is the arbiter — do not ship a push the project's own
// Fetch cannot pull.
func PushPackDir(ctx context.Context, dir, ociRef string) (string, error) {
	if !strings.HasPrefix(ociRef, "oci://") {
		return "", diag.New("CUBE-4015", fmt.Sprintf("pack push target %q is not an oci:// reference", ociRef),
			"use the form oci://host/repo:tag")
	}
	digest, err := pushDir(ctx, dir, strings.TrimPrefix(ociRef, "oci://")) // per the contract above
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
			digest, err := oci.PushPackDir(c.Context(), dir, ref)
			if err != nil {
				return err
			}
			fmt.Fprintf(c.OutOrStdout(), "pushed %s@%s\n", ref, digest)
			return nil
		},
	}
	packCmd.AddCommand(push)
	return packCmd
}
```

Register `newPackCmd()` in `cmd/root.go` (RECONCILE: registration pattern per 0.6). RECONCILE: `pack.Fetch` with an empty cacheDir for a local dir — Phase 1's implementation ignores cacheDir for local paths; if it does not, pass `os.TempDir()`.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/oci/ ./cmd/ -short -v && go build ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat: pack push command — publish pack directories as OCI artifacts"
```

---

### Task 4: Catalog packs — backstage, cert-manager, external-secrets, envoy-gateway

**Reconcile checkpoint:** requires 0.8 (gateway wiring: HTTPRoute parentRef name/namespace `cube-idp`/`traefik` and hostname scheme from Phase 1 Task 12, HTTPS after Phase 2 trust), plus the shipped pack format (`pack.cue` + `manifests/` + `chart.yaml` with a `values:` block — Phase 1 Tasks 8/12; Phase 2 added kustomize overlays, which these packs deliberately do not need).

Data only — no Go. Same conventions as the Phase 1 starter packs: pinned chart versions, `#Values` schemas, HTTPRoutes through the `cube-idp` Gateway, CLI-secret labels where there are credentials.

**Files:**
- Create:
  - `packs/cert-manager/{pack.cue,chart.yaml}`
  - `packs/external-secrets/{pack.cue,chart.yaml}`
  - `packs/envoy-gateway/{pack.cue,chart.yaml,manifests/10-gatewayclass.yaml}`
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

`packs/envoy-gateway/pack.cue`: `name: "envoy-gateway"`, `version: "0.1.0"`, `#Values: {}`.

`packs/envoy-gateway/chart.yaml` (OCI chart — exercises the oci:// chart path in the helm wrapper):

```yaml
chart: gateway-helm
repo: oci://docker.io/envoyproxy
version: "1.3.0"
releaseName: envoy-gateway
namespace: envoy-gateway-system
```

`packs/envoy-gateway/manifests/10-gatewayclass.yaml` (D3: envoy-gateway is the *alternative* gateway implementation; it ships a GatewayClass but does NOT ship a Gateway — the operator who swaps `spec.gateway.pack: envoy-gateway` gets the Gateway from their gateway pack config. Document this in a comment header inside the YAML):

```yaml
# envoy-gateway pack: provides the GatewayClass. To use it as THE cube-idp
# gateway, set spec.gateway.pack: envoy-gateway in cube.yaml; a Gateway named
# cube-idp is then expected in this namespace (mirror of the traefik pack's
# 10-gateway.yaml — copy it here with gatewayClassName: envoy-gateway).
apiVersion: gateway.networking.k8s.io/v1
kind: GatewayClass
metadata:
  name: envoy-gateway
spec:
  controllerName: gateway.envoyproxy.io/gatewayclass-controller
```

RECONCILE: the traefik pack (Phase 1 Task 12) resolves the gateway pack as `packs/<spec.gateway.pack>` and its Gateway carries the listener/nodePort wiring from checkpoint 0.8 — if `spec.gateway.pack: envoy-gateway` is to be genuinely usable, add `manifests/20-gateway.yaml` mirroring the final traefik Gateway (same name `cube-idp`, same listener port/protocol, `gatewayClassName: envoy-gateway`) and matching NodePort values in `chart.yaml` `values:`. Verify the final traefik pack shape first, then mirror it exactly.

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
      - name: APP_CONFIG_app_baseUrl
        value: "http://backstage.cube-idp.localtest.me:8443"
      - name: APP_CONFIG_backend_baseUrl
        value: "http://backstage.cube-idp.localtest.me:8443"
```

RECONCILE: scheme/host/port in those URLs come from checkpoint 0.8 (Phase 2 trust may have made the canonical scheme `https`) — derive from the final gateway decision, and note in the pack README that changing `spec.gateway.host/port` requires overriding these values.

`packs/backstage/manifests/20-httproute.yaml`:

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: backstage
  namespace: backstage
spec:
  parentRefs: [{name: cube-idp, namespace: traefik}]
  hostnames: ["backstage.cube-idp.localtest.me"]
  rules:
    - backendRefs: [{name: backstage, port: 7007}]
```

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

**Reconcile checkpoint:** requires Task 3 (`pack push`), Task 4 (catalog), 0.6 (`config.Default` and the `init --local` flag as actually shipped — Phase 1 Tasks 3/13), and the actual ghcr namespace decision (see the open question in Self-Review: spec writes `ghcr.io/cube-idp/packs/...` but the repo lives under `rafpe` — pick ONE and apply it consistently to `config.Default`, this workflow, and the packs' documented refs).

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
        with: {go-version: "1.24"}
      - uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}
      - name: build cube-idp
        run: go build -o ./cube-idp .
      - name: push all packs (version tag + latest)
        run: |
          set -euo pipefail
          # RECONCILE: replace ghcr.io/cube-idp with the reconciled namespace
          # (e.g. ghcr.io/rafpe/cube-idp) everywhere in this workflow AND in
          # internal/config Default before merging.
          NS="ghcr.io/cube-idp/packs"
          for dir in packs/*/; do
            name="$(basename "$dir")"
            ./cube-idp pack push "$dir" "oci://${NS}/${name}"          # tag = pack.cue version
            version="$(./cube-idp pack push "$dir" "oci://${NS}/${name}" | sed -E 's/.*:([^@]+)@.*/\1/')"
            ./cube-idp pack push "$dir" "oci://${NS}/${name}:latest"
            echo "published ${name} ${version} (+latest)"
          done
```

(Pushing an identical artifact twice is idempotent — same digest — so the version-extraction re-push is harmless; if the double-push offends, add a `pack version <dir>` printing subcommand to Task 3 instead and simplify this loop. Do not leave both.)

- [ ] **Step 2: Align `config.Default` with the published refs**

Phase 1's `Default` already emits `oci://ghcr.io/cube-idp/packs/{gitea,argocd}:0.1.0`. RECONCILE: (a) apply the reconciled ghcr namespace; (b) verify pack versions in `pack.cue` files match the refs `Default` writes (`0.1.0` at this point) — a mismatch means `up` fails on a fresh `init` the moment users rely on published packs, which is exactly the bug this task exists to close. Update `TestDefaultProfileIncludesGitea` alongside.

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

**Reconcile checkpoint:** requires 0.3 (the real `cube.lock` schema and its Go package — Phase 2), 0.2 (pack cache layout so bundled pack sources are `Fetch`-compatible — Phase 1/2), 0.11 (whether the lock already records the full image list per pack — Phase 2; if not, extend the LOCK package there first, in a preparatory commit, before this task). Task 3's push/pull symmetry is reused for artifact download.

Spec §4.1: "Pins recorded in `cube.lock` (digests + full image list) — feeds `cube-idp vendor` for air-gap." Vendor is a pure consumer: read lock → pull every pinned artifact and image → write one tarball. No cluster access, no config mutation.

**Files:**
- Create: `internal/bundle/bundle.go` (format + read/write), `internal/bundle/vendor.go`, `cmd/vendor.go`
- Test: `internal/bundle/bundle_test.go`

**Interfaces:**
- Consumes: the Phase 2 lock package (RESOLVED 2026-07-14 — the REAL schema differs from this plan's guess; Task 0 rewrites this task's bodies mechanically: `lock.File{APIVersion, Kind, Engine lock.EngineLock{Type}, Packs []lock.Entry{Ref, Name, Version, Resolved, RenderedHash, Images []string}}`, functions `lock.PathFor/Write/Read/RenderedHash/ImagesFrom`; there is NO `Digest` field — the pin string is `Resolved` with prefixes `oci:sha256:…` / `git+<sha>` / `dir:h1:…`, so `refWithDigest` parses `Resolved`, and git/dir-pinned packs are copied from the local cache, not re-pulled by digest; there are NO engine/registry image pins in the lock — vendor derives them at build time via `lock.ImagesFrom(eng.InstallManifests())` + `lock.ImagesFrom(registry.Manifests())`, which is Phase 2's existing image extractor), `pack.Fetch`, the OCI layout store per the Tech Stack decision (oras-go `content/oci` preferred), `diag`.
- Produces:

```go
package bundle

// Layout inside the tar.gz (format is versioned via manifest.json):
//   manifest.json     — {"formatVersion":1,"cube":"<name>","createdAt":RFC3339,"lockDigest":"sha256:…"}
//   cube.lock         — verbatim copy of the lock the bundle was built from
//   packs/<name>/     — pack source dir at the locked digest (Fetch-compatible)
//   images/           — one OCI image layout containing every locked image,
//                       tagged with its original reference
type Manifest struct {
	FormatVersion int    `json:"formatVersion"`
	Cube          string `json:"cube"`
	CreatedAt     string `json:"createdAt"`
	LockDigest    string `json:"lockDigest"` // sha256 of the embedded cube.lock bytes
}

func Vendor(ctx context.Context, lockPath, outPath string, progress io.Writer) error
	// CUBE-7001 lock missing/unreadable; CUBE-7002 any pull failure (names
	// the artifact/image that failed — no partial-success silence: a bundle
	// is either complete or an error)
func Open(bundlePath string) (*Opened, error)      // extracts to a temp dir, verifies manifest -> CUBE-7003
type Opened struct {
	Dir      string        // extraction root
	Manifest Manifest
	Lock     *lock.File    // parsed embedded lock  (RECONCILE: real lock type)
}
func (o *Opened) PackDir(name string) (string, error)   // CUBE-7004 if absent
func (o *Opened) ImagesLayout() string                  // path to images/ OCI layout
func (o *Opened) Verify() error                         // every lock digest present in the bundle -> else CUBE-7004
func (o *Opened) Close()                                // removes the temp dir
```

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
)

// RECONCILE: construct the lock via the real Phase 2 lock package instead of
// writing YAML by hand if it exposes a writer; the fixture below assumes the
// documented shape (packs with ref/digest/images).
const tinyLock = `
apiVersion: cube-idp.dev/v1alpha1
kind: CubeLock
cube: dev
packs:
  - name: demo
    ref: oci://127.0.0.1:0/packs/demo:0.9.9   # rewritten by the test to the live test registry
    digest: sha256:PLACEHOLDER                 # rewritten by the test after push
    images: []                                 # image pulls exercised separately below
`

func TestVendorThenOpenRoundTrip(t *testing.T) {
	// Arrange: a local registry with a pushed demo pack (reuse Task 3's
	// helpers — export localRegistry/writeDemoPack from a shared
	// internal/oci/ocitest package in this step rather than copy-pasting).
	// Push, capture digest, template it into tinyLock, write cube.lock.
	// Act:
	out := filepath.Join(t.TempDir(), "bundle.tar.gz")
	if err := Vendor(context.Background(), writeLockFixture(t), out, os.Stderr); err != nil {
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
	err := Vendor(context.Background(), "nope.lock", filepath.Join(t.TempDir(), "b.tgz"), os.Stderr)
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
	// returns CUBE-7004. Digest verification is content-addressed re-pull of
	// the packs/<name> dir tree hash against lock digest — see bundle.go.
}
```

(`writeLockFixture` is a test helper assembling the registry + lock; write it fully in this step. The image-pull path gets its coverage in Step 3's crane test below.)

Run: `go test ./internal/bundle/ -v` — FAIL (package does not exist).

- [ ] **Step 2: Implement bundle.go + vendor.go**

`internal/bundle/vendor.go` core:

```go
package bundle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/google/go-containerregistry/pkg/crane"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/layout"

	"github.com/rafpe/cube-idp/internal/diag"
	"github.com/rafpe/cube-idp/internal/pack"
	// RECONCILE: import the real Phase 2 lock package (checkpoint 0.3)
)

func Vendor(ctx context.Context, lockPath, outPath string, progress io.Writer) error {
	raw, err := os.ReadFile(lockPath)
	if err != nil {
		return diag.Wrap(err, "CUBE-7001", fmt.Sprintf("cannot read %s", lockPath),
			"run `cube-idp up` first — vendor bundles exactly what the lockfile pins")
	}
	lf, err := lock.Parse(raw) // RECONCILE: real parse entrypoint
	if err != nil {
		return diag.Wrap(err, "CUBE-7001", lockPath+" is not a valid cube.lock", "re-run `cube-idp up` to regenerate it")
	}

	stage, err := os.MkdirTemp("", "cube-idp-vendor-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	sum := sha256.Sum256(raw)
	writeJSON(filepath.Join(stage, "manifest.json"), Manifest{
		FormatVersion: 1, Cube: lf.Cube, CreatedAt: time.Now().UTC().Format(time.RFC3339),
		LockDigest: "sha256:" + hex.EncodeToString(sum[:]),
	})
	os.WriteFile(filepath.Join(stage, "cube.lock"), raw, 0o644)

	// 1. Pack sources, pinned by digest (ref@digest beats ref:tag — tags move).
	for _, p := range lf.Packs {
		fmt.Fprintf(progress, "▸ [vendor] pack %s (%s)\n", p.Name, p.Digest)
		pinned := refWithDigest(p.Ref, p.Digest) // oci://host/repo@sha256:… ; local-dir refs are copied verbatim
		fetched, err := pack.Fetch(ctx, pinned, stage+"/.cache")
		if err != nil {
			return diag.Wrap(err, "CUBE-7002", fmt.Sprintf("cannot pull pack %q at its locked digest", p.Name),
				"check network/registry access; if the artifact was deleted upstream, re-run `cube-idp up` to re-pin")
		}
		if err := copyTree(fetched.Dir, filepath.Join(stage, "packs", p.Name)); err != nil {
			return err
		}
	}

	// 2. Every locked image into ONE OCI layout.
	lp, err := layout.Write(filepath.Join(stage, "images"), emptyIndex())
	if err != nil {
		return err
	}
	for _, img := range lf.AllImages() { // packs + engine + registry pins; RECONCILE: real accessor or flatten inline
		fmt.Fprintf(progress, "▸ [vendor] image %s\n", img)
		image, err := crane.Pull(img, crane.WithContext(ctx))
		if err != nil {
			return diag.Wrap(err, "CUBE-7002", fmt.Sprintf("cannot pull image %s", img),
				"check network access and that the image still exists at its pinned tag/digest")
		}
		if err := lp.AppendImage(image, layout.WithAnnotations(map[string]string{
			"org.opencontainers.image.ref.name": img,
		})); err != nil {
			return err
		}
	}

	// 3. Single tar.gz, atomically (write to tmp, rename).
	if err := tarGzDir(stage, outPath); err != nil {
		return diag.Wrap(err, "CUBE-7002", "cannot write bundle "+outPath, "check disk space and permissions")
	}
	fmt.Fprintf(progress, "✔ bundle written: %s\n", outPath)
	return nil
}
```

Helpers to write fully in `bundle.go` (each is standard-library mechanical code; write them, no stubs): `writeJSON`, `refWithDigest` (splits `:tag`, appends `@sha256:…`), `copyTree`, `emptyIndex` (`empty.Index` from go-containerregistry), `tarGzDir`, and the `Open`/`Verify`/`PackDir`/`Close` side: `Open` extracts with a path-traversal guard (reject entries containing `..`), parses `manifest.json` (any failure → `CUBE-7003` "bundle is unreadable or corrupt" / remediation "re-run `cube-idp vendor`"), checks `FormatVersion == 1`; `Verify` recomputes the lock digest and checks every `lf.Packs[i]` has a `packs/<name>/pack.cue` and every locked image ref appears in the layout index annotations, else `CUBE-7004` naming the missing entry.

RECONCILE: `pack.Fetch` must accept `oci://host/repo@sha256:…` digest refs — Phase 1's plan only shows `:tag`. If it does not, extend `internal/pack/source.go` (in THIS task, with a unit test mirroring Task 3's round-trip but fetching by digest) rather than working around it in bundle code.

- [ ] **Step 3: Image-path test with the local registry**

Add to `bundle_test.go` a case where the lock lists one image hosted on the in-process test registry (push a tiny image with `crane.Push(mutate.AppendLayers(empty.Image, …))` or `random.Image(64, 1)` from go-containerregistry's `pkg/v1/random`), vendor it, open the bundle, and assert the layout index contains the image's ref annotation.

- [ ] **Step 4: Command**

`cmd/vendor.go`:

```go
package cmd

import (
	"github.com/spf13/cobra"

	"github.com/rafpe/cube-idp/internal/bundle"
)

func newVendorCmd() *cobra.Command {
	var lockPath, out string
	c := &cobra.Command{
		Use:   "vendor",
		Short: "Bundle every artifact and image pinned in cube.lock for air-gapped installs",
		RunE: func(c *cobra.Command, _ []string) error {
			return bundle.Vendor(c.Context(), lockPath, out, c.OutOrStdout())
		},
	}
	c.Flags().StringVar(&lockPath, "lock", "cube.lock", "path to cube.lock")
	c.Flags().StringVarP(&out, "output", "o", "cube-bundle.tar.gz", "bundle output path")
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

**Reconcile checkpoint:** requires Task 6, 0.1 (provider set), 0.3 (lock), and the actual `up.Run` signature + orchestration sequence after Phase 2 (Phase 1 Task 10: `up.Run(ctx, cfgPath string, out io.Writer) error`; Phase 2 added lock writing and possibly flags — read `internal/up/up.go` first). Also RECONCILE: how kind image-loading is exposed by the pinned kind library (`nodeutils.LoadImageArchive`) and k3d (`client.ImageImportIntoClusterMulti`).

Offline means: every pack source comes from the bundle, every image reaches the cluster nodes from the bundle, and any attempt to leave those rails is a typed error — never a silent network fallback.

**Files:**
- Create: `internal/bundle/load.go` (image loading into providers)
- Modify: `internal/up/up.go` (options struct + bundle wiring), `cmd/up.go` (`--bundle` flag), `internal/cluster/provider.go` (optional `ImageLoader` capability)
- Test: `internal/up/up_test.go` (pure resolution logic), e2e coverage in Task 13

**Interfaces:**
- Consumes: `bundle.Open/Verify/PackDir/ImagesLayout`, providers.
- Produces:

```go
package cluster
// ImageLoader is an optional capability of cluster-creating providers:
// load images from an OCI layout directly into the cluster nodes' runtime.
// kindp and k3dp implement it; `existing` does not (see CUBE-7005 below).
type ImageLoader interface {
	LoadImages(ctx context.Context, name string, ociLayoutDir string) error
}

package up
type Options struct {
	ConfigPath string
	Bundle     string    // path to a vendor bundle; "" = online mode
	Out        io.Writer
}
func Run(ctx context.Context, opts Options) error
// RECONCILE: adapt, don't replace — if Phase 2 already grew an options
// struct or extra params on Run, extend that shape and keep one Run.
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
	refs := []config.PackRef{{Ref: "oci://ghcr.io/cube-idp/packs/gitea:0.1.0"}}
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

	_, err = resolveBundleRefs([]config.PackRef{{Ref: "oci://ghcr.io/cube-idp/packs/absent:1.0.0"}}, func(string) (string, bool) { return "", false })
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-7004" {
		t.Fatalf("want CUBE-7004 for a ref missing from the bundle, got %v", err)
	}
}
```

Pack-name extraction from a ref: last path segment before `:tag`/`@digest` (`packs/gitea:0.1.0` → `gitea`). RECONCILE: if the Phase 2 lock stores name↔ref pairs (it does per 0.3's assumed schema), match refs through the opened bundle's lock instead of string surgery — prefer the lock lookup, keep the string fallback only for local-dir refs.

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
		if err := prov.(cluster.ImageLoader).LoadImages(ctx, cube.Metadata.Name, opened.ImagesLayout()); err != nil {
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

Everything downstream is unchanged: local-dir fetch → render → push to zot (the zot push is in-cluster delivery, not internet) → Deliver. The engine + zot images were loaded in deviation 2, so their pods start without pulling. RECONCILE: verify the Phase 1/2 zot and engine manifests use `imagePullPolicy: IfNotPresent` (default for pinned tags) — if any manifest says `Always`, change it in this task or node-loaded images will be ignored.

- [ ] **Step 3: Implement LoadImages for kindp and k3dp**

`internal/bundle/load.go` provides the shared half — iterate the OCI layout, write each image to a `docker-archive`-style tarball via go-containerregistry's `tarball.WriteToFile(tmp, ref, img)` — and each provider consumes it:

```go
// kindp.LoadImages: for each image tarball, stream it into every cluster
// node: nodes, _ := provider.ListNodes(name); for each node,
// nodeutils.LoadImageArchive(node, reader). RECONCILE: exact kind library
// helpers (sigs.k8s.io/kind/pkg/cluster/nodeutils) for the pinned version —
// `kind load image-archive` in kind's own cmd tree is the reference consumer.
//
// k3dp.LoadImages: k3dclient.ImageImportIntoClusterMulti(ctx,
// runtimes.SelectedRuntime, []string{tarPath}, &types.Cluster{Name: name},
// types.ImageImportOpts{}). RECONCILE: exact signature for the pinned k3d v5.
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

**Reconcile checkpoint:** requires 0.6 (the exact `cmd.Execute`/`ExecuteContext` shape and signal handling — Phase 1 Task 10), 0.1 (`provider.Kubeconfig` for the env contract), 0.5 (diag), and 0.10/0.9 only transitively. Spec §4.4 tier 2 is the contract: `cube-idp-<name>` on PATH (krew model), env vars `CUBE_IDP_KUBECONFIG`, `CUBE_IDP_CUBE_NAME`, `CUBE_IDP_REGISTRY`, explicit trust warning on first run.

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
type Env struct{ Kubeconfig, CubeName, Registry string } // "" entries are omitted from the environment

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
	interactive := term.IsTerminal(int(os.Stdin.Fd())) // golang.org/x/term (already transitive via huh; RECONCILE: confirm, else add)
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

In the file that owns `Execute` (RECONCILE: per 0.6), before cobra dispatch:

```go
func Execute() error {
	root := NewRootCmd()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt) // RECONCILE: reuse the existing signal wiring, do not duplicate it
	defer stop()
	if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-") {
		if _, _, err := root.Find(os.Args[1:]); err != nil { // not a built-in command
			if path, ok := plugin.Lookup(os.Args[1]); ok {
				return plugin.Exec(ctx, path, os.Args[2:], pluginEnv())
			}
			return diag.New("CUBE-7101",
				fmt.Sprintf("unknown command %q and no cube-idp-%s plugin found on PATH", os.Args[1], os.Args[1]),
				"run `cube-idp plugin list` to see discovered plugins, or `cube-idp --help` for built-in commands")
		}
	}
	return root.ExecuteContext(ctx)
}
```

`pluginEnv()` (same file) is **best-effort by design** — plugins must run even with no cube.yaml/cluster around: `CubeName` from `config.Load("cube.yaml")` if it loads (else empty); `Kubeconfig` = path to a `0600` temp file containing `provider.Kubeconfig(ctx, name)` if the provider resolves and the cluster exists (else empty); `Registry` = `registry.InClusterURL` when a kubeconfig was resolvable (the plugin reaches it via its own port-forward — document this in the README plugin section), else empty. Empty entries are omitted (see `Env`); a plugin that requires them must error itself. No cube-idp error is raised for missing env — that would break cluster-independent plugins.

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

const DefaultIndex = "https://github.com/cube-idp/plugin-index.git" // RECONCILE: apply the same ghcr/org naming decision as Task 5; until that org exists, `plugin install` without --index must fail with CUBE-7102 remediation "pass --index <git-url> — no default index is published yet". Do not point the default at a repo that does not exist.

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

(RECONCILE: if the default-index repo exists by execution time — see Task 5's namespace decision — set the flag default to `DefaultIndex` and delete the empty-check.)

- [ ] **Step 4: Run tests**

Run: `go test ./internal/plugin/ -v && go build ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat: plugin install from sha256-pinned git index with auto-trust on verified installs"
```

---

### Task 10: Engine `Poke` + `cube-idp sync <dir>` (one-shot) + generic port-forward

**Reconcile checkpoint:** requires 0.4 (the FULL post-Phase-2 `engine.Engine` method set, the factory package, and the engine contract suite — extending the interface without extending the suite for BOTH engines is forbidden), 0.9 (`oci.PushRendered` + insecure options), and the Phase 1 `registry.PortForward` (Task 7) which this task generalizes into `internal/kube` for reuse by Task 12.

D7: `cube-idp sync ./dir --watch` = fsnotify → OCI artifact push → engine reconciles. Engines poll their sources on an interval; for a live-feedback loop that interval is too slow, so the `Engine` interface grows `Poke` — "reconcile this pack's source now". Flux: patch the `reconcile.fluxcd.io/requestedAt` annotation on the pack's OCIRepository. Argo CD: set the `argocd.argoproj.io/refresh: normal` annotation on the pack's Application. Both are annotation patches through the Applier's client — no engine API clients.

**Files:**
- Create: `internal/kube/portforward.go`, `internal/syncer/syncer.go`, `cmd/sync.go`
- Modify: `internal/engine/engine.go` (+`Poke`), `internal/engine/flux/…`, the Phase 2 argocd engine package, the Phase 2 engine contract suite, `internal/registry/portforward.go` (delegate)
- Test: `internal/syncer/syncer_test.go`, flux/argocd `Poke` unit tests, contract-suite extension

**Interfaces:**
- Consumes: `pack.Fetch/Render`, `oci.PushRendered`, `engine.Engine`, `apply.Applier`, `registry`.
- Produces:

```go
package kube
// PortForward generalizes Phase 1's registry-only tunnel: forward a free
// local port to the first running pod matching selector in ns, targeting
// podPort. Returns "127.0.0.1:<port>" and a stop func. Failure -> the
// caller's domain code (it wraps), so this returns plain errors.
func PortForward(ctx context.Context, cfg *rest.Config, ns, selector string, podPort int) (string, func(), error)

package engine
// Poke asks the engine to reconcile the delivered pack now instead of on
// its poll interval. packName matches the name Deliver was called with.
// Implementations must be idempotent and cheap (an annotation patch).
Engine interface { …existing methods… ; Poke(ctx context.Context, a *apply.Applier, packName string) error }

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

RECONCILE: synthesis needs pack construction without `pack.cue`. If `pack.Pack`'s fields are exported with `Render` reading `manifests/` under `Dir` (Phase 1 shape), synthesize by staging a temp dir: copy `*.yaml` into `<tmp>/manifests/` and return `&pack.Pack{Name: base, Version: "0.0.0-dev", Dir: tmp}`. If Phase 2 changed `Pack` internals, add a small exported constructor to `internal/pack` instead (preferred long-term) — decide during reconciliation, then implement ONE of the two.

Run: `go test ./internal/syncer/ -v` — FAIL.

- [ ] **Step 2: Failing tests — engine Poke shapes**

Flux (`internal/engine/flux/…_test.go` — place next to the existing Deliver tests):

```go
func TestPokePatchesOCIRepositoryAnnotation(t *testing.T) {
	// Against envtest (RECONCILE: reuse the engine contract suite's envtest
	// scaffolding from checkpoint 0.4 — do not build a second harness):
	// 1. Apply the OCIRepository CRD + a Deliver-shaped OCIRepository for
	//    pack "demo".
	// 2. f.Poke(ctx, applier, "demo")
	// 3. Get the OCIRepository; assert metadata.annotations
	//    ["reconcile.fluxcd.io/requestedAt"] parses as a recent RFC3339Nano
	//    timestamp (that is the value `flux reconcile` writes).
}
```

Argo CD equivalent asserts `argocd.argoproj.io/refresh: "normal"` on the pack's Application. RECONCILE: the argocd engine's Application naming convention for a pack (Phase 2 Deliver) — Poke must resolve the same name; read the Phase 2 Deliver implementation and mirror it.

Contract-suite extension (in the Phase 2 suite package): a `Poke` case — Deliver a fake pack, `Poke` it, assert no error and that a second `Poke` also succeeds (idempotency). Both engines must pass.

- [ ] **Step 3: Implement Poke (both engines) + kube.PortForward + SyncOnce**

Flux `Poke` (~20 lines): `a.Client().Get` the OCIRepository `cube-idp-<packName>` in `flux-system` (RECONCILE: exact naming from the shipped Deliver — Phase 1 used `"cube-idp-" + r.Name`), set annotation `reconcile.fluxcd.io/requestedAt: time.Now().Format(time.RFC3339Nano)`, `a.Client().Update`. NotFound → `diag.New("CUBE-3007", fmt.Sprintf("pack %q has no delivery source to poke", packName), "run `cube-idp sync <dir>` or `cube-idp up` first — Poke only refreshes an existing delivery")`. Argo CD mirrors this on the Application with its refresh annotation.

`internal/kube/portforward.go`: move Phase 1's `registry.PortForward` body, parameterizing `ns`, `selector` (label selector string), `podPort`; `internal/registry/portforward.go` becomes a two-line delegate (`kube.PortForward(ctx, cfg, "cube-idp-system", "app=zot", 5000)`) wrapped with the existing `CUBE-5002` diag. Run the registry package's tests to prove neutrality.

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
//   deps.Applier.RecordInventory(ctx, objs)               // sync'd packs are down-able like everything else
//   deps.Engine.Poke(ctx, deps.Applier, rendered.Name)
//   return Result{rendered.Name, rendered.Version, ref digest if PushRendered exposes it}
// RECONCILE: whether PushRendered returns a digest (Phase 1 returned only
// ArtifactRef{Repo, Tag}); if not, extend its return in this task — Task 11's
// change-skip logic wants the digest — updating its existing callers (up).
```

Write it in full (~60 lines) following that sequence; every step's error is already typed by the layer that produced it — SyncOnce adds no codes of its own beyond `CUBE-7201`.

`cmd/sync.go` — assembles `Deps` exactly like `status`/`down` connect (config → provider Ensure → Applier → engine factory), takes `<dir>` as `cobra.ExactArgs(1)`, prints `✔ synced <name>@<version> — engine reconciling`. The `--watch` flag lands in Task 11; declare it now, and until Task 11 make `--watch` return `diag.New("CUBE-7202", "watch mode lands in the next task of this plan", "run without --watch")` so the flag surface is stable — remove that stub in Task 11.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/syncer/ ./internal/engine/... ./internal/kube/ ./internal/registry/ -short -v && go build ./...`
Expected: PASS, including both engines' Poke tests and the extended contract suite (envtest-gated packages skip without assets, per Phase 1 convention).

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat: engine Poke, generic port-forward, and one-shot sync command (D7 groundwork)"
```

---

### Task 11: `sync --watch` — fsnotify loop

**Reconcile checkpoint:** requires Task 10 (SyncOnce, Poke, digest-returning push). No new prior-phase surface — but re-verify 0.6 (signal context) so Ctrl-C cancellation flows into the watch loop.

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

Digest-skip: inside the real `SyncOnce` closure path, keep the last pushed digest in `Deps` (small mutable field set by `SyncOnce`'s caller loop) and print `▸ [sync] no manifest changes — skipped push` when the new digest equals the last (RECONCILE: uses the digest return added in Task 10 Step 3; if reconciliation dropped that extension, compare marshaled rendered bytes instead — pick one, implement it, delete this sentence from the reconciled plan).

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

**Reconcile checkpoint:** requires 0.10 (gitea admin secret name/ns/keys, `gitea-http` service name/port, external git hostname — Phase 1 Task 12, possibly reshaped by Phase 2 CoreDNS/trust), 0.4 (engine method set + contract suite for the `DeliverGit` extension — the argocd engine's git-source Application shape exists since Phase 2's cnoe-compat work; reuse its conventions), 0.8 (printed URL scheme/port), Task 10 (`kube.PortForward`).

Spec §6 Phase 3: "creates a Gitea repo and registers an engine source pointing at it — one command from empty repo to deployed." Two halves: a minimal Gitea REST client (create-repo only — not a git client), and the second engine interface extension, `DeliverGit`.

**Files:**
- Create: `internal/gitea/client.go`, `cmd/repo.go`
- Modify: `internal/engine/engine.go` (+`GitSource`, +`DeliverGit`), both engine implementations, the engine contract suite
- Test: `internal/gitea/client_test.go`, engine `DeliverGit` shape tests, contract-suite extension

**Interfaces:**
- Consumes: `apply.Applier` (secret read + object apply), `kube.PortForward`, engine factory.
- Produces:

```go
package engine
type GitSource struct {
	URL    string // in-cluster clone URL, e.g. http://gitea-http.gitea.svc.cluster.local:3000/<owner>/<repo>.git
	Branch string // default "main"
	Path   string // default "./"
}
// DeliverGit registers a continuously-synced git source with the engine
// (flux: GitRepository + Kustomization; argocd: Application with a git
// source). Same purity rule as Deliver: returns objects, caller applies.
Engine interface { … ; DeliverGit(ctx context.Context, name string, src GitSource) ([]*unstructured.Unstructured, error) }

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

- [ ] **Step 3: Failing tests — DeliverGit shapes, then implement (both engines + contract)**

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

Flux implementation mirrors `Deliver` with `source.toolkit.fluxcd.io` `GitRepository` (`spec.url`, `spec.ref.branch`, `interval: 30s`) + a `Kustomization` whose `sourceRef.kind` is `GitRepository` and `spec.path` is `src.Path`. Argo CD: one `Application` with `spec.source.repoURL/targetRevision/path` — RECONCILE: copy the Phase 2 argocd engine's Application scaffolding (project, destination, syncPolicy) from its `Deliver`, changing only the source block. `Poke` must work for git deliveries too: flux pokes the `GitRepository` (same annotation), argocd the Application — extend `Poke` name resolution accordingly and cover it in the contract suite along with a `DeliverGit` case (both engines produce applyable objects for the same `GitSource`).

- [ ] **Step 4: The command**

`cmd/repo.go`:

```go
package cmd

// newRepoCmd: `repo create <name> [--deploy] [-f cube.yaml]`
// Sequence (connection boilerplate identical to `status` — RECONCILE: copy
// the shipped status command's connect block):
//  1. config.Load; provider Ensure; apply.New.
//  2. Read the gitea admin secret via a.Client(): namespace "gitea", label
//     selector cube-idp.dev/cli-secret=true + cube-idp.dev/pack-name=gitea,
//     keys "username"/"password". Missing -> CUBE-7301: "the gitea pack is
//     not installed in this cube" / "add the gitea pack to cube.yaml and
//     re-run `cube-idp up`". RECONCILE: exact secret shape per checkpoint 0.10.
//  3. addr, stop := kube.PortForward(ctx, conn.REST, "gitea", "app.kubernetes.io/name=gitea", 3000)
//     wrapped -> CUBE-7301 on failure. RECONCILE: the gitea chart's pod
//     labels and http port per the shipped pack.
//  4. repo := (&gitea.Client{BaseURL: "http://" + addr, Username: u, Password: p}).EnsureRepo(ctx, name)
//  5. If --deploy:
//       eng := enginefactory.New(cube.Spec.Engine.Type)
//       objs := eng.DeliverGit(ctx, name, engine.GitSource{
//           URL: "http://gitea-http.gitea.svc.cluster.local:3000/" + repo.Owner + "/" + repo.Name + ".git",
//           Branch: repo.DefaultBranch, Path: "./"})   // in-cluster URL: the ENGINE clones, not the laptop
//       a.Apply(ctx, objs, false, 2*time.Minute); a.RecordInventory(ctx, objs)
//       any failure here -> wrap as CUBE-7303 "created the repo but could not
//       register the deploy source" / "re-run `cube-idp repo create <name>
//       --deploy` — repo creation is idempotent".
//  6. Print the access block:
//       ✔ repo <owner>/<name> created
//         clone:  http://gitea.<gw.Host>:<gw.Port>/<owner>/<name>.git   // RECONCILE: scheme+host per checkpoint 0.8/0.10
//         push:   git push <that-url> main
//         deploy: engine syncs ./ on branch <default-branch> (--deploy)   // only when --deploy
```

Write it in full following that sequence; register in root.

- [ ] **Step 5: Run tests**

Run: `go test ./internal/gitea/ ./internal/engine/... -short -v && go build ./...`
Expected: PASS including the extended contract suite for both engines.

- [ ] **Step 6: Commit**

```bash
git add -A && git commit -m "feat: repo create — Gitea repo plus engine git source in one command"
```

---

### Task 13: E2E matrix, CI, README

**Reconcile checkpoint:** requires everything above, plus 0.6 (e2e harness helpers `build`/`run` from Phase 1 Task 13 and whatever Phase 2 added), and the CI workflow as it stands after Phase 2 (the spec §5 matrix now reads {kind, k3d} × {flux, argocd}).

**Files:**
- Create: `tests/e2e/phase3_test.go`
- Modify: `.github/workflows/ci.yaml`, `README.md`

- [ ] **Step 1: E2E additions (gated by CUBE_IDP_E2E=1, reusing the Phase 1 harness helpers)**

`tests/e2e/phase3_test.go` — four scenarios (write them fully; the `build`/`run` helpers come from the existing e2e package — RECONCILE: their exact signatures):

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
```

- [ ] **Step 2: CI matrix + plugin/vendor units in CI**

`.github/workflows/ci.yaml` e2e job gains a provider matrix (RECONCILE: merge into the post-Phase-2 workflow, do not overwrite engine-matrix work Phase 2 may have added):

```yaml
  e2e:
    runs-on: ubuntu-latest
    timeout-minutes: 40
    strategy:
      fail-fast: false
      matrix:
        provider: [kind, k3d]
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: {go-version: "1.24"}
      - run: CUBE_IDP_E2E=1 CUBE_IDP_E2E_PROVIDER=${{ matrix.provider }} go test ./tests/e2e/ -v -timeout 35m
```

…and the e2e harness reads `CUBE_IDP_E2E_PROVIDER` to pick the provider it writes into cube.yaml (default kind). Unit job: no change needed — the new packages all run under `go test ./... -short` (registry/git/httptest fakes, no network).

- [ ] **Step 3: README**

Add sections (each a short paragraph + one example block): k3d provider (`provider: k3d`, render-cluster works for it), air-gap (`vendor` → carry the tarball → `up --bundle`), plugins (naming convention, env contract table, trust model, `plugin install --index`), `sync --watch` (D7, with the "git flows live in the gitea pack" boundary), `repo create` quickstart, published pack refs (Task 5's namespace).

- [ ] **Step 4: Full verification + commit**

Run: `go vet ./... && go test ./... -short && make test-apply && CUBE_IDP_E2E=1 go test ./tests/e2e/ -v -timeout 35m` (locally, docker required; then confirm both matrix legs green in CI).

```bash
git add -A && git commit -m "feat: phase 3 e2e (k3d, bundle, sync, repo create), CI provider matrix, docs"
```

---

## Self-Review

**1. Spec §6 Phase 3 coverage — item → task:**

| Spec §6 Phase 3 item | Task(s) |
|---|---|
| k3d provider (D4, D10 customization, contract parity) | 1 (contract suite), 2 (provider + merge + render-cluster) |
| `vendor` / `up --bundle` air-gap (driven by `cube.lock`, spec §4.1) | 6 (vendor + bundle format), 7 (offline up + image loading) |
| Exec-plugin discovery + index (spec §4.4 tier 2: PATH, env contract, sha256-pinned git index, first-run trust warning) | 8 (discovery/env/trust), 9 (index install) |
| Pack catalog buildout (backstage, cert-manager, external-secrets, envoy-gateway) | 4 (packs), 3 (`pack push` enabling publication), 5 (CI → ghcr; closes Phase 1 Task 13's `--local` wrinkle — `config.Default`'s OCI refs become real) |
| `sync --watch` (D7: fsnotify → OCI push → engine reconciles; git flow only via gitea pack) | 10 (one-shot + Poke), 11 (--watch), boundary documented in 11/13 |
| `repo create <name> [--deploy]` (empty repo → deployed) | 12, acceptance-tested in 13 |
| Cross-cutting: spec §5 matrix ({kind, k3d} e2e), doctor-grade errors, deadlines | 13; CUBE codes throughout |

**2. Placeholder scan:** no TBDs; every deferral is a `RECONCILE:` with a named artifact to verify and a stated resolution path (grep this file for `RECONCILE:` — each hit names its checkpoint). Comment-contract blocks (k3d.go, pushdir.go, LoadImages, repo.go) specify exact calls, error codes, and remediation strings, not "handle errors".

**3. Type consistency:** `cluster.Provider`/`kube.Conn` names follow Phase 1 Task 4/5 (incl. the `internal/kube` leaf move) — checkpoint 0.1 re-verifies; `engine.Engine` extensions (`Poke`, `DeliverGit`) are declared once in Task 10/12 Interfaces blocks and used with those exact signatures in Tasks 11–13; `bundle.Opened` methods used in Task 7 (`PackDirLookup` added explicitly in Task 7 Step 2); `oci.PushPackDir` (Task 3) vs `oci.PushRendered` (Phase 1) are distinct and both referenced consistently; the `PushRendered` digest-return extension is declared in Task 10 and consumed in Task 11 with a stated fallback.

**4. Known cross-task invariants to hold during reconciliation:** the gateway node-port single-source constant (Task 2 hoists `cluster.GatewayNodePort`; kindp refactored in the same step); the ghcr namespace decision applies to Task 5 (workflow + `config.Default`), Task 9 (`DefaultIndex`), and Task 13 (README) identically; the engine contract suite must gain `Poke` and `DeliverGit` cases in the same commits that extend the interface (Tasks 10 and 12) — an interface method without contract coverage for both engines is a D2 violation.

**Open design questions (could not be resolved from the spec — decide during reconciliation and record the decision here):**

1. **ghcr namespace:** spec examples say `oci://ghcr.io/cube-idp/packs/...` but the module lives at `github.com/rafpe/cube-idp` — publishing to `ghcr.io/cube-idp/...` requires owning that GitHub org. Decide: create the org, or switch every ref to `ghcr.io/rafpe/cube-idp/packs/...` (Tasks 5, 9, 13 + `config.Default`).
2. **`CUBE_IDP_REGISTRY` semantics for plugins:** the spec names the variable but not its value. This plan passes the in-cluster zot URL and documents that host-side plugins must open their own port-forward (via `CUBE_IDP_KUBECONFIG`). Alternative (nicer for plugin authors, heavier for cube-idp): spawn a tunnel for the plugin's lifetime and pass `127.0.0.1:<port>`. Confirm with the spec owner.
3. **`up --bundle` on `provider: existing`:** rejected with `CUBE-7005` in this plan (no node-loading capability). The alternative — pushing bundle images into a user-supplied registry plus mirror config — is real work with its own UX; if air-gapped-existing is demanded, it needs a plan amendment, not an inline extension.
4. **envoy-gateway as a first-class `spec.gateway.pack` choice:** D3 ships it as an alternative pack; whether Phase 3 must make `gateway.pack: envoy-gateway` fully turnkey (Gateway object + NodePort parity, Task 4's RECONCILE note) or just publish the pack is a scope call for the reconciler.
5. **Default plugin index repo:** `plugin install` requires `--index` until an official index repo exists (Task 9). Decide whether creating and seeding that repo is in Phase 3 scope.



