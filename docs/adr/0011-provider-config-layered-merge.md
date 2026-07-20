---
status: "accepted"
date: 2026-07-20
decision-makers: "cube-idp maintainers"
---

# 11. Provider Config Rendering: Four-Layer Merge and Owned Fields

## Context and Problem Statement

cube-idp creates local clusters through provider tools (kind, k3d) that each accept a rich,
fast-moving native configuration document. Users need full access to that native surface —
every field kind ships, not a curated subset — while cube-idp simultaneously needs a handful
of fields to be exactly what it requires: the gateway must be reachable on a known port, the
node image must match the requested Kubernetes version, and containerd must trust the local
registry.

Typing the whole provider surface in `cube.yaml` would mean chasing upstream releases forever
and blocking users on cube-idp support for any new field. Leaving the document fully opaque
would mean cube-idp could not guarantee the invariants its own components depend on. The
decision below defines how a user's provider-native document and cube-idp's required
injections are composed, which side wins on conflict, and which fields cube-idp claims
ownership of.

## Decision

Provider-native cluster configuration is expressed through a two-field ladder:
`providerConfigRef` (a base document fetched by reference) and `forProvider` (structured
inline overrides applied as an RFC 7386 merge patch). These are composed with typed
`cube.yaml` sugar and then with core injections, in a four-layer merge that is strict-decoded
into the upstream provider type inside a pure, unit-testable `RenderConfig` function.

Conflicts resolve by layer. Typed-sugar collisions at layer 3 are a hard error (CUBE-1201).
Core injections at layer 4 win over user-supplied values and emit a warning (CUBE-1206).

cube-idp forces the fields it owns — `kind`/`apiVersion`, `metadata.name`, gateway port
mappings, containerd registry and `certs.d` patches, the node image derived from
`kubernetesVersion`, and control-plane `extraMounts` — and synthesizes a single
control-plane node when the composed document declares none. Every other field remains
user-controlled and untyped by cube-idp.

`config render-cluster` exposes this merge as a pure, cluster-free, file-free operation that
creates no trust material and fails with CUBE-0004 on a provider that creates no cluster.
Combining `provider: existing` with node-creation fields is rejected with CUBE-1003.

## Consequences

* Good, because the entire upstream provider surface stays reachable without cube-idp typing
  it — new kind fields work the day kind ships them.
* Good, because the merge is a pure function over its inputs, so it is unit-testable and
  `render-cluster` can print the exact document without touching Docker, a cluster, or the
  filesystem.
* Good, because cube-idp's invariants (gateway reachability, node image, registry trust) hold
  regardless of what the user's base document says.
* Good, because the CUBE-1206 warning makes silent core overrides visible rather than
  mysterious.
* Bad, because a user who deliberately sets an owned field is overridden rather than obeyed,
  and only learns about it from a warning.
* Bad, because layers 3 and 4 resolve conflicts differently (hard error vs. warn-and-win),
  which is a rule users must learn rather than infer.
* Bad, because the strict decode means a typo anywhere in `forProvider` fails the whole render
  (CUBE-1202) instead of being ignored.

## Implementation Status

**This decision is implemented.** Confirmed against the code on 2026-07-20.

| Decision | Implemented at |
| --- | --- |
| Render kind config through a four-layer merge — fetched `providerConfigRef`, RFC 7386 `forProvider`, typed sugar, then core injections — composed and strict-decoded in a pure `RenderConfig`. | `internal/cluster/kindp/merge.go:59-178` |
| Layer 3 typed-sugar conflicts are a hard error (CUBE-1201); layer 4 core injections warn (CUBE-1206) and win over user-supplied values. | `internal/cluster/kindp/merge.go:61-64,143-158` |
| Emit kind configs pinned to `kind: Cluster` / `apiVersion: kind.x-k8s.io/v1alpha4`, forcing both fields rather than accepting user overrides. | `internal/cluster/kindp/merge.go:85-86` |
| Force `metadata.name` from the `cube.yaml` cluster name, and synthesize a single control-plane node when the composed document declares none. | `internal/cluster/kindp/merge.go:87-90` |
| Pin the gateway `extraPortMapping` containerPort to `config.GatewayNodePort` (30443), since a kind port mapping only reaches a NodePort Service when containerPort equals the Service nodePort. Applies when `spec.gateway.port` is set — a zero gateway port (spoke clusters) injects no mapping at all. | `internal/cluster/kindp/merge.go:36,107-120` |
| Mirror the same pinning for the opt-in plain-HTTP listener: host `spec.gateway.httpPort` maps to `config.GatewayHTTPNodePort` (30080), and is skipped entirely when unset. | `internal/cluster/kindp/merge.go:125-139`, `internal/config/types.go:151,163` |
| Derive the node image from `kubernetesVersion` as `kindest/node:<version>`; core wins with a CUBE-1206 warning if the composed config set a different image. | `internal/cluster/kindp/merge.go:97-101` |
| Write registry mirror and insecure-registry settings via `containerdConfigPatches`, and bind the `certs.d` directory (hosts.toml/ca.crt) through `CertsD`. | `internal/cluster/kindp/merge.go:166`, `merge.go:169-174`, `merge.go:205-216` |
| The composed document is decoded into the complete upstream `v1alpha4.Cluster`, and only the listed owned fields are reassigned — every other kind field stays user-controlled through layer 2 `forProvider` and is not typed by cube-idp. | `internal/cluster/kindp/merge.go:69-84` |
| `config render-cluster` is pure, cluster-free and file-free (zero `CertsD` / zero `ZotMirror`), creates no trust material, and fails with CUBE-0004 on a provider that creates no cluster. | `cmd/config.go:46,48` (zero values), `cmd/config.go:50-52` (CUBE-0004) |
| Combining `provider: existing` with node-creation fields is rejected with CUBE-1003 (`CodeClusterFieldsConflict`, config cross-validation). | `internal/config/load.go:188` |

### Verification

- [ ] `internal/cluster/kindp/merge.go` exposes `RenderConfig(ctx, name, spec, gw, certsd) ([]byte, []diag.Finding, error)` and performs no Docker or cluster I/O — the only side effect is the `providerConfigRef` fetch.
- [ ] `internal/cluster/kindp/merge.go:69` calls `compose.Compose(ctx, spec.ProviderConfigRef, spec.ForProvider, cacheDir)` (layers 1–2, RFC 7386 per `internal/cluster/compose/compose.go`).
- [ ] `internal/cluster/kindp/merge.go:79` uses `sigyaml.UnmarshalStrict` into `v1alpha4.Cluster`, failing with `diag.CodeKindConfigInvalid` (CUBE-1202) on an unknown field.
- [ ] `internal/cluster/kindp/merge.go:85-86` sets `cfg.Kind = "Cluster"` and `cfg.APIVersion = "kind.x-k8s.io/v1alpha4"` after the decode, with no CUBE-1206 warning.
- [ ] `internal/cluster/kindp/merge.go:36` declares `gatewayContainerPort = config.GatewayNodePort`, and `internal/config/types.go:142` pins `GatewayNodePort = 30443`; `merge_test.go` asserts containerPort 30443.
- [ ] `internal/cluster/kindp/merge.go:107` guards the whole gateway injection on `gw.Port > 0`, and `merge.go:125-139` injects the opt-in HTTP mapping only when `gw.HTTPPort > 0` (`internal/config/types.go:163`), pinned to `config.GatewayHTTPNodePort = 30080` (`internal/config/types.go:151`).
- [ ] `internal/cluster/kindp/merge.go:87-90` sets `cfg.Name = name` and defaults `cfg.Nodes` to a single control-plane node.
- [ ] `internal/cluster/kindp/merge.go:97-101` computes `"kindest/node:" + spec.KubernetesVersion` and warns before overwriting a differing `cp.Image`.
- [ ] `internal/cluster/kindp/merge.go:143-158` returns `diag.New(diag.CodeKindConfigMerge, ...)` (CUBE-1201) on every layer-3 `extraPorts` collision, while `merge.go:61-64` builds a `diag.SeverityWarning` finding with `diag.CodeKindCoreOverride` (CUBE-1206).
- [ ] `internal/cluster/kindp/merge.go:205-216` emits containerd mirror and `insecure_skip_verify` patches; `merge.go:166` appends them, and `merge.go:169-174` adds the `config_path = "/etc/containerd/certs.d"` patch plus the `CertsD` extraMount.
- [ ] `cmd/config.go:44-53` passes zero `kindp.CertsD{}` / `k3dp.ZotMirror{}` and returns `diag.CodeProviderMiss` (CUBE-0004) for any other provider; CUBE-1002 is absent from `internal/diag/codes.go`.
- [ ] `internal/config/load.go:185-192` rejects `provider: existing` combined with `extraPorts`/`mounts`/`providerConfigRef`/`forProvider`/`kubernetesVersion` via `diag.CodeClusterFieldsConflict` (CUBE-1003, defined at `internal/diag/codes.go:33`).

## History

An earlier design used a two-layer model: typed `spec.cluster` fields (extra port mappings,
registry mirrors, node image/version, host mounts) plus a single opaque
`spec.cluster.providerConfig` escape hatch accepting either an inline document or a file path,
where any collision between the two layers was an explicit conflict error.

That was replaced by the `providerConfigRef` + `forProvider` ladder described above. The file
path became `providerConfigRef: <path>`; the inline blob became structured fields under
`forProvider:`. Collisions with core injections are no longer errors at all — core wins and
emits a CUBE-1206 (kind) or CUBE-1306 (k3d) warning. Using the old `providerConfig` key is now
a hard migration error, CUBE-0011: `internal/config/load.go:57-83` probes for it before CUE
parsing so the user gets the migration recipe rather than a generic schema rejection.

## More Information

Origin: mined from the archived planning corpus (`docs/archive/superpowers/`) during the
2026-07-20 documentation audit; the underlying statements were validated against the code
before this record was written.

Member provenance:

- `docs/archive/superpowers/specs/2026-07-18-kind-config-reference.md:103` — four-layer merge in `RenderConfig`
- `docs/archive/superpowers/specs/2026-07-18-kind-config-reference.md:111` — layer 3 errors vs. layer 4 warnings
- `docs/archive/superpowers/specs/2026-07-18-kind-config-reference.md:117` — everything else stays user-controlled
- `docs/archive/superpowers/plans/2026-07-13-cube-idp-phase3-draft.md:322` — `render-cluster` purity and CUBE-0004
- `docs/archive/superpowers/plans/2026-07-15-cube-idp-phase4-first-release.md:39` — `provider: existing` cross-validation (CUBE-1003)
