---
status: "accepted"
date: 2026-07-20
decision-makers: "cube-idp maintainers"
---

# 15. In-Cluster zot Registry and Artifact Transport

## Context and Problem Statement

cube-idp provisions a local Kubernetes environment and delivers packs, engines and
bundles as OCI artifacts. That requires a registry that lives inside the cluster (so
in-cluster engines can pull without leaving the cluster network) and a way for the
*host* CLI to push into that same registry across provider boundaries (kind, k3d,
remote clusters) without assuming a LoadBalancer or provider-specific registry
exposure.

Two further problems follow from having a registry at all. First, plain-HTTP transport:
the in-cluster registry is not TLS-terminated, so some code path must decide when
insecure transport is acceptable — and if that decision is duplicated, the copies drift
and one of them ends up permitting plain HTTP against a real remote registry. Second,
pack refs are not all OCI: they may be local directories, git repositories, HTTP
archives or object-store URLs, so ref resolution needs a general fetcher whose
archive-extraction behaviour cube-idp can still constrain.

## Decision

The in-cluster OCI registry is **project-zot/zot**, reachable by engines at the fixed
address `zot.cube-idp-system.svc.cluster.local:5000`. Its manifests are embedded in the
binary, and every zot object except the Namespace itself lives in `cube-idp-system`,
comprising a Namespace, a Deployment and a Service of `type: NodePort` with
`nodePort: 30500` targeting 5000 — plus a gateway HTTPRoute built in Go at apply time
(`internal/registry/route.go:22`), not embedded. Gitea is used for git repository
delivery only.

**Host CLI push uses a port-forward to the zot Service on every provider.** The forward
binds an ephemeral free local port to zot's port 5000 and returns a `127.0.0.1` address
plus a stop function whose lifetime the caller owns. That port-forward is not the only
access path: the NodePort 30500 Service backs node-side image pulls (k3d containerd
mirror, kind certs.d) and the `cube-idp-registry` HTTPRoute publishes zot at
`registry.<host>` through the gateway for host docker/oras over TLS.

Artifact push uses **oras-go v2**. Plain-HTTP/insecure OCI transport is permitted only
for loopback registry hosts, decided by a **single exported `IsLocalRegistryHost`** in
`internal/pack` that every other package calls rather than redefining.

Pack ref resolution is delegated to **go-getter**, consumed as the fork
`github.com/cube-idp/go-getter v1.9.0` kept on the upstream import path
`github.com/hashicorp/go-getter` via a go.mod `replace` directive, with cube-idp's own
archive-extraction guards in front of it.
A ref carrying an unsupported URI scheme is rejected with `CodePackRefInvalid`
(CUBE-4001) rather than being silently treated as a local path.

## Consequences

* Good, because a fixed in-cluster DNS address means engine manifests need no
  templating or provider-specific registry configuration.
* Good, because port-forwarding works identically on every provider, so the host push
  path needs no bespoke per-provider registry-exposure story.
* Good, because one `IsLocalRegistryHost` definition makes the insecure-transport
  policy auditable in a single place.
* Good, because keeping the go-getter fork on the upstream import path means call
  sites and imports are unchanged if the fork is ever retired.
* Bad, because a port-forward is a live process: the caller must own its lifetime
  (a `defer stop()`, or an explicit stop as at `internal/up/up.go:495`), and a missed
  stop leaks a goroutine and a local port.
* Bad, because the `replace` directive is invisible to `go get` — a contributor running
  `go get -u` can silently drop back to upstream go-getter.
* Bad, because embedding manifests in the binary means upgrading zot requires shipping
  a new cube-idp release.

## Implementation Status

**This decision is implemented.** Confirmed against the code on 2026-07-20.

| Decision | Implemented at |
| --- | --- |
| The in-cluster OCI registry is zot at the fixed address `zot.cube-idp-system.svc.cluster.local:5000`, with manifests embedded in the binary and every object except the Namespace in `cube-idp-system`. | `internal/registry/zot.go:17`, `internal/registry/zot.go:19`, `internal/registry/manifests/zot.yaml` |
| The registry image is `ghcr.io/project-zot/zot:v2.1.2`. | `internal/registry/manifests/zot.yaml:24` |
| oras-go v2 is used for artifact push. | `go.mod:26` |
| A gateway HTTPRoute `cube-idp-registry` publishing zot at `registry.<host>` is built in Go at apply time, not embedded. | `internal/registry/route.go:22-34` |
| Host CLI push is a port-forward to the zot Service on every provider, binding an ephemeral local port to port 5000 and returning a `127.0.0.1` address plus a caller-owned stop function. | `internal/registry/portforward.go:21-28`, `internal/kube/portforward.go:78` |
| Node-side pulls go through the NodePort 30500 Service rather than the port-forward. | `internal/registry/route.go:9`, `internal/cluster/k3dp/merge.go:241`, `internal/cluster/kindp/kind.go:132` |
| Plain-HTTP/insecure OCI transport is acceptable only for loopback registry hosts, decided by a single exported `IsLocalRegistryHost` in `internal/pack`. | `internal/pack/source.go:164-173` |
| Pack ref resolution is delegated to go-getter, consumed as the fork `github.com/cube-idp/go-getter v1.9.0` on the upstream import path via a `replace` directive, with cube-idp's archive-extraction guards in front. | `go.mod:314`, `internal/pack/getter.go:10`, `internal/pack/getter.go:137` |
| A ref containing an unsupported URI scheme is rejected with `CodePackRefInvalid` rather than being treated as a local path. | `internal/pack/source.go:65-67` |

### Verification

- [ ] `internal/registry/zot.go:17` declares `const InClusterURL = "zot.cube-idp-system.svc.cluster.local:5000"` and line 19 embeds `manifests/zot.yaml` via `//go:embed`.
- [ ] `internal/registry/manifests/zot.yaml` contains exactly a Namespace `cube-idp-system`, a Deployment `zot` (image `ghcr.io/project-zot/zot:v2.1.2`, line 24) and a Service `zot` of `type: NodePort` with `nodePort: 30500` targeting 5000 (lines 39-41), the latter two both `namespace: cube-idp-system`.
- [ ] `internal/registry/route.go:22` `GatewayRoute()` returns an unstructured HTTPRoute named `cube-idp-registry` in `cube-idp-system` with hostname `registry.<host>` and backendRef `zot:5000`; `internal/registry/route.go:9` declares `const NodePort = 30500`.
- [ ] `internal/registry/portforward.go:21-28` forwards to selector `app=zot` on remote port 5000 in `apply.SystemNamespace` and wraps failures as `CodePortForwardFail` (CUBE-5002).
- [ ] `internal/kube/portforward.go:78` returns a `127.0.0.1:<ephemeral>` address plus a stop closure; `internal/up/up.go:287-291` calls it and `defer stop()`s, and `internal/up/up.go:485` opens a second, explicitly-stopped tunnel (`selfStop()` at `internal/up/up.go:495`) for the engine self-push.
- [ ] `grep -rn IsLocalRegistryHost --include='*.go' .` shows exactly one definition (`internal/pack/source.go:167`) and only call sites elsewhere (`internal/pack/resolve.go`, `internal/oci/pull.go`, `internal/oci/pushdir.go`, `internal/bundle/vendor.go`).
- [ ] `go.mod:314` is exactly `replace github.com/hashicorp/go-getter => github.com/cube-idp/go-getter v1.9.0`, and `go.mod` requires only the upstream path.
- [ ] `internal/pack/getter.go:137` calls `GuardTree` on the fetched tree before it lands at the destination.
- [ ] `internal/pack/source.go:65-67` has a `case strings.Contains(ref, "://")` arm placed *before* the local-path default, returning `diag.CodePackRefInvalid` (CUBE-4001); `internal/pack/fetchfile.go:40-42` mirrors it for value refs.

## History

The insecure-transport rule was originally stated more broadly: `IsLocalRegistryHost`
was to recognize the IPv6 loopback literal `[::1]` in addition to `127.0.0.1` and
`localhost`, and pack pulls were to use anonymous authentication only.

The single-definition half of that rule survives unchanged. The other two clauses do
not. The shipped implementation splits at the first `:` and compares only against
`127.0.0.1` and `localhost`, so `[::1]` is not recognized (and `[::1]:5000` truncates to
`[`). Pulls are also no longer anonymous: `internal/pack/source.go:120-126` sets
`repo.Client` from `RegistryClient()` — the docker credential store — before the
`PlainHTTP` flip.

One clause of the original registry decision was never implemented: `fluxcd/pkg/oci` was
named alongside oras-go for artifact push, but it is absent from `go.mod`; push runs on
oras-go alone.

## More Information

Origin: mined from the archived planning corpus (`docs/archive/superpowers/`) during the
2026-07-20 documentation audit; the underlying statements were validated against the
code before this record was written.

Member provenance:

- `docs/archive/superpowers/plans/2026-07-13-cube-idp-phase1-mvp.md:1780` — in-cluster zot registry and manifest layout
- `docs/archive/superpowers/plans/2026-07-13-cube-idp-phase1-mvp.md:1931` — port-forward as the host-to-registry path
- `docs/archive/superpowers/plans/2026-07-15-cube-idp-phase4-first-release.md:1379` — single `IsLocalRegistryHost` / plain-HTTP policy
- `docs/archive/superpowers/plans/2026-07-16-org-migration.md:16` — go-getter fork via `replace`
- `docs/archive/superpowers/plans/2026-07-19-valuesref-remote-config.md:179` — unsupported-scheme rejection
