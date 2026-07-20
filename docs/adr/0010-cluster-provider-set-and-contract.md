---
status: "accepted"
date: 2026-07-20
decision-makers: "cube-idp maintainers"
---

# 10. Cluster Provider Set: kind, k3d, existing, Behind a Fixed Interface

## Context and Problem Statement

cube-idp provisions a Kubernetes cluster before it can install anything into it. The set of
ways to obtain that cluster is open-ended — local container-backed clusters, hosted clusters,
clusters a user already runs — and each one has a different lifecycle, a different container
runtime story, and different capabilities (some can side-load images into their nodes, some
cannot). Left unconstrained, this becomes a plugin surface: an RPC protocol, versioned
contracts, third-party binaries, and the compatibility burden that follows.

Two things had to be pinned down. First, how many provisioning backends exist and how a
caller selects one, so that a typo in configuration produces a clear error instead of a nil
provider deep in an install. Second, what a provider is actually required to do, so that
commands like `up`, `down`, `status` and `doctor` can be written once against a single seam
rather than against three special cases — and so that capabilities only some backends have
(image loading, log streaming) do not leak into the mandatory interface.

## Decision

cube-idp compiles in exactly three cluster providers — `kind`, `k3d`, and `existing` —
selected by a factory that rejects any unknown provider value with CUBE-1001. Every provider
implements the fixed `Provider` interface (`Ensure`, `Delete`, `Exists`, `Kubeconfig`,
`Diagnose`) and must satisfy a shared contract test: `Ensure` is idempotent, `Exists` is
truthful, `Kubeconfig` is non-empty for a live cluster, `Delete` is clean, and `Diagnose`
never panics and reports no error-severity findings on a healthy cluster.

Optional capabilities are separate interfaces — `ImageLoader`, `LogSink`/`Loggable` — that
only some providers implement. Consequently `up --bundle` against `provider: existing` fails
fast with CUBE-7005 before any cluster mutation, and container-runtime detection is delegated
to kind and k3d rather than coupling cube-idp to a docker client. New providers arrive as
in-tree pull requests; there is no plugin protocol. Read-only commands are guarded by
`requireClusterExists` (CUBE-1004) so they never implicitly create a cluster.

## Consequences

* Good, because every command targets one interface; adding a provider does not touch `up`,
  `down`, `status`, or `doctor`.
* Good, because the contract test makes "is this provider correct?" an executable question
  rather than a review opinion.
* Good, because a misconfigured `provider:` value fails at factory construction with a
  diagnosable code, not as a nil dereference later.
* Good, because keeping image loading and log streaming out of the mandatory interface lets
  `existing` — which controls neither nodes nor provisioning — remain a first-class provider.
* Bad, because a third party cannot ship a provider without a pull request into this repo;
  there is deliberately no out-of-tree extension path.
* Bad, because capability gaps surface as command-specific errors (CUBE-7005) rather than
  being visible in the type of the provider a user configured.
* Bad, because the contract test needs a live container runtime, so it is gated behind an
  environment variable and does not run in a plain `go test ./...`.

## Implementation Status

**This decision is implemented.** Confirmed against the code on 2026-07-20.

| Decision | Implemented at |
| --- | --- |
| The cluster provider set is `kind`, `k3d`, and `existing`; an unknown provider value is rejected at factory construction with CUBE-1001. | `internal/cluster/provider.go:90` |
| Every cluster provider satisfies a shared behavioral contract: idempotent `Ensure`, truthful `Exists`, non-empty `Kubeconfig` for a live cluster, clean `Delete`, and a `Diagnose` that never panics and reports no error-severity findings on a healthy cluster. | `internal/cluster/contracttest/contracttest.go:23` |
| The `Provider` interface is exactly `Ensure` (idempotent), `Delete`, `Exists`, `Kubeconfig`, and `Diagnose` (which feeds `doctor`). | `internal/cluster/provider.go:53-60` |
| `requireClusterExists` guards side-effect-free commands for the cluster-creating providers (kind and k3d) with CUBE-1004, so read-only commands — including `repo create` — never implicitly create a cluster. | `cmd/root.go:238` |
| `cluster.ImageLoader` is an optional capability implemented by kindp and k3dp but not by `existing`; `up --bundle` with `provider: existing` fails fast with CUBE-7005 before any mutation. | `internal/cluster/provider.go:74-86` |
| The kind and k3d providers stream provisioning narration into cube-idp's StepLog vocabulary through a `cluster.LogSink`/`cluster.Loggable` seam, with kind verbosity above V(0) dropped. | `internal/cluster/provider.go:24-39` |
| Container-runtime handling is delegated to kind/k3d, which auto-detect docker/podman/nerdctl; `existing` selects its kubeconfig context via client-go `clientcmd` honoring `KUBECONFIG` rather than hardcoding `~/.kube/config`. | `internal/cluster/existing.go:20-23` |
| Provider and extension implementations are compiled into the binary with no plugin protocol; new implementations arrive as pull requests. | `internal/cluster/provider.go:1-2` |
| No new cluster providers (Talos, vcluster), no Extism/Wasm plugin RPC, and no in-cluster cube-idp operator ship; all remain deferred and gated on demand evidence. | `internal/cluster/provider.go:89-102` |

### Verification

- [ ] `internal/cluster/provider.go:90` — `New` switches over exactly `"kind"`, `"k3d"`,
      `"existing"`, and its default arm returns `diag.CodeClusterTypeUnknown` (CUBE-1001).
- [ ] `internal/config/schema.cue:9` pins the same enum: `provider: *"kind" | "existing" | "k3d"`.
- [ ] `internal/cluster/provider.go:53-60` declares `type Provider interface` with exactly the
      five methods `Ensure`, `Delete`, `Exists`, `Kubeconfig`, `Diagnose` — no more.
- [ ] `internal/cluster/contracttest/contracttest.go:23` (`Run`) exercises pre-state `Exists`
      false, `Ensure`, re-`Ensure` idempotency, `Exists` true, non-empty `Kubeconfig`,
      error-free `Diagnose`, then `Delete` and `Exists` false.
- [ ] `internal/cluster/provider.go:84-85` asserts `ImageLoader` for `*kindp.Kind` and
      `*k3dp.K3d`, and no equivalent assertion exists for `existing`.
- [ ] `internal/up/up.go:154-160` type-asserts `prov.(cluster.ImageLoader)` and returns
      `diag.CodeBundleNoImageLoader` before `prov.Ensure` is called;
      `internal/diag/codes.go:142` maps it to CUBE-7005.
- [ ] `cmd/root.go:238` (`requireClusterExists`) returns nil for providers other than kind/k3d
      and otherwise `diag.CodeClusterNotExists` (CUBE-1004, `internal/diag/codes.go:34`); it is
      called from `cmd/status.go`, `cmd/get.go`, `cmd/sync.go`, `cmd/cnoe.go`, `cmd/down.go`,
      and `cmd/repo.go:89`.
- [ ] `internal/cluster/provider.go:30` declares `type LogSink = func(line string)` as a type
      alias and `:35` `Loggable`; `internal/cluster/kindp/kindlog.go:20-25` returns `nopInfo{}`
      for any kind verbosity level above 0.
- [ ] `internal/cluster/existing.go:21` uses `clientcmd.NewDefaultClientConfigLoadingRules()`
      and `~/.kube/config` appears nowhere in that file.
- [ ] Grepping `internal/` and `cmd/` for `hashicorp/go-plugin`, a gRPC provider protocol, or
      extism/wasm plugin RPC returns nothing.

## History

The provider set was originally scoped as kind plus `existing` as the CI-gated tier-1
providers with k3d deferred past the first release. k3d was in fact built, and the CI e2e
matrix now gates kind × k3d rather than kind and `existing` (`.github/workflows/ci.yaml:28`);
`existing` is exercised only as a test branch, not as a matrix entry.

The compiled-in, no-plugin-protocol commitment was originally recorded alongside a list of
three in-tree Go extension seams: `ClusterProvider`, `GitOpsEngine`, and `PackSource`. Only
two of those exist as interfaces today — `Provider` at `internal/cluster/provider.go:53` and
`Engine` at `internal/engine/engine.go:59`. There is no `PackSource` interface; pack sourcing
was implemented without one. The no-plugin-protocol commitment itself remains binding.

## More Information

Origin: mined from the archived planning corpus (`docs/archive/superpowers/`) during the
2026-07-20 documentation audit; the underlying statements were validated against the code
before this record was written.

- `docs/archive/superpowers/specs/2026-07-13-cube-idp-architecture-design.md:33` — original provider set and tiering
- `docs/archive/superpowers/specs/2026-07-13-cube-idp-architecture-design.md:112` — the five-method provider interface
- `docs/archive/superpowers/specs/2026-07-13-cube-idp-architecture-design.md:270` — compiled-in seams, no plugin protocol
- `docs/archive/superpowers/plans/2026-07-13-cube-idp-phase3-draft.md:201` — the shared provider contract test
- `docs/archive/superpowers/plans/2026-07-13-cube-idp-phase3-draft.md:1643` — `ImageLoader` as an optional capability
