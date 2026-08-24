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

    // Version, when set, is asserted against the embedded FluxVersion — a
    // mismatch is rejected (CUBE-BST-008) before any apply; empty selects
    // the embedded version. It never selects or fetches a different Flux;
    // the embedded asset is authoritative in M7.
    Version string `json:"version,omitempty"`

    // Source points the engine's sync at a location; absent means Flux is
    // installed without a sync.
    Source *EngineSource `json:"source,omitempty"`
}

// EngineSource is the finalized (M7) git|oci discriminated contract.
type EngineSource struct {
    Kind     EngineSourceKind `json:"kind,omitempty"` // "git" (default) | "oci"
    URL      string           `json:"url"`            // git URL, or oci:// ref for kind oci
    Ref      string           `json:"ref,omitempty"`  // git branch / oci tag (default main / latest)
    Path     string           `json:"path,omitempty"` // Kustomization path (default "./")
    Interval string           `json:"interval,omitempty"` // reconcile interval (default "10m")
}
```

- **Minimal-typed on purpose.** There is no engine *driver seam* until M10,
  so there is no provider to own an opaque payload; a typed shape is
  validatable in `api/` today. When M10 formalizes the engine seam,
  migrating `spec.engine` to the cluster-style `provider` + opaque
  `forProvider` pattern is a **design-gate event**, not a drive-by edit.
  The `EngineSource` shape below is **finalized**, not provisional.
- Absent `spec.engine` defaults to Flux (the engine is mandatory).
- **`EngineSource` is discriminated by an explicit `kind`** (git or oci),
  not URL sniffing — mirroring `spec.cluster.provider`. `Default()` fills
  `kind`→git, `ref`→main (git) / latest (oci), `path`→`./`, `interval`→10m.
  `Validate()` rejects an unknown `kind` (`spec.engine.source.kind`), a
  missing URL or a URL whose scheme contradicts the kind — `oci` requires
  an `oci://` URL, `git` rejects one (`spec.engine.source.url`) — and an
  unparseable `interval` (`spec.engine.source.interval`). All are
  config-domain `CUBE-CFG-*` document errors (exit 2). M7 uses **public
  URLs only**; credential Secrets return with a real consumer.

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

One capability expectation rides on the injected mapper and is contract,
not implementation detail: bootstrap expects a **resettable
discovery-cached mapper**. Because it installs CRDs and then applies CRs
of those CRDs, on a mapping miss it asserts the narrow consumer-side
`resettableRESTMapper` interface (`Reset()`, declared in `apply.go` where
it is consumed) to invalidate the discovery cache and retry once. The
memory-cached `RESTMapper` that `internal/kube` currently constructs
(client-go's `DeferredDiscoveryRESTMapper`) satisfies this capability; a
mapper without it degrades loudly, not silently — the retry is skipped
and the miss surfaces as `CUBE-BST-003`.

## Flux acquisition (embedded, pinned)

The Flux install manifests are **embedded** in the binary via `go:embed`
of vendored `flux install --export` output. The component set
(`--components=…` in the `make flux-manifests` target) is
**source-controller + kustomize-controller + helm-controller**: the first
two since M7, helm-controller added by M9, because its helm packs render a
`HelmRelease` and are inert without helm-controller and its
`helmreleases.helm.toolkit.fluxcd.io` CRD. The source-controller CRDs a
helm pack's source CR needs
(`HelmRepository`, `OCIRepository`, `HelmChart`) were already in the M7
asset. Adding a component is an asset regeneration + a new recorded sha256,
**not** a change to this domain's code: the kind-set below filters by
*kind*, so a new controller Deployment and a new CRD are waited on the
moment they are in the asset. They are **data, not a Go dependency**:

- a **version constant** + a recorded **sha256** pin the exact bytes;
- a `make` target regenerates the asset deliberately (reviewed diff), the
  same discipline as the committed deepcopy and the C4 SVGs;
- nothing is fetched at runtime — the hermetic gate and the air-gap
  posture are preserved. The air-gap *override* (a local-manifest path) is
  deferred to the bus milestone's (M12) air-gap decision.

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

The set is **by kind, not by name**, and that is load-bearing: it is why
M9's helm-controller Deployment and `helmreleases` CRD join the wait with
no code change here, and why any future component does too.

Engine-CR readiness (GitRepository/Kustomization *reconciled*) is **out of
scope** — it belongs to the M10 engine seam.

## Source + sync CRs, and the install sequence

When `spec.engine.source` is set, bootstrap generates a Flux source CR —
`GitRepository` or `OCIRepository` (`source.toolkit.fluxcd.io/v1`, the
latter with `provider: generic`) by `kind` — plus a `Kustomization`
(`kustomize.toolkit.fluxcd.io/v1`) that applies `path` on `interval`, both
named `flux-system` in the Flux namespace.

`Applier.InstallEngine` sequences the whole bootstrap in the one order that
is correct and recoverable:

1. apply the embedded Flux objects,
2. record the inventory (an applied-but-not-yet-ready install is already
   visible to `down`; an apply failure mid-stream returns before this
   step — resolving that gap is an up/down-milestone (M13) design question),
3. wait for the bootstrap kind-set (this establishes the Flux CRDs),
4. re-record the inventory with the source CRs included — before they are
   applied, so a half-applied source is already visible to `down`,
5. **then** apply the source + Kustomization CRs — they are CRs of the Flux
   CRDs, so they can only be applied once those CRDs are established.

Because the injected `RESTMapper` is a discovery cache primed *before* the
Flux CRDs existed, the adapter **resets it and retries once** on a mapping
miss, so the just-installed CR kinds resolve. Bootstrap applies and records
the engine CRs but **does not wait on their reconciliation** (M10).

## Inventory (seed of `down`)

`bootstrap` records what it applied (a ConfigMap inventory) so a future
`down` can find and remove it. In-domain and self-contained — **not** a
reusable applier seam. (The pre-M8 "inventory-inside-Apply" obligation is
superseded: packs are delivered through the Flux source, not through a
cube-idp applier — DECISIONS 2026-08-06 / Q1.)

## Interface doctrine applied

**No Kind-B driver seam in M7.** Flux is the only engine; the swappable
engine seam is an M10 concern. Consumer-side (Kind A) doctrine governs: the
CLI edge injects concrete client-go interfaces; `bootstrap` defines any
narrow interface it needs where it uses it, mocked with hand-rolled
function-field structs. Argo CD, if it ever returns, arrives as an engine
*pack* and never as a compile-time dependency.

## Error codes (`CUBE-BST-*`, exit 1)

| Code | Meaning |
|---|---|
| `CUBE-BST-001` | embedded Flux manifests failed their sha256 provenance check (build/asset integrity) |
| `CUBE-BST-002` | embedded Flux manifests failed to parse into objects |
| `CUBE-BST-003` | no REST mapping for an object's kind (even after a discovery refresh) |
| `CUBE-BST-004` | server-side apply of a bootstrap object failed (wrapped cause) |
| `CUBE-BST-005` | bootstrap kind-set readiness wait timed out (names the pending objects) |
| `CUBE-BST-006` | inventory encode failed before recording |
| `CUBE-BST-007` | unsupported engine source kind (defensive; config validation is the primary gate) |
| `CUBE-BST-008` | requested `spec.engine.version` differs from this binary's embedded Flux |

`spec.engine` *document* validation errors are config-domain
`CUBE-CFG-*`/`field.ErrorList` at load time — codes are never re-tagged
across domains.

## Testing

Hermetic gate tests drive the apply/wait/inventory/source machinery against
a **hand-rolled function-field fake** of the narrow `cluster` seam (the
client-go fake dynamic client cannot model server-side apply on unstructured
objects); the real GVK→resource scope resolution and the mapper reset-retry
are covered against the client-go dynamic fake. No live cluster, no Docker.
The real Flux round-trip (install → kind-set ready → source CRs applied →
`GitRepository` reconciled `Ready` against a kind cluster and a public git
source, worktree-local KUBECONFIG per CLAUDE.md §7) runs only behind
`make test-e2e` and is never part of the green gate.

## CLI surface

`cube-idp bootstrap` — cobra wiring only: load config → resolve the cube's
kubeconfig target + context via `cluster.Status` → read the bytes at the
edge → build the kube client → inject `dynamic.Interface` +
`meta.RESTMapper` into `internal/bootstrap`, then call `InstallEngine` and
render. Flags: `--kubeconfig`, `--kubeconfig-context-name`, `--timeout`
(default 5m, bounds readiness). The verb makes the product demo-able (up →
gitops-managed cluster). The `apply` verb reserved on 2026-08-03 is
superseded and stays retired.

## M10 (gated 2026-08-24) — two tiers: the substrate leaves, the seam covers the engine

Decided at the M10 design gate (`docs/DECISIONS.md` 2026-08-24; living
contract of both tiers: `docs/domains/engine.md`). Everything in this
section is the M10 target state, approved ahead of code; the sections
above describe the shipped M7/M9 behavior until the milestone lands, at
which point they are updated in place. The model: **tier 1** is the
invariant Flux substrate (not driver-selected), **tier 2** is the engine,
user-selected via `spec.engine.provider` and immutable per cube — flux
today, the degenerate case where the substrate doubles as the engine.

- **Bootstrap keeps the engine-agnostic machinery** — SSA apply, the
  by-kind bootstrap kind-set wait, the inventory, the install
  sequencing. What it applies day-0 is **injected**: the substrate
  objects (from `internal/engine/substrate`, which owns the embedded
  asset re-homed as a *pack*, same version-constant + sha256 + `make`
  regeneration discipline) and the driver's sync wiring
  (`SourceObjects`). Bootstrap no longer embeds or knows Flux; the
  substrate home and the driver supply content, the edge composes.
- **Phased readiness, one `--timeout` total budget (never per phase):**
  1. the **kind-set wait** over what its SSA applied — unchanged;
  2. the **reconciliation wait** over the driver's sync wiring, polling
     with injected per-object predicates (function values cross the
     edge; bootstrap imports nothing new) — engine-CR readiness thereby
     moves from "out of scope" to delivered;
  3. the **engine-readiness wait** over the driver's declared
     `EngineObjects` — content bootstrap did **not** apply (it arrives
     through the tier-1 source), polled by declared identity with the
     same predicate machinery. **Empty and skipped for flux**; the phase
     contract exists so a second driver's gate fills it without a new
     seam method.
- **Transient discovery is pending, never terminal, in phases 2–3.**
  Declared content may not exist yet by design — a tier-1-delivered
  engine CRD may still be establishing when phase 3 first polls — so
  within these phases, *no REST mapping yet* and *object not yet
  created* (NotFound) are **pending states, retried until the shared
  deadline**; permanent errors — forbidden, malformed declared identity,
  any non-transient retrieval failure — fail immediately as a new
  machinery code, **`CUBE-BST-010`** (readiness polling failed on a
  permanent error, wrapped cause), coded at the point of failure so it
  is already-coded before the pass-through boundary and neither timeout
  code ever retags it. This departs deliberately from the apply path's one-shot
  mapper reset-and-retry, which stays one-shot where it serves applying:
  the wait path re-consults discovery on each poll rather than
  converting a mapping miss into a terminal `CUBE-BST-003`.
  NotFound-as-pending matches today's wait behavior; no-mapping-as-
  pending is the genuinely new polling semantic, stated at the gate so
  "the same machinery" holds when implementation starts.
- **The reconciliation/engine-wait timeout is a new machinery code,
  `CUBE-BST-009`** — not a broadening of `CUBE-BST-005`, whose meaning
  stays exactly "kind-set wait timed out". The two codes cut along the
  polling contract — **kind-set rollout polling vs driver-declared
  reconciliation polling** — which fail and remediate differently, so
  they get different codes. `CUBE-BST-009` names the
  pending objects **with the driver's pending reasons** — that is what
  the `Reconciled` reason string is for. The pass-through rule landed
  with PR #159 (an already-coded **permanent** cause keeps its code;
  transient discovery conditions are pending states, not causes;
  wrapping in the wait code only on deadline with objects still
  pending) applies to the new waits identically: an `ENG`-coded
  predicate error surfacing through bootstrap's poll keeps its `ENG`
  code — codes are never re-tagged, machinery included.
- **Inventory: bootstrap records exactly what bootstrap applies** —
  substrate + sync wiring — into the **substrate namespace**, an
  invariant fact exported by the substrate home and injected as a
  string at the composition edge (a green-gate test in the substrate
  ties the name to a `Namespace` object in its payload; bootstrap
  records where it is told). Content delivered through the tier-1
  source — a future engine bundle, every ordinary pack — is
  **deliberately not** in this inventory: it lives under the
  `prune: true` sync, so its teardown is source-level. The `down`
  composition order — (a) source-level teardown, (b) tier-1 teardown
  from the inventory, (c) cluster teardown — is published as a
  **requirement on the up/down milestone (M13)**, its real semantics
  owed at that gate, not promised here.
- **`CUBE-BST-001`, `-002`, `-007`, `-008`** (asset provenance, asset
  parse, unsupported engine source kind, embedded-version mismatch) are
  all raised by content that moves — they follow it (asset checks to the
  substrate home, source-kind and version checks per the engine
  contract) and are **superseded** by `CUBE-ENG-*` codes at
  implementation — rows kept, numbers never reused, the same discipline
  as `APP` and `CUBE-PKG-020`. The machinery codes `CUBE-BST-003..006`
  (plus the new `-009` and `-010`) stay.
- **Retained codes lose their Flux-specific voice.** The machinery codes
  keep their identities, but their user-facing summaries/remediations
  currently say "Flux CRDs", "inspect the Flux controllers", and
  `kubectl -n flux-system …`. As part of the same narrowing they go
  engine-neutral (naming the injected namespace and generic "engine
  controllers" where needed) — bootstrap's *text* must not know Flux any
  more than its code does.
- **Unchanged, explicitly**: the inventory (still this domain's, still
  the seed of `down`), the no-second-applier rule, the injection
  contract, and delivery-through-engine — bootstrap installs and hands
  over, exactly as before; it just stops owning what it installs.

## Contracts for future domains

- **Pack delivery (M8 renders, the bus delivers)**: the pack domain (M8,
  shipped) renders and never applies; packs reach a cluster by being
  written into the Flux **source** that bootstrap wired (the M12 bus) —
  *not* through a cube-idp applier. Packs conform to the live Flux loop.
- **M10 (engine)** establishes the two-tier model — invariant substrate,
  driver-selected engine — and the reshape of this domain is the
  delimited M10 section above. The `spec.engine` `provider`+`forProvider`
  migration was weighed at the gate and **not taken** — no longer for
  "no second provider on the horizon" (void under the recorded vision of
  user engine choice) but because `EngineSource` is shared sync-wiring
  vocabulary every driver consumes and no driver-specific knob exists
  yet (`docs/domains/engine.md`).
- **M11 (gateway)** is the trust-fabric prerequisite delivered through
  the substrate ahead of ordinary packs; its own design gate defines it.
- **M13 (`down`)** reads the bootstrap inventory to tear down what
  bootstrap installed, composed with the M5 cluster teardown and the
  source-level teardown phase recorded in the M10 section above.
- Not in this domain, ever on the current horizon: watch
  machinery/informers, controller-runtime, kstatus/`cli-utils`, typed
  workload clientsets, a second applier, arbitrary user-manifest apply
  (that is the engine's job).
