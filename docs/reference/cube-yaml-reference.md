# cube.yaml — full reference (maxed-out example)

Derived from the authoritative CUE schema (`internal/config/schema.cue`) and
field docs (`internal/config/types.go`) as of the engine-as-pack change
(2026-07-19). Every field, option, and twist is exercised below.

> **This is a "kitchen-sink" document for illustration.** Several combinations
> here are mutually exclusive or unusual in practice — they're flagged inline
> and summarised in the "Twists" table. For the smallest valid config, see
> [Minimal](#minimal-valid-cubeyaml) at the bottom.

## Maxed-out example

```yaml
apiVersion: cube-idp.dev/v1alpha1
kind: Cube
metadata:
  name: maxed-cube                     # ^[a-z0-9][a-z0-9-]{0,30}$

spec:
  # ─────────────────────────────────────────────────────────────
  # CLUSTER — how/where the k8s cluster is provisioned
  # ─────────────────────────────────────────────────────────────
  cluster:
    provider: kind                      # kind (default) | k3d | existing
    context: kind-maxed-cube            # kubeconfig context (esp. for provider: existing)
    kubernetesVersion: v1.33.1          # node image version — REJECTED (CUBE-1003) if provider: existing
    # Extra host→node port mappings beyond the gateway port:
    extraPorts:
      - {hostPort: 15432, nodePort: 30432}   # e.g. expose a NodePort DB locally
      - {hostPort: 19000, nodePort: 30900}
    # Layer-1 registry config for the node's containerd:
    registry:
      mirrors:
        docker.io: https://mirror.gcr.io
        ghcr.io:   https://ghcr.io
      insecure:
        - zot.cube-idp-system.svc.cluster.local:5000
        - my-corp-registry.local:5000
    # Host paths bind-mounted into the node:
    mounts:
      - {hostPath: /Users/me/data,   nodePath: /mnt/data}
      - {hostPath: /Users/me/certs,  nodePath: /mnt/certs}
    # Layer-1 escape hatch: a ref (oci:// , a local dir, or an absolute path)
    # to a provider-native config document — one YAML file, the fetched base.
    providerConfigRef: ./kind-extra.yaml
    # Layer-2 escape hatch: inline provider-native overrides, merge-patched
    # (RFC 7386) on top of the providerConfigRef base.
    # (providerConfigRef and forProvider are both "escape hatches" — usually you
    #  use one, not both; shown together only to exhaust the schema.)
    forProvider:
      nodes:
        - role: control-plane
        - role: worker
        - role: worker

  # ─────────────────────────────────────────────────────────────
  # ENGINE — the GitOps reconciler (engine-as-pack, 2026-07-19)
  # ─────────────────────────────────────────────────────────────
  engine:
    type: argocd                        # flux (default) | argocd
    # Optional engine-pack source override; unset = published default
    #   oci://ghcr.io/cube-idp/packs/cube-engine-argocd:0.1.0
    # (accepts oci:// , a local dir, or an absolute path)
    ref: oci://ghcr.io/cube-idp/packs/cube-engine-argocd:0.1.0
    # OPEN chart values for the engine pack (validated by helm, not CUE).
    # ⚠ argocd ONLY — the flux engine pack is chartless (vendored manifests),
    #    so engine.values with type: flux is CUBE-4016. Under argocd:
    values:
      repoServer:
        replicas: 2
      server:
        insecure: true
      global:
        image:
          imagePullPolicy: IfNotPresent
    # Opt-in engine self-management: after install, the engine
    # reconciles its own install from the cube-engine zot artifact.
    selfManage: true

  # ─────────────────────────────────────────────────────────────
  # GATEWAY — the ingress / Gateway API implementation
  # ─────────────────────────────────────────────────────────────
  gateway:
    pack: traefik                       # traefik (default) | envoy-gateway | any pack name (with ref)
    host: cube-idp.localtest.me         # routable hostname for delivered packs
    port: 8443                          # host port → gateway's HTTPS (websecure) listener
    httpPort: 8080                      # OPT-IN host port → gateway's plain-HTTP listener (NodePort 30080)
    # If pack != the ref'd pack.cue name → CUBE-0008. `init` fills the published
    # default; unset in a hand-written cube.yaml falls back to `packs/<pack>`
    # (a checkout-relative last resort, only resolves from a packs checkout root).
    ref: oci://ghcr.io/cube-idp/packs/traefik:0.2.0

  # ─────────────────────────────────────────────────────────────
  # PREREQUISITES — packs the CLI applies (SSA) BEFORE the engine,
  # in list order (ADR-0045). The bootstrap ground the engine stands
  # on: cluster-scoped CRDs, a policy controller, etc.
  # ─────────────────────────────────────────────────────────────
  # A prerequisite is an ORDINARY pack, but:
  #   • the CLI applies it itself and waits (kstatus), before the engine
  #     and every pack — so its CRDs/namespaces are Established up front;
  #   • it takes NO delivery/dependsOn — never engine-delivered, no place
  #     in the dependency graph. List order is the only ordering contract.
  #   • it appears in `kubectl get packs` (DELIVERY: prerequisite) and
  #     cube.lock like any pack, and `down` removes it via the inventory
  #     cascade.
  # A ref present in BOTH prerequisites and packs is CUBE-0016 (one owner
  # per pack). Only {ref, valuesRef?, values?, extraManifests?} are allowed.
  prerequisites:
    # Canonical first use: the Gateway API CRDs (#25). Listing them here
    # applies + Establishes the CRDs before any HTTPRoute-bearing pack, so
    # the check is validated UP FRONT instead of failing late during
    # deployment — and capability inference then treats
    # gateway.networking.k8s.io as satisfied (no phantom gateway-pack
    # dependency for packs that render HTTPRoutes).
    - ref: oci://ghcr.io/cube-idp/packs/gateway-api-crds:0.1.0
    # A prerequisite may be customized with values, exactly like any pack:
    - ref: oci://ghcr.io/cube-idp/packs/kyverno:0.1.0
      values:
        replicaCount: 1

  # ─────────────────────────────────────────────────────────────
  # PACKS — workloads delivered after the gateway (extensibility tier 1)
  # ─────────────────────────────────────────────────────────────
  packs:
    # A vanilla published pack (no customization):
    - ref: oci://ghcr.io/cube-idp/packs/gitea:0.2.0

    # A chart pack CUSTOMIZED with helm values (→ CUSTOMIZED in `kubectl get packs`):
    - ref: oci://ghcr.io/cube-idp/packs/prometheus-stack:0.1.0
      # Optional fetched values BASE — same ref grammar as `ref` (oci:// ,
      # git subdir, or a local path; NOT a raw https object URL). Resolved by
      # up/diff only; pinned in cube.lock as valuesPin. Fetch/shape/merge
      # failure = CUBE-4021. Inline `values:` below are merge-patched ON TOP
      # (RFC 7386: inline wins, null deletes, arrays replace).
      valuesRef: oci://ghcr.io/acme/values/prometheus-stack:1.2.0
      values:                          # helm values, only, always (chartless pack + values = CUBE-4016)
        grafana:
          adminPassword: changeme
        prometheus:
          prometheusSpec:
            retention: 15d

    # A pack with appended extra manifests (works for ANY pack kind):
    - ref: oci://ghcr.io/cube-idp/packs/cert-manager:0.1.0
      extraManifests: |
        apiVersion: cert-manager.io/v1
        kind: ClusterIssuer
        metadata:
          name: selfsigned
        spec:
          selfSigned: {}

    # A LOCAL-dir pack, REPO-delivered (editable in-cluster fork via Gitea),
    # with explicit dependency ordering:
    - ref: ./packs/my-app                # oci:// | local dir | (git refs: future)
      delivery: repo                     # oci (default) | repo — repo REQUIRES the gitea pack present
      dependsOn:                         # pack NAMES (not refs); unioned with pack.cue dependsOn
        - gitea
        - cert-manager

  # ─────────────────────────────────────────────────────────────
  # SPOKES — managed spoke clusters this cube's engine registers
  # ─────────────────────────────────────────────────────────────
  spokes:
    - name: spoke-dev                   # ^[a-z0-9][a-z0-9-]{0,30}$
      cluster:
        provider: kind                  # kind | existing (k3d spokes deferred)
        kubernetesVersion: v1.33.1
        # spokes reuse ClusterSpec but extraPorts/mounts are disallowed:
        registry:
          mirrors:
            docker.io: https://mirror.gcr.io
        providerConfigRef: ./spoke-extra.yaml
        forProvider:
          nodes:
            - role: control-plane
    - name: spoke-prod
      cluster:
        provider: existing
        context: prod-cluster-context
```

## Twists worth calling out (easy to miss)

| Twist | Rule |
|---|---|
| **`engine.values` + flux** | CUBE-4016 — the flux engine pack is chartless (vendored `flux install --export`); values only work with **argocd**. |
| **No `engine.pack` field** | Unlike `gateway.pack`, the engine pack name is fixed by `type` (`cube-engine-<type>`). Only `ref` overrides the source; `up` verifies the fetched name matches → CUBE-0013. |
| **`kubernetesVersion` + `provider: existing`** | Rejected (CUBE-1003) — you can't set a node-creation field on a cluster you don't create. |
| **`delivery: repo`** | Requires the `gitea` pack in `spec.packs`; gitea itself can never be repo-delivered (CUBE-7304). |
| **`gateway.pack` vs `gateway.ref`** | If both set, the ref decides what's fetched; mismatch with `pack` → CUBE-0008. |
| **`httpPort`** | Opt-in only; must differ from `gateway.port` and every `extraPorts.hostPort` (CUBE-0002). Cluster-shape field — recreate the cluster to change it. |
| **`providerConfigRef` vs `forProvider`** | Two escape-hatch layers (named-ref vs inline). Usually one or the other; both shown only to exhaust the schema. |
| **`values` normalization** | CUE ints round-trip as Go `int`, never `int64` — relevant if you script cube.yaml generation. |
| **`valuesRef` is git/oci-shaped** | A raw `https://…/values.yaml` object URL does NOT work (every `http(s)://` ref is fetched as a directory). Use an OCI artifact, a git `//subdir`, or a local path; a directory-shaped ref must hold exactly one top-level `*.yaml`. Bare-git refs need `@rev` (CUBE-4007). |
| **`valuesRef` + chartless pack** | CUBE-4016, same rule as `values` — a `valuesRef` is helm values, nothing else. |
| **remote `-f`** | The same grammar loads the whole cube (`cube-idp up -f oci://…`). Read-only: `pack install` / `spoke add` / `spoke remove` against a remote ref are CUBE-0014, and a fetch failure is CUBE-0015. `cube.lock` is then written to `./cube.lock` in the working directory. A local file of the same name always wins over a ref. |
| **`engine.tuning`** | **Removed** (CUBE-0012 migration error) — replaced by `engine.ref`/`engine.values`. Don't add it. |
| **Retired but reserved codes** | `CUBE-3003`, `CUBE-3009` are retired-in-place (never emitted) — you won't see them. |

## Defaults (what you get if you omit things)

- `cluster.provider: kind`, `cluster.kubernetesVersion: v1.33.1` (kind only)
- `engine.type: flux` + the published engine ref
  `oci://ghcr.io/cube-idp/packs/cube-engine-flux:0.1.0`
- `gateway`: `pack: traefik`, `host: cube-idp.localtest.me`, `port: 8443`;
  `init` writes ref `oci://ghcr.io/cube-idp/packs/traefik:0.2.0` (an unset ref
  in a hand-written cube.yaml instead falls back to `packs/traefik`)
- `spec.packs`: none injected when omitted; `cube-idp init` scaffolds
  gitea + argocd into the default cube.yaml

## Minimal valid cube.yaml

```yaml
apiVersion: cube-idp.dev/v1alpha1
kind: Cube
metadata:
  name: dev
spec: {}
```

## See also

- `internal/config/schema.cue` — the authoritative schema (`cube-idp config schema` prints it)
- `docs/reference/pack-contract-v1.md` — the pack format contract (v1.1)
- `cube-idp config render-cluster` / `render-engine` — preview the rendered
  provider config and engine install before `up`
