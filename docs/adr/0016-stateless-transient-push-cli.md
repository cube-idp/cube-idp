---
status: "accepted"
date: 2026-07-20
decision-makers: "cube-idp maintainers"
---

# 16. Stateless, Transient Push-Based CLI with No Resident Process

## Context and Problem Statement

A tool that provisions a local Kubernetes environment can be built two ways. It can be an
*operator*: something that installs a controller, watches resources, and reconciles
continuously — which makes the tool itself part of the running system, with its own
upgrade path, its own CRDs and its own failure modes. Or it can be a *pusher*: a binary
the developer runs, which mutates the cluster and exits, leaving the cluster's own GitOps
engine to do the reconciling.

The operator shape is expensive here. It puts long-lived state on the developer's laptop,
makes the tool's version a live dependency of every cluster it ever touched, and turns
debugging into "which of the two reconcilers is fighting me". It also invites unbounded
extensibility — RPC plugin runtimes, Wasm sandboxes, background agents — each of which is
a new resident process and a new compatibility surface.

cube-idp therefore needs an explicit, enforceable boundary: what the binary is allowed to
leave behind, what the command packages are allowed to contain, and how far extensibility
may go before a proposal is rejected outright rather than accommodated.

## Decision

cube-idp is a stateless, transient push-based CLI that exits — not an operator. It ensures
a cluster exists, server-side-applies a GitOps engine plus the in-cluster zot registry,
renders and delivers data-only packs, diagnoses, and exits. It never runs continuous
reconciliation itself, installs no in-process controller-runtime manager, runs no git
server in the core, and leaves nothing resident on the developer machine: no daemon, no
laptop-resident operator, no persistent full-screen dashboard, and no alt-screen TUI.

`up` runs a fixed sequence — ensure the local CA, load config, ensure cluster, install
registry, install engine, open the registry tunnel, then fetch/render/push/deliver each
pack, then wait for health — using plain server-side-apply library calls with
deterministic exit codes, per-step timeouts, and the single field manager `cube-idp`.

The binary is a thin kernel. Cobra command packages contain no business logic: every
command is a thin shell over an `internal/` orchestrator, and engines stay behind the
`internal/engine.Engine` interface.

Extensibility is limited to exactly three tiers — data-only packs, PATH-discovered exec
plugins, and in-tree Go interfaces — with no RPC plugins, no Wasm runtime and no
long-running daemons. Integrations that could be packs ship as packs; Argo CD and Gitea in
particular are optional packs, not built-in parts of the tool. Any new extension mechanism
must fit one of the three tiers or be rejected.

Exec plugins receive a best-effort environment contract (`CUBE_IDP_KUBECONFIG`,
`CUBE_IDP_CUBE_NAME`, `CUBE_IDP_REGISTRY`, `CUBE_IDP_CA`) in which missing inputs omit
variables rather than erroring, and running a plugin never creates a CA. Preflight checks
and provider `Diagnose` run on demand via `cube-idp doctor`.

## Consequences

* Good, because the cluster keeps working when the CLI is uninstalled, downgraded or
  simply never run again — nothing in the cluster depends on a laptop process.
* Good, because a run is a bounded, deterministic sequence with per-step timeouts and
  CUBE-xxxx exit codes, so failures are attributable to a step rather than to a race
  between reconcilers.
* Good, because a single field manager (`cube-idp`) makes ownership of every applied field
  unambiguous, and pruning/adoption behaviour follows from it.
* Good, because a closed three-tier extension model gives a clear rejection criterion for
  new mechanisms instead of an ever-growing plugin surface.
* Bad, because there is no drift correction between runs: anything the GitOps engine does
  not own stays drifted until the user runs `up` again.
* Bad, because preflight is opt-in via `doctor`, so `up` can fail late on a condition a
  mandatory preflight would have caught early.
* Bad, because "no resident process" rules out genuinely useful long-lived UX (a watching
  dashboard, a background updater); `sync --watch` is the single sanctioned foreground
  carve-out and is explicitly not a daemon.
* Bad, because the no-CRD purity of the original decision could not be held: the kernel now
  ships an inert `packs.cube-idp.dev` CRD and a CubeLock record.

## Implementation Status

**This decision is implemented.** Confirmed against the code on 2026-07-20.

| Decision | Implemented at |
| --- | --- |
| cube-idp is a stateless push-based CLI, not an operator: it ensures a cluster, server-side-applies a GitOps engine plus an in-cluster zot registry, delivers data-only packs, diagnoses, and exits — never reconciling continuously itself. | `internal/up/up.go:97` |
| cube-idp ships as a single static Go binary that pushes state and exits — no long-running daemon, no embedded git server, nothing left running on the developer machine; `sync --watch` is the only sanctioned long-running foreground mode and is not a daemon. | `internal/up/up.go:209-224` |
| Nothing runs on the developer's machine after the command exits: no in-process CRD controllers on the bootstrap path, which uses plain SSA library calls with deterministic exit codes and per-step timeouts. | `internal/apply/applier.go:45` |
| The kernel binary installs no Kubernetes controllers of its own: no in-process controller-runtime manager, no git server in the core, and no RPC or Wasm plugin runtime. | `internal/up/up.go:209-224` |
| `up` runs a fixed sequence: ensure the local CA, load config, ensure cluster, install registry, install engine, open the registry tunnel, then fetch/render/push/deliver each pack, and finally wait for health. | `internal/up/up.go:122-135` |
| All server-side applies use the field manager `cube-idp`. | `internal/apply/applier.go:21` |
| CUBE error codes are partitioned by range: 0xxx preflight/config, 1xxx cluster, 2xxx apply, 3xxx engine, 4xxx pack, 5xxx registry, 6xxx trust/hostname, 7xxx vendor/plugin/sync/repo, 8xxx spoke. | `internal/diag/codes.go:136-171` |
| Cobra command packages contain no business logic; every command is a thin shell over an `internal/` orchestrator, engines stay behind `internal/engine.Engine`, and `internal/trust` is a leaf package with zero implicit side effects. | `cmd/up.go:12-36` |
| Extensibility is limited to exactly three tiers — data-only packs, PATH-discovered exec plugins, and in-tree Go interfaces — with no RPC plugins, no Wasm runtime and no daemons. | `internal/plugin/discover.go:35-63` |
| The exec-plugin environment contract passes `CUBE_IDP_KUBECONFIG`, `CUBE_IDP_CUBE_NAME`, `CUBE_IDP_REGISTRY` and `CUBE_IDP_CA`, assembled best-effort: missing inputs yield omitted env vars rather than an error, and running a plugin never creates a CA. | `internal/plugin/exec.go:36-59` |
| Argo CD and Gitea ship as optional packs, not as core components of the tool. | `internal/config/types.go:251-252` |
| The live UI uses Bubble Tea inline mode only — the program lives strictly inside `RunE`, quits on RunDone/Diagnosis, and leaves no goroutine surviving process exit. | `internal/ui/render/live.go:36` |
| No persistent full-screen dashboard or resident cluster-browser UI ships, and no command uses alt-screen mode. | `cmd/status.go:92` |
| Preflight checks and provider `Diagnose` run on demand via `cube-idp doctor`. | `internal/doctor/doctor.go:433-487` |
| `existing`-cluster mode gets no extra guard: `VerifyEnginePackRef` plus the `engineHealthyAtStart` preflight suffice, and SSA onto a foreign pre-existing engine install is documented operator error. | `internal/pack/enginepack.go:31-42` |
| The core stack is published to the project's public ghcr namespace and consumed through the same pack mechanism users extend with, so the mechanism is dogfooded; only a minimal core remains embedded. | `internal/registry/zot.go:19` |
| The built-in pack catalog is an offline fallback only; the live catalog is fetched from the published index artifact. | `cmd/pack.go:71-81` |

### Verification

- [ ] `internal/apply/applier.go:21` declares `FieldManager = "cube-idp"` and it is the only
      `ssa.Owner{Field: ...}` constructed in the package.
- [ ] No `manager.Start` / controller-runtime manager exists anywhere under `internal/`;
      controller-runtime appears only as a client library.
- [ ] `grep -rn "AltScreen" cmd internal` yields only the doc comment at `cmd/status.go:92`
      and the assertion fence in `internal/ui/render/live_test.go` — never an enabling call.
- [ ] `internal/ui/render/live.go:36` constructs `tea.NewProgram` with only `tea.WithOutput`
      and `tea.WithInput`.
- [ ] `internal/up/up.go:122-135` runs `trust.Dir()` / `trust.EnsureCA` before any cluster
      provider `Ensure` call.
- [ ] `internal/plugin/exec.go:36-59` sets exactly the four `CUBE_IDP_*` contract variables
      and appends only non-empty values.
- [ ] `internal/plugin/discover.go:35-63` discovers plugins solely via `exec.LookPath` over
      `$PATH` and `InstallDir()`; `go.mod` contains no `hashicorp/go-plugin` and no Wasm runtime.
- [ ] `internal/pack/guards.go` strips every symlink from a fetched pack tree (packs are data-only).
- [ ] `internal/diag/codes.go:136-171` allocates 70xx vendor/air-gap, 71xx plugin, 72xx sync,
      73xx repo and 8xxx spoke codes (CUBE-7005, CUBE-7301, CUBE-8001 are live, not reserved).
- [ ] `internal/pack/enginepack.go:31-42` raises `CUBE-0013` when the fetched pack name is not
      `cube-engine-<engine.type>`, before any cluster mutation.
- [ ] `cmd/up.go` is a cobra shell delegating to `internal/up`; no doctor check is invoked from
      `cmd/up.go` or `internal/up/up.go`.
- [ ] `internal/config/types.go:251-252` lists gitea and argocd as ordinary `spec.packs` OCI refs.

## History

The "no CRDs" clause of the original single-binary decision was dropped: `up` now applies the
inert `packs.cube-idp.dev` CRD (`internal/up/up.go:209-224`) and writes a CubeLock record under
`cube-idp.dev/v1alpha1`. The no-daemon, single-binary, no-controller and no-plugin-runtime
halves remain intact, as does the `sync --watch` foreground carve-out.

The error-code partition was extended. The original scheme reserved 7xxx; that reservation was
lifted and 7xxx is now sub-ranged into vendor/air-gap (70xx), exec plugin (71xx), sync (72xx)
and repo (73xx), with a new 8xxx spoke range added.

Preflight originally ran automatically before every `up`. It now runs on demand via
`cube-idp doctor` (plus a targeted port check in `init`), over a narrower check set than
originally specified — container-runtime, gateway-port, http-port, disk-space, inotify and
git-cli, without cgroup pid limits, registry rate limits or MITM proxy detection.

"Everything installed is a pack, nothing is compiled into the binary" was narrowed. The
engine-as-pack work (2026-07-19) completed the dogfooding for the engine, but a minimal core —
the zot registry manifests and the Pack CRD — remains embedded in the binary.

The launch pack catalog was originally fixed at traefik, argocd, gitea, backstage, cert-manager
and external-secrets plus a cnoe-compat loader. The published catalog has since grown past those
six (envoy-gateway as an alternative gateway, plus the two `cube-engine-*` packs), and the binary
now carries only a two-pack offline fallback with the live list fetched from the published index.

## More Information

Origin: mined from the archived planning corpus (`docs/archive/superpowers/`) during the
2026-07-20 documentation audit; the underlying statements were validated against the code
before this record was written.

Member provenance:

- `docs/archive/superpowers/specs/2026-07-13-cube-idp-architecture-design.md:13` — stateless push-based CLI, not an operator
- `docs/archive/superpowers/specs/2026-07-13-cube-idp-architecture-design.md:57` — nothing resident on the developer machine
- `docs/archive/superpowers/specs/2026-07-13-cube-idp-architecture-design.md:253` — the three extension tiers
- `docs/archive/superpowers/plans/2026-07-13-cube-idp-phase2-draft.md:13` — thin cobra shells over `internal/` orchestrators
- `docs/archive/superpowers/plans/2026-07-13-cube-idp-phase3-draft.md:48` — single static binary, no daemon
