---
status: "accepted"
date: 2026-07-20
decision-makers: "cube-idp maintainers"
---

# 12. Canonical Gateway Hostname, Host Port, and NodePort Mapping

## Context and Problem Statement

cube-idp provisions a local Kubernetes cluster (kind or k3d) and exposes the platform's
services through a single gateway pack. Two problems follow from that shape.

First, addresses. A service reached from the developer's laptop and the same service
reached from a pod inside the cluster would otherwise need different URLs — split-horizon
addressing that leaks into every pack's config, every generated link, and every test.

Second, ports. A container port mapping published by kind or k3d only reaches a NodePort
Service when the container port equals that Service's `nodePort`. If the gateway pack pins
one number and the provider maps another, the gateway is simply unreachable from the host,
with no error at cluster-creation time. Both cluster-creating providers and both gateway
packs (traefik, envoy-gateway) therefore have to agree on the same constants, and users
need a way to add a plain-HTTP host port without that becoming a second source of drift.

## Decision

A single canonical hostname resolves to the gateway identically inside and outside the
cluster. CoreDNS is rewritten so `*.<gateway.host>` (default `*.cube-idp.localtest.me`)
resolves to the gateway Service. The rewrite is idempotent, appears exactly once, replaces
its own block when the host changes, and is fully removable, restoring the original
Corefile. The rewrite target is whatever `up` resolves for the gateway pack's data-plane
Service; see ADR-0006 for the authoritative statement of how that target is derived from
the optional `pack.cue` `gatewayService: {name, namespace}` block and its
`<pack>.<pack>.svc` fallback.

Every cluster-creating provider maps the host gateway port (default 8443) onto the single
shared node port `cluster.GatewayNodePort` = 30443, matching the gateway pack's websecure
HTTPS NodePort — whenever the cluster has a gateway. A zero `gateway.port` means "no
gateway on this cluster" (spoke clusters render with a zero `GatewaySpec`) and injects no
mapping at all. TLS is terminated at the gateway with a cube-idp CA-issued certificate.

A host-exposed non-TLS HTTP port is opt-in via `spec.gateway.httpPort`. When set, both
cluster-creating providers emit a second mapping onto `GatewayHTTPNodePort` = 30080. Both
first-party gateway packs pin 30080 in the separate packs repo, so no pack change is
required; cube-idp asserts that cross-repo contract only through `tests/packs_render_test.go`.
When absent, no HTTP
mapping is emitted. `gateway.httpPort` must differ from `gateway.port` and from every
`extraPorts.hostPort`; collisions are rejected at config load as a CUBE-0002-family error.
Like `gateway.port`, it is a cluster-shape field subject to the recreate caveat.

The e2e harness binds the gateway to a host port configurable via
`CUBE_IDP_E2E_GATEWAY_PORT`, an exclusive resource shared with docker.

## Consequences

* Good, because one hostname works from the laptop and from inside a pod, so generated
 URLs, pack values, and tests never branch on where they run.
* Good, because the NodePort numbers are single constants in `internal/config`, aliased
 rather than redefined by providers, so a provider cannot silently disagree with a pack.
* Good, because the plain-HTTP port is off by default: an absent `httpPort` produces
 byte-identical cluster config to before the field existed.
* Good, because port collisions surface at config load with an actionable diagnostic
 rather than as an unreachable gateway after a slow cluster creation.
* Bad, because `gateway.port` and `gateway.httpPort` are baked in at cluster creation —
 changing either requires recreating the cluster.
* Bad, because 30443 and 30080 are hard-coded contract numbers a third-party gateway pack
 must honour; a pack pinning anything else is unreachable.
* Bad, because `up` mutates the cluster's CoreDNS ConfigMap, which is shared state a user
 may also be editing.
* Bad, because the gateway host port is an exclusive host resource, so only one live
 cluster leg can run at a time.

## Implementation Status

**This decision is implemented.** Confirmed against the code on 2026-07-20.

| Decision | Implemented at |
| --- | --- |
| Every cluster-creating provider maps the host gateway port onto the shared node port `cluster.GatewayNodePort` = 30443, matching the gateway pack's websecure NodePort — but only when `gateway.port > 0`; a zero port (spoke clusters) injects no mapping at all. | `internal/config/types.go`; `internal/cluster/provider.go`; `internal/cluster/kindp/merge.go`; `internal/cluster/k3dp/merge.go` |
| `gateway.httpPort` must differ from `gateway.port` and from every `extraPorts.hostPort`; a collision is rejected at config load with a CUBE-0002-family error. | `internal/config/load.go` |
| The e2e harness binds the gateway to a host port configurable via `CUBE_IDP_E2E_GATEWAY_PORT`, an exclusive resource shared with docker. | `tests/e2e/e2e_test.go` |
| When `spec.gateway.httpPort` is set, a second mapping onto `GatewayHTTPNodePort` (30080) is emitted by both cluster-creating providers; the field is cluster-shape and subject to the recreate caveat. | `internal/config/types.go`; `internal/cluster/kindp/merge.go`; `internal/cluster/k3dp/merge.go` |
| The gateway host port defaults to 8443 on host `cube-idp.localtest.me` and maps to NodePort 30443, with TLS terminated at the gateway using a cube-idp CA-issued certificate. | `internal/config/types.go`; `internal/cluster/kindp/merge.go`; `internal/up/tls.go` |
| CoreDNS is rewritten so `*.<gateway.host>` resolves to the gateway Service; the rewrite is idempotent, replaces its own block on host change, and is fully removable. | `internal/trust/coredns.go` |
| The CoreDNS rewrite target is the gateway pack's resolved data-plane Service FQDN. (Derivation from the optional `pack.cue` `gatewayService:` block and its `<pack>.<pack>.svc` fallback is owned by ADR-0006.) | `internal/up/up.go` |
| The non-TLS HTTP port is opt-in and never enabled by default (`Default()` leaves `HTTPPort` zero); both first-party gateway packs pin 30080 in the separate packs repo, a cross-repo contract cube-idp asserts only in test. | `internal/config/types.go`; `tests/packs_render_test.go` |

### Verification

- [ ] `internal/config/types.go` defines `const GatewayNodePort = 30443` and `const GatewayHTTPNodePort = 30080`.
- [ ] `internal/cluster/provider.go` aliases `config.GatewayNodePort` rather than redefining the number, and `internal/cluster/kindp/merge.go` aliases it as `gatewayContainerPort`.
- [ ] `internal/config/types.go` `Default()` sets gateway `Host: "cube-idp.localtest.me"` and `Port: 8443`, and does **not** set `HTTPPort`.
- [ ] `internal/config/load.go` returns `diag.CodeConfigInvalid` (CUBE-0002) when `gateway.httpPort` equals `gateway.port` or any `spec.cluster.extraPorts` hostPort.
- [ ] `internal/cluster/kindp/merge.go` and `internal/cluster/k3dp/merge.go` each emit an HTTP mapping only when `gw.HTTPPort > 0`, always onto `config.GatewayHTTPNodePort`.
- [ ] `internal/config/schema.cue` declares `httpPort?: int & >0 & <65536` as optional.
- [ ] `internal/trust/coredns.go` fences its block with `cube-idp:rewrite:begin`/`:end`, calls `removeManagedBlock` before inserting, and exposes `RemoveCoreDNSRewrite`.
- [ ] `internal/cluster/kindp/merge.go` and `internal/cluster/k3dp/merge.go` each emit the HTTPS gateway mapping only when `gw.Port > 0`.
- [ ] `internal/up/tls.go` issues a CA cert for `gw.Host` and `*.<gw.Host>` into the `cube-idp-gateway-tls` Secret.
- [ ] `tests/e2e/e2e_test.go` `gatewayPort` falls back to `8443` when `CUBE_IDP_E2E_GATEWAY_PORT` is unset.

## History

The e2e harness originally defaulted `CUBE_IDP_E2E_GATEWAY_PORT` to 18443. That default is
now 8443 — cube-idp's own default, so CI exercises the same port mapping users get. 18443
survives only as a documented local override for hosts that already have 8443 bound.

Two claims carried in the source material were not confirmed against this repository and
are therefore not asserted above: the specific in-cluster plain-HTTP `web` listener port
(8000), and any enforcement of the "only one live cluster leg at a time" exclusivity — no
serialization or lock exists in the harness; exclusivity is a property of the host port,
not of enforced code.

## More Information

Origin: mined from the archived planning corpus (`docs/archive/superpowers/`) during the
2026-07-20 documentation audit; the underlying statements were validated against the code
before this record was written.

- `docs/archive/superpowers/plans/2026-07-13-cube-idp-phase2-draft.md:3081` — gateway host port, NodePort 30443, TLS termination
- `docs/archive/superpowers/plans/2026-07-13-cube-idp-phase2-draft.md:3445` — canonical hostname and CoreDNS rewrite
- `docs/archive/superpowers/plans/2026-07-18-cube-idp-phase5.md:199` — shared NodePort mapping across providers
- `docs/archive/superpowers/plans/2026-07-18-cube-idp-phase5.md:1711` — `gateway.httpPort` collision validation
- `docs/archive/superpowers/plans/2026-07-15-cube-idp-phase4-first-release.md:1225` — optional `pack.cue` `gatewayService` field
