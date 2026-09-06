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

### The kind driver's ingress-ready default (M11)

From the M11 design gate (`docs/DECISIONS.md`
2026-08-27, decision 5c/5d — an operator override of the scoped
recommendation), shipped in M11-C3 (PR #190, closing #184).

**No explicit `forProvider`** means an absent payload: `forProvider`
nil, or present with a zero-length body. That — and only that — takes
the default. A present-but-empty `forProvider: {}` is an explicit
payload and is therefore the documented minimal opt-out: no port
mappings, no label, an empty generated config.

When the user supplies no explicit `forProvider`, the kind driver
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

The generated shape is pinned: exactly **one explicit node** of role
`control-plane`, carrying the label `ingress-ready: "true"` and both
mappings with protocol **TCP**. Node image and every other field stay
kind's own defaults. The driver contract is tested on three branches —
absent → the default shape; `{}` → nothing defaulted; non-empty → the
decoded payload, unmerged.

`make test-e2e` (kind driver conformance against real Docker) now
creates a default-shaped cluster and therefore **binds host ports 8080
and 8443**. The suite is environment-sensitive: a `create` failure
caused by an occupied host port is an environment condition, not a
driver regression.

## Kubeconfig machinery

**Name ownership.** cube-idp owns the exact context name it installs, and
ownership is decided **by that name alone** — the model carries no
provenance, so `Merge` upserts and `Remove` drop purely on a name match.
The `cube-idp.dev/` prefix is **reserved** for cube-idp, but reservation
of the prefix and the scope of an operation are different things: a
merge-path `Init` and every `Delete` act on the entries matching *that
cube's* exact context name — `ContextName(name)` by default, or the
caller-supplied override, which may be inside or outside the prefix.
Deleting one cube therefore never sweeps the prefix; it removes one name.
Standalone `Init` (`InitOptions.KubeconfigPath`) is outside this rule
entirely: it replaces the target file rather than matching names in it. What the reservation buys
is that an entry sitting under a name a cube installs is treated as
cube-owned whoever wrote it. Warning about an entry "the operator did not
install" is not implementable here: it would need provenance the data
model does not carry, and adding that is a gate event, not an inference.

Own minimal typed model over `sigs.k8s.io/yaml` (no client-go):
`ContextName(name)` = `<API group>/<name>` (single source of truth for the
`cube-idp.dev/` prefix), `Rebrand(raw, contextName, namespace)`,
`Merge(existing, incoming)`, and `Remove(existing, contextName)` — the
exact reverse of Merge-installing a Rebrand-ed config: entries dropped by
name over the same map-based lossless model, `current-context` unset only
when it pointed at the removed context, and a changed-flag so callers
skip rewriting untouched files.

**`current-context` ownership.** The selector is the one global key in a
kubeconfig, so taking it over retargets every `kubectl` the operator has
open. `Merge` therefore adopts the incoming selector **only when the
destination has none** — absent and present-but-empty both count as none,
so a first cube on a fresh file is still selected, while a selection the
operator made is never displaced. `Remove`'s unset is the exact
counterpart, and is decided **by name, not by provenance** — nothing in a
kubeconfig records who selected a context, so provenance is not available
to decide on. `Remove` therefore clears `current-context` when it names
the context being removed, **even if the operator selected it manually**,
rather than leaving a dangling selector at a context that no longer
exists — the behaviour kubectl has, and the one this domain chose not to
imitate silently. A selector naming any other context is preserved, which
is the guarantee `Merge`'s rule delivers. No previous value has to be
restored, because **on the merge path** the existing selector is no
longer overwritten. That qualification matters: named entries that
collide are still upserted, and the standalone path still replaces the
whole file. The `--kubeconfig` standalone
path is specified separately and always selects the cube: that path
replaces the file wholesale (no merge), so there is no surviving
selection to protect, and preserving the replaced file's selector would
leave it naming a context the new file does not contain.

The unset was previously justified in-code as "matching kubectl's
delete-context behavior". That is **false** — verified against `kubectl`
v1.35.0, which leaves a dangling `current-context` and prints a warning
instead. The justification is the symmetry with `Merge` above, and the
claim has been removed. The context `namespace` is a method-level
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
  (operator decision 2026-08-02). `changed` reports whether the file was
  **modified**, so a failed write is `false`: the write is atomic, so it
  leaves the target exactly as it was.
- `Status(ctx, Provisioner, StatusOptions) (StatusReport, error)`:
  read-only — seam `Exists` plus a typed parse of the kubeconfig target,
  reporting `ClusterExists`/`ContextInstalled` with the resolved names.
  A missing kubeconfig file is "not installed"; only failures to
  determine the answer are errors. The kubeconfig-side ones carry
  `CUBE-CLU-006`, never the write-side `CUBE-CLU-005`; a seam `Exists`
  failure is the driver's own coded error and still propagates unchanged.

`Init`'s and `Delete`'s signatures are unchanged, and neither can express
a partial success in its return value — so the **partial state is carried
by the error's words**, built at the one seam that knows which stage
completed. Once `Ensure` has returned **successfully**, the cluster
exists; once the seam `Delete` has returned successfully, the cluster is
gone (including the already-absent
no-op, which is why the wording is "is gone", not "was deleted"). Every
later failure therefore says so, keeping `CUBE-CLU-005` and the wrapped
cause and naming the command to re-run — `Ensure` is idempotent by name,
so a re-run of `create` cannot rebuild the cluster. A failure **before**
the stage completes carries no such claim: the driver's own coded error
surfaces untouched. The fact reaches the operator on stderr inside the
coded error rather than as a stdout line, which is the accepted cost of
leaving both signatures alone.

## Error codes (`CUBE-CLU-*`, exit 1)

| Code | Meaning |
|---|---|
| `CUBE-CLU-001` | no cluster configured |
| `CUBE-CLU-002` | no driver for provider |
| `CUBE-CLU-003` | invalid `forProvider` payload (from M4 also surfaced by `config validate`) |
| `CUBE-CLU-004` | provisioning failed |
| `CUBE-CLU-005` | kubeconfig update failed (generation, merge, write, or cleanup) |
| `CUBE-CLU-006` | kubeconfig read failed (a read-only operation could not read, parse, or even locate the target) |

`CUBE-CLU-005` and `CUBE-CLU-006` split by **what the operation was
doing**, not by which call failed. `CUBE-CLU-005` is the write side and
stays exactly as wide as its row: `Init` and `Delete`, including the reads
they perform as steps *inside* a merge or a cleanup. `CUBE-CLU-006` is
raised only where nothing is being changed — `Status`, and the CLI edge's
own pre-apply and reachability reads — because "kubeconfig **update**
failed", and a remediation offering to write elsewhere, describe neither
what happened nor what the operator should do on a read-only verb.

`CUBE-CLU-006`'s remediation is cause-specific, since the ways a read
fails want opposite advice: an **absent** file is a missing prerequisite
and points at `cube-idp create`; an unreadable or malformed one is a
problem with the file that is already there — malformed covers a typed
decode failure, so the guidance says "a valid kubeconfig" rather than
"valid YAML"; and an **unresolvable
location** — no `KUBECONFIG`, no home directory — names neither, because
there is no file to inspect and `create` would fail the same way. A
kubeconfig that is present but simply lacks the cube context is not a
CLU error at all: the read succeeds. For `status` it is not an error at
any code — the report just says "not installed". Where a client is
actually being built from it (the bootstrap edge, and `status`'s own
reachability probe), `internal/kube` reports the missing context as
`CUBE-KUB-002` with its own `create` guidance.

The three branches are not all reachable from every raiser, and that is
deliberate rather than dead code. `Status` never sees the absent-file
branch — `contextInstalled` answers `fs.ErrNotExist` with "not installed"
before any error is built — so the prerequisite guidance exists for the
CLI edge's reads, which is why its constructor is exported while the
location one, raised at the single site that resolves the default path,
is not. The domain owns the vocabulary; the edge reaches the branch the
domain cannot.

`CUBE-CLU-004`'s remediation varies by action: a **create** can fail
because the container runtime is absent *or* because the host ports the
cluster publishes are already bound — the second-default-cube collision
described above — so its guidance names both causes and points at
`spec.cluster.forProvider` as the escape. `list` and `delete` bind no
ports and keep the runtime-only guidance. The port *numbers* stay in the
driver contract (the ingress-ready default above) and are not duplicated
in this generic guidance: the constructor receives an action, not resolved
port mappings, and naming a default would misdirect anyone who supplied
their own `forProvider`.

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
On success, one line of output states whether kubeconfig changes were
needed. When the cluster went but the cleanup failed, that line is not
reached — the coded error on stderr carries the fact instead, per the
partial-state rule in Operations above.

`status [-f cube.yaml] [--kubeconfig <path>]
[--kubeconfig-context-name <n>]` — the Status operation rendered as three
lines (cluster exists/not found; context installed in `<path>`/not
installed; api server reachable/unreachable/not checked — the third line
is composed at the CLI edge via the kube domain, see
`docs/domains/kube.md`, M6). Exit 0 whenever the report succeeds — an
absent cluster or unreachable API server is a finding, not a failure;
coded errors keep their usual exit semantics.

## Contracts for future domains

Consumers receive kubeconfig bytes by injection at the orchestrator/CLI
edge (never by importing `internal/cluster`), or derive the merged context
name from the API group constant (importing only leaf `api/`).

The domain is lifecycle-complete as of M5 (epic #72). M11 changed one
driver default and nothing else here — no seam change, no new
operation, no new code — which is the shape a milestone in a
neighbouring domain should have on this one. Future cluster work (new
providers, drift detection) starts as a new milestone against this
contract.
