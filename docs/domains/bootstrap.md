# Domain: bootstrap

Living contract of the bootstrap domain (`internal/bootstrap`).
Cross-cutting rules:
`docs/ARCHITECTURE.md`. Originating design gate: `docs/DECISIONS.md`
2026-08-06 (M7, epic #92); narrowed to engine-agnostic machinery by the
two-tier M10 gate, 2026-08-24 (epic #152; the two-tier model itself lives
in `docs/domains/engine.md`).

## Purpose

`internal/bootstrap` is the **engine-agnostic micro-bootstrap
machinery**: it SSA-applies the **injected** tier-1 substrate objects and
the **injected** driver sync wiring, executes the **three-phase
readiness** below, records a **bootstrap inventory** (the seed of a
future `down`) into the **injected** substrate namespace, and stops.
It embeds nothing and knows no engine — the substrate home
(`internal/engine/substrate`) and the selected tier-2 driver supply
content; the CLI/orchestrator edge composes. Steady-state ownership of
every pack and manifest is the engine's from handover on — **no-engine
operation is not a supported mode**.

## Config surface

Since M10 this domain has **no config surface of its own**: `spec.engine`
belongs to the engine domain's contract (`docs/domains/engine.md`, which
carries the `EngineSpec`/`EngineSource` shapes and their validation), and
bootstrap does not import `api/config` at all — the edge reads the spec,
selects the driver, and hands bootstrap only objects, predicates, and a
namespace string.

## The injection contract

`bootstrap` receives, at the CLI/orchestrator edge:

- **client-go interface values** — a `dynamic.Interface` and a
  `meta.RESTMapper` — constructed by `internal/kube`;
- the **inventory namespace** as a string (`NewApplier(dyn, mapper,
  inventoryNamespace)`), sourced from the substrate's invariant
  namespace fact — bootstrap records where it is told;
- the **substrate objects** and the **driver sync wiring** as
  `[]*unstructured.Unstructured` parameters to `InstallEngine`;
- the driver's judgment and declared bundle as an
  `EngineWait{Reconciled ReconciledFunc; EngineObjects
  []*unstructured.Unstructured}` — function values and neutral
  vocabulary cross the edge; no engine type does.

It **never imports `internal/kube` or `internal/engine`** (domains never
import each other; the kube contract sanctions consumers referencing
client-go's stable interface types in signatures). It never reads files
and never constructs clients itself. Composition — reading config,
building the kube client, reading the substrate, selecting the driver,
injecting everything — lives in `internal/cli`.

One capability expectation rides on the injected mapper and is contract,
not implementation detail: bootstrap expects a **resettable
discovery-cached mapper**. Because it installs CRDs and then applies CRs
of those CRDs, it asserts the narrow consumer-side
`resettableRESTMapper` interface (`Reset()`, declared in `apply.go`
where it is consumed) to invalidate the discovery cache. On the **apply
path** the reset-and-retry is one-shot per mapping miss; on the **wait
path** discovery is re-consulted on every poll (see the pending
semantics below). The memory-cached `RESTMapper` `internal/kube`
constructs satisfies the capability; a mapper without it degrades
loudly, not silently — the retry is skipped and the miss surfaces as
`CUBE-BST-003`.

## SSA + three-phase readiness (hand-rolled on client-go)

Server-side apply (field manager `cube-idp`) and the readiness waits are
hand-rolled on the injected client-go interfaces — no `fluxcd/pkg/ssa`,
no kstatus/`cli-utils`, no controller-runtime (measured rejection,
DECISIONS 2026-08-06). Predicates read off `unstructured` status. All
phases share the CLI's `--timeout` as **one total budget**, never per
phase.

**Phase 1 — the kind-set wait**, over what bootstrap's SSA applied:

| Kind | Ready when |
|---|---|
| CustomResourceDefinition | `Established` condition true |
| Deployment / StatefulSet | observed generation rolled out, replicas available |
| Job | `Complete` condition true |
| Namespace | phase `Active` |

The set is **by kind, not by name**, and that is load-bearing: a new
component in the substrate payload joins the wait with no code change
here (M9's helm-controller proved it).

**Phase 2 — the reconciliation wait**, over the driver's sync wiring,
polling each object and judging it with the injected `ReconciledFunc`
(for flux: `Ready` condition true **and** `status.observedGeneration`
equal to `metadata.generation` — stale success never counts).

**Phase 3 — the engine-readiness wait**, over the driver's declared
`EngineWait.EngineObjects` — content bootstrap did **not** apply (it
arrives through the tier-1 source), polled by declared identity with the
same machinery. **Empty and skipped for flux** (the degenerate driver);
the phase exists so a second driver's gate fills it without new seam
surface.

**Transient discovery is pending, never terminal, in phases 2–3.**
Declared content may not exist yet by design, so *no REST mapping yet*
("kind not served by the cluster yet") and *NotFound* ("object not
created yet") are pending states retried until the shared deadline;
anything else — a read the API server rejected for a reason waiting
cannot fix, or a judgment failure carrying no code of its own — fails
immediately as `CUBE-BST-010`, coded at the point of failure. A judgment
error already carrying a `*cubeerr.Coded` (an `ENG`-coded predicate
error) passes through untouched — codes are never re-tagged, machinery
included. Timeouts: phase 1 → `CUBE-BST-005` (kind-set rollout polling);
phases 2–3 → `CUBE-BST-009`, naming the pending objects **with the
driver's pending reasons** (that is what the seam's `Reconciled` reason
string is for).

## The install sequence

*(This section describes the shipped M10 machinery; the M11 amendment
below inserts the ordered prerequisite units between steps 3 and 4 and
folds in at the M11 closeout.)*

`Applier.InstallEngine(ctx, substrateObjs, wiringObjs, engineWait)`
sequences the whole bootstrap in the one order that is correct and
recoverable:

1. apply the injected substrate objects,
2. record the inventory (an applied-but-not-yet-ready install is already
   visible to `down`; an apply failure mid-stream returns before this
   step — resolving that gap is an up/down-milestone (M13) design
   question),
3. wait the bootstrap kind-set (phase 1 — this establishes the CRDs the
   wiring needs),
4. when wiring exists: re-record the inventory with the wiring included —
   **before** it is applied, so a half-applied sync is already visible
   to `down`,
5. apply the wiring, then wait it reconciled (phase 2),
6. wait the declared engine objects reconciled (phase 3 — a no-op when
   the driver declares none, the flux case).

What the wiring looks like — and whether any exists — is the driver's
business, decided at the edge; the version assertion (`CUBE-ENG-005`)
also happened there, before any cluster contact.

## Inventory (seed of `down`)

`bootstrap` records what **it** applied — substrate + sync wiring — as a
ConfigMap in the injected namespace, so a future `down` can find and
remove it. In-domain and self-contained — **not** a reusable applier
seam. Content delivered through the tier-1 source (a future engine
bundle, every ordinary pack) is **deliberately not** in this inventory:
it lives under the `prune: true` sync, so its teardown is source-level.
The `down` composition order — (a) source-level teardown, (b) tier-1
teardown from the inventory, (c) cluster teardown — is a published
**requirement on the up/down milestone (M13)**, its real semantics owed
at that gate.

## Interface doctrine applied

Consumer-side (Kind A) doctrine governs this domain: the CLI edge
injects concrete client-go interfaces, content slices, and function
values; `bootstrap` defines the narrow interfaces it needs where it uses
them (`cluster`, `resettableRESTMapper`), mocked with hand-rolled
function-field structs. The Kind-B seam this machinery serves —
`engine.Provider` — lives in `internal/engine`, not here; bootstrap
executes, it never selects.

## Error codes (`CUBE-BST-*`, exit 1)

| Code | Meaning |
|---|---|
| `CUBE-BST-001` | SUPERSEDED by `CUBE-ENG-003` (M10: provenance moved with the substrate; number never reused) |
| `CUBE-BST-002` | SUPERSEDED by `CUBE-ENG-004` (M10: payload parse moved with the substrate) |
| `CUBE-BST-003` | no REST mapping for an object's kind (apply path, even after a discovery refresh) |
| `CUBE-BST-004` | server-side apply of a bootstrap object failed (wrapped cause) |
| `CUBE-BST-005` | kind-set readiness wait timed out (phase 1 only; names the pending objects) — *text generalizes to any kind-set wait under the M11 amendment below* |
| `CUBE-BST-006` | inventory encode failed before recording |
| `CUBE-BST-007` | SUPERSEDED by `CUBE-ENG-006` (M10: the wiring shapes and their source-kind check moved to the flux driver) |
| `CUBE-BST-008` | SUPERSEDED by `CUBE-ENG-005` (M10: the version pin moved to the substrate; asserted at the edge) |
| `CUBE-BST-009` | reconciliation/engine wait timed out (phases 2–3; names the pending objects with the driver's reasons) — *text generalizes to any declared-object reconciliation wait under the M11 amendment below* |
| `CUBE-BST-010` | readiness polling failed on a permanent error (wrapped cause; coded at the failure point, never retagged as a timeout) |

The retained codes' summaries/remediations are engine-neutral (they name
the injected namespace and generic "engine controllers"); superseded
numbers stay declared as tombstones. `spec.engine` *document* validation
errors are config-domain `CUBE-CFG-*`/`field.ErrorList` at load time —
codes are never re-tagged across domains.

## Testing

Hermetic gate tests drive the apply/wait/reconcile/inventory machinery
against a **hand-rolled function-field fake** of the narrow `cluster`
seam (the client-go fake dynamic client cannot model server-side apply
on unstructured objects); the real GVK→resource scope resolution and the
mapper reset behavior are covered against the client-go dynamic fake;
the pending-vs-terminal classification has first-class rows. No live
cluster, no Docker. The real round-trip (substrate install → kind-set
ready → wiring applied → `GitRepository` reconciled `Ready` against a
kind cluster and a public git source, worktree-local KUBECONFIG per
CLAUDE.md §7) runs only behind `make test-e2e` and is never part of the
green gate.

## CLI surface

`cube-idp bootstrap` — cobra wiring only: load config → assert
`spec.engine.version` against the substrate (`CUBE-ENG-005`, before any
cluster contact) → read the substrate objects → select the tier-2 driver
via the injected engine-driver factory (`CUBE-ENG-001` when none) and
collect its wiring + `EngineWait` → resolve the cube's kubeconfig target
+ context via `cluster.Status` → build the kube client → construct the
applier with the substrate namespace → `InstallEngine` under
`--timeout`. Success output names tier 1: `flux 2.9.2 installed — syncing
from <url> (<kind>)` (or without the suffix when no source is
configured). Flags: `--kubeconfig`, `--kubeconfig-context-name`,
`--timeout` (default 5m, the total readiness budget). The `apply` verb
reserved on 2026-08-03 stays retired.

## M11 amendment (gated 2026-08-27, ahead of code): the ordered prerequisite list

Delimited amendment from the M11 design gate (`docs/DECISIONS.md`
2026-08-27); it folds into the living body at the M11 closeout, and no
code implements it before the M11 breakdown is aligned.

- **The install sequencing generalizes.** Between phase 1 (substrate
  kind-set ready) and the wiring steps, the applier executes an
  **ordered list of prerequisite units** — in M11, derived at the edge
  from the gateway domain's namespace object and prerequisite packs
  and the CA Secrets, in this order: the `gateway-namespace` Namespace
  unit → the `gateway-api-crds` pack → the CA-material inert unit →
  the `traefik-gateway` thin-helm pack (`docs/domains/gateway.md`,
  `docs/domains/ca.md`). Per unit, in list
  order: **re-record the inventory with the unit included, then apply,
  then wait it ready before the next unit applies** — record-before-
  apply extends to prerequisites exactly as it governs the wiring, and
  the per-unit wait is what guarantees CRDs are `Established` — and
  the target namespace `Active` — before
  any dependent list member needs them. **Every re-record is
  cumulative for the whole run**: each record covers everything
  applied so far, and the final wiring record includes every
  prerequisite unit — no unit ever drops from the deletion seed.
- **Existing machinery, no new phase concept — three unit flavors.**
  A **raw** unit (the `gateway-namespace` Namespace object, the
  Gateway API CRDs pack) is waited with the kind-set machinery
  (Namespace `Active`, CRD `Established`); a **CR** unit (the
  thin-helm gateway pair) is waited with the reconciliation machinery
  under injected predicates; an **inert** unit (the CA Secrets —
  status-less objects) declares no post-apply judge: **apply success
  is its readiness.** The inert flavor names semantics, not new
  machinery — the kind-set wait already ignores objects outside its
  filter, so an SSA'd Secret is done when the apply succeeds, and a
  later reader must not "fix" the skip. Everything crosses the edge
  as the established
  neutral vocabulary — object slices plus judge function values;
  bootstrap still knows no packs, no gateway, no CA. All waits still
  share the CLI's one `--timeout` as a total budget (the breakdown
  sanity-checks the 5m default against a cold cluster's in-cluster
  OCI chart pull — the day-0 network dependence is new, D1). The
  exact entry-
  point signature fixes at implementation within this contract (the
  M10 precedent).
- **Wait-code contract text generalizes; numbers and mechanics do
  not.** `CUBE-BST-005` reads as *any* kind-set readiness wait timing
  out (no longer "phase 1 only"); `CUBE-BST-009`/`-010` read as **a
  bootstrap-executed reconciliation wait over declared objects** —
  engine or prerequisite alike — with diagnostics naming the object
  set and the injected predicates' pending reasons. The engine-
  flavored wording ("engine resources", "check the configured
  source") is deliberately generalized at this gate; the pending-vs-
  terminal classification and the pass-through rule are unchanged.
  Two named breakdown items ride on this: (a) the generalized wording
  also lives in compiled `errors.go` message/remediation strings — the
  small string change is owned explicitly at the closeout fold-in,
  not discovered; (b) the kind-set wait has no `CUBE-BST-010`
  analogue today (an uncoded permanent poll failure during `WaitReady`
  is wrapped as `BST-005`, whose text says timeout) — pre-existing
  behavior, but generalizing raw waits to more units broadens the
  exposure, so the breakdown either routes permanent kind-set poll
  failures to the `-010` failed-at-point classification or records on
  the contract that `-005` covers them.
- **Two new edge behaviors are named — neither is bootstrap
  machinery.** (1) The **CA-reuse read**: the edge reads the existing
  CA Secret with the dynamic client it already constructs and passes
  key material to the `ca` domain's pure ensure function — the
  exported machinery has no read operation and deliberately does not
  grow one. (2) The **CoreDNS read-modify-write**: the edge splices
  the gateway's marker block under optimistic concurrency; the
  ConfigMap is **never inventory-recorded** (the inventory is a
  deletion seed and must never name a system object), and its
  restore-not-delete teardown is a published M13 requirement.
- **Two inventory questions are published to the M13 gate** (M11
  multiplies both record events and content-swap likelihood):
  (1) **replacement semantics across re-bootstraps** —
  `RecordInventory` overwrites with a fresh list, so a unit renamed or
  dropped between bootstraps (a gateway content swap is exactly that)
  vanishes from the inventory while staying live; union/prune/orphan
  semantics are M13's, and M12 (which may vary prerequisite content)
  must preserve inventory continuity until they are settled;
  (2) **deletion ordering** — with `gateway-system` inventoried
  alongside its Secrets and CRs, `down` must delete namespaced
  dependents before their Namespace; the inventory's sort is
  deterministic, not dependency-aware.

## Contracts for future domains

- **Pack delivery (M8 renders, the bus delivers)**: the pack domain (M8,
  shipped) renders and never applies; packs reach a cluster by being
  written into the source the sync wiring established (the M12 bus) —
  *not* through a cube-idp applier.
- **M11 (gateway + ca)** is gated (2026-08-27): the ordered
  prerequisite list above, the gateway's packs, and the CA material —
  living contracts `docs/domains/gateway.md` and `docs/domains/ca.md`.
- **M13 (`down`)** reads the bootstrap inventory to tear down what
  bootstrap installed, composed with the M5 cluster teardown and the
  source-level teardown phase recorded above.
- Not in this domain, ever on the current horizon: watch
  machinery/informers, controller-runtime, kstatus/`cli-utils`, typed
  workload clientsets, a second applier, engine selection (the edge's),
  arbitrary user-manifest apply (that is the engine's job).
