# Domain: cluster

Living contract of the cluster domain (`internal/cluster` + the kind
driver). Cross-cutting rules: `docs/ARCHITECTURE.md`. Originating design
(M3, approved 2026-07-29):
`docs/archived/design/2026-07-29-cluster-domain.md`.

## Purpose

Provision and manage the cluster declared in `spec.cluster`, and install
cube-owned kubeconfig contexts. Cluster name = `metadata.name` (the cube
identity); the config document is the single source of truth — no flag
ever provisions state that disagrees with it.

## API (`spec.cluster`)

Crossplane-shaped: `provider` (typed constant, only `kind`, defaulted) +
`forProvider` (`runtime.RawExtension` — opaque at load time, strictly
decoded and validated by the selected provider; for kind: a
`kind.x-k8s.io/v1alpha4` Cluster). Absent `spec.cluster` = not managing a
cluster (`init` fails with `CUBE-CLU-001`); present-and-empty = default
kind cluster.

## The driver seam (Kind B)

```go
type Provisioner interface {
    Ensure(ctx context.Context, s Spec) error      // idempotent by name; no drift diff
    Exists(ctx context.Context, name string) (bool, error)
    Delete(ctx context.Context, name string) error // absent cluster → no-op
    Kubeconfig(ctx context.Context, name string) ([]byte, error) // provider-native names
}
```

`RunClusterConformance(t, factory)` asserts the lifecycle contract; a
stateful fake runs it in the green gate; the kind driver runs it for real
behind `make test-e2e` (opt-in, auto-skips without Docker).
`internal/cluster/kind` is the sole importer of `sigs.k8s.io/kind`.

## Kubeconfig machinery

Own minimal typed model over `sigs.k8s.io/yaml` (no client-go):
`ContextName(name)` = `<API group>/<name>` (single source of truth for the
`cube-idp.dev/` prefix), `Rebrand(raw, contextName, namespace)`,
`Merge(existing, incoming)`. The context `namespace` is a method-level
option (`InitOptions.Namespace`) with no config surface: kubectl treats it
as the context default, and clientcmd exposes it programmatically —
future domains re-apply the same pattern locally (namespace as an option
on their own operations), never by rewriting kubeconfig.

## The Init operation

`Init(ctx, Provisioner, InitOptions)`: Ensure → Kubeconfig → Rebrand →
merge into `$KUBECONFIG`/`~/.kube/config` (default) or write standalone
file (`--kubeconfig`, no merge). Driver selection happens at the CLI edge.

## Error codes (`CUBE-CLU-*`, exit 1)

| Code | Meaning |
|---|---|
| `CUBE-CLU-001` | no cluster configured |
| `CUBE-CLU-002` | no driver for provider |
| `CUBE-CLU-003` | invalid `forProvider` payload |
| `CUBE-CLU-004` | provisioning failed |
| `CUBE-CLU-005` | kubeconfig generation/merge/write failed |

## CLI surface

`init [-f cube.yaml] [--kubeconfig <path>] [--kubeconfig-context-name <n>]`.

## Contracts for future domains

Consumers receive kubeconfig bytes by injection at the orchestrator/CLI
edge (never by importing `internal/cluster`), or derive the merged context
name from the API group constant (importing only leaf `api/`).

## Pending (M5 — cluster lifecycle completion)

- `delete` (or `down`) command exposing the seam's `Delete`; `status`;
  kubeconfig cleanup on deletion. Command naming and exact scope decided
  at plan time.
