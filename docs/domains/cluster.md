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
cluster (`create`/`delete`/`status` fail with `CUBE-CLU-001`);
present-and-empty = default kind cluster.

## The driver seam (Kind B)

```go
type Provisioner interface {
    Ensure(ctx context.Context, s Spec) error      // idempotent by name; no drift diff
    Exists(ctx context.Context, name string) (bool, error)
    Delete(ctx context.Context, name string) error // absent cluster → no-op
    Kubeconfig(ctx context.Context, name string) ([]byte, error) // provider-native names
}
```

`Kubeconfig` bytes are stable — identical across calls — while the
cluster is untouched. That stability is part of the seam contract: it is
the strongest signal the seam exposes for proving `Ensure` idempotency,
and the conformance suite relies on it (a second `Ensure` that recreates
the cluster changes certificates/endpoint and fails the suite). A future
provider that cannot satisfy it (e.g. rotating exec-token kubeconfigs)
needs the guarantee relaxed at a design gate — it is never a driver-side
choice.

`RunClusterConformance(t, factory)` asserts the lifecycle contract; a
stateful fake runs it in the green gate; the kind driver runs it for real
behind `make test-e2e` (opt-in, auto-skips without Docker).
`internal/cluster/kind` is the sole importer of `sigs.k8s.io/kind`.

### Optional capability: spec validation (M4)

Per the interface doctrine, optional capabilities are separate small
type-asserted interfaces beside the seam:

```go
type SpecValidator interface {
    ValidateSpec(s Spec) error // pure: no I/O, no side effects
}
```

The kind driver implements it by strict-decoding `forProvider` as a
`kind.x-k8s.io/v1alpha4` Cluster — the same decode `Ensure` performs,
extracted into a shared helper — returning `CUBE-CLU-003` on failure.
Constraint discovered in code: driver *construction* must not require a
container runtime (`config validate` must work without Docker), so kind's
runtime detection moves from `New()` to first provisioning call
(`Ensure`/`Exists`/`Delete`/`Kubeconfig`); `ValidateSpec` never triggers
it. The conformance suite gains an optional sub-test: if the provisioner
implements `SpecValidator`, an invalid payload must yield a coded error.

## Kubeconfig machinery

Own minimal typed model over `sigs.k8s.io/yaml` (no client-go):
`ContextName(name)` = `<API group>/<name>` (single source of truth for the
`cube-idp.dev/` prefix), `Rebrand(raw, contextName, namespace)`,
`Merge(existing, incoming)`, and `Remove(existing, contextName)` — the
exact reverse of Merge-installing a Rebrand-ed config: entries dropped by
name over the same map-based lossless model, `current-context` unset only
when it pointed at the removed context, and a changed-flag so callers
skip rewriting untouched files. The context `namespace` is a method-level
option (`InitOptions.Namespace`) with no config surface: kubectl treats it
as the context default, and clientcmd exposes it programmatically —
future domains re-apply the same pattern locally (namespace as an option
on their own operations), never by rewriting kubeconfig.

## Operations (M5: full lifecycle)

Driver selection happens at the CLI edge for all three; the domain never
prints.

- `Init(ctx, Provisioner, InitOptions)`: Ensure → Kubeconfig → Rebrand →
  merge into `$KUBECONFIG`/`~/.kube/config` (default) or write standalone
  file (`--kubeconfig`, no merge).
- `Delete(ctx, Provisioner, DeleteOptions) (changed bool, err error)`:
  the reverse — seam `Delete` (absent cluster no-op), then `Remove` from
  the same kubeconfig target Init writes, atomically and only when
  something matched. A missing kubeconfig file is a clean no-op, and a
  file is **never unlinked** — an emptied kubeconfig stays on disk
  (operator decision 2026-08-02).
- `Status(ctx, Provisioner, StatusOptions) (StatusReport, error)`:
  read-only — seam `Exists` plus a typed parse of the kubeconfig target,
  reporting `ClusterExists`/`ContextInstalled` with the resolved names.
  A missing kubeconfig file is "not installed"; only failures to
  determine the answer are errors.

## Error codes (`CUBE-CLU-*`, exit 1)

| Code | Meaning |
|---|---|
| `CUBE-CLU-001` | no cluster configured |
| `CUBE-CLU-002` | no driver for provider |
| `CUBE-CLU-003` | invalid `forProvider` payload (from M4 also surfaced by `config validate`) |
| `CUBE-CLU-004` | provisioning failed |
| `CUBE-CLU-005` | kubeconfig update failed (generation, merge, write, or cleanup) |

## CLI surface

`init [-f cube.yaml] [--name <cube-name>]` — config-only since the M5
split (operator decision 2026-08-03, `docs/DECISIONS.md`):
scaffold-if-absent → load → report, exit 0 and idempotent. It never
provisions and never touches a kubeconfig. When the config file does not
exist, `init` scaffolds it (`metadata.name` from `--name`, else a
generated docker-style name) and prints a notice naming the created file
and cube plus a `create` next-step hint. The scaffold machinery belongs
to the config domain (`docs/domains/config.md`) — this domain never
writes config. `--name` never mutates an existing document: a mismatch
with the loaded `metadata.name` is `CUBE-CFG-005`; a match proceeds
(idempotent re-runs stay cheap).

`create [-f cube.yaml] [--kubeconfig <path>]
[--kubeconfig-context-name <n>]` — load → provision via the seam →
install the cube-owned kubeconfig context (the Init operation above,
formerly `init`'s job). `create` never scaffolds: a missing config file
is the loader's coded error, keeping the config document the single
source of truth.

`delete [-f cube.yaml] [--kubeconfig <path>]
[--kubeconfig-context-name <n>]` — the reverse of `create` (the Delete
operation above): resolves the cube from the config document (no
`--name`, never scaffolds), removes the cluster, and cleans the
cube-owned context out of the same kubeconfig target `create` writes.
One line of output states whether kubeconfig changes were needed.

`status [-f cube.yaml] [--kubeconfig <path>]
[--kubeconfig-context-name <n>]` — the Status operation rendered as three
lines (cluster exists/not found; context installed in `<path>`/not
installed; api server reachable/unreachable/not checked — the third line
is composed at the CLI edge via the kube domain, see
`docs/domains/kube.md`, M6). Exit 0 whenever the report succeeds — an
absent cluster or unreachable API server is a finding, not a failure;
coded errors keep their usual exit semantics.

## M11 amendment (gated 2026-08-27, ahead of code): the kind driver's ingress-ready default

Delimited amendment from the M11 design gate (`docs/DECISIONS.md`
2026-08-27, decision 5c/5d — an operator override of the scoped
recommendation); it folds into the living body at the M11 closeout, and
no code implements it before the M11 breakdown is aligned.

When the user supplies **no explicit `forProvider`**, the kind driver
defaults the generated cluster config to kind's documented
ingress-ready shape, on **high host ports** (above the conventional
privileged-port range; URLs carry ports):

- `extraPortMappings`: host **8080 → containerPort 80** and host
  **8443 → containerPort 443** on the (single, default) node;
- the `ingress-ready=true` node label on that node — the gateway's
  Deployment pins to it (`nodeSelector` + hostPorts 80/443; the
  in-cluster half is the gateway contract's, `docs/domains/gateway.md`).

**Explicit `forProvider` always wins, wholesale** — the driver never
merges the default into a user-supplied payload; supplying any
`forProvider` config means owning ports and labels entirely. Recorded
boundaries: the driver **cannot see `spec.gateway`** (it receives
`{Name, ForProvider}` only, so the default is unconditional — coherent
with the gateway's absent-means-installed posture, never
gateway-triggered); port mappings exist only at cluster **create**
(create-before-bootstrap coupling: a gateway wanted on a cluster
created without them needs a recreate); and a host-port collision — a
second default cube — fails `create` loudly with the provider's coded
error, explicit `forProvider` being the escape.

Implementation-task notes, recorded for the M11 breakdown (domain-lead
review, 2026-08-27): (a) pin the meaning of "no explicit
`forProvider`" — the current decode treats nil-or-empty `Raw` as
absent, which makes a present-but-empty `forProvider: {}` an explicit
(default-suppressing) payload; state that as the documented opt-out
rather than leaving it emergent from the decode branch; (b) pin the
exact generated shape (one explicit control-plane node, both mappings
TCP, label `ingress-ready: "true"`) and the three-branch driver
contract tests (absent → defaults; `{}` → none; non-empty → unmerged);
(c) note in the e2e guidance that `make test-e2e` now binds host
8080/8443 and becomes environment-sensitive to occupied ports — an
occupied-port failure is not a driver regression.

## Contracts for future domains

Consumers receive kubeconfig bytes by injection at the orchestrator/CLI
edge (never by importing `internal/cluster`), or derive the merged context
name from the API group constant (importing only leaf `api/`).

The domain is lifecycle-complete as of M5 (epic #72); the M11 amendment
above is the one gated addition pending its breakdown. Future cluster
work (new providers, drift detection) starts as a new milestone against
this contract.
