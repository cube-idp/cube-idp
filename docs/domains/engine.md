# Domain: engine

Living contract of the engine domain (`internal/engine` + the flux
driver). Cross-cutting rules: `docs/ARCHITECTURE.md`. Originating design
gate: `docs/DECISIONS.md` 2026-08-24 (M10, epic #152). **Gated ahead of
code**: everything here describes the M10 target contract, approved before
implementation, exactly as the M8 and M9 gates did for `pack`.

## Purpose

Formalize what a gitops **engine** *is* — the component that owns steady
state after bootstrap hands over — as the repo's second Kind-B driver
seam, and re-express Flux as a **conforming pack**. The domain answers
three questions the rest of the system needs answered per engine, and
nothing else:

1. **What installs it, and where it lives** — the pinned install content
   (for Flux: the embedded engine pack) and its install namespace, the
   placement fact bootstrap's inventory needs.
2. **What wires its sync** — the source + sync CRs derived from
   `spec.engine.source`.
3. **What "reconciled" means** — per-kind readiness predicates over the
   engine's own CRs.

The engine domain **supplies content and predicates; it never applies and
never waits**. Applying and waiting stay in `internal/bootstrap`'s
machinery, which M10 narrows to being engine-agnostic; composition stays
at the CLI/orchestrator edge. Delivery of packs *through* the engine is
M11's contract, not this domain's.

## The driver seam (Kind B)

`spec.engine.provider` selects the driver, mirroring
`spec.cluster.provider` → `cluster.Provisioner`. Contract-level shape
(exact signatures are fixed at implementation within this contract, per
the house rules; every export carries a doc comment):

```go
// Provider is the gitops-engine driver seam. It is PURE: every method
// returns data or judges data; no method performs I/O or touches a
// cluster. Applying objects and polling status belong to the caller
// (bootstrap machinery, composed at the CLI edge).
type Provider interface {
    // InstallObjects returns the engine's pinned install content as
    // parsed objects, in apply order. For flux: the rendered payload of
    // the embedded engine pack. A spec.engine.version that contradicts
    // the driver's pinned version is a coded error.
    InstallObjects(ctx context.Context, spec v1alpha1.EngineSpec) ([]*unstructured.Unstructured, error)

    // SourceObjects returns the source + sync CRs expressing
    // spec.engine.source, or nil when no source is configured.
    SourceObjects(ctx context.Context, spec v1alpha1.EngineSpec) ([]*unstructured.Unstructured, error)

    // Reconciled judges one applied source/sync object's status:
    // reconciled, not yet (with a human-readable reason — it feeds the
    // reconciliation-wait timeout diagnostics, CUBE-BST-009), or a coded
    // error for an object the driver does not recognize. Pure — it reads
    // the unstructured status it is handed, it never fetches.
    Reconciled(obj *unstructured.Unstructured) (bool, string, error)

    // InstallNamespace names the namespace the driver's install content
    // lives in — where the source/sync CRs land and where bootstrap
    // records its inventory. Pure and constant per driver (flux:
    // "flux-system"); it is the engine-neutral channel through which
    // the edge supplies inventory placement to bootstrap.
    InstallNamespace() string
}
```

Optional capability, beside the seam (the `SpecValidator` pattern from
cluster, M4):

```go
// SpecValidator is implemented by drivers that can validate the
// engine spec without any cluster. Pure: no I/O, no side effects.
type SpecValidator interface {
    ValidateSpec(spec v1alpha1.EngineSpec) error
}
```

Why the seam is pure, stated once: the interface doctrine (§4) says
"interfaces stay pure where possible (return objects; the caller
applies)", and here it is entirely possible — the only I/O an engine
needs (SSA, polling) is machinery bootstrap already owns and that must
not be duplicated (the no-second-applier rule, 2026-08-06). Purity also
collapses the conformance problem: a pure seam needs **no fake and no
cluster** to be tested — the green gate runs the suite against the real
flux driver (see Testing).

**What deliberately does NOT cross the seam** (each with the reason):

- **Apply/wait machinery** — bootstrap's; a second applier was rejected
  at the M7 gate and stays rejected.
- **The bootstrap inventory** — bootstrap's, the seed of M12 `down`.
- **A delivery-target descriptor** ("where rendered packs get written") —
  M11's contract. M10 commits only this reservation: every fact needed to
  locate the delivery target (URL, ref, path) is already fully derivable
  from `spec.engine.source` in `api/`, and the seam must never make
  engine-domain private state necessary for locating it — so M11 can land
  without reopening the seam.
- **Credential material** — #142's. The hook point is reserved, not
  implemented: a future `secretRef` lands as an additive field on
  `EngineSource` (api/) and flows through `SourceObjects` unchanged,
  because the method already receives the whole spec. The same #142 gate
  designs the pack domain's helm source CRs; the two emitters are decided
  together, once.

## The flux driver (`internal/engine/flux`)

The driver subpackage owns everything Flux-specific that `internal/
bootstrap` owns today:

- **The embedded engine pack.** The vendored `flux install --export`
  asset moves from `internal/bootstrap/assets/` to the driver, re-homed
  as an embedded **pack directory** — `pack.cue` (`name: "flux"`,
  `version` = the pinned Flux version, `type: raw`,
  `category: "engine"`, no `#Values`, no `namespace`) with the manifests
  as its `manifests/` payload. **Version spelling is clean SemVer**: the
  pack's `version` is `2.9.2`, never the upstream `v`-prefixed tag
  spelling (the repo versioning rule), and `spec.engine.version` accepts
  the clean spelling; the driver alone maps to and from `v2.9.2` where
  the vendored asset or upstream tags require it, inside its superseding
  version check. The pin discipline is unchanged: version
  constant + recorded sha256 + `make flux-manifests` regeneration, never
  fetched at runtime. The asset remains **data, not a dependency**.
- **Source/sync CR emission** — the `GitRepository`/`OCIRepository` +
  `Kustomization` shapes bootstrap emits today, moved verbatim; the
  git|oci discrimination and its validation stay in `api/` (unchanged).
- **Reconciled predicates** — for all three kinds (`GitRepository`/
  `OCIRepository`/`Kustomization`): `Ready` condition true **and**
  `status.observedGeneration` equal to `metadata.generation` — a stale
  `Ready` from before a spec change must not count as reconciled. Read
  off `unstructured` status, same style as bootstrap's kind-set
  predicates; no kstatus, no controller-runtime.

The driver imports `internal/cubeerr`, `api/config`, apimachinery, and
the stdlib. It does **not** import `internal/pack` (domains never import
each other): a `type: raw` pack with no `#Values` and no `namespace`
renders as "parse the manifests in sorted order", which is precisely the
parsing the bootstrap asset already gets — the driver does that itself.

**"Conforming pack" is enforced, not asserted.** A green-gate test at the
composition edge (which legitimately imports both domains) loads the
driver's embedded directory through `internal/pack` and asserts
`pack.Load` + `Render` succeed and yield exactly the driver's
`InstallObjects` — **deep equality of the ordered object lists after
both parse paths**, not membership or byte comparison, so the raw-render
semantics (lexical file order, `.yaml|.yml` filtering, empty-document
skipping, empty-render rejection) are all in scope. It is the contract's
first serious dogfood: `Load`, the payload check, raw rendering,
determinism, and **building** the bundled-CRD scope index over the real
Flux payload (the pack declares no `namespace`, so no scope answer
affects an object — the index is exercised, not its decisions). If the
pack contract and the driver ever disagree, the gate breaks; neither
side may drift silently.

**What the engine pack is not.** It carries no instance state — the
source/sync CRs are config-derived and driver-emitted, never part of the
pack — per the pack/instance boundary (M9): a pack that cannot be handed
to a different operator unchanged is carrying instance state. And in M10
it is **not delivered through the engine**: cube-idp bootstraps the
engine itself, the one sanctioned direct apply, because a circular
"engine delivers the engine" day-0 path cannot exist. Whether the running
engine later reconciles its own upgrades (the Flux pack written into the
source) is an M11+ question, explicitly left open.

## What M10 changes in bootstrap

Recorded here because the seam defines the split; the bootstrap contract
carries the mirror text (`docs/domains/bootstrap.md`, M10 section):

- `internal/bootstrap` keeps the **engine-agnostic machinery**: SSA
  apply, the by-kind bootstrap kind-set wait, the inventory, and the
  install sequencing — and now also executes the **reconciliation wait**
  after the source CRs are applied, polling with **injected** driver
  predicates (function values cross the edge; no import). Its timeout is
  a new machinery code, `CUBE-BST-009`, distinct from the kind-set wait's
  `-005`, carrying the pending objects **and the driver's pending
  reasons** — which is where `Reconciled`'s reason string goes. The
  wait-code pass-through rule applies unchanged: an already-coded cause
  (including an `ENG`-coded predicate error) keeps its code; the wait
  code wraps only a deadline with objects still pending. The existing
  `--timeout` bounds both waits as one total budget, not per phase.
- **Inventory placement is injected.** Bootstrap's inventory ConfigMap
  namespace (`flux-system` today) is engine knowledge: post-M10 the
  namespace is a **string supplied at the composition edge from the
  driver's `InstallNamespace`**, beside the predicates — the seam
  carries the fact, the edge passes the string, bootstrap records where
  it is told. The retained machinery codes' Flux-specific
  summaries/remediations go engine-neutral in the same narrowing
  (mirror text: `docs/domains/bootstrap.md`, M10 section).
- Everything Flux-specific leaves: the embedded asset, the version
  constant + sha256, the source/sync CR shapes. `CUBE-BST-001`, `-002`,
  `-007` and `-008` — the asset-provenance, asset-parse,
  unsupported-source-kind and embedded-version checks, all raised by
  content that moves — follow it to the driver and are **superseded** by
  `CUBE-ENG-*` codes at implementation (rows kept, numbers never reused,
  the `APP`/`PKG-020` discipline). The machinery codes
  (`CUBE-BST-003..006`) stay.
- Engine-CR readiness moves from "explicitly out of scope (M10)" to
  delivered: `cube-idp bootstrap` completes only when the engine reports
  the sync CRs reconciled, bounded by the existing `--timeout`. Whether
  `status` gains a fourth (engine) line is an implementation-breakdown
  decision, not gate-fixed.

## The Argo decision: layer, and why (traceable to M9)

**Flux is the committed substrate. Argo CD, if it ever arrives, arrives
as an ordinary pack that Flux delivers ("layer"). It does not arrive as a
second engine driver ("replace") on any current horizon.**

The constraint is traceable to M9, not taste: `type: helm` packs render
**Flux-specific CRs** (`HelmRelease` + `HelmRepository`/`OCIRepository`),
so under a non-Flux steady-state engine every helm pack is inert unless
one of three things happens — helm packs are re-rendered per engine (a
pack-contract break), Flux's helm-controller ships anyway (which *is*
layering), or the pack output contract is made engine-portable (a
different product). The gate picks **substrate** and records the price
knowingly: pack output may stay Flux-shaped, which is what keeps helm
packs thin and rendering hermetic.

Consequently the driver seam exists for **separation and testability**
(content vs machinery; a pure, conformance-tested contract), **not as an
Argo invitation** — nobody should read `internal/engine/flux` as half a
promise of `internal/engine/argo`. The standing rule is unchanged either
way: Argo is never a compile-time dependency. Reversing "layer" later is
a design-gate event that must also answer the M9 consequence above.

## Config surface (`spec.engine`)

**Unchanged in M10 — the `provider` + opaque `forProvider` migration is
deliberately not taken.** That migration exists to give provider-specific
knobs an opaque home; with Argo settled as layer there is no second
provider on the horizon, so an opaque payload would carry exactly the
already-finalized typed `EngineSource` — ceremony without a consumer,
against the same doctrine that defers interfaces until a real second
implementation exists. The typed shape is also the friendlier base for
#142's additive `secretRef`. Migration remains a reserved design-gate
event should a second provider ever actually land.

## Error codes (`CUBE-ENG-*`, exit 1)

The catalog is fixed at implementation, in the domain's own `errors.go`,
per §5. Known from the gate: codes for the superseded
`CUBE-BST-001/002/007/008` checks (asset provenance, asset parse,
unsupported source kind, version mismatch), an unsupported-provider code
(the `CUBE-CLU-002` analogue, raised when the edge finds no driver), and
an unrecognized-object code for `Reconciled`.
Document-layer `spec.engine` errors stay `CUBE-CFG-*` (exit 2); codes are
never re-tagged across domains.

## Testing

- **`RunEngineConformance(t, factory)`** lives in `internal/engine`;
  every driver runs it. Because the seam is pure, the suite is hermetic
  **against the real driver** — no stateful fake is needed and none is
  written (a fake would test the fake). It asserts, with per-driver
  fixtures passed in by the factory side (the cluster suite's R2b lesson:
  no hardcoded "universally invalid" payloads):
  - install objects parse, are non-empty, and agree with the driver's
    pinned version; a contradicting `spec.engine.version` yields the
    coded error, asserted by `errors.As` + code equality;
  - source objects match the git|oci contract per fixture, and a nil
    source yields none;
  - `Reconciled` answers correctly over ready / not-ready / unknown
    status fixtures, and unknown objects yield the coded error;
  - `InstallNamespace` is non-empty and names a `Namespace` object
    present in the driver's install objects — placement is tied to
    content, not asserted on faith;
  - the `SpecValidator` sub-test runs iff implemented, with driver-owned
    valid/invalid fixtures;
  - documented semantics are enforced, not just non-nil-checked — the
    cluster suite's R3 lesson, applied from day one.
- **Compile-time assertions** in every driver
  (`var _ engine.Provider = (*Flux)(nil)`, one per capability).
- The **edge-level dogfood test** (pack-renders-the-embedded-pack ≡
  driver objects) runs in the green gate.
- The **real reconciliation round-trip** (bootstrap → source CRs applied
  → driver predicates report reconciled against a kind cluster) extends
  the existing `make test-e2e` path; never part of the green gate.

## Dependencies

**M10 adds no runtime dependency.** The seam and driver are client-go
interface types, apimachinery `unstructured`, and embedded data — the
same closed-set outcome as M7 and M9, stated at the gate so §8 stays
closed by decision rather than by accident. The asset move changes an
import path, not the module graph.

## Contracts for future domains

- **M11 (bus)** writes rendered packs into the source the engine wired.
  Its delivery-target facts come from `spec.engine.source` via `api/`
  (the reservation above); the real `pre` semantics and the air-gap
  answer are due there. The air-gap local-manifest override deferred at
  M7 now lands per-driver (the driver owns its asset). The M11 PR also
  **instantiates the `internal/plan` shared-infrastructure leaf** listed
  at this gate (operator decision, §2): the delivery vocabulary —
  `RenderPlan`, `ResolvedGraph`, instance identity, `EffectiveIDs` —
  moves there, `pack` produces its types, delivery and the M12
  orchestrator consume them. The two delivery guardrails are thereby
  **properties of the shared types, not promises each consumer
  repeats**: the `Prerequisites`/`Objects` group boundary is preserved
  because the one delivery type has it, and effective-id derivation is
  single-sourced because the leaf's `EffectiveIDs` is the only
  implementation anywhere.
- **M12 (`up`/`down`)** composes bootstrap + engine at the orchestrator
  edge exactly as `cube-idp bootstrap` does at the CLI edge; `down` reads
  bootstrap's inventory, which never moved.
- **#142 (trust)** attaches `secretRef` to `EngineSource` (additive,
  `api/`) and flows it through `SourceObjects`; designed together with
  the pack domain's helm source CRs.
- Not in this domain, ever on the current horizon: applying or waiting
  (bootstrap's), delivering packs (M11's), inventory (bootstrap's), watch
  machinery/informers/controller-runtime/kstatus, an Argo driver, engine-
  or gateway-aware behavior keyed on pack `category`.
