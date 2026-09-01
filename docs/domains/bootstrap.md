# Domain: bootstrap

Living contract of the bootstrap domain (`internal/bootstrap`).
Cross-cutting rules:
`docs/ARCHITECTURE.md`. Originating design gate: `docs/DECISIONS.md`
2026-08-06 (M7, epic #92); narrowed to engine-agnostic machinery by the
two-tier M10 gate, 2026-08-24 (epic #152; the two-tier model itself lives
in `docs/domains/engine.md`); generalized to carry ordered prerequisite
units by the M11 gate, 2026-08-27, shipped in M11-C5 (PR #195).

## Purpose

`internal/bootstrap` is the **engine-agnostic micro-bootstrap
machinery**: it SSA-applies the **injected** tier-1 substrate objects,
then the **injected** ordered prerequisite units, then the **injected**
driver sync wiring, executing the readiness wait each step declares,
re-recording a **bootstrap inventory** (the seed of a
future `down`) into the **injected** substrate namespace as the owned
set grows, and stops.
It embeds nothing and knows no engine — nor any pack, gateway, or CA.
The substrate home
(`internal/engine/substrate`) and the selected tier-2 driver supply
engine content, the edge resolves prerequisite content from
`spec.prerequisites`, and everything reaches this domain as object
slices plus function values. Steady-state ownership of
every pack and manifest is the engine's from handover on — **no-engine
operation is not a supported mode**.

## Config surface

Since M10 this domain has **no config surface of its own**, and M11 did
not give it one: `spec.engine`
belongs to the engine domain's contract (`docs/domains/engine.md`, which
carries the `EngineSpec`/`EngineSource` shapes and their validation) and
`spec.prerequisites` to the gateway's (`docs/domains/gateway.md`), while
bootstrap does not import `api/config` at all — the edge reads the spec,
selects the driver, resolves the prerequisite list, and hands bootstrap
only objects, predicates, and a namespace string. That the prerequisite
list became configuration in M11 changed the **edge**, not this
contract: bootstrap still never reads a `Config` and still cannot tell
a defaulted list from a user-written one.

## The injection contract

`bootstrap` receives, at the CLI/orchestrator edge:

- **client-go interface values** — a `dynamic.Interface` and a
  `meta.RESTMapper` — constructed by `internal/kube`;
- the **inventory namespace** as a string (`NewApplier(dyn, mapper,
  inventoryNamespace)`), sourced from the substrate's invariant
  namespace fact — bootstrap records where it is told;
- the whole content of one run as a single injected struct,
  `EngineInstall{Substrate []*unstructured.Unstructured; Prerequisites
  []Unit; Wiring []*unstructured.Unstructured; Wait EngineWait}` — the
  entry-point shape the M11 gate left to implementation, resolved to
  `InstallEngine(ctx, EngineInstall)`;
- the driver's judgment and declared bundle as an
  `EngineWait{Reconciled ReconciledFunc; EngineObjects
  []*unstructured.Unstructured}` — function values and neutral
  vocabulary cross the edge; no engine type does.

**A `Unit` is the prerequisite vocabulary, and it carries no exported
fields** — it is built only through its three constructors, so its
declared flavor can never be edited after the fact:

- `NewRawUnit(name, objects)` — waited with the **kind-set** machinery;
- `NewCRUnit(name, objects, reconciled)` — waited with the
  **reconciliation** machinery under the injected predicate;
- `NewInertUnit(name, objects)` — **no post-apply wait at all**.

Which wait runs is dispatched on the flavor the constructor declared,
**never inferred from the objects' actual kinds**. That is deliberate:
inference would make a unit's readiness semantics a function of its
content, so adding an object could silently change how the unit is
judged. The same principle explains the flavors carrying names — a
timeout names the unit that failed, not just the object set.

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

**Prerequisite units reuse those two waits — there is no third.** A
**raw** unit takes the phase-1 kind-set wait (so a Namespace must be
`Active` and a CRD `Established`, while objects outside the kind-set —
a Service, say — are ignored by design). A **CR** unit takes the
phase-2 reconciliation wait under the predicate the edge injected. An
**inert** unit declares no post-apply wait at all: **apply success is
its readiness.** That last flavor names semantics rather than adding
machinery — the kind-set wait already ignored objects outside its
filter, so a status-less Secret was always done the moment its apply
succeeded. Making it an explicit flavor is what stops a later reader
from "fixing" the absent wait, which is exactly the kind of
well-intentioned repair that would hang a bootstrap forever on an
object that has no status to reach.

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

`Applier.InstallEngine(ctx, EngineInstall)` sequences the whole
bootstrap in the one order that is correct and recoverable.

**Before any of it, the units are checked.** A composition defect —
today, a CR unit built with a nil judge — is caught pre-flight and
fails as `CUBE-BST-010`, ahead of even the substrate apply. That
defect is dangerous precisely because it would otherwise **pass
silently**: a nil predicate makes the reconciliation wait skip, so the
unit would count as ready the moment its apply succeeded, and a cube
would report success over content that never reconciled. Checking
ahead of everything also means a defective run **installs nothing at
all**. It is a defect at the CLI edge rather than a cluster condition,
which is why it is coded where it is found and never surfaces later as
a timeout. Then, in order:

1. apply the injected substrate objects,
2. record the inventory (an applied-but-not-yet-ready install is already
   visible to `down`; an apply failure mid-stream returns before this
   step — resolving that gap is an up/down-milestone (M13) design
   question),
3. wait the bootstrap kind-set (phase 1 — this establishes the CRDs the
   prerequisites and the wiring need),
4. **install each prerequisite unit, in list order.** Per unit:
   re-record the inventory with the unit's objects included, **then**
   apply them, **then** run the wait that unit's flavor declares —
   and only then move to the next unit. Record-before-apply extends to
   prerequisites exactly as it governs the wiring, and the per-unit
   wait is what guarantees CRDs are `Established`, and a target
   namespace `Active`, before any later member needs them,
5. when wiring exists: re-record the inventory with the wiring
   included — **before** it is applied, so a half-applied sync is
   already visible to `down`,
6. apply the wiring, then wait it reconciled (phase 2),
7. wait the declared engine objects reconciled (phase 3 — a no-op when
   the driver declares none, the flux case).

The order is the edge's declaration, not a graph this domain solves:
bootstrap installs the list it is handed, in the sequence it is handed,
and has no notion of a dependency between units. Nothing about
prerequisites is a new *phase*, which is why step 4 carries no phase
number of its own: each unit borrows phase 1's or phase 2's wait
according to its declared flavor, or waits not at all.

What the wiring looks like — and whether any exists — is the driver's
business, decided at the edge; the version assertion (`CUBE-ENG-005`)
also happened there, before any cluster contact.

## Inventory (seed of `down`)

`bootstrap` records what **it** applied — substrate, every prerequisite
unit, and the sync wiring — as a
ConfigMap in the injected namespace, so a future `down` can find and
remove it. In-domain and self-contained — **not** a reusable applier
seam.

**Every re-record is cumulative for the whole run.** Each record covers
everything applied so far, so the final one includes every prerequisite
unit and no unit ever drops out of the deletion seed. The record call
itself is deliberately dumb — a plain SSA apply of a ConfigMap built
from whatever slice it is handed, with overwrite semantics — and the
cumulative property lives in the callers, which thread an ever-growing
applied set through the sequence. Keeping accumulation in the sequencing
rather than inside the recorder is what makes "record before the apply
it covers" checkable by reading one function. Object references are
sorted by namespace, kind, and name before encoding, so the recorded
document is byte-deterministic across runs.

Content delivered through the tier-1 source (a future engine
bundle, every ordinary pack) is **deliberately not** in this inventory:
it lives under the `prune: true` sync, so its teardown is source-level.
The `down` composition order — (a) source-level teardown, (b) tier-1
teardown from the inventory, (c) cluster teardown — is a published
**requirement on the up/down milestone (M13)**, its real semantics owed
at that gate.

**Two further inventory questions are published to that same gate**,
because M11 multiplied both the number of record events and the
likelihood of the content changing between runs:

1. **Replacement semantics across re-bootstraps.** Recording overwrites
   with a fresh list, so a unit renamed or dropped between bootstraps —
   and a gateway content swap is exactly that — vanishes from the
   inventory while staying live in the cluster. Union, prune, and
   orphan semantics are M13's to settle; until they are, M12 (which may
   vary prerequisite content) must preserve inventory continuity.
2. **Deletion ordering.** With `gateway-system` inventoried alongside
   the Secrets and CRs inside it, `down` must delete namespaced
   dependents before their Namespace. The inventory's sort is
   deterministic but **not dependency-aware**, and deliberately so — it
   exists to make the document reproducible, not to encode a teardown
   order that only the deleting milestone can define.

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
| `CUBE-BST-005` | a kind-set readiness wait timed out — **any** kind-set wait bootstrap executes, the substrate's or a prerequisite unit's; names the pending objects |
| `CUBE-BST-006` | inventory encode failed before recording |
| `CUBE-BST-007` | SUPERSEDED by `CUBE-ENG-006` (M10: the wiring shapes and their source-kind check moved to the flux driver) |
| `CUBE-BST-008` | SUPERSEDED by `CUBE-ENG-005` (M10: the version pin moved to the substrate; asserted at the edge) |
| `CUBE-BST-009` | a reconciliation wait timed out — **any** wait bootstrap executes over declared objects: the applied sync wiring, the declared engine content, or a prerequisite unit; names the pending objects with the judgment's reasons |
| `CUBE-BST-010` | readiness polling failed on a permanent error, **or** a unit was composed with no judge (caught pre-flight); wrapped cause, coded at the failure point, never retagged as a timeout |

**M11 generalized wording, not numbers.** The gate left one question
open — whether a permanent poll failure during a kind-set wait should
route to `-010` or be recorded as covered by `-005`, whose text says
*timeout*. It shipped as the former: a single shared terminal-failure
coder serves both wait paths, so a permanent failure is `-010` on
either, and `-005`/`-009` mean what they say. The engine-flavored
phrasing the M10 codes carried ("engine resources", "check the
configured source") was generalized in the same change; the
pending-versus-terminal classification and the pass-through rule for
already-coded judgment errors are unchanged.

The retained codes' summaries/remediations are engine-neutral (they name
the injected namespace and generic "engine controllers"); superseded
numbers stay declared as tombstones. `spec.engine` and
`spec.prerequisites` *document* validation
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
applier with the substrate namespace → **ensure the CA material** (the
first cluster read) → **resolve the prerequisite list into units** →
`InstallEngine` under
`--timeout` → **finish**: splice CoreDNS, sync the `ca.crt` artifact,
report.

Success output names tier 1 and then one line per fabric the resolved
list actually carried: `flux 2.9.2 installed — syncing
from <url> (<kind>)` (or without the suffix when no source is
configured); `gateway installed — *.<domain> routed to
<stable Service FQDN>` when the gateway fabric was spliced — the line
deliberately does **not** say "ready", and carries no URL, because a
truthful one needs the host port and the edge cannot know it; and
`cube CA written to <path>` when the CA artifact was synced.

Flags: `--kubeconfig`, `--kubeconfig-context-name`,
`--timeout` — **default 10m**, the total readiness budget. It was 5m
through M10 and was raised in M11 for a stated reason rather than a
comfort margin: the run is now network-dependent *inside* the cluster,
because the helm-controller pulls the pinned gateway chart from its OCI
registry during the gateway unit's wait, on a cluster still warming its
images. The edge's own two round-trips — the CA read and the CoreDNS
read-modify-write — are bounded separately at 30s each and are
explicitly **not** drawn from `--timeout`: a readiness budget nearly
spent by waiting should not cause an edge round-trip to fail for want
of time rather than for a fault. The `apply` verb
reserved on 2026-08-03 stays retired.

**Two edge behaviors are M11's, and neither is bootstrap machinery.**
(1) The **CA-reuse read**: the edge reads the existing CA Secret with
the dynamic client it already constructs and passes any key material to
the `ca` domain's pure ensure function — the exported machinery here
has no read operation and deliberately does not grow one
(`docs/domains/ca.md`). (2) The **CoreDNS read-modify-write**: the edge
splices the gateway's marker block under bounded optimistic
concurrency, after `InstallEngine` returns and before success is
reported. The ConfigMap is **never inventory-recorded** — the inventory
is a deletion seed and must never name a system object — and its
restore-not-delete teardown is a published M13 requirement
(`docs/domains/gateway.md`).

## Contracts for future domains

- **Pack delivery (M8 renders, the bus delivers)**: the pack domain (M8,
  shipped) renders and never applies; packs reach a cluster by being
  written into the source the sync wiring established (the M12 bus) —
  *not* through a cube-idp applier.
- **M11 (gateway + ca)** shipped against this contract without adding
  to it: the ordered prerequisite units above carry the gateway's
  packs and the CA material, and the machinery gained unit flavors and
  cumulative inventory but no config surface, no new code number, and
  no knowledge of what a prerequisite contains — living contracts
  `docs/domains/gateway.md` and `docs/domains/ca.md`.
- **M13 (`down`)** reads the bootstrap inventory to tear down what
  bootstrap installed, composed with the M5 cluster teardown and the
  source-level teardown phase recorded above.
- Not in this domain, ever on the current horizon: watch
  machinery/informers, controller-runtime, kstatus/`cli-utils`, typed
  workload clientsets, a second applier, engine selection (the edge's),
  arbitrary user-manifest apply (that is the engine's job).
