# cube-idp architecture (living document)

This is the binding, cross-cutting architecture of cube-idp — updated in
place as domains land, never forked into dated copies. Per-domain contracts
live in `docs/domains/<name>.md` (one file per domain, created when the
domain lands). Dated decisions and their rationale: `docs/DECISIONS.md`.
Pre-reset history: `docs/archived/` (read-only, not binding). The original
approved design this document grew from is
`docs/archived/design/2026-07-27-back-to-basics-structure.md`.

## 1. Structure in one line

Standard Go layout (`cmd/` + `internal/` + public `api/`); one package per
component domain inside `internal/`; consumer-side interfaces by default
with a driver-pattern exception for swappable backends; error machinery
separated from per-domain error catalogs; a thin phase-runner orchestrator
(future); CI-enforced size limits.

## 2. Layout and import direction

```
cmd/cube-idp ──▶ internal/cli ──▶ internal/config    ──▶ api/config/v1alpha1
                      │      ├──▶ internal/cluster   ──▶ api/config/v1alpha1
                      │      │        │  └── cluster/kind (driver subpackage)
                      │      ├──▶ internal/kube  (M6 shared-infra leaf: injected
                      │      │        kubeconfig bytes + context name → clients)
                      │      ├──▶ internal/bootstrap (M7, narrowed M10: SSA/wait/
                      │      │        inventory machinery via injected client-go
                      │      │        ifaces + injected content; engine-agnostic)
                      │      ├──▶ internal/engine (M10: invariant Flux
                      │      │        │  substrate + tier-2 driver seam → api/config)
                      │      │        ├── engine/substrate (embedded substrate pack)
                      │      │        └── engine/flux (driver: sync wiring, predicates)
                      │      ├──▶ internal/pack (M8: load/validate/render packs
                      │      │        │  → api/config; renders, never applies)
                      │      │        └──▶ internal/ref (M8 shared-infra leaf:
                      │      │               ref grammar → tree/file)
                      │      ├──▶ internal/gateway (M11: trust-fabric
                      │      │        prerequisite units + CoreDNS marker
                      │      │        block + predicates → api/config)
                      │      ├──▶ internal/ca (M11: CA mint/reuse, marker
                      │      │        identity, trust ledger + OS trust-store
                      │      │        drivers; imports no api/config)
                      └──────┴──▶ internal/cubeerr ◀── (every package above)
```

Three package categories, and only these:

1. **`api/` and `internal/cubeerr`** — pure leaves: they import nothing
   from `internal/` — ever.
2. **Component domains** (`config`, `cluster`, `bootstrap`, `pack`,
   `engine` — landed by M10, gated 2026-08-24 — and `gateway` + `ca`,
   gated by the M11 design gate 2026-08-27 and landed by the M11 stack,
   PRs #192/#193/#194/#195/#196/#197/#198) — one
   domain = one package = one `docs/domains/` file. **Domains never import
   each other.**
3. **Shared-infrastructure leaves** — a closed, listed set:
   **`internal/ref`**, **`internal/kube`** (the latter documented in
   `docs/domains/kube.md` since M6, which predates this category and does
   not make it a component domain), and **`internal/plan`** (**listed**
   at the M10 design gate, 2026-08-24; **instantiated** by the M12 bus PR
   that lands the first real cross-domain consumer — the delivery
   vocabulary: `RenderPlan`, `ResolvedGraph`, instance identity, and the
   single `EffectiveIDs` derivation; closed content rule below). A
   component domain **MAY**
   import a listed leaf directly. A leaf **MAY NOT** import a component
   domain or `api/config`; it imports only `internal/cubeerr` and its own
   backend SDKs (for `plan`: apimachinery, the neutral KRM vocabulary its
   types carry — nothing else). Adding a package to this list is a
   design-gate event
   (2026-08-21), never an inference from shape: a package qualifies only
   when it is genuinely shared infrastructure carrying **no concept owned
   by a single component domain** (2026-08-24 precision — `ref`/`kube`
   carry backend-neutral machinery; `plan` carries contract vocabulary
   that belongs to several domains at once, which is exactly why no one
   domain may own it), and the alternative — re-implementing it per
   domain, or making one domain the accidental home of shared machinery —
   is worse.
   `internal/plan` additionally carries a **closed content rule**: every
   type or function added to it is gate-approved by name, so the leaf can
   never drift into a general types bucket. Its M10-approved member set:
   `RenderPlan`, `ResolvedGraph`, `InstanceID`, `EffectiveIDs`, and
   `InstanceIdentity` — the neutral input `EffectiveIDs` derives from,
   carrying exactly a pack name and an optional explicit id, so the
   derivation is implementable without the leaf seeing `internal/pack`
   or `api/config` (`pack` maps its instances into it).

   **MAY is not SHOULD.** Injection at the CLI/orchestrator edge remains
   the preferred crossing wherever the value is already an interface:
   `internal/bootstrap` deliberately does **not** import `internal/kube`,
   and the M7 contract stands unchanged. `internal/pack` imports
   `internal/ref` directly because what crosses is a concrete resolver
   with no second implementation, and a `Resolver` interface would be
   premature under the doctrine in §4.

- Imports flow strictly left to right.
- `internal/kube` is a shared-infrastructure leaf (M6): it imports only
  `internal/cubeerr`, `k8s.io/client-go`, and `k8s.io/apimachinery` —
  never `api/` or any domain. Kubeconfig bytes and the context name are
  injected by the
  CLI/orchestrator edge; the domain never reads files and never derives
  the `cube-idp.dev/<name>` context name itself.
- `internal/bootstrap` (M7, narrowed M10) is the **engine-agnostic
  micro-bootstrap machinery**: it SSA-applies **injected** substrate
  objects and driver sync wiring, executes three-phase readiness
  (kind-set; reconciliation with injected predicates; declared engine
  objects — transient discovery pending, never terminal), records an
  inventory into the injected substrate namespace, then hands over
  permanently — steady-state ownership of all packs/manifests is the
  engine's (no-engine operation is not a supported mode). It imports
  only `internal/cubeerr`, apimachinery, and `client-go/dynamic`; it
  embeds nothing and knows no engine. Its machinery runs against
  **injected client-go interface types** (`dynamic.Interface`,
  `meta.RESTMapper`) supplied by the CLI edge — it **does not import
  `internal/kube` or `internal/engine`** (domains never import each
  other; content and predicates cross the edge as neutral vocabulary).
  SSA is hand-rolled on client-go; readiness predicates read off
  `unstructured` status (no kstatus/`cli-utils`, no controller-runtime).
- `internal/pack` (M8) **defines, loads, validates, and renders** packs —
  it never applies anything. Under delivery-through-engine, packs reach a
  cluster by being written into the source the sync wiring established (the M12 bus); rendering
  is a pure function of its inputs, so the domain is hermetic and has no
  e2e. It imports `api/config` (the `spec.packs` sub-struct),
  `internal/cubeerr`, apimachinery/yaml, `cuelang.org/go`, and the
  `internal/ref` leaf.
- `internal/engine` (M10) is the **two-tier** engine domain: the
  **invariant substrate** (`engine/substrate` — the embedded, pinned Flux
  install re-homed as a *pack*, plus the substrate-namespace fact; not
  driver-selected, present in every cube) and the **tier-2 driver seam**
  (Kind B, `engine.Provider`): a **pure** seam — sync wiring, an engine
  bundle delivered through tier 1, per-object reconciled predicates, and
  the engine-namespace fact; no method performs I/O. The flux driver is
  the degenerate case (the substrate doubles as the engine; empty
  bundle). Applying and waiting stay `internal/bootstrap`'s machinery,
  which M10 narrows to engine-agnostic. Composition — driver selection,
  handing substrate content, wiring, and predicates to bootstrap — lives
  at the CLI/orchestrator edge. Living contract: `docs/domains/engine.md`.
- `internal/gateway` (M11) is the **bootstrap-phase trust fabric**: it
  owns the **model** of the ordered prerequisite list bootstrap
  installs **ahead of the engine** — the list itself being
  `spec.prerequisites` **data**, a surface this domain's contract owns
  (M11-A0, 2026-08-28) while `api/config/v1alpha1` states its
  vocabulary and materializes its compiled defaults. The four default
  units are the `gateway-platform` unit (the `gateway-system`
  Namespace plus the **stable `gateway` Service** — the cube-owned indirection
  in front of the implementation; internal DNS and future routing
  target its name, never an implementation Service), the embedded
  Gateway
  API CRDs pack
  (its own list member by design: a future engine may ship its own
  Gateway API CRDs, and the list may vary per setup), the CA-material
  inert unit, and the thin-helm
  Traefik gateway pack reconciled by the substrate's helm-controller —
  plus the CoreDNS marker rewrite block and the readiness predicates
  for its declared CRs. Pure like the substrate: embedded content and
  emitted objects/blocks/predicates, no I/O anywhere in the package;
  the edge applies (via bootstrap) and performs the Corefile
  read-modify-write. It imports `api/config/v1alpha1` (for the
  well-known prerequisite names, which it single-sources rather than
  respells), `internal/cubeerr`, and apimachinery — never
  `internal/pack`, whose contract its embedded packs nonetheless
  satisfy. Living contract: `docs/domains/gateway.md`.
- `internal/ca` (M11) owns the cube's certificate authority:
  stdlib-minted per-cube CA + wildcard leaf, the mint-if-absent reuse
  contract (the edge reads the existing Secret; custody is in-cluster
  only), the marker CN/OU identity, and the operator trust surface
  (ledger, artifact paths, and the two OS trust-store drivers behind
  the `trust` verbs — which drive the OS tools through an **injected
  `Runner`**, so `os/exec` stays at the edge and the drivers are
  gate-testable against a fake). It is the one component domain that
  **imports no `api/config` at all**: it takes the provider as a plain
  string and
  the Secret names as injected strings (the gateway domain's exported
  platform facts), so its imports are `internal/cubeerr`, stdlib
  crypto, apimachinery `unstructured`, and `sigs.k8s.io/yaml`.
  The `spec.ca.provider` surface therefore lives entirely in
  `api/config/v1alpha1`, and the edge — not the domain — compares it
  against the domain's own `ca.ProviderCube` constant before
  dispatching. The surface is provider-seam-**ready** (`cube` the only
  v0 value, immutable per cube — the engine precedent) while the Go
  seam waits for a real second implementation. Living contract:
  `docs/domains/ca.md`.
- Domains never import each other. Values cross domains by injection at
  the CLI/orchestrator edge, where factories and composition live. The
  one sanctioned exception is a listed shared-infrastructure leaf, above.
  **This rule has no exported-types escape hatch** (stated at the M10
  gate, 2026-08-24): naming another domain's type in a signature is an
  import, and stays banned domain-to-domain. When one domain's output is
  another's input, the crossing is one of exactly three sanctioned
  forms — **neutral vocabulary** (apimachinery `unstructured`, client-go
  interface types, function values, strings) injected at the edge, which
  is how M10's `engine` predicates reach `bootstrap`; **a listed
  shared-infrastructure leaf** owning the shared shapes, which is how the
  delivery vocabulary reaches M12/M13 — `internal/plan` (listed at this
  gate, instantiated at the M12 bus) will own `RenderPlan`, `ResolvedGraph`,
  instance identity, and the `EffectiveIDs` derivation, with `pack`
  producing its types and delivery/orchestration consuming them; or
  **promotion to `api/`**, reserved for actual config surface.
  Consumer-side mirror types were rejected at the gate (operator
  decision: no duplicated shapes), and an *uncurated* types-only package
  remains the import-cycle-workaround smell §2 bans — `internal/plan` is
  neither: it is a gate-listed leaf whose every member is approved by
  name and which carries real derivation logic, not a bucket.
- One component domain = one package under `internal/` = one file under
  `docs/domains/`. New components add a package; they never grow an
  existing one. A shared-infrastructure leaf is not a component domain:
  it is documented inside its consumer's contract until a second consumer
  makes a file of its own worth having (`internal/ref` lives in
  `docs/domains/pack.md` today; `internal/kube` has its own file from M6).
- Driver subpackages (`internal/cluster/kind`) are the only importers of
  their backend SDK.

## 3. The Config API

One aggregate `Config` kind in root group `cube-idp.dev` (`v1alpha1`),
KRM-shaped and CRD-ready: real `metav1.TypeMeta`/`ObjectMeta`, deepcopy
generated by controller-gen and committed. `ConfigSpec` grows exactly one
typed sub-struct per component, with its defaults and validation beside it;
the loading machinery never changes. A sub-struct may also carry a
**cross-component surface owned by one component's contract** instead of
being named for a component — `spec.prerequisites` is the gateway
component's (M11-A0, 2026-08-28, `docs/domains/gateway.md`).
Load pipeline order is fixed: strict
decode → `Default()` → `Validate()`; a non-nil `*Config` is always complete
and valid.

One nuance of that guarantee is worth stating cross-cuttingly, because
M11 made the edge depend on it: `Default()` fills a **sub-struct the
user wrote**, never one they omitted. An absent `spec.gateway` or
`spec.ca` stays nil through the pipeline — so the CLI edge re-derives
those two defaults itself (`gatewayDomain`, `caProvider` in
`internal/cli`) rather than reading a field defaulting never filled.
List surfaces differ and are simpler: an absent or empty
`spec.prerequisites` **is** materialized by `Default()` into the four
compiled default entries, in order, because absent and empty mean the
same thing for a slice.

Per-component API groups (`<component>.cube-idp.dev`) are
reserved for kinds a component actually owns, never applied preemptively.

## 4. Interface doctrine

Two kinds of seams, bright line between them:

- **Kind A — consumer-side (the default):** the consuming package defines
  the 1–3 method interface it needs, only once a real second consumer or
  implementation exists. Domains return concrete structs. Mocks are
  hand-rolled function-field structs — no mockgen. Test seams are
  injected — a factory is passed as a parameter or struct field to the
  code that uses it, never exposed as a mutable package-level `var` that
  tests overwrite with save/restore; mutable package-level state is
  banned outside `main`.
- **Kind B — driver seams (the exception):** only for genuinely swappable
  backends (cluster providers, gitops engines). Interface + exported
  `Run<Seam>Conformance(t, factory)` suite live in the domain package;
  implementations live in subpackages and each runs the shared suite.
  Interfaces stay pure where possible (return objects; the caller
  applies); optional capabilities are separate small type-asserted
  interfaces; seams narrow over time, never widen casually.

Current seams: `cluster.Provisioner` (Kind B, kind driver);
`engine.Provider` (Kind B, tier-2 engine drivers; flux today — M10,
gated 2026-08-24). The engine seam covers the engine only — the tier-1
substrate is invariant platform and not behind it — and is deliberately
**pure** (content + predicates only, the caller applies), which lets its
conformance suite run hermetically against the real driver — no stateful
fake needed, and none is written. Purity also makes the seam
apply-path-agnostic, which is what lets a future second driver (Argo is
a legitimate one, behind its own design gate) land without reopening it
(see `docs/domains/engine.md`, the two-tier model).

## 5. Error architecture

Machinery (`internal/cubeerr`: `Code`, `Coded`, `Wrap`, `ExitCode`) is
separated from catalogs and never grows one. Each domain owns its
`CUBE-<TAG>-NNN` codes in its own `errors.go`; every user-reaching error is
a `*cubeerr.Coded` with summary + remediation wrapping the technical cause
(`%w` on every hop). Coded-error constructors are functions named
`new<Thing>Error` (exported: `New<Thing>Error`) — the `Err` prefix is
reserved for sentinel `var`s comparable with `errors.Is`, and this repo
has none; error identity is always the `cubeerr.Code`, asserted via
`errors.As`. Only `internal/cli/exit.go` renders errors and maps
exit codes: 0 success, 2 config error, 1 anything else. Domains never
print.

Tag registry (a new component adds a row; nothing renumbers):

| Tag | Component | Package | Status |
|---|---|---|---|
| `CFG` | config (api types + loader) | `internal/config` | active (`001..007`; M11 added `spec.gateway`/`spec.ca`/`spec.prerequisites` validation and **no new codes** — document-layer validation is `field.ErrorList` machinery, aggregated under the existing `CUBE-CFG-003`) |
| `CLI` | cli / output | `internal/cli` | active (no codes yet — the edge renders and composes; it has not yet originated one. See the queued gate event below) |
| `CLU` | cluster provider | `internal/cluster` | active (M3) |
| `KUB` | kube client access | `internal/kube` | active (M6) |
| `BST` | bootstrap (SSA/wait/inventory machinery) | `internal/bootstrap` | active (M7, narrowed M10: codes `003..006` plus the reconciliation-wait codes `009`/`010`. M11 **generalized the shipped wording, adding no number**: `005` now covers any kind-set wait bootstrap executes — the substrate or a prerequisite unit — and `009` any reconciliation wait over declared objects — the sync wiring, the declared engine content, or a prerequisite unit; `010` is raised at the failure point on both wait paths and never retagged as a timeout. `001`/`002`/`007`/`008` superseded by `ENG-003/004/006/005` — tombstoned, never reused) |
| `ENG` | gitops engine (invariant substrate + tier-2 driver seam) | `internal/engine` | active (M10: codes `001..006`; `003..006` supersede the moved `BST-001/002/008/007` checks) |
| `GWY` | gateway (trust-fabric prerequisite units, CoreDNS block) | `internal/gateway` | active (M11: codes `001..004`; all constructors unexported — the domain has no subpackages) |
| `CA` | certificate authority (mint/reuse, marker, trust ledger + store drivers) | `internal/ca` | active (M11: codes `001..005`; the ledger read, the provider switch, and the trust verbs' preconditions are raised through **exported** constructors, because the domain never touches files and never chooses the store) |
| `PKG` | pack contract, values, render, identity + deps | `internal/pack` | active (M8; M9 helm packs reused it, adding no tag and no code — `020` retired) |
| `REF` | reference resolution (grammar → tree/file) | `internal/ref` | active (M8) |
| `REG` | registry / OCI **publish** side | `internal/registry` | reserved |
| `APP` | apply / SSA / inventory | `internal/apply` | superseded (M7 — SSA absorbed privately by `internal/bootstrap`; no standalone apply domain, see 2026-08-06) |
| `TLS` | trust & credential bindings (#142: `secretRef`, source verification, `lock`/`mirror`) | `internal/trust` | reserved (re-scoped at the M11 gate, 2026-08-27: certificates/CA moved to `CA`) |
| `SPK` | spokes | `internal/spoke` | reserved |
| `ORC` | orchestrator (phases) | `internal/orchestrator` | reserved |

**Queued gate event — whether the CLI edge earns `CUBE-CLI-*`.** M11
grew the edge's composition substantially: the `trust` verb group, the
CA-reuse cluster read, the CoreDNS read-modify-write, and prerequisite
resolution. Those paths fail today by wrapping **another domain's**
code (`CUBE-CA-*` for a refused removal, `CUBE-GWY-004` for a corrupted
Corefile, `CUBE-REF-*`/`CUBE-PKG-*` for an override entry) or by
wrapping a stdlib/API-server cause uncoded (`readCAMaterial`'s
non-NotFound failure, the splice's exhausted conflict retry). That is a
deliberate reading of the rule that codes are never re-tagged across
domains — the edge does not claim another domain's failure — but it
leaves a class of genuinely edge-originated failures rendering without
a code. Whether `internal/cli` should therefore open its own catalog is
**recorded here as a future gate event, not a pending action**: it is a
decision to be taken at a gate, so that if it happens it is a choice
and if it does not it is also a choice, never drift. Nothing about it
is owed by M11.

`REF` and `REG` split OCI cleanly and must stay split: `ref` is the
**read** side (resolve a reference to a tree or a file, for any backend);
`registry` is the **publish** side (pushing pack artifacts). Neither owns
the other's errors. `PKG` no longer covers fetching — that moved to `REF` —
nor delivery, which is the M12 bus's.

## 6. Orchestration guard (future)

When `up` arrives it is a kubeadm-style phase runner: `[]Phase{Name, Run}`
sequenced by a function that only orders and times out. The orchestrator
depends on interfaces injected via a deps struct — it sequences, it never
decides. Leaf domains stay leaf; composition stays at the CLI/orchestrator
edge; size limits (function <50 lines, file <300) are CI-enforced.

## 7. Testing strategy

Table-driven tests wherever cases share one code path; error paths are
first-class rows. When cases need conditional setup, per-case mocking,
or branching assertions, write separate test functions instead of
forcing a table — a table with `if tt.explicit`-style branches is a
smell, not compliance. Error *identity* is asserted via `errors.As` into
`*cubeerr.Coded` + code equality — never by matching message strings.
Substring checks are fine for what they actually test: rendered CLI
output (codes, field paths, remediation on stderr) and context carried
in messages (paths). Golden files are the one sanctioned byte-exact
check, for CLI stdout. Tests and conformance suites obtain their context
from `t.Context()`, never `context.Background()` — cancellation at test
end is part of the contract being exercised. Filesystem access through
injected `fs.FS`, mocked with `fstest.MapFS`. Driver seams ship conformance suites
designed to run without live infrastructure (stateful fakes in the green
gate); real-backend runs are opt-in (`make test-e2e`) and never part of the
gate.

## 8. Dependencies

Runtime dependencies are a closed set; adding one requires an
owner-approved architecture/decision update, never a plan footnote:

| Module | Why | Confinement |
|---|---|---|
| `k8s.io/apimachinery` | metav1 types, validation/field — the KRM convention | — |
| `sigs.k8s.io/yaml` | strict YAML→JSON decoding honoring json tags | — |
| `github.com/spf13/cobra` | CLI framework (K8s ecosystem norm) | `internal/cli` |
| `sigs.k8s.io/kind` | library-first cluster provisioning (M3) | `internal/cluster/kind` only |
| `k8s.io/client-go` | Kubernetes client construction — REST config from kubeconfig bytes, discovery, RESTMapper, dynamic client (M6); everything downstream (apply, engine, doctor) builds on it | construction confined to `internal/kube` (see below) |
| `cuelang.org/go` | the pack metadata language (M8) — `#Values` is a **closed** definition, so a pack author can lock down, expose, and default their values surface; no cheaper format offers that | `internal/pack` only (the metadata/values files, plus the `pack new` scaffold's `cue/format` use) |
| `sigs.k8s.io/kustomize/api` + `kyaml` | kustomize rendering (M8) — building a kustomization is the tool's own job; exec-ing `kubectl kustomize` was rejected (no runtime dependency on a binary we do not ship) | `internal/pack/kustomize.go` only |
| `golang.org/x/mod/semver` | exact-SemVer validation of a helm pack's `chart.version` (M9) — a CUE regex is the first thing to drift from the spec it approximates, so the parser is the authority. Already in the module graph transitively, so this row records a **promotion to a direct import**, not a new module | `internal/pack` only |

Build-only: `sigs.k8s.io/controller-tools` (controller-gen, pinned Go tool
dependency). Heavy SDKs adopted later are confined to a single importing
file or subpackage. `k8s.io/client-go` deviates deliberately (decision
2026-08-04): its confinement is **construction-scoped** — only
`internal/kube` turns kubeconfig bytes into clients, but consumers may
reference its stable interface types (e.g. `dynamic.Interface`,
`meta.RESTMapper`) in signatures rather than mirror-wrapping them;
client-go sits closer to apimachinery's status than to kind's. Its version
is pinned to the apimachinery minor.

**M8 (pack) adds two runtime dependencies, each at its own gate:**
`cuelang.org/go` (2026-08-21, the design gate) and
`sigs.k8s.io/kustomize/api` + `kyaml` (with kustomize rendering). Both are
confined to a single file or file-group inside `internal/pack`; the
kustomize SDK is imported by `kustomize.go` and nothing else.

Kustomize carries one behaviour worth recording here, because it shapes the
domain's contract: **it resolves remote references over the network
unconditionally, and `krusty.Options` has no switch to forbid it.** Since
rendering is defined as a pure function of its inputs, `internal/pack`
rejects remote references in a pack payload before invoking kustomize
(`CUBE-PKG-021`) rather than letting a render reach the network. See
`docs/domains/pack.md`.

Still deferred to their own gates, and not importable before then:
`go-git/v5` (git backend), `oras-go/v2` (OCI backend, aligned with the M12 bus),
and the AWS SDK (S3 backend, on demand). `internal/ref`'s M8 backends —
local tree/file and HTTPS file — are **stdlib-only**. Exec-ing
`kubectl kustomize` stays rejected.

**`helm.sh/helm/v4` is removed from the deferred set** (decision
2026-08-23) — not deferred further, but *not owed at all*. Helm packs
delegate: a `type: helm` pack renders to a Flux `HelmRelease` plus its
source CR and the engine's helm-controller templates the chart in cluster,
so cube-idp never runs Helm and emitting the CRs is `unstructured` plus
apimachinery. See `docs/domains/pack.md`.

**M9 (helm packs) adds no new module, and promotes one.** The distinction
is worth keeping straight, because "no new dependency" and "no change to
the table" are not the same claim:

- **No new module.** Nothing was fetched that the build did not already
  carry: `go.sum` is byte-identical across the milestone, and the binary
  gained no third-party code. That is the same outcome M7 reached by
  embedding Flux rather than importing it.
- **One promotion, which the table records.**
  `golang.org/x/mod/semver` was already in the module graph transitively
  (via the k8s/cue/kustomize trees) and is now a **direct import** of
  `internal/pack`, so `go.mod` moves it out of the `// indirect` block. It
  earns a row above rather than a footnote: the closed set is the list of
  modules this code *imports*, not the list it *downloads*, and a reader
  auditing imports should find it there.

Adding a genuinely new module for this — `Masterminds/semver/v3`, Helm's
own, the fallback if the canonical round-trip proves too strict — **would**
be a full §8 gate event, and gets one if it happens.

**M9 extends the embedded Flux asset, not the dependency table.** The
vendored `flux install --export` output gains **helm-controller** and the
`helmreleases.helm.toolkit.fluxcd.io` CRD — regenerated at the pinned
v2.9.2 with `--components=source-controller,kustomize-controller,helm-controller`
and re-pinned by sha256. It stays what it already was: external data with
recorded provenance, regenerated by a `make` target, never fetched at
runtime. The bootstrap kind-set needs no change (it filters by kind).

**M10 (engine) adds no runtime dependency** — stated at its design gate
(2026-08-24) so the closed set stays closed by decision rather than by
accident. The seam is client-go interface types, apimachinery
`unstructured`, and function values; the substrate is embedded data plus
the stdlib, and the flux driver supplies wiring shapes and predicates
with the same vocabulary. The embedded Flux asset **moved** to
`internal/engine/substrate` — the invariant tier — re-homed as an
embedded *pack*: an import path and layout change, not a module-graph
change; the pin discipline (version constant, recorded sha256, `make`
regeneration, nothing fetched at runtime) transfers with it unchanged.

**M11 (gateway + ca) added no runtime dependency** — stated at its
design gate (2026-08-27) and **confirmed on merge**: `go.mod` is
byte-identical across the whole milestone stack. CA
minting is stdlib `crypto/x509` + `crypto/ecdsa`; the gateway is
content — an embedded Gateway API CRDs pack (substrate pin discipline:
the `CRDsVersion` constant, the recorded `crdsSHA256`, regeneration by
the `make gateway-api-manifests` target, never fetched at runtime; at
1,170,953 bytes the largest embedded asset, as the gate measured) and a
thin-helm CR pair emitted as `unstructured` (the M9 delegation shape),
digest-pinned by `ChartVersion` + `ChartDigest`.
One adjacent record: the shipped `trust` verbs execute the
**operating system's own trust tooling** (`security`(1) on macOS,
p11-kit's `trust`(1) on Linux) — a platform-tool
dependency, not a module, and distinct from
the rejected `kubectl kustomize` exec because no in-process
alternative to the OS trust store exists; a missing tool, an unusable
one, and an OS with no driver at all each fail coded (`CUBE-CA-004`),
and nothing is bundled.

**M7 (bootstrap) adds no runtime dependency** — a deliberate, load-bearing
outcome of two M7 decisions (2026-08-06). The Flux install manifests are
**embedded data** (`go:embed` of vendored `flux install --export` output),
not a Go import: the binary carries pinned bytes, gated by a version
constant + recorded sha256 provenance and regenerated by a `make` target —
never fetched at runtime (hermetic gate + air-gap posture preserved). SSA
and the readiness wait are **hand-rolled on the already-present
`k8s.io/client-go` and `k8s.io/apimachinery`**: the measured alternative,
`fluxcd/pkg/ssa`, would have added +37 modules, +72 `go.sum` lines, and
~3.6 MB to the binary while back-dooring `sigs.k8s.io/controller-runtime`
past the M6 gate's explicit rejection — rejected for a job reduced to
applying one known manifest set. The vendored Flux manifests are an
external artifact with recorded provenance, not a dependency-table row.

## 9. Architecture diagrams (C4)

C4 views of the system, exported from the Structurizr model at
`docs/architecture/workspace.dsl` (the source of truth, which also carries
the regeneration commands). The SVG images embedded below live in
`docs/architecture/` and are rendered from that DSL via the
c4-architecture pipeline (C4-PlantUML) — regenerated, never hand-edited.
Arrow labels are the relationship action with its technology in
brackets; each diagram carries its own legend.

System context — who and what surrounds the cube-idp binary:

![SystemContext view](architecture/structurizr-SystemContext.svg)

Containers — the binary and the content it owns, shares, or reads on
the operator's machine:

![Containers view](architecture/structurizr-Containers.svg)

Components — the packages inside the binary and their import direction:

![Components view](architecture/structurizr-Components.svg)
