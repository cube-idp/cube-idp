# cube-idp — M3: the cluster domain

Date: 2026-07-29
Status: approved (owner-reviewed in session)
Branch: `RafPe/implement-core-cluster-features`
Parent design: `2026-07-27-back-to-basics-structure.md` (structural authority)

## 1. Context and scope

M3 adds the first component domain after the v0 config baseline: cluster
provisioning. Per the ROADMAP it delivers the `spec.cluster` sub-struct, the
`internal/cluster` driver seam with a conformance suite, a kind provider, and
an `init` command. This document records the owner decisions (2026-07-29
session) that a design doc must carry for M3: a new domain, a new driver
seam, and a new runtime dependency.

Owner decisions:

1. CLI surface is `init` only. No `delete`/`down`/`status` commands in M3;
   the seam still carries the full lifecycle (see §4) because conformance
   and e2e cleanup need it.
2. kind is driven as a Go library (`sigs.k8s.io/kind`), not by exec-ing the
   kind binary.
3. `spec.cluster` is Crossplane-shaped: a typed `provider` constant plus an
   opaque `forProvider` payload owned and validated by the selected
   provider.
4. `forProvider` is `runtime.RawExtension` at load time; the provider
   strict-decodes and validates it at `init` time. A follow-up (ROADMAP)
   brings provider-side validation into `config validate` without breaking
   import direction.
5. Kubeconfig behavior follows kind's default (merge into `$KUBECONFIG` or
   `~/.kube/config`) but with cube-owned context names
   `cube-idp.dev/<cluster-name>`, derived from the API group constant.
   `--kubeconfig <path>` writes to that file instead of merging;
   `--kubeconfig-context-name` overrides the derived name.
6. The context namespace is a programmatic, method-level option
   (`InitOptions.Namespace`), not config surface: there is no
   `spec.cluster.kubeconfig` sub-struct. Today the CLI passes it empty; the
   future orchestrator/apply flow sets it when it knows the target
   namespace, and clients built from the kubeconfig inherit it via the
   context's `namespace` field (clientcmd `Namespace()`).
7. Test split: the green gate stays hermetic — conformance runs against a
   stateful fake in `make test`; the kind provider runs the same suite for
   real behind `make test-e2e`, auto-skipping when Docker is unreachable.

Verified constraint driving decision 5: kind hardcodes context names as
`"kind-" + clusterName` (`KINDClusterKey`, in a non-importable `internal/`
package); no public provider or create option customizes it. Cube-owned
context names therefore require post-processing the kubeconfig ourselves.

## 2. CLI surface

```
cube-idp init [-f cube.yaml] [--kubeconfig <path>] [--kubeconfig-context-name <name>]
```

- Default: ensure the cluster exists, rebrand its kubeconfig entries to
  `cube-idp.dev/<cluster-name>`, merge into the standard kubeconfig
  location, set `current-context`.
- `--kubeconfig <path>`: write the rebranded kubeconfig to `<path>` only —
  no merge into the default location.
- `--kubeconfig-context-name <name>`: use `<name>` instead of the derived
  context name.
- Exit codes: 0 success, 2 config error (`CUBE-CFG-*`), 1 anything else
  (including `CUBE-CLU-*`). `internal/cli/exit.go` is unchanged.

`internal/cli` stays flag-mapping only: it loads the config via
`internal/config`, maps flags + `spec.cluster` into `cluster.InitOptions`,
picks the driver from `spec.cluster.provider` (factory at the CLI edge, per
the composition rule), and calls `cluster.Init`.

## 3. The `spec.cluster` API

```go
// api/config/v1alpha1/cluster_types.go

// ClusterProvider identifies a cluster backend implementation.
type ClusterProvider string

// ClusterProviderKind provisions clusters with kind (Kubernetes-in-Docker).
const ClusterProviderKind ClusterProvider = "kind"

// ClusterSpec declares the cluster cube-idp manages.
type ClusterSpec struct {
    // Provider selects the backend. Defaults to "kind".
    Provider ClusterProvider `json:"provider,omitempty"`

    // ForProvider carries provider-specific configuration, passed through
    // opaquely at load time and strictly decoded + validated by the
    // selected provider (for kind: a kind.x-k8s.io/v1alpha4 Cluster).
    ForProvider *runtime.RawExtension `json:"forProvider,omitempty"`
}
```

`ConfigSpec` gains its first sub-struct:

```go
type ConfigSpec struct {
    // Cluster is optional: a config without it is valid (config-only use);
    // `init` requires it and fails with CUBE-CLU-001 when absent.
    Cluster *ClusterSpec `json:"cluster,omitempty"`
}
```

- The pointer is justified by "unset vs zero must differ": absent means
  *not managing a cluster*; present-and-empty means *default kind cluster*.
- `Default()`: when `spec.cluster` is present and `provider` is empty, set
  it to `kind`.
- `Validate()`: `provider` must be a known value (only `kind`).
  `forProvider` is not validated at load time (§8 follow-up).
- Context-name derivation has one source of truth:
  `v1alpha1.GroupVersion.Group + "/" + <cluster-name>` — the
  `cube-idp.dev` prefix comes from the API group constant, never a second
  hardcoded string.
- Cluster name = `metadata.name` (the cube identity).

Sample document:

```yaml
apiVersion: cube-idp.dev/v1alpha1
kind: Config
metadata:
  name: dev
spec:
  cluster:
    provider: kind
    forProvider:
      # kind.x-k8s.io/v1alpha4 Cluster fields, e.g.:
      nodes:
        - role: control-plane
```

## 4. The driver seam — `internal/cluster`

Kind-B seam per the parent design §6: interface + conformance suite live in
the domain package; implementations live in subpackages.

```go
// internal/cluster/cluster.go

// Spec is the provider-neutral input to a Provisioner.
type Spec struct {
    Name        string                // cluster name (from metadata.name)
    ForProvider *runtime.RawExtension // provider-specific config, opaque here
}

type Provisioner interface {
    // Ensure creates the cluster if absent; no-op if it exists. Idempotent.
    Ensure(ctx context.Context, s Spec) error
    Exists(ctx context.Context, name string) (bool, error)
    Delete(ctx context.Context, name string) error
    // Kubeconfig returns the raw admin kubeconfig (provider-native names).
    Kubeconfig(ctx context.Context, name string) ([]byte, error)
}
```

The seam is full-lifecycle even though only `init` ships: the conformance
suite asserts create→exists→delete round-trips and e2e runs need `Delete`
for cleanup. `Delete` gains a CLI command in a later milestone.

The domain operation the CLI calls:

```go
// internal/cluster/init.go

type InitOptions struct {
    Spec           Spec
    ContextName    string // "" → GroupVersion.Group + "/" + Spec.Name
    Namespace      string // "" → omitted from the generated context
    KubeconfigPath string // "" → merge into default location; else write file, no merge
}

func Init(ctx context.Context, p Provisioner, opts InitOptions) error
```

Flow: `Ensure` → `Kubeconfig` → `Rebrand` → merge-or-write. `Init` consumes
the seam inside the same package; driver selection stays at the CLI edge.

### Kubeconfig machinery — `internal/cluster/kubeconfig.go`

Minimal typed kubeconfig model (clusters/contexts/users/current-context)
over `sigs.k8s.io/yaml` — no client-go. Pure, table-tested functions:

- `Rebrand(raw []byte, contextName, namespace string) ([]byte, error)` —
  renames cluster/context/user entries from provider-native names to
  `contextName`; stamps `context.namespace` when non-empty. Fields our
  model does not touch round-trip unchanged.
- `Merge(existing, incoming []byte) ([]byte, error)` — entry-wise upsert
  by name; sets `current-context` to the incoming context. Merging into an
  empty or missing file yields the incoming config.

## 5. The kind provider — `internal/cluster/kind`

Sole importer of `sigs.k8s.io/kind`:

- `Ensure`: strict-decode `ForProvider` into kind's `v1alpha4.Cluster`
  (unknown fields rejected → `CUBE-CLU-003`); force the cluster name from
  `Spec.Name`; create via the kind library with kind's own kubeconfig write
  pointed at a throwaway path so kind never touches the user's file.
  Existing cluster → no-op. Name existence is the whole idempotency check
  in M3: `Ensure` does not diff a live cluster against `forProvider` (drift
  detection is a later-milestone concern, if ever).
- `Exists` / `Delete`: thin wrappers over the kind provider API.
- `Kubeconfig`: returns `KubeConfig(name, false)` bytes untouched
  (`kind-<name>` names — rebranding is the domain's job; the driver stays
  pure).

## 6. Errors — `CUBE-CLU-*`

`internal/cluster/errors.go` owns the catalog; `cubeerr` is unchanged. The
parent design §5.2 registry row `CLU` is hereby active (arrives: M3).

| Code | Meaning | Remediation hint |
|---|---|---|
| `CUBE-CLU-001` | no cluster configured (`spec.cluster` absent on `init`) | add `spec.cluster` to the config |
| `CUBE-CLU-002` | no driver for provider (factory found no match) | use a supported `spec.cluster.provider` |
| `CUBE-CLU-003` | invalid `forProvider` payload (strict decode failed) | fix the kind config fields listed |
| `CUBE-CLU-004` | provisioning failed (wrapped provider error) | check Docker is running; see cause |
| `CUBE-CLU-005` | kubeconfig generation/merge/write failed | see cause; check file permissions |

Exit code for `CLU` is 1 (only `CFG` maps to 2).

## 7. Testing

- Conformance suite (`internal/cluster/conformance.go`): exported
  `RunClusterConformance(t, factory)` asserting the lifecycle contract —
  `Exists` false before / true after `Ensure`; `Ensure` twice no-ops;
  `Kubeconfig` returns parseable YAML naming the cluster; `Delete` removes
  and `Exists` goes false again.
- A stateful fake provider inside `internal/cluster` tests runs the suite
  in `make test` — proving the suite itself, Docker-free.
- The kind provider runs the same suite for real; it auto-skips when Docker
  is unreachable; `make test-e2e` runs it with worktree-local `KUBECONFIG`
  (CLAUDE.md §7). The green gate stays hermetic.
- `Rebrand`/`Merge`: table-driven with fixture kubeconfigs; unknown-field
  round-trip and empty/missing merge targets are first-class cases.
- CLI: hand-rolled function-field mock of `Provisioner`; error assertions
  via `errors.As` into `*cubeerr.Coded` + code equality.

## 8. Dependency decision

`sigs.k8s.io/kind` joins the closed runtime set:

| Module | Why |
|---|---|
| `sigs.k8s.io/kind` | library-first cluster provisioning — no external binary contract, no CLI-output parsing; pinned in `go.mod`; structured errors; prior art: idpbuilder embeds kind the same way |

Confinement: only `internal/cluster/kind` may import it. Notably **no
client-go**: kubeconfig manipulation uses our own minimal structs over the
already-present `sigs.k8s.io/yaml`.

## 9. Future work (recorded, not designed here)

- `config validate` covering `forProvider` via provider-side validation
  surfaced at the CLI edge — never `internal/config` importing
  `internal/cluster`. Queued in the ROADMAP.
- `delete`/`down` command exposing the seam's `Delete`; `status` later.
- Kubeconfig/namespace contract for future domains: consumers receive
  kubeconfig bytes by injection at the orchestrator/CLI edge (never by
  importing `internal/cluster`), or locate the merged context by deriving
  its name from the API group constant (importing only `api/`). The
  namespace stamped by `InitOptions.Namespace` is the human/kubectl
  default only; programmatic consumers re-apply the same pattern locally —
  namespace as an option on their own operations (client construction
  override, per-object namespace in SSA) — never by rewriting kubeconfig.
- ROADMAP housekeeping lands in this PR: M1/M2 to Done, M3 in progress
  linking this document.
