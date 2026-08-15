# Domain: bootstrap

Living contract of the bootstrap domain (`internal/bootstrap` +
`api/config/v1alpha1` `spec.engine`). Cross-cutting rules:
`docs/ARCHITECTURE.md`. Originating design gate: `docs/DECISIONS.md`
2026-08-06 (M7, epic #92).

## Purpose

Stand up the gitops **engine** (Flux, the mandatory default) and hand over
permanently. `internal/bootstrap` is a **micro-bootstrap applier**: it
SSA-applies the embedded, pinned Flux install manifests, applies the
source + sync CRs derived from `spec.engine`, waits on the **bootstrap
kind-set**, records a **bootstrap inventory** (the seed of a future
`down`), and stops. Steady-state ownership of every pack and manifest is
the engine's from that point on — **no-engine operation is not a supported
mode**. cube-idp installs Flux and its wiring; Flux reconciles everything
else.

## Config surface (`spec.engine`)

A new optional sub-struct on `ConfigSpec`, defaults and validation beside
it in `api/config/v1alpha1` (the loading machinery never changes):

```go
// EngineSpec declares the gitops engine cube-idp bootstraps.
type EngineSpec struct {
    // Provider selects the engine backend. Defaults to "flux";
    // "flux" is the only value in M7.
    Provider EngineProvider `json:"provider,omitempty"`

    // Version pins the embedded Flux distribution (must match the
    // vendored manifests' recorded version; see "Flux acquisition").
    Version string `json:"version,omitempty"`

    // Source points the engine's sync at a location (git URL / OCI ref).
    Source *EngineSource `json:"source,omitempty"`
}
```

- **Minimal-typed on purpose.** There is no engine *driver seam* until M9,
  so there is no provider to own an opaque payload; a typed shape is
  validatable in `api/` today. When M9 formalizes the engine seam,
  migrating `spec.engine` to the cluster-style `provider` + opaque
  `forProvider` pattern is a **design-gate event**, not a drive-by edit.
- Absent `spec.engine` defaults to Flux (the engine is mandatory).
- The exact `EngineSource` field set (URL/path/branch/interval, git vs
  OCI) is settled by the **`M7-demo-source` checkpoint** — deferred
  operator call (Q6); source/sync generation (T5) waits on it.

## The injection contract

`bootstrap` receives, at the CLI/orchestrator edge:

- the validated `*EngineSpec` (from `api/config`), and
- **client-go interface values** — a `dynamic.Interface` and a
  `meta.RESTMapper` — constructed by `internal/kube` and passed across the
  edge.

It **never imports `internal/kube`** (domains never import each other; the
kube contract sanctions consumers referencing client-go's stable interface
types in signatures). It never reads files and never constructs clients
itself. Composition — reading config, building the kube client, injecting
the interfaces — lives in `internal/cli`.

## Flux acquisition (embedded, pinned)

The Flux install manifests are **embedded** in the binary via `go:embed`
of vendored `flux install --export` output (source-controller +
kustomize-controller at minimum — the components the bootstrap kind-set
waits on). They are **data, not a Go dependency**:

- a **version constant** + a recorded **sha256** pin the exact bytes;
- a `make` target regenerates the asset deliberately (reviewed diff), the
  same discipline as the committed deepcopy and the C4 SVGs;
- nothing is fetched at runtime — the hermetic gate and the air-gap
  posture are preserved. The air-gap *override* (a local-manifest path) is
  deferred to the M10 air-gap decision.

## SSA + readiness (hand-rolled on client-go)

Server-side apply (field manager `cube-idp`) and the readiness wait are
hand-rolled on the injected client-go interfaces — no `fluxcd/pkg/ssa`, no
kstatus/`cli-utils`, no controller-runtime (measured rejection, see
DECISIONS 2026-08-06). Readiness predicates read off `unstructured` status.

**Bootstrap kind-set** (the only wait scope in M7):

| Kind | Ready when |
|---|---|
| CustomResourceDefinition | `Established` condition true |
| Deployment / StatefulSet | observed generation rolled out, replicas available |
| Job | `Complete` condition true |
| Namespace | phase `Active` |

Engine-CR readiness (GitRepository/Kustomization *reconciled*) is **out of
scope** — it belongs to the M9 engine seam.

## Inventory (seed of `down`)

`bootstrap` records what it applied (a ConfigMap inventory) so a future
`down` can find and remove it. In-domain and self-contained — **not** a
reusable applier seam. (The pack-groundwork §2.1 "inventory-inside-Apply"
obligation is superseded: M8 delivers packs through the Flux source, not
through a cube-idp applier — DECISIONS 2026-08-06 / Q1.)

## Interface doctrine applied

**No Kind-B driver seam in M7.** Flux is the only engine; the swappable
engine seam is an M9 concern. Consumer-side (Kind A) doctrine governs: the
CLI edge injects concrete client-go interfaces; `bootstrap` defines any
narrow interface it needs where it uses it, mocked with hand-rolled
function-field structs. Argo CD, if it ever returns, arrives as an engine
*pack* and never as a compile-time dependency.

## Error codes (`CUBE-BST-*`, exit 1)

Illustrative catalog; the implementing task fixes exact numbers:

| Code | Meaning |
|---|---|
| `CUBE-BST-001` | embedded Flux manifests failed to decode (build/asset integrity) |
| `CUBE-BST-002` | SSA apply of a bootstrap object failed (wrapped cause) |
| `CUBE-BST-003` | bootstrap kind-set readiness wait timed out |
| `CUBE-BST-004` | `spec.engine.source` invalid / unsupported for this build |
| `CUBE-BST-005` | inventory record write failed |

`spec.engine` *document* validation errors are config-domain
`CUBE-CFG-*`/`field.ErrorList` at load time — codes are never re-tagged
across domains.

## Testing

Hermetic gate tests drive the SSA + wait machinery against a **fake
dynamic client** (apply outcomes and status transitions scripted as
table rows — timeout, not-ready-then-ready, apply-conflict) — no live
cluster, no Docker. The real Flux round-trip (install → kind-set ready
against a kind cluster, worktree-local KUBECONFIG per CLAUDE.md §7) runs
only behind `make test-e2e` and is never part of the green gate.

## CLI surface

`cube-idp bootstrap` — cobra wiring only: load config → build the kube
client → inject `dynamic.Interface` + `meta.RESTMapper` into
`internal/bootstrap`. The verb makes the product demo-able (up →
gitops-managed cluster). The `apply` verb reserved on 2026-08-03 is
superseded and stays retired.

## Contracts for future domains

- **M8 (pack)** delivers packs by writing to the Flux **source** that
  bootstrap wired — *not* through a cube-idp applier. Pack conforms to the
  live Flux loop.
- **M9 (engine)** formalizes the engine driver seam and re-expresses Flux
  as a conforming pack; it consumes the same `spec.engine` sub-struct and
  may migrate its shape (design-gate event). Engine-CR readiness lands here.
- **M11 (`down`)** reads the bootstrap inventory to tear down what
  bootstrap installed, composed with the M5 cluster teardown.
- Not in this domain, ever on the current horizon: watch
  machinery/informers, controller-runtime, kstatus/`cli-utils`, typed
  workload clientsets, a second-engine driver seam (M9), engine-CR
  readiness, arbitrary user-manifest apply (that is the engine's job).
