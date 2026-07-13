# neocube — Architecture Design

**Date:** 2026-07-13
**Status:** Approved direction, pending spec review
**Replaces:** [cnoe-io/idpbuilder](https://github.com/cnoe-io/idpbuilder) (studied at v0.10.2)

## 1. Vision

neocube is a single static Go binary that stands up a complete internal developer
platform on a local or existing Kubernetes cluster in under a minute — and then
gets out of the way.

**Core thesis: neocube is a pusher, not an operator.** The binary does four
things: (1) ensures a cluster exists, (2) server-side-applies a GitOps engine
plus a tiny in-cluster OCI registry, (3) renders and delivers data-only
*packs*, (4) diagnoses loudly and exits. Continuous reconciliation is the
GitOps engine's job in-cluster. Re-running `neocube up` **is** the upgrade
command. The inventory makes `neocube down` a true cascading delete.

This single structural choice deletes idpbuilder's deepest complexity — its
in-process controller-runtime manager, three CRDs, embedded git server with an
admin user, and the `cnoe://` URL-rewriting scheme — and with it, its worst
recurring issue clusters (fragile bootstrap #430, cert/trust pain #451/#545,
no remote clusters #74, no upgrade story #566, weak teardown #351/#458/#489).

## 2. Decisions (made 2026-07-13)

| # | Decision | Choice |
|---|----------|--------|
| D1 | Base architecture | **Kernel+Packs** — minimal core, everything optional is a pack |
| D2 | GitOps engine | **Fully swappable at v1** — `GitOpsEngine` Go interface with Flux and Argo CD implementations compiled in; Flux is the default. Interface exists from day one; Flux impl ships in Phase 1, Argo CD impl in Phase 2, both required before v1.0 |
| D3 | Default ingress | **Traefik implementing Gateway API** — Gateway API resources are the canonical routing surface; Traefik is the default implementation (shipped as the default gateway pack). Envoy Gateway ships as an alternative pack. ingress-nginx (EOL 2026-03) only via community pack |
| D4 | MVP cluster providers | **kind + existing** — kind via `sigs.k8s.io/kind` as library; `existing` targets any kubeconfig context (EKS, k3s, rancher-desktop…). k3d lands Phase 3 |
| D5 | Config schema | `neocube.dev/v1alpha1` during pre-1.0 with a `neocube migrate` command committed at the v1 freeze (avoid k3d's multi-year alpha churn by freezing early) |
| D6 | Trust posture | OS trust-store changes only via an explicit, separately-consented `neocube trust` command; full uninstall on `neocube down`. Never implicit |
| D7 | Watch mode | `neocube sync ./dir --watch` = fsnotify → OCI artifact push → engine reconciles. A git-push-based flow is available only when the Gitea pack is installed (migration aid, not core) |
| D8 | Plugin protocols | **No gRPC (hashicorp/go-plugin) and no Wasm at v1.** Reserve a `hooks` media type in the pack format for a future Extism integration |

## 3. Goals and non-goals

**Goals**
- `neocube up` → working platform (cluster + GitOps engine + gateway) in <60s on a laptop, with only a container runtime installed.
- Same command, same config, targets an existing remote cluster: `neocube up --context my-eks`.
- Everything beyond the kernel is a versioned, shareable, OCI-distributed pack.
- Honest day-2: `diff`, `upgrade --plan`, idempotent `up`, cascading `down`, air-gapped `vendor`/`--bundle`.
- Every failure ends in a rendered diagnosis (typed `NEO-xxxx` code + remediation hint), never an infinite spinner.
- Migration path for idpbuilder/CNOE users via a cnoe-compat loader.

**Non-goals**
- neocube does not run anything on the developer's machine after it exits (no daemon, no laptop-resident operator).
- No neocube CRDs. The cluster's record of truth is the GitOps engine's own resources plus an SSA inventory.
- Not a Backstage/portal product — portals are packs.
- No plugin RPC protocol at v1 (see D8).

## 4. Architecture

```
                       ┌─────────────────────────────────────────────┐
                       │            neocube (single Go binary)        │
                       │                                             │
 cube.yaml ──────────▶ │  Config loader          Doctor / Preflight  │
 (YAML authored;       │  (CUE-validated)        (NEO-codes,         │
  JSON Schema for      │        │                 remediation hints) │
  editors; CUE inside) │        ▼                        │           │
                       │  ClusterProvider iface ◀────────┘           │
                       │   ├── kind     (library, docker/podman)     │
                       │   └── existing (any kubeconfig context)     │
                       │        │                                    │
                       │        ▼                                    │
                       │  Apply engine (fluxcd/pkg/ssa)              │
                       │   SSA + kstatus waits + inventory + prune   │
                       │   powers: up / diff / upgrade --plan / down │
                       │        │                                    │
                       │  GitOpsEngine iface                         │
                       │   ├── flux  (default: source/kustomize/helm │
                       │   │          controllers + OCIRepository)   │
                       │   └── argocd (Applications + OCI repo src)  │
                       │        │                                    │
                       │  Pack engine            Trust helper        │
                       │   fetch (oci/git/dir)    local CA (mkcert   │
                       │   CUE #Values schema     mechanism),        │
                       │   Helm v4 + kustomize    containerd certs.d,│
                       │   render in-process      CoreDNS rewrite    │
                       └────────┼────────────────────────────────────┘
                                │ SSA apply            │ OCI push (oras-go)
                                ▼                      ▼
                  ┌──────────────────────────────────────────────┐
                  │ cluster:  GitOps engine (Flux dflt | ArgoCD) │
                  │           zot OCI registry (~20MB)           │
                  │           gateway pack: traefik (GW API)     │
                  │           packs: argocd-ui | gitea | backstage│
                  │                  cert-manager | ext-secrets …│
                  │           (all optional, all OCI artifacts)  │
                  └──────────────────────────────────────────────┘
```

### 4.1 Units and interfaces

Each unit has one purpose, a small interface, and is testable in isolation.

**ClusterProvider** (`internal/cluster`) — ensures a cluster exists and is reachable.

```go
type ClusterProvider interface {
    Ensure(ctx context.Context, cfg ClusterConfig) (Conn, error) // idempotent
    Delete(ctx context.Context, name string) error
    Exists(ctx context.Context, name string) (bool, error)
    Kubeconfig(ctx context.Context, name string) ([]byte, error)
    Diagnose(ctx context.Context, name string) []Finding // feeds doctor
}
```
Implementations are compiled in (no plugin protocol): `kind`
(`sigs.k8s.io/kind/pkg/cluster`, auto-detects docker/podman/nerdctl) and
`existing` (client-go `clientcmd`, honors `KUBECONFIG`). New providers are PRs.

**GitOpsEngine** (`internal/engine`) — installs the reconciler and translates
delivery intents into engine-native resources. This is Facet's key graft,
promoted to v1 per D2:

```go
type GitOpsEngine interface {
    Install(ctx context.Context, a *Applier, opts EngineOptions) error // SSA-applies engine manifests (go:embed, works offline)
    Deliver(ctx context.Context, pack RenderedPack, src ArtifactRef) ([]unstructured.Unstructured, error)
        // flux: OCIRepository + Kustomization/HelmRelease
        // argocd: Application with OCI repo source
    Health(ctx context.Context) ([]ComponentHealth, error)
    Uninstall(ctx context.Context, a *Applier) error
}
```
Rule learned from Facet's feasibility score: *an abstraction with one
implementation is a lie* — the interface is only trusted once both Flux and
Argo CD implementations pass the same contract-test suite. Neither engine's
types leak above this interface; packs describe *intent* (chart/kustomize/raw
manifests + values), engines translate.

**Applier** (`internal/apply`) — thin wrapper over `fluxcd/pkg/ssa
ResourceManager`: server-side apply with field manager `neocube`, kstatus
health waits with hard deadlines, an inventory ConfigMap per cube for
diff/prune/down, and prune-preview with an opt-out annotation
(`neocube.dev/prune: disabled`) from v0.1.

**Pack engine** (`internal/pack`) — fetch, validate, render:
- Sources: local dir, `github.com/org/repo//path@vX` (commit-pinned via
  go-getter-style resolution), `oci://ghcr.io/org/pack:v1` (oras-go v2).
- A pack is **data only**: `pack.cue` (name, semver, deps, `#Values` schema)
  + manifests / chart references / kustomize overlays.
- Rendering in-process: Helm v4 SDK (`helm.sh/helm/v4` action pkg, wrapped
  behind an internal interface — it is young), `sigs.k8s.io/kustomize/api`,
  `cuelang.org/go` for schema validation. `neocube add <pack>` validates
  values against `#Values` *before* touching the cluster.
- Pins recorded in `cube.lock` (digests + full image list) — feeds
  `neocube vendor` for air-gap and makes installs reproducible.

**Trust helper** (`internal/trust`) — smallstep/truststore (the mkcert
mechanism) for opt-in OS CA install via `neocube trust`; containerd `certs.d`
config through the kind provider; CoreDNS rewrite for one canonical hostname
(default `*.neocube.localtest.me`, overridable).

**Doctor / errors** (`internal/diag`) — every user-facing failure carries a
typed `NEO-xxxx` code, a one-line cause, and a copy-pasteable remediation.
Every wait has a deadline and ends in a rendered diagnosis (kstatus
conditions + pod events + last log lines). `neocube doctor` runs the
providers' and engines' `Diagnose`/`Health` plus preflights (runtime present,
ports free, disk space, inotify limits).

**CLI** (`cmd/`) — cobra v1.10; `huh` for the `neocube init` wizard and
missing-value prompts; `lipgloss` status lines. No full Bubble Tea app in the
kernel — a spinner is not a TUI.

### 4.2 Configuration

Users author plain YAML (`cube.yaml`); CUE is internal plumbing, never the
authoring surface. A published JSON Schema powers editor completion.

```yaml
apiVersion: neocube.dev/v1alpha1   # D5: frozen to v1 with `neocube migrate`
kind: Cube
metadata:
  name: dev
spec:
  cluster:
    provider: kind            # kind | existing
    # context: my-eks         # for provider: existing
    kubernetesVersion: v1.33
  engine:
    type: flux                # flux | argocd   (D2)
  gateway:
    pack: traefik             # D3: Gateway API surface, traefik default impl
    host: neocube.localtest.me
    port: 8443
  packs:
    - ref: oci://ghcr.io/neocube/packs/argocd:1.x    # UI, optional
    - ref: ./platform/backstage                      # local dir
      values:
        replicas: 1
```

### 4.3 Data flow of `neocube up`

1. Load + CUE-validate `cube.yaml`; run preflight doctor checks.
2. `ClusterProvider.Ensure` — create kind cluster or verify existing context.
3. `Applier` SSA-applies zot registry manifests; wait healthy.
4. `GitOpsEngine.Install` — SSA-apply Flux (or Argo CD) from embedded manifests.
5. For each pack: fetch → validate values against `#Values` → render →
   push rendered artifact to zot (oras-go) → `GitOpsEngine.Deliver`.
6. Wait (deadline-bound) for engine health of all delivered packs; write
   inventory + `cube.lock`; print access table (URLs, credentials pointers); exit 0.

`down` reads the inventory and deletes in reverse dependency order, then
deletes the cluster (kind) or only neocube-managed resources (existing), and
reverts any `neocube trust` changes.

### 4.4 Extensibility model — three tiers, no more

1. **Packs (target: 90% of extensions)** — data-only directories as above.
   Official catalog at launch: `traefik`, `argocd`, `gitea`, `backstage`,
   `cert-manager`, `external-secrets`, plus the **cnoe-compat loader** that
   ingests existing CNOE/idpbuilder Argo `Application`/`ApplicationSet` YAMLs
   and translates `cnoe://` paths into OCI pushes. Launch-critical.
2. **Exec plugins** — `neocube-<name>` on `PATH` (krew model), env-var
   contract (`NEOCUBE_KUBECONFIG`, `NEOCUBE_CUBE_NAME`, `NEOCUBE_REGISTRY`),
   sha256-pinned git index, explicit trust warning on first run.
3. **In-tree Go interfaces** — `ClusterProvider`, `GitOpsEngine`,
   `PackSource`; new implementations are PRs, not plugins (D8).

### 4.5 Error handling principles

- No silent fallbacks: a pack that fails to render aborts before any cluster
  mutation; a partially-applied `up` reports exactly which inventory entries
  exist and how to resume (`up` is idempotent, so resume = re-run).
- Deadlines everywhere; timeout output includes the diagnosis bundle.
- `--verbose` streams the underlying SSA/engine operations; default output is
  the lipgloss status view.

## 5. Testing strategy

- **Unit:** config loader (CUE schema fixtures), pack renderer golden-file
  tests (input pack → rendered manifests), lockfile determinism.
- **Contract tests:** one shared suite run against every `ClusterProvider`
  and every `GitOpsEngine` implementation — the mechanism that keeps D2
  honest (Flux and Argo CD must both pass identical delivery/health/uninstall
  assertions, against envtest + a fake OCI registry where possible).
- **E2E (CI):** matrix — {kind} × {flux, argocd} × {up, add, diff, upgrade,
  down} on GitHub Actions runners; smoke test for `existing` via a k3s
  container. Sub-minute `up` is a tracked CI metric, not a slogan.
- **Doctor tests:** fault-injection fixtures (port squatting, missing
  runtime, broken kubeconfig) asserting the right `NEO-xxxx` code appears.

## 6. Phased roadmap

- **Phase 1 — MVP (~3-4 wks):** cobra shell; `cube.yaml` loader + JSON
  Schema; `kind` + `existing` providers; SSA apply engine with
  inventory/deadlines/diagnosis; `GitOpsEngine` interface + **Flux**
  implementation; embedded zot; local-dir and OCI packs; `up / down /
  status / get secrets`; starter packs: traefik (Gateway API), argocd (UI),
  gitea.
- **Phase 2 (~2-3 wks):** **Argo CD engine implementation + contract suite**
  (D2); `neocube trust` + CoreDNS canonical hostname; `diff`; `doctor` with
  NEO codes; `cube.lock` + `upgrade --plan`; git-ref pack source;
  cnoe-compat loader.
- **Phase 3:** k3d provider; `vendor` / `up --bundle` air-gap; exec-plugin
  discovery + index; pack catalog buildout (backstage, cert-manager,
  external-secrets, envoy-gateway); `sync --watch`.
- **Phase 4 (post-1.0, evidence-driven):** Talos/vcluster providers if
  demanded; Extism hooks if Helm 4's plugin system (HIP-0026) matures;
  optional in-cluster neocube-operator for self-healing the engine install.

## 7. Risks

- **Helm v4 SDK youth** — mitigated by wrapping it behind one internal
  interface; can pin to v3 SDK if v4 breaks.
- **Two engines at v1 doubles delivery-path surface** (accepted via D2) —
  mitigated by the contract-test suite and by keeping the interface at
  "deliver rendered artifact" altitude, not engine-feature parity.
- **Argo CD OCI source maturity** — if Argo's OCI repository support proves
  insufficient for the Deliver path, the Argo engine falls back to delivering
  via a lightweight in-cluster git repo provided by the gitea pack
  (documented as an engine-specific requirement, not a core component).
- **CNOE ecosystem gravity** — mitigated by the cnoe-compat loader and by
  competing on the deleted-complexity story (no CRDs, no daemon, sub-minute
  up, every failure explained).

## 8. Supporting research

Full agent-team outputs (research dossier on idpbuilder internals & pain
points, ecosystem scan, extensibility-pattern survey, three competing
proposals, judge scoring) are archived under
`docs/superpowers/research/2026-07-13-neocube-brainstorm/`.
