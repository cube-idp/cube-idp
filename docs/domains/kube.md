# Domain: kube

Living contract of the kube domain (`internal/kube`). Cross-cutting rules:
`docs/ARCHITECTURE.md`. Originating design gate: `docs/DECISIONS.md`
2026-08-04 (M6, epic #81).

## Purpose

Construct Kubernetes client access from **injected kubeconfig bytes**,
exposing the minimal surface later domains need — nothing more. The domain
is a shared **leaf**: it imports only `internal/cubeerr`,
`k8s.io/client-go`, and `k8s.io/apimachinery` — never `api/` or any other
domain — and is consumed by injection at the CLI/orchestrator edge
exactly like every cross-domain value in this repo.

## The injection contract

`kube` receives **kubeconfig bytes + a context name** as parameters:

- It never reads or writes files — kubeconfig file management belongs to
  the cluster domain; the CLI edge reads the same kubeconfig target
  `create` writes and passes the bytes in.
- It never derives the `cube-idp.dev/<name>` context name — the edge
  derives it from the `api/` group constant (per the cluster contract's
  "Contracts for future domains").
- It never imports `internal/cluster`.

Bytes-plus-context-name is provider-agnostic: a future `existing` or k3d
provider changes nothing here (no near-term pull — operator decision
2026-08-04; no extra room reserved).

## Exported surface

Contract-level shape (exact signatures are fixed at implementation within
this contract; every export carries a doc comment per the house rules):

```go
// New builds client access from kubeconfig bytes, selecting contextName
// (empty = the kubeconfig's current-context). Pure construction: no I/O,
// no network — errors are kubeconfig/context/construction problems only.
func New(kubeconfig []byte, contextName string) (*Client, error)

// Client bundles the minimal client set consumers need.
// Accessors return client-go's stable interface types directly
// (construction-confined dependency stance — ARCHITECTURE §8):
//   REST config, discovery client, memory-cached RESTMapper, dynamic client.
type Client struct{ /* unexported */ }

// Ping reports whether the API server behind the selected context is
// reachable (readiness endpoint round-trip). The only network call in M6.
func (c *Client) Ping(ctx context.Context) error
```

## Interface doctrine applied

**No Kind-B driver seam.** There is exactly one Kubernetes API; nothing is
swappable, so a `Provisioner`-style seam would be ceremony (explicit
decision, 2026-08-04). Consumer-side (Kind A) doctrine governs instead:
`kube` returns concrete types, and each consumer (M7 apply first) defines
the 1–3 method interface *it* needs where it uses it, mocked with
hand-rolled function-field structs.

## Error codes (`CUBE-KUB-*`, exit 1)

| Code | Meaning |
|---|---|
| `CUBE-KUB-001` | invalid kubeconfig bytes (unparseable) |
| `CUBE-KUB-002` | context not found in the provided kubeconfig |
| `CUBE-KUB-003` | client construction failed |
| `CUBE-KUB-004` | API server unreachable (Ping) |

## Testing

Hermetic gate tests over in-memory fixture kubeconfigs (bytes injected
directly — no filesystem involved): parse failures, context selection
(found/missing/empty-name default), construction error rows —
first-class table rows, no live cluster, no Docker. `Ping` against a real cluster runs only behind
`make test-e2e` (kind path, worktree-local KUBECONFIG per CLAUDE.md §7).

## CLI surface

None owned by this domain. M6's user-visible proof is one line added to
`cube-idp status` — API-server reachability — composed at the CLI edge
beside `cluster.Status` (bytes + context name injected into `kube.Ping`).
Unreachable is a finding, not a failure: `status` keeps its exit-0-on-
successful-report doctrine; only failures to determine the answer are
errors.

## Contracts for future domains

- M7 apply consumes the dynamic client + RESTMapper for SSA; it defines
  its own narrow consumer-side interface over what it uses and receives a
  constructed `*kube.Client` (or the relevant interfaces) by injection at
  the edge.
- Construction stays confined here: no other package may turn kubeconfig
  bytes into clients. Consumers referencing client-go's stable interface
  types in signatures is sanctioned (ARCHITECTURE §8).
- Not in this domain, ever on the current horizon: watch
  machinery/informers, controller-runtime, typed workload clientsets,
  retry/backoff frameworks, port-forward/exec/logs, `runtime.Scheme`.
