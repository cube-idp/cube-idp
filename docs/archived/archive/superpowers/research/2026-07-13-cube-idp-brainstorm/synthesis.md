## Recommended architecture: Kernel+Packs, hardened with Lattice's diagnostics and Facet's day-2 verbs

**Base:** Proposal 1 (Kernel+Packs), essentially intact. **Grafts:** Lattice's doctor/typed-error/diff UX and cube parameter schemas; Facet's `upgrade --plan` and lockfile discipline, and its *interface shapes* (but not its plugin protocol or provider matrix).

### Core thesis

neocube is a pusher, not an operator. The binary does four things: (1) ensure a cluster exists, (2) server-side-apply Flux + a tiny in-cluster OCI registry (zot), (3) render and deliver data-only packs, (4) diagnose loudly and exit. There is no in-process controller-runtime manager, no neocube CRDs, no git server in the core, no RPC or Wasm plugin runtime at v1. Continuous reconciliation is Flux's job in-cluster; neocube's job ends when the desired state is applied and healthy. Re-running `up` is the upgrade command; the inventory makes `down` a true cascading delete. This one structural choice dissolves idpbuilder's worst issue clusters (#351/#458/#489, no upgrade story) instead of patching them.

### Component diagram

```
                         ┌─────────────────────────────────────────────┐
                         │              neocube (single Go binary)      │
                         │                                             │
  cube.yaml ──────────▶  │  Config loader          Doctor/Preflight    │
  (JSON-Schema in        │  (CUE-validated YAML)   (typed NEO-codes,   │
   editor; CUE inside)   │        │                 remediation hints) │
                         │        ▼                        │           │
                         │  ClusterProvider iface ◀────────┘           │
                         │   ├── kind    (library)                     │
                         │   ├── k3d     (library)                     │
                         │   └── existing (any kubeconfig ctx)         │
                         │        │                                    │
                         │        ▼                                    │
                         │  Apply engine (fluxcd/pkg/ssa)              │
                         │   SSA + kstatus waits + inventory + prune   │
                         │   powers: up / diff / upgrade --plan / down │
                         │        │                                    │
                         │  Pack engine            Trust helper        │
                         │   fetch (oci/git/dir)    local CA (mkcert-  │
                         │   CUE #Values schema     style), containerd │
                         │   Helm v4 + kustomize    trust, CoreDNS     │
                         │   render in-process      rewrite            │
                         └────────┼────────────────────────────────────┘
                                  │ SSA apply            │ OCI push (oras-go)
                                  ▼                      ▼
                    ┌──────────────────────────────────────────────┐
                    │ cluster:  Flux (source/kustomize/helm ctrl)  │
                    │           zot OCI registry (~20MB)           │
                    │           packs: traefik | argocd | gitea |  │
                    │                  backstage | cert-manager …  │
                    │           (all optional, all OCI artifacts)  │
                    └──────────────────────────────────────────────┘
```

### Concrete library choices

- **CLI:** cobra v1.10; huh v2 for `neocube init` wizard and missing-value prompts; lipgloss v2 status lines. No full Bubble Tea app in the kernel (Kernel+Packs is right: a spinner is not a TUI) — but adopt Lattice's rule that every wait has a hard deadline and ends in a rendered diagnosis (kstatus conditions + pod events + last log lines), never an infinite spinner (#430's 57 comments).
- **Cluster:** `sigs.k8s.io/kind/pkg/cluster` v0.32 (default; docker/podman/nerdctl auto-detect), `k3d-io/k3d/v5`, client-go clientcmd for `existing` (honors KUBECONFIG, closes #74/#246/#362). One 5-method interface: Ensure/Delete/Exists/Kubeconfig/Diagnose. Compiled-in only — reject go-plugin gRPC at v1; two-to-four providers do not justify protocol versioning.
- **Apply:** `fluxcd/pkg/ssa` ResourceManager — SSA, WaitForSet, kstatus health, inventory, prune. Add Facet/Lattice's `neocube diff` (server-side dry-run) and a prune-preview with opt-out annotation from v0.1 (the Crossplane footgun).
- **GitOps:** Flux v2.9 source/kustomize/helm controller manifests embedded via go:embed (works offline); `project-zot/zot` as the in-cluster registry; `oras-go` v2.6 + `fluxcd/pkg/oci` for artifact push. Local dirs become Flux OCIRepository artifacts — Gitea, giteaAdmin, password drift, cnoe://, and split-horizon git URLs (#296/#300/#304/#398) are simply gone. Argo CD and Gitea ship as *packs* for people who want the UIs.
- **Packs:** Helm v4 SDK (`helm.sh/helm/v4` action pkg, wrapped behind one internal interface — it's young), `sigs.k8s.io/kustomize/api`, `cuelang.org/go` v0.17 for pack.cue metadata + #Values schemas (graft Lattice's typed-params idea: `neocube add` validates values before touching the cluster). Pins recorded in `cube.lock` (Facet's lockfile discipline) — digests + full image list, feeding `neocube vendor` for air-gap.
- **Trust:** smallstep/truststore (mkcert mechanism) for opt-in OS trust via a separate, loudly-consented `neocube trust`; containerd certs.d via the kind provider; CoreDNS rewrite for one canonical hostname. Kills #451/#545/#389/#534.
- **Config:** `cube.yaml`, apiVersion neocube.dev/v1, CUE-validated with published JSON Schema. Users write YAML; CUE is internal plumbing (do NOT make CUE the authoring surface — Lattice's biggest adoption risk).

### Extensibility model (three tiers, no more)

1. **Packs (90%):** data-only directories — pack.cue (name, semver, deps, #Values schema) + manifests/HelmRelease/chart refs. Addressed as `./dir`, `github.com/org/repo//path@vX` (commit-pinned), or `oci://ghcr.io/org/pack:v1`. Official catalog at launch: traefik, argocd, gitea, backstage, cert-manager, external-secrets, plus a **cnoe-compat loader** that ingests existing CNOE stacks' Argo Application YAMLs and translates cnoe:// paths to OCI pushes — this is launch-critical, not nice-to-have.
2. **Exec plugins:** `neocube-<name>` on PATH (krew model), env-var contract, sha256-pinned git index, explicit trust warning.
3. **In-tree Go interfaces:** ClusterProvider and PackSource; new implementations are PRs. Extism/Wasm hooks tracked (reserve a `hooks` media type in the pack format, per Lattice) but adopted only after Helm 4's HIP-0026 proves out.

### Example UX

```
$ neocube up                          # kind + Flux + zot, <60s, exits 0
$ neocube up --context my-eks         # same platform on an existing cluster
$ neocube trust                       # one-time, consented CA install
$ neocube sync ./platform --watch     # fsnotify -> OCI push -> Flux reconciles
$ neocube diff                        # dry-run drift vs cube.lock + inventory
$ neocube upgrade --plan              # resolve pins -> show pack+image diff
$ neocube doctor                      # NEO-coded diagnosis + fix command
$ neocube vendor && neocube up --bundle platform.tar   # air-gapped
$ neocube down                        # inventory-driven cascade + ctx cleanup
```

### Phased roadmap

- **Phase 1 (MVP, ~3-4 weeks):** cobra shell, cube.yaml loader + JSON Schema, kind + existing providers, SSA apply engine with inventory/timeouts/diagnosis, embedded Flux + zot, local-dir and OCI packs, `up/down/status/get secrets`, traefik + argocd + gitea starter packs.
- **Phase 2 (~2-3 weeks):** `neocube trust` + CoreDNS canonical hostname, `diff`, `doctor` preflights with NEO codes, cube.lock + `upgrade --plan`, git-ref pack source, cnoe-compat loader.
- **Phase 3:** k3d provider, `vendor`/`--bundle` air-gap, exec-plugin discovery + index, pack catalog buildout (backstage, cert-manager, external-secrets), `sync --watch`.
- **Phase 4 (post-1.0, evidence-driven):** Talos/vcluster providers if demanded, Extism hooks if Helm 4's system matures, optional in-cluster neocube-operator for self-healing the Flux install itself.

The strategic bet: win on the *deleted-complexity* story (no CRDs, no git server, no daemon, sub-minute up, every failure explained) against ksail's breadth and idpbuilder's CNOE gravity — and keep maintainers holding the "packs are data, providers are PRs" line until real usage demands more.