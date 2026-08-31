# Domain: engine

Living contract of the engine domain (`internal/engine`: the invariant
**substrate**, the tier-2 **driver seam**, and the flux driver).
Cross-cutting rules: `docs/ARCHITECTURE.md`. Originating design gate:
`docs/DECISIONS.md` 2026-08-24 (M10, epic #152; shipped by the
M10-C1..C5 stack — PRs #169/#171/#172/#173/#175).

## Purpose: the two-tier model

The founding vision, verbatim (operator, recorded at this gate): *"we use
Flux to deliver the prerequisites and the engine itself (that being Flux
or ArgoCD) — from there the engine takes over installation and
coordination of packs."* This domain gives that vision its structure:

- **Tier 1 — the substrate (invariant).** The embedded, pinned Flux
  install (source-controller, kustomize-controller, helm-controller +
  their CRDs). Bootstrap SSA-applies it on day 0. It is **not
  driver-selected and not behind the seam** — it is committed platform
  substrate, present in every cube whatever the engine choice. This is
  the one thing M9 actually forces: helm packs render Flux CRs
  (`HelmRelease` + source CR) that tier-1 controllers reconcile
  **whoever coordinates packs**, so the substrate is a permanent
  prerequisite — and, being invariant, it is what makes Flux-shaped pack
  output safe and helm packs never inert.
- **Tier 2 — the engine.** The coordinator that owns steady-state pack
  installation and coordination, **user-selected at day 0 via
  `spec.engine.provider` and immutable for the cube's lifetime** — no
  handover, migration, or engine-switching semantics exist, ever. The
  engine arrives **through tier 1** (delivered into the source the
  substrate watches), which is the vision's normal path, not a
  circularity. **Flux is the only driver today — the degenerate case**:
  the substrate doubles as the engine, no second install occurs, and the
  driver contributes only sync wiring and readiness.

The domain answers, per engine: how the cube's declared source becomes
this engine's coordination loop, what (if anything) must be delivered
through tier 1 for the engine itself, what "reconciled/ready" means in
the engine's own vocabulary, and where engine content lives. It
**supplies content and predicates; it never applies and never waits** —
applying and waiting stay `internal/bootstrap`'s machinery; composition
stays at the CLI/orchestrator edge. Delivery of ordinary packs through
the engine is the bus milestone's contract (M12), not this domain's.

## The substrate (`internal/engine/substrate`)

The invariant tier, owned by a subpackage that is **not a driver**:

- **The embedded substrate pack.** The vendored `flux install --export`
  asset moved from `internal/bootstrap/assets/` here, re-homed as an
  embedded **pack directory** — `pack.cue` (`name: "flux"`, `version` =
  the pinned Flux version, `type: raw`, `category: "engine"`, no
  `#Values`, no `namespace`) with the manifests as its `manifests/`
  payload. **Version spelling is clean SemVer**: the pack's `version` is
  `2.9.2`, never the upstream `v`-prefixed tag spelling (the repo
  versioning rule); `spec.engine.version` accepts the clean spelling;
  the substrate alone maps to and from `v2.9.2` where the vendored asset
  or upstream tags require it, inside the superseding version check.
  The pin discipline is unchanged: version constant + recorded sha256 +
  `make flux-manifests` regeneration, never fetched at runtime — **data,
  not a dependency**.
- **The substrate namespace** (`flux-system`) — an exported invariant
  fact. It is where the substrate lives and **where bootstrap records
  its inventory**; the edge injects it into bootstrap as a string.
  A green-gate test ties the fact to content: the named namespace must
  exist as a `Namespace` object in the substrate pack's payload.
- The substrate parses its own payload (a raw, values-free pack renders
  as a sorted manifest parse) — it does **not** import `internal/pack`.

**"Conforming pack" is enforced, not asserted.** A green-gate test at the
composition edge (which legitimately imports both domains) loads the
substrate's embedded directory through `internal/pack` and asserts
`pack.Load` + `Render` succeed and yield exactly the substrate's parsed
objects — **deep equality of the ordered object lists after both parse
paths**, not membership or byte comparison, so the raw-render semantics
(lexical file order, `.yaml|.yml` filtering, empty-document skipping,
empty-render rejection) are all in scope. It is the pack contract's
first serious dogfood: `Load`, the payload check, raw rendering,
determinism, and **building** the bundled-CRD scope index over the real
Flux payload (the pack declares no `namespace`, so no scope answer
affects an object — the index is exercised, not its decisions). If the
pack contract and the substrate ever disagree, the gate breaks; neither
side may drift silently.

**What the substrate pack is not.** It carries no instance state — the
sync wiring is config-derived and driver-emitted, never pack content
(the M9 pack/instance boundary). Day-0 direct apply is sanctioned for
the substrate because nothing exists yet to deliver it; a tier-2
engine's bundle is **not** circular to deliver through the running
substrate — that is the vision's normal path. Whether the running
system later reconciles the substrate's own upgrades through the source
is left open for the bus milestone and beyond.

## The driver seam (Kind B) — tier 2 only

`spec.engine.provider` selects the driver, mirroring
`spec.cluster.provider` → `cluster.Provisioner`. The seam covers **the
engine only**; nothing about the substrate crosses it. The shipped
seam (`internal/engine/engine.go`; every export carries a doc comment):

```go
// Provider is the tier-2 gitops-engine driver seam. It covers the
// ENGINE only — the tier-1 substrate is invariant platform and is not
// behind this seam. It is PURE: every method returns data or judges
// data; no method performs I/O. Applying objects and polling status
// belong to the caller (bootstrap machinery, composed at the edge).
type Provider interface {
    // SourceObjects returns the engine's sync wiring derived from
    // spec.engine.source — how the cube's declared source becomes this
    // engine's coordination loop — or nil when no source is configured.
    // flux (degenerate): the GitRepository|OCIRepository +
    // Kustomization pair; the substrate doubles as the engine, so the
    // wiring is substrate vocabulary.
    SourceObjects(ctx context.Context, spec v1alpha1.EngineSpec) ([]*unstructured.Unstructured, error)

    // EngineObjects returns the engine's own install bundle —
    // pack-shaped content delivered THROUGH tier 1 (written into the
    // source; never applied by bootstrap's SSA). flux (degenerate):
    // empty — the substrate already is the engine and no second
    // install occurs.
    EngineObjects(ctx context.Context, spec v1alpha1.EngineSpec) ([]*unstructured.Unstructured, error)

    // Reconciled judges one declared object's status: reconciled, not
    // yet (with a human-readable reason — it feeds the
    // reconciliation-wait timeout diagnostics, CUBE-BST-009), or a
    // coded error for an object the driver does not recognize. It
    // covers the driver's declared objects — sync wiring and engine
    // bundle. Pure — it reads the unstructured status it is handed, it
    // never fetches.
    Reconciled(obj *unstructured.Unstructured) (bool, string, error)

    // EngineNamespace names the namespace tier-2 engine content lives
    // in. flux (degenerate): the substrate namespace. It is distinct
    // from the substrate namespace fact, which is invariant and owns
    // inventory placement.
    EngineNamespace() string
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

**`SpecValidator` is currently implemented by no driver** — the flux
driver deliberately declines it (`spec.engine` is fully validated in
`api/` plus the edge's version assertion, so a driver check would
duplicate), and the CLI edge wires nothing to it: `config validate`
type-asserts only the cluster capability today. The capability is real
contract surface with a conformance sub-test that runs iff implemented —
but until a driver implements it, it is dormant by design, not by
accident. Recorded here so nobody mistakes the seam text for a claim
that engine specs get provider-side validation today.

Why the seam is pure, stated once: the interface doctrine (§4) says
"interfaces stay pure where possible (return objects; the caller
applies)", and here it is entirely possible — the only I/O an engine
needs (SSA, polling) is machinery bootstrap already owns and that must
not be duplicated (the no-second-applier rule, 2026-08-06). Purity is
also what makes the seam **apply-path-agnostic**: it does not care
whether its objects are SSA-applied at day 0 (flux sync wiring) or
delivered through the tier-1 source (a future engine bundle) — which is
exactly what lets a second driver land without reopening the seam.

**The `InstallNamespace` split, explicit.** The previous draft's single
accessor conflated two facts that diverge under two-tier: *where the
inventory records* is invariant (the substrate namespace — a substrate
fact, not a seam method) and *where engine content lives* is the
driver's (`EngineNamespace`; degenerate for flux). Each fact now lives
with its owner.

**What deliberately does NOT cross the seam** (each with the reason):

- **The substrate** — invariant platform, not a driver choice; it is
  the reason helm packs are never inert under any engine.
- **Apply/wait machinery** — bootstrap's; a second applier was rejected
  at the M7 gate and stays rejected.
- **The bootstrap inventory** — bootstrap's, recorded in the substrate
  namespace, the seed of `down`.
- **A delivery-target descriptor** ("where rendered packs get written") —
  the bus milestone's contract (M12). M10 commits only this reservation:
  every fact needed to locate the delivery target (URL, ref, path) is
  already fully derivable from `spec.engine.source` in `api/`, and the
  seam must never make engine-domain private state necessary for
  locating it — so the bus lands without reopening the seam. (Substrate
  source vs engine revision vs pack-target *topology* questions beyond
  the one configured source are the second-driver gate's, recorded
  below.)
- **Credential material** — #142's. The hook point is reserved, not
  implemented: a future `secretRef` lands as an additive field on
  `EngineSource` (api/) and flows through `SourceObjects` unchanged,
  because the method already receives the whole spec. The same #142 gate
  designs the pack domain's helm source CRs; the two emitters are
  decided together, once.

## The flux driver (`internal/engine/flux`) — the degenerate case

The substrate doubles as the engine, so the driver contributes **sync
wiring and readiness only**:

- **`SourceObjects`** — the `GitRepository`/`OCIRepository` +
  `Kustomization` shapes bootstrap emits today, moved verbatim; the
  git|oci discrimination and its validation stay in `api/` (unchanged).
- **`EngineObjects`** — empty. No second install occurs.
- **`Reconciled` predicates** — for all three kinds (`GitRepository`/
  `OCIRepository`/`Kustomization`): `Ready` condition true **and**
  `status.observedGeneration` equal to `metadata.generation` — a stale
  `Ready` from before a spec change must not count as reconciled. Read
  off `unstructured` status, same style as bootstrap's kind-set
  predicates; no kstatus, no controller-runtime. The generalizable seam
  principle behind it, stated driver-neutrally for every future driver:
  **no stale success may count as reconciled — each driver's predicates
  reject staleness in its own CRs' freshness vocabulary.**
- **`EngineNamespace`** — the substrate namespace.

## What M10 changed in bootstrap (shipped)

The full narrowed contract lives in `docs/domains/bootstrap.md` (folded
into its living body at the M10 closeout); the seam-side summary:

- Bootstrap keeps the engine-agnostic machinery and applies only
  **injected** content: `Applier.InstallEngine(ctx, substrateObjs,
  wiringObjs, engineWait)` applies the substrate, records the inventory,
  waits the kind-set (phase 1), then — when wiring exists — re-records
  the inventory **before** applying the wiring, applies it, and waits it
  reconciled (phase 2), then waits the declared
  `EngineWait.EngineObjects` reconciled (phase 3 — a no-op for flux,
  which declares none). One `--timeout` bounds all phases.
- The driver crosses the edge as neutral vocabulary: object slices plus
  `EngineWait{Reconciled ReconciledFunc; EngineObjects
  []*unstructured.Unstructured}` — function values, no engine type.
- **Transient discovery is pending, never terminal, in phases 2–3**: a
  not-yet-served kind or a NotFound is retried until the shared
  deadline (the wait path re-consults discovery per poll — deliberately
  unlike the apply path's one-shot reset-and-retry); permanent failures
  are `CUBE-BST-010`, coded at the failure point; already-coded causes
  (including `ENG`-coded predicate errors) pass through untouched.
  Phase-2/3 timeouts are `CUBE-BST-009`, carrying the driver's pending
  reasons; `CUBE-BST-005` stays kind-set-only.
- **Inventory** records exactly what bootstrap applies — substrate +
  wiring — into the substrate namespace, injected as a
  `NewApplier(dyn, mapper, inventoryNamespace)` argument. Source-
  delivered content is deliberately outside it; the (a) source-level /
  (b) tier-1-inventory / (c) cluster `down` order is a published M13
  requirement.
- `CUBE-BST-001/002/007/008` are superseded by the `ENG` codes above
  (tombstoned, never reused); the retained machinery codes' text is
  engine-neutral.

## The Argo future: a legitimate tier-2 driver, at its own gate

**Argo CD is a legitimate future engine driver.** Under the two-tier
model it arrives exactly as the vision says: delivered through tier 1
as pack-shaped content (`EngineObjects`), then owning steady-state pack
coordination — while the invariant substrate keeps reconciling the
Flux-shaped output of helm packs, which is why nothing about M9 blocks
it. It arrives via **its own design gate**, whose recorded first-class
design inputs — from the two-tier analyses at this gate — are:

- **False-green Application health**: gitops-engine computes no health
  for CR kinds with no registered check, so an Application holding only
  a `HelmRelease` + source-CR pair rolls up Synced/Healthy while the
  release fails. The Argo driver's install content must ship health
  checks for the Flux CR kinds, or the operator flies blind — confident
  wrong visibility, the sharpest cost.
- **Prune/finalizer coupling**: uninstall-on-prune works through
  helm-controller's finalizer, so tier-1 liveness gates Argo prunes, and
  a CR deleted in a finalizer-free window orphans helm state.
- **Per-driver sync mapping at the bus**: `ResolvedGraph` edges map to
  Flux `Kustomization.dependsOn` today; an Argo driver maps the same
  neutral data to Applications/sync waves — the bus milestone's
  engine-facing half doubles.
- Plus, from the machinery side: the two-bundle delivery/provenance
  story (who emits the tier-1 wiring that delivers the bundle), the
  day-0 bundle path (requires the bus), source topology beyond the one
  configured source, tier-1 self-management under a non-Flux engine
  (who reconciles the substrate itself), and an Argo-vocabulary
  freshness rule (Application CRs carry no Flux-style
  `observedGeneration` contract — the driver defines and tests its own
  stale-success rejection).

Standing constraints, unchanged: Argo is never a compile-time
dependency, and the engine choice is immutable per cube — a second
driver adds a day-0 option for new cubes, never a migration for
existing ones.

## Config surface (`spec.engine`)

Owned by this domain's contract since M10 (bootstrap no longer imports
`api/config`; the edge reads the spec and injects derived content). An
optional sub-struct on `ConfigSpec`, defaults and validation beside it
in `api/config/v1alpha1` (the loading machinery never changes):

```go
// EngineSpec declares the gitops engine cube-idp bootstraps.
type EngineSpec struct {
    // Provider selects the tier-2 engine only — the invariant tier-1
    // substrate is never selectable. Immutable per cube; "flux" is the
    // default and the only admitted value today ("argo" is additive at
    // its own design gate).
    Provider EngineProvider `json:"provider,omitempty"`

    // Version, when set, is asserted at the edge against the substrate's
    // pinned version, in clean SemVer spelling ("2.9.2", never
    // v-prefixed) — a mismatch is rejected (CUBE-ENG-005, superseding
    // CUBE-BST-008) before any apply; empty selects the embedded
    // version. It never selects or fetches a different Flux.
    Version string `json:"version,omitempty"`

    // Source points the engine's sync at a location; absent means the
    // substrate is installed without a sync.
    Source *EngineSource `json:"source,omitempty"`
}

// EngineSource is the finalized (M7) git|oci discriminated contract —
// shared sync-wiring vocabulary every driver consumes.
type EngineSource struct {
    Kind     EngineSourceKind `json:"kind,omitempty"` // "git" (default) | "oci"
    URL      string           `json:"url"`            // git URL, or oci:// ref for kind oci
    Ref      string           `json:"ref,omitempty"`  // git branch / oci tag (default main / latest)
    Path     string           `json:"path,omitempty"` // sync path (default "./")
    Interval string           `json:"interval,omitempty"` // reconcile interval (default "10m")
}
```

- **Typed on purpose; no opaque `forProvider`.** Weighed at the M10 gate
  and not taken: `EngineSource` is shared sync-wiring vocabulary every
  driver consumes, and no driver-specific knob exists yet — an empty
  opaque payload is ceremony. A second driver's gate migrates the shape
  if and as needed.
- Absent `spec.engine` defaults to the flux engine (the engine is
  mandatory).
- **`EngineSource` is discriminated by an explicit `kind`** (git or oci),
  not URL sniffing — mirroring `spec.cluster.provider`. `Default()` fills
  `kind`→git, `ref`→main (git) / latest (oci), `path`→`./`, `interval`→10m.
  `Validate()` rejects an unknown `kind` (`spec.engine.source.kind`), a
  missing URL or a URL whose scheme contradicts the kind — `oci` requires
  an `oci://` URL, `git` rejects one (`spec.engine.source.url`) — and an
  unparseable `interval` (`spec.engine.source.interval`). All are
  config-domain `CUBE-CFG-*` document errors (exit 2). **Public URLs
  only**; the credential hook (`secretRef`) is reserved for #142.
  `spec.engine.version` carries **no document-layer check** — the
  spelling and match are asserted at the bootstrap edge (`CUBE-ENG-005`),
  so `config validate` does not catch a `v`-prefixed version; recorded as
  a known gap, not an accident.

- **`provider` is re-scoped, not added**: the existing field now selects
  the **tier-2 engine only** — the substrate is never selectable.
  `"flux"` (the default) remains the only accepted value in M10; adding
  `"argo"` is the second driver's gate event, additive. **The choice is
  immutable for the cube's lifetime** — recorded as contract now;
  mechanical enforcement (rejecting a provider change against an
  existing cube) lands with the second driver, since today there is
  nothing to switch to.
- **`version`** asserts the *engine* version; for flux that is the
  substrate version — degenerate, like everything else about the flux
  driver. Clean-SemVer spelling as above.
- **The `provider` + opaque `forProvider` migration is still not
  taken — for a new reason.** The old rationale ("no second provider on
  the horizon") is void: user engine choice is the recorded vision. The
  standing reason is concrete: the typed `EngineSource` is **shared
  sync-wiring vocabulary every driver consumes** (the source through
  which tier 1 delivers is not driver-private), and no driver-specific
  knob exists yet to justify an opaque payload — an empty `forProvider`
  is ceremony. The second driver's gate, which knows Argo's actual
  knobs and source-topology needs, migrates the shape if and as needed;
  `EngineSource` (M7-finalized) must cross any such migration losslessly
  or be superseded on the record.

## Error codes (`CUBE-ENG-*`, exit 1)

Declared in the domain's `errors.go`; constructors exported because the
substrate, the driver subpackages, and the CLI edge raise them:

| Code | Meaning |
|---|---|
| `CUBE-ENG-001` | no driver for `spec.engine.provider` (raised at the composition edge; the `CUBE-CLU-002` analogue) |
| `CUBE-ENG-002` | object handed to `Reconciled` outside the driver's declared coverage |
| `CUBE-ENG-003` | embedded substrate payload fails its sha256 provenance (supersedes `CUBE-BST-001`) |
| `CUBE-ENG-004` | embedded substrate payload fails to parse into objects (supersedes `CUBE-BST-002`) |
| `CUBE-ENG-005` | requested `spec.engine.version` differs from the pinned substrate — asserted at the bootstrap edge, before any apply; the remediation names the clean-SemVer spelling (supersedes `CUBE-BST-008`) |
| `CUBE-ENG-006` | engine source kind the driver cannot turn into sync wiring (defensive; config validation is the primary gate; supersedes `CUBE-BST-007`) |

`CUBE-ENG-005` runs **only at the bootstrap edge**: `config validate`
performs no version check, so a `v`-prefixed `spec.engine.version`
passes document validation and is rejected at bootstrap time — a known,
recorded gap, not an accident. Document-layer `spec.engine` errors stay
`CUBE-CFG-*` (exit 2); codes are never re-tagged across domains.

## Testing

- **`RunEngineConformance(t, factory)`** lives in `internal/engine`;
  every driver runs it. Because the seam is pure, the suite is hermetic
  **against the real driver** — no stateful fake is needed and none is
  written (a fake would test the fake). It asserts, with per-driver
  fixtures passed in by the factory side (the cluster suite's R2b
  lesson: no hardcoded "universally invalid" payloads):
  - source objects match the git|oci contract per fixture, and a nil
    source yields none;
  - `EngineObjects` is internally consistent: empty (degenerate), or a
    pack-shaped bundle in which `EngineNamespace` names a `Namespace`
    object;
  - `EngineNamespace` is non-empty (for flux: equals the substrate
    namespace);
  - `Reconciled` answers correctly over ready / not-ready / stale
    (fresh-looking `Ready` with an outdated generation or the driver's
    equivalent) / unknown status fixtures, and unknown objects yield
    the coded error — the no-stale-success principle is a documented,
    enforced suite semantic;
  - the `SpecValidator` sub-test runs iff implemented, with driver-owned
    valid/invalid fixtures;
  - documented semantics are enforced, not just non-nil-checked — the
    cluster suite's R3 lesson, applied from day one.
- **Compile-time assertions** in every driver
  (`var _ engine.Provider = (*Flux)(nil)`, one per capability).
- The **substrate's own green-gate checks**: sha256 provenance, payload
  parse, the namespace-fact-matches-content tie, and the edge-level
  dogfood test (pack-renders-the-embedded-pack ≡ substrate objects).
- The **real round-trip** (bootstrap → substrate + wiring applied →
  reconciliation reported against a kind cluster) extends the existing
  `make test-e2e` path; never part of the green gate.

## Dependencies

**M10 adds no runtime dependency.** The substrate, seam, and driver are
client-go interface types, apimachinery `unstructured`, function values,
and embedded data — the same closed-set outcome as M7 and M9, stated at
the gate so §8 stays closed by decision rather than by accident. The
asset move changes an import path, not the module graph.

## Contracts for future domains

- **M11 (gateway + ca)** — gated 2026-08-27, shipped
  (`docs/domains/gateway.md`,
  `docs/domains/ca.md`): the trust fabric arrives as an **ordered list
  of prerequisite units, bootstrap-installed ahead of tier 2** —
  between the substrate's kind-set readiness and this domain's sync
  wiring/bundle steps, reconciled by tier-1 controllers where
  applicable — because the
  identity fabric is what the engine's own endpoints presume. This is
  the substrate's sanctioned day-0 SSA path, **not** this contract's
  "delivered through tier 1" (the `EngineObjects` source path);
  nothing enters the watched source, and no source need be
  configured. Two
  touchpoints on this contract: the substrate's helm-controller became
  day-0 load-bearing for prerequisites (the gateway pack is thin-helm,
  reconciled by tier 1 before any engine exists — which is also why the
  bootstrap readiness budget rose to 10m), and the Gateway API
  CRDs are deliberately their **own** list member so the CRD conflict
  with a future driver's `EngineObjects` bundle is never locked
  inside one pack and stays independently negotiable at that driver's
  gate — the prerequisite's copy still exists when such a bundle
  arrives, so co-ownership/version alignment is that gate's question.
  The seam itself came through untouched —
  prerequisites are not engine content and nothing about them crosses
  `engine.Provider`, which the shipped milestone confirmed: the units
  reach bootstrap as neutral vocabulary, never through this interface.
  Their delivery semantics feed the bus milestone's
  `Prerequisites` handling as prior art.
- **M12 (bus)** writes rendered packs into the source the sync wiring
  established. Its delivery-target facts come from `spec.engine.source`
  via `api/` (the reservation above); the real `pre` semantics and the
  air-gap answer are due there; the air-gap local-manifest override
  deferred at M7 now lands with the substrate (which owns the asset).
  The bus PR also **instantiates the `internal/plan`
  shared-infrastructure leaf** listed at this gate (§2): the delivery
  vocabulary — `RenderPlan`, `ResolvedGraph`, instance identity,
  `EffectiveIDs` — moves there, `pack` produces its types, delivery and
  the orchestrator consume them. The two delivery guardrails are
  thereby **properties of the shared types, not promises each consumer
  repeats**: the `Prerequisites`/`Objects` group boundary is preserved
  because the one delivery type has it, and effective-id derivation is
  single-sourced because the leaf's `EffectiveIDs` is the only
  implementation anywhere. Under a future non-flux driver the
  engine-facing half of delivery is per-driver (sync-topology mapping);
  the pack-side vocabulary is neutral and does not change.
- **M13 (`up`/`down`)** composes bootstrap + engine at the orchestrator
  edge exactly as `cube-idp bootstrap` does at the CLI edge; `down`
  executes the (a) source-level / (b) tier-1-inventory / (c) cluster
  teardown order recorded above, defining its reconcile-the-removal and
  dependency-aware semantics at its own gate.
- **#142 (trust)** attaches `secretRef` to `EngineSource` (additive,
  `api/`) and flows it through `SourceObjects`; designed together with
  the pack domain's helm source CRs.
- Not in this domain, ever on the current horizon: applying or waiting
  (bootstrap's), delivering packs (the bus's), inventory (bootstrap's),
  watch machinery/informers/controller-runtime/kstatus, engine- or
  gateway-aware behavior keyed on pack `category`. An **Argo driver** is
  deliberately absent from this list: it is future work behind its own
  design gate, not an exclusion.
