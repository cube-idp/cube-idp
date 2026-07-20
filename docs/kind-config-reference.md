# kind cluster config (v1alpha4) field reference

Date: 2026-07-18
Status: REFERENCE (research, no decisions)
Prior art: `2026-07-13-cube-idp-architecture-design.md` (§4.1 ClusterProvider,
D10 two-layer merge), `internal/cluster/kindp/merge.go` (the code this
reference informs).
Sources: kind `main` as of 2026-07-18 — `pkg/apis/config/v1alpha4/types.go`,
`pkg/apis/config/v1alpha4/default.go`, `pkg/internal/apis/config/validate.go`,
<https://kind.sigs.k8s.io/docs/user/configuration/>. Current release:
**kind v0.32.0** (2026-06-02), default node image
`kindest/node:v1.36.1@sha256:3489c7674813ba5d8b1a9977baea8a6e553784dab7b84759d1014dbd78f7ebd5`.

## 1. Purpose

Complete field-level reference for `kind: Cluster` /
`apiVersion: kind.x-k8s.io/v1alpha4` so cube-idp can customize kind cluster
creation deliberately: what exists, what defaults, what validates, and what
only fails at bootstrap. §6 maps the surface onto the kindp D10 merge.

`v1alpha4` is the only config version kind ships; the schema is stable but
behavior details (e.g. `nftables` proxy mode, containerd config version)
shift between kind releases. CLI flags (`--name`, `--image`,
`KIND_CLUSTER_NAME`) always override the file.

## 2. Top-level `Cluster` fields

| Field | Type | Behavior |
| --- | --- | --- |
| `name` | string | Cluster name; must match `^[a-z0-9.-]+$`. Overridden by `--name` / `KIND_CLUSTER_NAME`. |
| `nodes` | `[]Node` | Defaults to one `control-plane` node. **≥2 control-plane nodes implicitly provisions an external HAProxy load-balancer container.** Validation requires ≥1 control-plane. |
| `networking` | `Networking` | See §4. |
| `featureGates` | `map[string]bool` | Passed to **all** K8s components as flags/config. Upstream: "not all feature gates are tested". |
| `runtimeConfig` | `map[string]string` | Joined comma-separated into kube-apiserver `--runtime-config`; the knob for alpha/deprecated APIs (e.g. `"api/alpha": "false"`). |
| `kubeadmConfigPatches` | `[]string` | Inline YAML blob-strings, RFC 7386 merge patches against the generated kubeadm config. Patch `kind` must match target object (`InitConfiguration`, `ClusterConfiguration`, `JoinConfiguration`, `KubeProxyConfiguration`, `KubeletConfiguration`); optional `apiVersion` narrows further. Cluster-level applies **before** node-level. |
| `kubeadmConfigPatchesJSON6902` | `[]PatchJSON6902` | RFC 6902 variant; target selected by `group`/`version`/`kind`. `name`/`namespace` fields parse but are documented no-ops. |
| `containerdConfigPatches` | `[]string` | TOML merge patches applied to **every** node's containerd config, in order. **Version-aware:** a patch whose `version` field mismatches the node's containerd config is silently skipped — ship a v2 and a v3 patch to cover node-image generations. Canonical hook for registry mirrors (`config_path = "/etc/containerd/certs.d"`). |
| `containerdConfigPatchesJSON6902` | `[]string` | RFC 6902 for containerd config; **not** version-aware. |

## 3. `Node` fields

| Field | Type | Behavior |
| --- | --- | --- |
| `role` | string | `control-plane` (default) or `worker`; nothing else validates. Single-node clusters get the control-plane taint removed. |
| `image` | string | Node image = the Kubernetes version knob, per node (mixed versions allowed, e.g. upgrade tests). Upstream requires the `@sha256:` digest for reproducibility — matches our Phase 5 digest-pinned-e2e decision. |
| `labels` | `map[string]string` | Applied as K8s node labels (drives `nodeSelector` scenarios). |
| `extraMounts` | `[]Mount` | Host bind mounts into the node container (§3.1). |
| `extraPortMappings` | `[]PortMapping` | Host→node port forwards (§3.2). |
| `kubeadmConfigPatches`, `kubeadmConfigPatchesJSON6902` | as cluster-level | Node-scoped; applied **after** cluster-level. Gotcha: kubeadm reads `KubeletConfiguration` only from the **first** node and applies it to all — per-node kubelet/taint tweaks must go through `JoinConfiguration`/`InitConfiguration` `nodeRegistration` instead. |

### 3.1 `Mount`

`containerPath`, `hostPath`, `readOnly` (bool), `selinuxRelabel` (bool),
`propagation` ∈ `None` (private, default) | `HostToContainer` (rslave) |
`Bidirectional` (rshared). Caveats: upstream says you very likely do not
need `propagation`; it cannot be used on macOS Docker Desktop for mounts
originating from the Mac filesystem; Docker Desktop requires `hostPath`
inside the File Sharing allowlist.

### 3.2 `PortMapping`

`containerPort`, `hostPort`, `listenAddress` (default `0.0.0.0`),
`protocol` ∈ `TCP` (default) | `UDP` | `SCTP`.

- `hostPort` unset/`0` → kind picks a random host port. `-1` → the container
  backend (docker/podman) picks; **re-randomizes on container restart**.
- Duplicate (listenAddress, hostPort, protocol) tuples are rejected;
  `0.0.0.0` and `::` are treated as the same wildcard, and a wildcard
  conflicts with any specific address on the same port/protocol.
- To reach a NodePort Service through a mapping, `containerPort` **must
  equal** the Service `nodePort` — exactly why kindp pins
  `gatewayContainerPort` to `config.GatewayNodePort` (30443).
- On bare-Linux Docker, node IPs are host-routable and mappings are often
  unnecessary; on Docker Desktop (macOS) they are the only path in.

## 4. `Networking` fields

| Field | Default | Behavior |
| --- | --- | --- |
| `ipFamily` | `ipv4` | `ipv4` \| `ipv6` \| `dual`. IPv6 needs host IPv6 enabled; on Mac/Windows the API server still needs an IPv4 forward. |
| `apiServerAddress` | `127.0.0.1` (`::1` ipv6) | Host listen address. Upstream strongly recommends loopback — kind has no hardening or update story. |
| `apiServerPort` | random | `-1` delegates to docker/podman (re-randomizes on restart). Range −1…65535. Leave unset for parallel clusters on one host. |
| `podSubnet` | `10.244.0.0/16` / `fd00:10:244::/56` (v6) / both comma-joined (dual) | CIDR(s); exactly one for single-stack, one per family for dual. |
| `serviceSubnet` | `10.96.0.0/16` / `fd00:10:96::/112` (v6) / both (dual) | Deliberately /16 (not kubeadm's /12) to keep etcd bitmaps small. Same family rules. |
| `disableDefaultCNI` | `false` | Skips kindnetd for BYO CNI (Calico/Cilium). Works, but "power user feature with limited support". |
| `kubeProxyMode` | `iptables` | `iptables` \| `ipvs` \| `nftables` (K8s ≥1.31) \| `none`. **`none` is absent from the public v1alpha4 constants but accepted by the validator and documented** — the kube-proxy-replacement (Cilium) path. |
| `dnsSearch` | inherit host | `*[]string`; set `[]` to explicitly clear search domains rather than inherit. |

## 5. Validation vs. bootstrap failure

Validated up front by `kind create cluster`: name regex; ports −1…65535;
`ipFamily`/`kubeProxyMode`/`role` enums; subnet CIDR parsing + single/dual
count rules; duplicate port mappings; ≥1 control-plane node.

**Not validated:** the *content* of kubeadm/containerd patches. Bad patches
surface as `kubeadm init` / containerd startup failures mid-provision, not
as config errors. Anything cube-idp injects into patches should be
pre-validated on our side (or covered by e2e) because kind will not catch it
early.

## 6. Mapping to cube-idp (kindp D10 merge)

Layer 1 (`providerConfigRef`, fetched) → layer 2 (`forProvider`, RFC 7386) →
layer 3 typed sugar (hard error on conflict) → layer 4 core injections
(warn-and-win, CUBE-1206). See `2026-07-18-cluster-forprovider-design.md`.
Composed and strict-decoded in `internal/cluster/kindp/merge.go`
(`RenderConfig`, pure/unit-testable). What we currently occupy:

| kind field | cube-idp writer | Notes |
| --- | --- | --- |
| `nodes[control-plane].extraPortMappings` | gateway `hostPort` → containerPort 30443 (`config.GatewayNodePort`); typed `extraPorts` | CUBE-1206 warning, core wins if providerConfigRef/forProvider maps gw.Port to a different containerPort. NodePort-equality rule (§3.2) is the constraint. Typed `extraPorts` collisions stay CUBE-1201 (layer 3, hard error). |
| `containerdConfigPatches` | registry mirrors/insecure; certs.d bind via `CertsD` (hosts.toml/ca.crt, D6) | Version-aware skip (§2) matters if we ever emit `version`-tagged TOML. |
| `nodes[*].image` | derived from `kubernetesVersion` (`kindest/node:<version>`) | CUBE-1206 warning, core wins if providerConfigRef/forProvider sets a different image. Digest pinning (Phase 5) belongs here. |
| `nodes[control-plane].extraMounts` | typed mounts | — |
| `kind`/`apiVersion` | forced `Cluster` / `kind.x-k8s.io/v1alpha4` | — |

Everything else in §§2–4 is user territory via layer 2 today. Fields worth
awareness when extending the typed layer: `networking.apiServerPort`
(parallel-cluster collisions — cf. local e2e port conflicts,
`CUBE_IDP_E2E_GATEWAY_PORT=18443`), `disableDefaultCNI` + `kubeProxyMode:
none` (BYO-CNI packs), `featureGates`/`runtimeConfig` (per-pack alpha API
needs), multi-node `nodes` lists (implicit HAProxy LB with ≥2
control-planes), and the first-node-only `KubeletConfiguration` gotcha if we
ever generate per-node kubelet patches.

## 7. Annotated full example

```yaml
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
name: cube-e2e                      # ^[a-z0-9.-]+$; --name wins
featureGates:
  MyFeature: true                   # all components
runtimeConfig:
  "api/beta": "true"                # kube-apiserver --runtime-config
networking:
  ipFamily: ipv4                    # ipv4|ipv6|dual
  apiServerAddress: 127.0.0.1       # keep loopback (upstream security stance)
  # apiServerPort: unset -> random; best for parallel clusters
  podSubnet: 10.244.0.0/16
  serviceSubnet: 10.96.0.0/16
  disableDefaultCNI: false          # true = BYO CNI
  kubeProxyMode: iptables           # iptables|ipvs|nftables|none
kubeadmConfigPatches:               # RFC 7386, cluster-level first
  - |
    kind: ClusterConfiguration
    apiServer:
      extraArgs:
        v: "2"
containerdConfigPatches:            # TOML merge, every node, version-aware
  - |-
    [plugins."io.containerd.grpc.v1.cri".registry]
      config_path = "/etc/containerd/certs.d"
nodes:
  - role: control-plane
    image: kindest/node:v1.36.1@sha256:3489c7674813ba5d8b1a9977baea8a6e553784dab7b84759d1014dbd78f7ebd5
    labels: { tier: system }
    extraPortMappings:
      - containerPort: 30443        # MUST equal the Service nodePort
        hostPort: 18443             # unset/0 = kind-random, -1 = backend-random
        listenAddress: 127.0.0.1    # default 0.0.0.0
        protocol: TCP               # TCP|UDP|SCTP
    extraMounts:
      - hostPath: ./e2e/fixtures    # Docker Desktop: must be file-shared
        containerPath: /fixtures
        readOnly: true
        # propagation: None|HostToContainer|Bidirectional — rarely needed
  - role: worker
    kubeadmConfigPatches:
      - |
        kind: JoinConfiguration     # per-node kubelet/taints go here,
        nodeRegistration:           # NOT KubeletConfiguration (first-node-only)
          taints: [{ key: dedicated, value: gateway, effect: NoSchedule }]
```
