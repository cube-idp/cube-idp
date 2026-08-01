# Pack groundwork — research + shaping input for the M8 design doc

Date: 2026-08-01 (reworked 2026-08-01 after owner review)
Status: groundwork (research record + owner-directed shape). **Not a design
doc** — the M8 design doc in `docs/design/` formalizes the contract,
owner-approved. This file feeds it. Everything marked **owner-decided
2026-08-01** is a fixed input to that doc, no longer an option; §2.0 lists
the decisions, Part 3 turns them into tasks.
Inputs: old codebase at `9a1edd9` (`internal/pack` + `internal/oci` +
`internal/config`), frozen `docs/reference/pack-contract-v1.md`,
pack-relevant ADRs (0002–0009, 0015, 0016, 0021, 0022, 0035, 0045 —
read-only history, not binding), the back-to-basics structure doc (§6
interface doctrine), the cluster-domain design (worked example), the
roadmap direction doc, and the owner feedback session of 2026-08-01.

## Part 1 — what we had (old pack at 9a1edd9)

### 1.1 Subsystem map

`internal/pack` was 58 tree entries: 17 source files (~92 KB), 17 test
files, 1 embedded CRD manifest, ~23 testdata fixtures. Two more packages
were pack machinery living elsewhere only for import-cycle reasons:
`internal/oci` (push/pull artifact contract) and `internal/cnoe`
(ArgoCD-Application importer). Pack was imported by ~30 files across
`cmd/`, `up`, `diff`, `doctor`, `upgrade`, `bundle`, `syncer`, both
engines, both cluster providers, `refval`, and `ui` — the load-bearing
core of the old product.

| Subsystem | Files (bytes) | What it did |
| --- | --- | --- |
| Metadata + core model | `pack.go` (12.3k), `expose.go` (5.6k) | `Pack`/`Rendered` types; CUE `pack.cue` parsing (name/version/description/`#Values`/expose/images/dependsOn/gatewayService); `${GATEWAY_*}` token engine |
| Fetch / sources | `source.go` (10.9k), `getter.go` (8.9k), `resolve.go` (5k), `fetchfile.go` (3.8k), `guards.go`, `authclient.go`, `cachedir.go` | ref grammar → dir + pin: local dir, `oci://`, bare-git `//sub@rev`, explicit go-getter; symlink stripping, zip-slip guards, digest-keyed cache, upgrade-plan probing |
| Render backends | `render.go` (6.7k), `helm.go` (**17k**, largest file), `kustomize.go` (1.8k), `values.go` (6.2k) | raw sorted-walk, krusty build, helm-v4 client-side template + hook/CRD recovery; two distinct values-merge primitives |
| Dependency graph | `depgraph.go` (8.3k) | Kahn topo-sort over pack names + two *implicit* edge classes (gateway objects → gateway pack; repo delivery → gitea) |
| Catalog / index | `catalog.go` (6.2k) | OCI-hosted `index.json` (schemaVersion 1), 24h disk cache, built-in fallback |
| Engine/CR glue | `enginepack.go`, `discovery.go`, `manifests/pack-crd.yaml` | engine-as-pack verification; inert `packs.cube-idp.dev` Pack record CRD |
| OCI push (in `internal/oci`) | `push.go`, `pushdir.go`, `pull.go` | flux-shaped artifacts (fixed-epoch `created` → reproducible digests): rendered `all.yaml` for engine delivery; source dir for later fetch |

Dependencies it dragged in: `cuelang.org/go`, `helm.sh/helm/v4` (plus the
docker/containerd/sql-migrate transitive stack), `sigs.k8s.io/kustomize/api`
+ `kyaml`, `oras.land/oras-go/v2`, `go-containerregistry`, a **forked**
`go-getter` (pulling AWS SDK), `go-git/v5`, `json-patch/v5`, `golang.org/x/mod`.

### 1.2 What caused the ~59-file sprawl

Not interface ceremony — the old package had essentially one exported seam
and no mocks. Four concrete drivers:

1. **The ref grammar implemented three times.** One operation ("resolve a
   ref to bytes/dir + a pin") existed as `Fetch`, `FetchFile`, and
   `ResolveRemote`, each re-walking the same four-way scheme switch
   (~28 KB across four files). Every new source kind multiplied by three.
2. **Helm's impedance mismatch.** `helm.go` was dominated by recovering
   what helm's dry-run drops: install hooks, `crds/` CRDs, namespace
   injection, annotation stripping, dedupe — as large as the rest of the
   render layer combined, brushing the 300-line file limit.
3. **`${GATEWAY_*}` token substitution threaded through four seams** (raw
   bytes, kustomize output, merged helm values, expose URLs), each with a
   duplicated no-gateway guard and a justifying comment block.
4. **Import-cycle workarounds producing deliberate duplication.**
   `values.go` re-implemented `internal/refval`'s merge verbatim;
   `authclient.go` and `IsLocalRegistryHost` lived in `pack` only because
   the natural home (`oci`) would have cycled. The greenfield "factories at
   the edge / domains never import each other" rules are a direct reaction
   to this class.

Tests were ~1:1 with source plus a per-render-path fixture family — the
file count is mostly a faithful shadow of the scope creep above.

### 1.3 Recorded consumer requirements (what the M8 contract is written against)

- **Engine installs as an ordinary pack (ADR-0007).** `cube-engine-<type>`
  packs fetched/rendered/applied/locked through the *same* code path as any
  pack; identity guard (fetched pack name must equal expected name) before
  any mutation. ⇒ the new contract needs a machine-checkable pack **name**
  and a render path with no engine special-casing.
- **Registry push contract (contract §5, ADRs 0008/0015/0035).** OCI
  artifact of the *source dir*: ORAS PackManifest, flux config/content
  media types, one deterministic gzip-tar layer, fixed-epoch `created`
  annotation (identical content → identical digest), **tag == pack
  version**, immutable digests. ⇒ the new metadata needs **name + version**
  fields even though OCI is out of M8 scope, and the directory layout must
  stay cleanly tarrable (data-only, no symlinks).
- **Orchestrator ordering (ADRs 0005/0021).** Deps declared as pack names;
  deterministic topo-sort with declared-order tie-break; order translated
  to engine-native intent below the engine seam. ⇒ depgraph *execution* is
  excluded from the first pack milestones, but `dependsOn` is now
  **in-contract from day one** (owner-decided 2026-08-01, §2.8) — and
  render output must stay per-pack (never pre-merged across packs).
- **ADR-0045 prerequisites via SSA-with-wait.** Bootstrap packs (and the
  engine pack itself) delivered by one loop: fetch → render → SSA-with-wait
  → inventory record. ⇒ **`pack install` = render → apply-via-M7 is
  not a stopgap; it is literally the ADR-0045 delivery path.** Getting it
  right is getting the pre-engine bootstrap path right forever.
- **Cross-cutting invariants that earned their keep:** all rendering
  in-process (no shelled helm/kubectl); heavy SDKs confined to a single
  importing file/package (ADR-0022); data-only packs, zero-object render is
  an error; digest pinning for reproducibility.

### 1.4 What users actually touched vs machinery

User surface: `pack.cue` (name, version, description, `#Values`, expose,
dependsOn), the three payload shapes (`manifests/*.yaml`,
`kustomization.yaml`, `chart.yaml`) and their precedence, values/valuesRef
in `cube.yaml`, the `${GATEWAY_*}` tokens, the ref grammar, and the CLI
verbs. Everything else — pins, caches, guards, OCI media types, hook
recovery, cred chains — was invisible machinery, and that is where the
sprawl lived. Lesson: **the contract users author is small; keep the new
package as close to that contract as possible.**

### 1.5 Old CLI surface and UX pain

Verbs: `pack install [ref…]` (edits cube.yaml only), `pack list
--available`, `pack search`, plus a CI tier (`push`, `publish`,
`index build|push`). Recorded pain worth designing against:

- **No `pack render` existed.** No way to locally render/inspect/validate
  a pack; validation happened implicitly inside `up`/`diff` or as a
  publish side effect. (The new surface fixes exactly this.)
- **No scaffolding existed.** Every pack started as a hand-copied
  directory; nothing generated a valid `pack.cue`. (Fixed by `pack new`,
  §2.9.)
- `pack list` bare form was a trap (refused, reserved for future use).
- Two near-identical publish verbs differing only in tag enforcement.
- kustomization-vs-manifests precedence was a **silent behavioral cliff**
  (kustomization.yaml quietly took over the whole directory). (Killed by
  the explicit `type` field, §2.3.)
- Errors like "values on a chartless pack" fired only after a network
  fetch; gitea-presence checks used ref-substring matching.
- Old exit-code scheme was flat (everything → 1); greenfield's coded exit
  mapping is strictly better.

### 1.6 Verified v1 semantics the rework carries forward (verified 2026-08-01)

Re-checked in the code at `9a1edd9` against the owner feedback, because
several decisions hang on how v1 *actually* behaved:

- **`#Values` lockdown (pack.go `validateValues`) — the killer feature,
  confirmed.** User values are CUE-unified with the pack's optional
  `#Values` definition, then `Validate(cue.Concrete(true))` + decode.
  Because `#Values` is a CUE *definition* (closed struct), any user field
  the pack owner did not declare fails validation — the owner locks the
  surface down, exposing only what is needed. CUE `*default` markers fill
  unset fields, so the decoded result is the concrete, defaulted map.
  Packs declaring no `#Values` accepted any values unchecked (pass-through
  — keep-or-tighten is a design-doc detail). This whole mechanism carries
  forward verbatim in intent (§2.3, §2.6).
- **`valuesRef` was real at `9a1edd9`** — ADR-0004's "History" section
  (claiming it was never built) is stale. `internal/config/types.go`
  `PackRef.ValuesRef` fetches a BASE values document (exactly one YAML
  mapping) through the ref grammar via `FetchFile`; inline `values` are
  **RFC 7386 merge-patched on top** (null deletes, arrays replace) before
  the chart-defaults merge; the resolved pin was recorded in cube.lock
  (`valuesPin`); chartless guard CUBE-4016 applied to it like inline
  values. This is the prior art for §2.6.
- **`extraManifests` (v1's externalManifests) — prior art confirmed
  (ADR-0004, `internal/pack/render.go`).** A per-pack *inline* multi-doc
  YAML string in cube.yaml: parsed, `${GATEWAY_*}`-substituted, appended
  **after the pack's own rendered objects**, delivered and inventoried with
  them; invalid YAML was CUBE-4017; a pack with non-empty values or
  extraManifests was marked CUSTOMIZED on its Pack record. v1 had exactly
  one lifecycle position ("with the pack, appended after") and no remote
  refs — the inline channel was its ONLY channel, carried as one opaque
  string, and ADR-0004's own cons list records the cost ("mistakes
  surface only as a parse error rather than schema-level feedback"). The
  new `externalManifests` (§2.7) keeps both lessons: lifecycle groups fed
  by resolver-fetched refs AND inline manifests — the latter typed
  objects, not a string.
- **The ref grammar and its triplication** (`source.go` `Fetch`,
  `fetchfile.go` `FetchFile`, `resolve.go` `ResolveRemote`): accepted
  forms were a local directory path, `oci://host/repo:tag|@digest`,
  bare-git `<host>/<org>/<repo>[//subdir]@rev`, and explicit go-getter
  forms (`git::…`, `s3::…`, `http(s)://…`); pins were `oci:<digest>`,
  `git+<sha>`, `dir:<dirhash>`, `file:<sha256>`. Each of the three
  functions re-implemented the same scheme switch. One resolver replaces
  all three (§2.1). And ref resolution was never pack-only: `FetchFile`'s
  own doc comment names `spec.cluster.providerConfigRef` and remote `-f`
  (a cube.yaml fetched by ref) as consumers alongside
  `packs[].valuesRef` — the cross-domain evidence behind the §2.1
  placement call.
- **ADR-0007's identity guard is a NAME check.** `VerifyEnginePackRef`
  rejected a fetched pack whose declared `pack.cue` name ≠
  `cube-engine-<type>` (CUBE-0013) before any mutation. Relevant to the
  uuid analysis (§2.4): the guard verifies *what the artifact is*, and
  stays a name check; uuid never participates.
- **The two implicit depgraph edge classes** (gateway objects → gateway
  pack; repo delivery → gitea) existed in `depgraph.go` as documented.
  Per owner decision #9 they do **not** return: the new graph has explicit
  `dependsOn` edges only.

## Part 2 — the new `internal/pack`, owner-directed shape

### 2.0 Owner decisions (2026-08-01) — fixed inputs to the design doc

| # | Decision |
| --- | --- |
| 1 | **CUE stays.** `pack.cue` IS the metadata language; the `#Values` lockdown/expose/default mechanism (§1.6) carries forward. The old Fork-1/Q2 is answered. |
| 2 | **A scaffolding command exists** for pack boilerplate; may land early as a coded not-implemented stub with a `#todo` marker. Naming proposal in §2.9. |
| 3 | **The pack CR entry carries `packRef`** — the pack source, one of the supported backends (local dir, https, git, oci, s3, …). ONE resolver serves `packRef`, `valuesRef`, and `externalManifests` refs. Owner follow-up 2026-08-01: the resolver is shared infrastructure beyond pack (v1's `FetchFile` already served cluster's `providerConfigRef` and remote `-f`) — placement assessed in the bigger picture in §2.1; lean: an `internal/ref` leaf package, so future consumers never face a sideways import. |
| 4 | **`pack.cue` default field set:** `name`; `namespace` (optional — only to force everything into one namespace); `type: raw\|helm\|kustomize` (explicit — kills the v1 silent-takeover cliff); `uuid` (so two differently-customized copies of one pack can coexist). |
| 4a | **`category` field, in the CR too:** restricted spellings `gateway` and `engine` (at minimum), anything else free-form. Identification metadata only — never behavior (see #9). Enforcement decided (owner, 2026-08-01): reserved spelling only, no uniqueness rule; the spellings are intended to feed later *validation* (§2.5). |
| 5 | **`pack render` stays as previously proposed** (pure, fs.FS, stdout multi-doc, client-side dry-run). |
| 6 | **Values: inline `values` AND `valuesRef`** (remote ref to a values file) — helm and kustomize types only. Kustomize semantics decided (owner, 2026-08-01): post-build `${VAR}` substitution, Flux-postBuild-style, schema'd by `#Values` (§2.6). |
| 7 | **`externalManifests` returns:** extra manifests with lifecycle relative to the pack's rendered output; each lifecycle group joins into ONE YAML document; no fine-grained ordering inside a group. Entries carry a remote `ref` OR an inline manifest under key `manifest` (v1's inline channel returns — reassessed shape proposal in §2.7). Lifecycle set decided (owner, 2026-08-01): `pre \| with`, no `post` group (§2.7). |
| 8 | **`dependsOn[]` is in-contract from day one:** entries accept the uuid OR the name of any pack in the setup. Resolution rules + error cases in §2.8. |
| 9 | **Engine and gateway are NOT special:** ordinary packs, rendered and deployed like any other. No implicit depgraph edges, no special-cased code paths. `category` identifies them; it never changes behavior. |
| — | **Helm implication:** `type: helm` is in the contract from day one, but helm *rendering* stays excluded from the first pack milestones — render/install on a helm pack return a coded not-implemented error until the helm milestone lands (same stub pattern as the scaffold command). |

### 2.1 Interfaces: where they genuinely belong

**Render backends are NOT a Kind-B driver seam** — unchanged from the
first draft, and *reinforced* by the explicit `type` field: dispatch is now
a declared enum, not payload sniffing. `Render` is one exported concrete
function; raw / kustomize / helm are unexported concrete functions behind
a `type` switch; helm's branch returns the coded not-implemented error
until its milestone. When helm lands, the first move is ADR-0022
confinement (single importing file or `pack/helm` subpackage), not an
interface.

**Fetch backends: assessed against the §6 Kind-B criteria — NOT a driver
seam either** (owner asked for an honest assessment; here it is):

| Driver-seam criterion (cluster/engine satisfy these) | Fetch backends |
| --- | --- |
| User *configures* exactly one backend (`provider: kind`) | Selection is per-ref by scheme; one setup freely mixes `git`, `oci://`, `https`, local-dir refs simultaneously |
| Backends are mutually exclusive alternatives | They coexist; the scheme *is* the selection — nobody "swaps s3 for git" on the same ref |
| Swap is a real product promise | The promise is *coverage* (more schemes), not swappability |
| One behavioral contract a conformance suite asserts | **Partially genuine**: "resolve ref → dir/bytes + stable pin, idempotent, symlink-safe" is a real shared contract — but it needs a shared *test helper*, not an exported interface |

**Recommendation: a closed set of concrete functions behind ONE exported
entry point.** The resolver exports
`Resolve(ctx, ref, opts) (Resolved, error)` where `Resolved` carries a
directory (packRef mode) or a single document's bytes (valuesRef /
externalManifests mode) plus the pin. Inside: one unexported
scheme→fetch-function table, one file per backend. This kills the §1.2 #1
triplication structurally — one scheme switch, two post-conditions,
instead of three switches. Per-backend invariants (pin stability, symlink
rejection, single-file mode) are asserted by a shared table-driven test
helper run over every implemented backend. Unimplemented schemes return a
coded not-implemented error (stub-first, §3). **Per-backend dependency
gates are design-doc events**: git → `go-git` (not the forked go-getter),
oci → `oras-go/v2` (aligned with the M10 registry milestone's choice),
s3 → an AWS SDK (demand-driven; the old forked-go-getter/AWS-SDK drag is
exactly what the closed dependency set exists to prevent). Local dir and
https need stdlib only and come first.

**Placement — assessed in the bigger picture (owner follow-up,
2026-08-01): the resolver is NOT pack-private, so it must not live inside
`internal/pack`.** The v1 evidence is direct (§1.6): `FetchFile`'s own
doc comment names `spec.cluster.providerConfigRef` and remote `-f` (the
cube.yaml itself fetched by ref) as consumers alongside
`packs[].valuesRef` — ref resolution served three domains even in the old
tree, and future cluster/provider-side ref fields (a `providerConfigRef`
analog, a provider `valuesRef`) would need it again. Parking it under
pack would leave every such consumer exactly three bad options: import
`internal/pack` sideways (banned — domains never import each other),
re-implement (the §1.2 triplication reborn *across* domains instead of
within one), or have the CLI edge inject pack's resolver (workable, but
it makes pack the accidental home of shared infrastructure — precisely
the `authclient`/`IsLocalRegistryHost` misplacement class this rework
exists to prevent). **Recommendation: `internal/ref`, a shared LEAF
package** — the same pattern the roadmap already uses for `internal/kube`
(a leaf consumed by later domains): it imports only `cubeerr` and its
per-backend SDKs (never `api/` or any domain), pack imports it today, and
cluster / the CLI `-f` edge / registry import the same leaf when they
grow ref fields — the import direction stays acyclic by construction. It
owns its own error catalog under a new tag, `CUBE-REF-*` (`REF` is unused
in the tag registry; adding the row is part of the T1 design event per
the doc map). Consumers wrap its errors with their own context per the
`%w` rule; exit code stays 1.

**The one genuine interface: a Kind-A consumer-side applier seam, arriving
with `pack install`** — unchanged. Pack must not import `internal/apply`
(domains never import each other), and `internal/cli` must not grow the
render→apply→wait sequencing (zero business logic):

```go
// internal/pack/install.go — defined WHERE USED, 1 method, real impl exists (M7)
type Applier interface {
    Apply(ctx context.Context, objs []*unstructured.Unstructured) error // SSA + wait
}

func Install(ctx context.Context, a Applier, opts InstallOptions) error
```

The CLI edge constructs the M7 apply implementation and injects it —
mirror image of `cluster.Init(ctx, p Provisioner, opts)`. Hand-rolled
function-field mock in tests. **Method shape (owner-decided 2026-08-01):
error-only — `Apply` does NOT return the applied object set.** Rationale:
inventory is the apply domain's own responsibility (one audited recording
path inside the M7 implementation, instead of bookkeeping every apply
consumer must remember); SSA applies exactly the set it is handed, so a
return adds no information pack needs; and the narrowest seam keeps the
mock trivial, widened later only if a real consumer arrives (doctrine:
narrow over time, never widen casually). Ownership flows *down*, not
back up: pack stamps identity labels (pack name + effective uuid) on the
objects it hands to `Apply`, and the M7 inventory records what it sees —
`down`/`diff` later read M7's inventory, never pack. Whether stamping
happens at render or just before apply (keeping `pack render` stdout
pristine) is a design-doc detail. Dry-run is construction-time
configuration of the M7 applier at the CLI edge, not a seam method. The
M7 design doc must adopt the inventory-inside-Apply contract — that is
the one obligation this decision exports.
The `externalManifests` `pre` lifecycle group (§2.7) rides the same seam:
an ordered sequence of `Apply` calls, no new interface.

Decision #9 lands here too: the old `enginepack.go` has **no analog**. The
ADR-0007 name guard generalizes into a plain exported helper ("fetched
pack's declared name must equal expected name") that any consumer — the
engine milestone included — may call; pack itself contains zero
engine-aware or gateway-aware code paths.

### 2.2 Package shape and import direction

```
cmd/cube-idp ──▶ internal/cli ──▶ internal/config  ──▶ api/config/v1alpha1
                      │      ├──▶ internal/cluster ──▶ api/config/v1alpha1
                      │      ├──▶ internal/pack ─────▶ api/config/v1alpha1
                      │      │        │ (imports api + cubeerr + apimachinery/yaml
                      │      │        │  + cuelang.org/go; kustomize confined
                      │      │        │  per §2.1/ADR-0022)
                      │      │        └──▶ internal/ref  (shared LEAF resolver, §2.1:
                      │      │               imports cubeerr + per-backend SDKs only;
                      │      │               future consumers: cluster, cli -f, registry)
                      │      ├──▶ internal/apply (M7) — pack NEVER imports it;
                      │      │        cli passes apply's concrete type into pack.Install
                      └──────┴──▶ internal/cubeerr ◀── (config, cluster, pack, ref)
```

Hard rules restated for pack: never sideways into `cluster`, `kube`, or
`apply`; no factory inside the domain; no package whose only job is
cycle-breaking (that smell *is* the finding); the old `authclient`/
`IsLocalRegistryHost` misplacements must have no analog. Proposed file
shape (each under the 300-line gate; grows by task, §3):

```
internal/pack/
├── pack.go        # Pack, Rendered types; LoadDir (pack.cue parse + payload check)
├── values.go      # #Values unify/validate/default; valuesRef⊕inline merge
├── render.go      # Render() — type switch: raw walk; kustomize/helm dispatch
├── kustomize.go   # sole importer of the kustomize lib
├── deps.go        # dependsOn resolution over the installed set (§2.8)
├── install.go     # Applier seam + Install(); externalManifests lifecycle
├── new.go         # scaffold (stub first, then real)
├── errors.go      # CUBE-PKG-* catalog
└── *_test.go      # table-driven; fstest.MapFS fixtures

internal/ref/      # shared leaf resolver (§2.1 placement) — NOT under pack
├── resolve.go     # Resolve(): grammar, scheme table, dir/single-doc modes, pins
├── local.go       # local-dir backend (stdlib)
├── https.go       # https backend (stdlib)
├── stub.go        # git/oci/s3 not-implemented stubs (replaced by T14–T16)
├── errors.go      # CUBE-REF-* catalog (new tag registry row, T1)
└── *_test.go      # shared per-backend invariant helper + tables
```

**`spec.packs` — the pack CR entry (owner-decided 2026-08-01: this shape
is in-contract; timing is settled by the task breakdown, not deferred).**
Sketch for the design doc:

```go
type ConfigSpec struct {
    Cluster *ClusterSpec `json:"cluster,omitempty"`
    Packs   []PackSpec   `json:"packs,omitempty"` // absent = no packs managed
}

// PackSpec is one pack in the setup.
type PackSpec struct {
    // PackRef is the pack source: local dir, https, git, oci, s3, …
    // (one ref grammar, one resolver — §2.1).
    PackRef string `json:"packRef"`

    // UUID optionally overrides the pack.cue uuid for THIS instance —
    // required when the same packRef is listed twice (§2.4).
    UUID string `json:"uuid,omitempty"`

    // Category mirrors/overrides pack.cue category (§2.5).
    Category string `json:"category,omitempty"`

    Values    map[string]any `json:"values,omitempty"`    // helm+kustomize only
    ValuesRef string         `json:"valuesRef,omitempty"` // same resolver; base doc

    ExternalManifests []ExternalManifest `json:"externalManifests,omitempty"` // §2.7

    // DependsOn entries are the uuid OR name of any pack in the setup (§2.8).
    DependsOn []string `json:"dependsOn,omitempty"`
}

// ExternalManifest is one entry: exactly ONE of Ref / Manifest (§2.7).
type ExternalManifest struct {
    Ref       string                `json:"ref,omitempty"`      // same resolver, single-document mode
    Manifest  *runtime.RawExtension `json:"manifest,omitempty"` // one inline Kubernetes object
    Lifecycle string                `json:"lifecycle"`          // §2.7 naming proposal
}
```

### 2.3 The pack.cue contract (owner-decided 2026-08-01: CUE is the language)

Decision #1 closes the first draft's Fork 1 and collapses its §2.5 options
A/B/C: the metadata file is **`pack.cue`**, keeping v1 continuity where v1
was good (`#Values`), with the owner's new default field set. Consequence:
**`cuelang.org/go` joins the closed runtime set — a design-doc event**,
recorded and justified in the M8 design doc (T1), confined per ADR-0022
discipline (only `internal/pack`'s metadata/values files import it).

Field set (the design doc fixes exact optionality and validation):

| Field | Req? | Semantics |
| --- | --- | --- |
| `name` | required | Human/type identity: *what the pack is*. ADR-0007-style expected-name guards check this; the future OCI repo path derives from it. Must NOT be uuid-shaped (§2.8). |
| `version` | required (kept from v1) | The future OCI push contract needs tag == version (§1.3); cheap to require now. |
| `uuid` | required, scaffold-generated | Instance/lineage identity (RFC 4122). Full analysis §2.4. |
| `type` | required: `raw \| helm \| kustomize` | `raw` ⇒ manifest files (`manifests/*.yaml`, sorted walk); `helm` ⇒ helm chart yaml; `kustomize` ⇒ kustomization file + structure in manifests. Explicit type replaces v1's payload sniffing — the silent-takeover cliff (§1.5) becomes unrepresentable. Payload not matching the declared type is a coded error, not a guess. |
| `namespace` | optional | ONLY to force everything into one namespace: when set, all rendered namespaced objects land in it (cluster-scoped objects untouched). Whether a conflicting in-manifest namespace is overridden or an error is a design-doc detail (lean: error — silent override is a new cliff). Absent ⇒ objects keep their own namespaces. |
| `category` | optional | §2.5. |
| `dependsOn` | optional | Author-declared deps; names only in practice (an author cannot know installer-side uuids) — unioned with the CR entry's `dependsOn` (v1 precedent). §2.8. |
| `#Values` | optional | The lockdown mechanism, carried forward verbatim in intent (§1.6): closed definition — undeclared user fields rejected; `*defaults` fill; validated+defaulted map feeds the type-specific values application (§2.6). |
| `description` | optional | Kept from v1; surfaced by future list/catalog UX. |

Dropped from the v1 field set (return only with their features, each via
its own design doc): `expose`, `images`, `gatewayService`, the
`${GATEWAY_*}` token engine — all gateway/air-gap features with no
consumer in the current horizon. Decision #9 makes `gatewayService`'s old
implicit-edge role permanently dead.

**Helm stub semantics (owner-decided):** the contract — pack.cue schema,
CR validation, scaffold — recognizes `type: helm` from day one; `Render`
and `Install` on a helm pack return `CUBE-PKG-###` "helm rendering not yet
implemented" (with the milestone pointer in the remediation) until the
helm milestone lands. Mirrors the scaffold stub pattern; both are explicit
tasks in §3 whose real implementations depend on them.

### 2.4 uuid vs name — the interplay analysis (requirement #4)

Two identities, deliberately separated:

- **`name` = what the pack is** (e.g. `traefik`, `cube-engine-flux`).
  Human-facing; the ADR-0007-style expected-name guard checks it; the
  future OCI repo path derives from it; display and docs use it.
- **`uuid` = which copy this is.** RFC 4122, generated by the scaffold
  (`pack new`) into pack.cue, never hand-typed. It exists so two
  differently-customized copies of one pack can coexist in a setup and be
  depended on individually.

Interplay findings the design doc must carry:

1. **ADR-0007 name checks: unaffected.** The guard answers "is this
   artifact the flux engine pack" — an artifact-kind question, so it stays
   a name check. Two copies of the engine pack in one setup are as legal
   as two copies of anything (decision #9); uuid distinguishes them.
2. **OCI tag==version: a real collision, flagged for the OCI milestone.**
   The recorded push contract keys the repo path on name and the tag on
   version. Two same-name packs with different uuids and different content
   would collide at `<repo>/<name>:<version>` with different digests —
   breaking the immutable-digest promise. Options for that design doc:
   (a) uuid becomes part of the repo path (ugly, unreadable refs);
   (b) publishing requires a unique name — forking a pack for publication
   means renaming it, uuid still records lineage; (c) push-time collision
   error. Lean: (b) — coexisting same-name copies are an *install-side*
   feature; the publish side stays name-keyed.
3. **The same-ref-twice hole, and the CR override.** If customization
   happens via CR `values` (not by forking the directory), two entries
   with the same `packRef` share one pack.cue — and therefore one uuid.
   Proposed answer (visible in the §2.2 sketch): `spec.packs[].uuid`
   optionally overrides the pack.cue uuid for that instance, and
   validation requires the *effective* uuid to be unique across the setup
   (duplicate ⇒ coded error). Listing one packRef twice without distinct
   uuid overrides is exactly that error. **Owner-confirmed 2026-08-01:
   the uuid override IS the answer to same-ref-twice instance identity.**
4. **Fork workflow.** Copying a pack directory to customize its contents
   creates a new artifact ⇒ new uuid. The blessed path is the scaffold
   (`pack new --from <ref>`: copy + fresh uuid, a real-scaffold-task
   feature); hand-copiers who keep the old uuid are caught by the
   duplicate-effective-uuid validation only if both copies enter one
   setup — a documented sharp edge, acceptable.
5. **dependsOn:** uuid gives `dependsOn` an unambiguous target when names
   collide — the resolution rules in §2.8 depend on it.

### 2.5 category — identification metadata, never behavior (requirement #4a)

A free-form string field, in pack.cue (author-declared) and in the CR
entry (mirror/override for packs whose metadata lacks it), with
**restricted spellings**: `gateway` and `engine` at minimum (the design
doc may reserve more). Reconciliation with decision #9 is the whole
design: category **identifies** — it feeds status/summary output ("your
gateway is …"), discovery, and future doctor checks — and **never
selects a code path**: no implicit depgraph edges (the two v1 edge
classes are dead, §1.6), no render/install special-casing, no
category-keyed dispatch anywhere in `internal/pack`. A grep for
`category` in the domain should only ever hit parsing, validation, and
plumbing-through.

**Enforcement (owner-decided 2026-08-01): reserved spelling only** — no
uniqueness rule; two packs may both claim `gateway`. Owner note recorded
for the design doc: the reserved spellings are intended to feed later
*validation* — checks keyed to what a pack identifies as (e.g. expecting
a `gateway`-categorized pack to satisfy gateway-shaped expectations).
That is still identification/validation, never behavior, so decision #9
holds unchanged.

### 2.6 Values: inline + valuesRef; the kustomize assessment (requirement #6)

Carried v1 mechanics (verified, §1.6): `valuesRef` fetches a BASE values
document — exactly one YAML mapping — through the shared resolver
(single-document mode, pin recorded); inline `values` are RFC 7386
merge-patched on top (null deletes, arrays replace); the merged map then
passes through `#Values` (lockdown + defaults). Both fields are valid on
`type: helm` and `type: kustomize` only; on `type: raw` they are a coded
error — and because `type` is declared, the error fires as soon as
pack.cue is loaded, earlier than v1's post-fetch CUBE-4016 (the §1.5
pain).

Type-specific application of the validated map:

- **helm** (when its milestone lands): v1 order carried — chart defaults
  first, the validated user map merged over them, then template. (The
  `${GATEWAY_*}` substitution layer of v1's order is gone with the
  gateway features.)
- **kustomize — the honest assessment the owner asked for:** kustomize
  has no native values concept; its own customization axis is
  overlays/patches, which belong in the pack payload, not the CR. The
  only semantic that doesn't fight the tool is **post-build variable
  substitution**: `${VAR}`-style tokens in the built output replaced from
  the validated values map — precisely Flux Kustomization's
  `postBuild.substitute`, so there is real prior art and user
  familiarity. With `#Values` in front of it, kustomize packs get
  something Flux doesn't offer: a schema with lockdown and defaults over
  the substitution variables. Alternatives considered and rejected:
  values-as-strategic-merge-patch (duplicates what overlays already do,
  invites structural drift) and configmap-generator injection (only
  reaches workloads that mount it). **Owner-decided 2026-08-01: the
  recommendation is accepted** — valuesRef/values apply to kustomize
  packs with post-build `${VAR}` substitution as the defined semantic,
  schema'd (locked down and defaulted) by `#Values`.

### 2.7 externalManifests: lifecycle groups, ref or inline (requirement #7)

v1 prior art (§1.6): the ONLY channel was inline — one opaque multi-doc
YAML string under `extraManifests`, appended after the pack's objects,
one implicit lifecycle position. The return generalizes both axes:
**where** the manifests come from (a remote `ref` OR an inline manifest
under key `manifest` — the v1 inline channel, reassessed below) and
**when** they apply (explicit lifecycle groups). A full entry in the new
setup:

```yaml
spec:
  packs:
    - packRef: oci://registry/monitoring:2.1.0
      externalManifests:
        # remote: shared resolver, single-document mode
        - ref: https://example.com/crds.yaml
          lifecycle: pre
        # inline: one typed Kubernetes object per entry — real YAML,
        # not a string block; schema feedback at load time
        - manifest:
            apiVersion: v1
            kind: Namespace
            metadata:
              name: monitoring
          lifecycle: pre
        - manifest:
            apiVersion: v1
            kind: ConfigMap
            metadata:
              name: extra-dashboards
              namespace: monitoring
            data:
              custom.json: "{...}"
          lifecycle: with                  # v1's position, now explicit
        - ref: oci://registry/extras:1.0
          lifecycle: with
```

**Inline-shape reassessment (owner follow-up, 2026-08-01).** The v1
inline channel returns, but not in its v1 shape. v1 carried the objects
as one unstructured string, and ADR-0004's own cons list names the price:
mistakes surfaced only as a parse error (CUBE-4017), never as
schema-level feedback. The reassessed proposal: **`manifest` holds
exactly ONE Kubernetes object as a typed embedded value**
(`runtime.RawExtension` in the Go types — the same mechanism
`spec.cluster.forProvider` already uses, so the precedent is in-house),
authored as real nested YAML rather than an escaped `|` block. Several
inline objects = several entries, each with its own lifecycle — which is
what makes per-object lifecycle assignment expressible at all (the v1
string bound every object to one implicit position). Load-time
validation checks each entry is exactly one of `ref`/`manifest` and that
an inline object carries `apiVersion` + `kind`; the multi-doc-string
alternative (v1 continuity) was considered and rejected for exactly the
ADR-0004 cons. Inline entries never touch the resolver and need no pin —
the config document itself is their provenance.

Owner-sketched semantics, integrated: entries are grouped by lifecycle;
each group's documents — fetched and inline alike — are **joined into ONE
multi-doc YAML document** (join order = declared order, purely for
reproducible output — no ordering *semantics* inside a group); the `pre`
group is applied before the pack, the `with` group is appended to the
pack's rendered output and applied with it (exactly where v1 put
extraManifests). Application rides the §2.1 Applier seam — `pre` is
simply an earlier `Apply` call; whether `pre` waits for readiness before
the pack applies is an M7-alignment detail for the design doc (lean: yes
— that is the ADR-0045 SSA-with-wait shape, and it is what makes `pre`
mean something).

**Lifecycle set (owner-decided 2026-08-01): `pre | with` — exactly two
groups, no `post`.** Short, self-explaining relative to the pack
("before the pack" / "with the pack"). The earlier wording ambiguity
("before & post apply" vs "before or with the pack") is resolved in
favor of the two-group model; if an after-the-pack group is ever wanted,
it slots into the same design (one more group) at near-zero structural
cost — but that would be its own recorded decision.

External manifests — fetched and inline — are parsed and validated like
any rendered output (a group yielding zero objects is an error, same as
a pack); render-time visibility (`pack render` printing them, marked by
group) is a design-doc detail worth having.

### 2.8 dependsOn: in-contract, uuid-or-name resolution (requirement #8)

`dependsOn[]` lives in both pack.cue (author intent — names in practice,
§2.3) and the CR entry (setup intent — names or uuids), unioned per v1
precedent. Proposed resolution, over the set of packs in the setup:

1. **Syntax splits the namespaces.** An entry that parses as an RFC 4122
   UUID resolves against effective uuids (§2.4); anything else resolves
   against names. To keep the split airtight, pack **names must not be
   uuid-shaped** — a pack.cue validation rule, closing the ambiguity
   class at the source.
2. **Unknown ref** (no pack in the setup has that uuid/name) ⇒ coded
   error naming the entry and the pack that declared it.
3. **Ambiguous name** (two+ packs in the setup share the name — legal
   under §2.4) ⇒ coded error listing the candidates' uuids; remediation:
   depend on a uuid.
4. **Self-dependency** ⇒ coded error. **Cycles** ⇒ coded error naming the
   cycle (Kahn topo-sort carried from v1, with v1's deterministic
   declared-order tie-break).
5. **No implicit edges, ever** (decision #9): the graph contains exactly
   the declared entries. Resolution + validation live in `internal/pack`
   (`deps.go`) and can land early; *executing* the order remains the
   orchestrator's job (M11), which consumes the resolved order as data.

### 2.9 UX: command surface and ergonomics

```
cube-idp pack render  <dir> [--out <dir>]        # pure; no cluster, no config file needed
cube-idp pack install <dir> [-f cube.yaml] [--dry-run]
cube-idp pack new     <dir> [--type raw|helm|kustomize] [--name <n>] [--from <ref>]
```

`render` and `install` stay as previously proposed (decision #5): `render`
exists from day one; no bare-form traps; multi-doc YAML to **stdout**,
diagnostics to **stderr** (`cube-idp pack render d/ | kubectl apply -f -`
works before install lands); `render` is the client-side dry-run,
`install --dry-run` the server-side one; exit codes per the standing map.

**Scaffold naming proposal (requirement #2): `pack new`.** Rejected
alternative `pack init`: `init` already means "provision the cluster" at
the top level — reusing the verb for "write boilerplate files" overloads
one word with two unrelated semantics in one CLI. `new` says "create a
new pack" and has helm precedent (`helm create` was considered as `pack
create`; `new` is shorter and reads naturally after `pack`). Stub-first:
the verb registers early, returns `CUBE-PKG-###` not-implemented with a
`#todo` marker in the code; the real implementation generates the
directory + a valid pack.cue (fresh uuid v4, `--type`-appropriate payload
skeleton, `--from` fork mode per §2.4).

First pack in under a minute (real-scaffold era):

```
$ cube-idp pack new hello --type raw
$ kubectl create ns hello --dry-run=client -o yaml > hello/manifests/ns.yaml
$ cube-idp pack render hello
apiVersion: v1
kind: Namespace
metadata:
  name: hello
```

Failure, in the house error style:

```
$ cube-idp pack render ./packs/broken
✗ CUBE-PKG-004: cannot parse manifests/deploy.yaml
    yaml: line 12: mapping values are not allowed in this context
  → fix the YAML at manifests/deploy.yaml:12 and re-run `cube-idp pack render`
$ echo $?
1
```

### 2.10 Error catalog sketch (numbers illustrative; the design doc fixes them)

| Code | Meaning |
| --- | --- |
| `CUBE-PKG-001` | pack directory missing/unreadable |
| `CUBE-PKG-002` | invalid pack.cue (does not compile / missing or invalid fields / uuid-shaped name) |
| `CUBE-PKG-003` | payload does not match declared `type` (e.g. `type: kustomize`, no kustomization.yaml) |
| `CUBE-PKG-004` | manifest parse failure (file:line in cause) |
| `CUBE-PKG-005` | kustomize build failed |
| `CUBE-PKG-006` | pack (or externalManifests group) rendered zero objects |
| `CUBE-PKG-007` | install/apply failed (wrapped) |
| `CUBE-PKG-008` | not implemented in this build (helm render, scaffold — the stub code; message names the feature and its milestone) |
| `CUBE-PKG-011` | values/valuesRef on a `type: raw` pack |
| `CUBE-PKG-012` | values rejected by `#Values` (lockdown/validation failure) |
| `CUBE-PKG-013` | dependsOn: unknown reference |
| `CUBE-PKG-014` | dependsOn: ambiguous name (remediation: use uuid) |
| `CUBE-PKG-015` | dependsOn: cycle / self-dependency |
| `CUBE-PKG-016` | duplicate effective uuid in the setup |
| `CUBE-PKG-017` | invalid externalManifests entry (both or neither of `ref`/`manifest`; inline object missing apiVersion/kind) |

Ref-resolution errors leave the pack catalog with the resolver (§2.1
placement): `internal/ref` owns `CUBE-REF-*` — invalid/unsupported ref
(grammar error / unknown scheme), fetch failed (wrapped backend cause),
and backend not implemented (the resolver's own stub code, replacing the
former PKG-009/010 rows). Pack — and any future consumer — wraps them
with its own context per the `%w` rule; the tag registry gains the `REF`
row in T1.

Kustomize dependency (`sigs.k8s.io/kustomize/api` + `kyaml`) remains a
design-doc-gated adoption, confined to one importing file — unchanged from
the first draft; exec-ing `kubectl kustomize` stays rejected (ADR-0022
precedent).

## Part 3 — structured task breakdown

Discrete tasks first; milestone-sized chunks (one green PR each, per the
CLAUDE.md flow) after. "Gate: design doc" means the task cannot start
before that doc (or its section) is owner-approved. External dependencies
are named explicitly: **M6** (kube client), **M7** (apply/SSA + inventory,
and its Applier method shape), **M10** (registry/OCI milestone), and the
per-backend dependency gates from §2.1.

### 3.1 Tasks

**T1 — M8 pack design doc.**
Scope: formalize everything Part 2 shapes: the pack.cue schema (field set
§2.3, `#Values` semantics, uuid/name/category rules), the `spec.packs` CR
shape (§2.2), the resolver architecture + ref grammar + backend set and
stub policy (§2.1), values semantics per type incl. the kustomize
substitution call (§2.6), externalManifests lifecycle naming (§2.7, after
the clarify items resolve), dependsOn resolution rules (§2.8), the
resolver placement (`internal/ref` shared leaf, §2.1) with its
`CUBE-REF-*` tag registry row, the CUBE-PKG catalog, and the dependency
adoptions this milestone makes (`cuelang.org/go`, kustomize libs).
Deliverable: owner-approved design per the doc map — `docs/domains/pack.md`
(new domain file) + a `docs/DECISIONS.md` entry.
Dependencies: answers to the final open-questions list.
**Gate: this IS the design-doc gate for CUE + kustomize; per-backend
fetch SDKs and helm get their own later gates.**

**T2 — pack.cue loader + `#Values` machinery.**
Scope: `LoadDir` over `fs.FS`: parse + validate pack.cue (all §2.3
fields, uuid-shaped-name rejection, type/payload consistency check);
`#Values` unify/validate/default (§1.6 semantics). Sole importer of
`cuelang.org/go`.
Deliverable: `pack.go` + `values.go` (validation half) + table tests over
`fstest.MapFS`.
Dependencies: T1.

**T3 — raw render + `pack render` CLI.**
Scope: `Render()` with the `type` switch; `raw` implemented (sorted
`manifests/*.yaml` walk, parse, zero-object error, namespace forcing per
§2.3); stdout/stderr discipline; `--out`.
Deliverable: `render.go`, `internal/cli/pack.go`, golden-file tests.
Dependencies: T2.

**T4 — helm type stub** *(explicit stub task; the real helm task depends
on it).*
Scope: `type: helm` accepted by loader + scaffold + CR validation; render
and install branches return `CUBE-PKG-008` with a `#todo` marker.
Deliverable: the stub branch + an error-path test row.
Dependencies: T2 (lands inside the T3 PR at near-zero cost).

**T5 — `pack new` scaffold stub** *(explicit stub task; T12 depends on
it).*
Scope: cobra verb registered per §2.9 naming; returns `CUBE-PKG-008` with
`#todo`.
Deliverable: `new.go` stub + CLI test.
Dependencies: T1 (naming only) — may land early, per the owner note.

**T6 — kustomize rendering.**
Scope: `type: kustomize` branch — krusty build, kustomize libs confined
to `kustomize.go`.
Deliverable: real branch replacing nothing (type switch was built in T3),
fixture family, dep adoption recorded by T1.
Dependencies: T3. Gate: covered by T1's kustomize section.

**T7 — ref resolver core + backend stubs** *(the stub half of "exotic
backends"; T13–T16 depend on it).*
Scope: `internal/ref` — the shared LEAF package per the §2.1 placement
call, NOT under pack: `Resolve` with dir mode + single-document mode;
grammar; pins (`dir:<dirhash>`, `file:<sha256>`); symlink/zip-slip
guards; local-dir + https backends (stdlib only); scheme table returning
the `CUBE-REF-*` not-implemented code for git/oci/s3; the `CUBE-REF-*`
catalog + tag registry row; the shared per-backend test helper (§2.1).
Pack is the first importer; cluster ref fields, remote `-f`, and registry
import the same leaf later without any sideways dependency.
Deliverable: `internal/ref` package + tests.
Dependencies: T1.

**T8 — `spec.packs` CR sub-struct.**
Scope: `PackSpec` + `ExternalManifest` types per §2.2 in
`api/config/v1alpha1`, defaults + validation (packRef required, category
spellings, lifecycle enum, `ref`-XOR-`manifest` + inline
apiVersion/kind checks, effective-uuid uniqueness), `config
validate`/`show` coverage, deepcopy regen.
Deliverable: api types + validation tests.
Dependencies: T1. Independent of the render chain (parallel to T2–T6).

**T9 — dependsOn resolution.**
Scope: `deps.go` — graph build over the setup, §2.8 rules 1–5, resolved
deterministic order exposed as data for the future orchestrator.
Deliverable: resolution + topo-sort with full error-row coverage.
Dependencies: T2, T8.

**T10 — `pack install` + Applier seam.**
Scope: `install.go` per §2.1; `--dry-run` via server-side dry-run through
the seam; wiring at the CLI edge.
Deliverable: install path + function-field mock tests.
Dependencies: T3; **external: M7 (and M6 kube underneath it)**. The seam
shape is decided (§2.1: error-only, inventory inside the M7
implementation) — the remaining wait is M7 landing with that contract.

**T11 — externalManifests.**
Scope: fetch via T7 single-document mode; inline `manifest` entries
decoded from the CR (no resolver, no pin); §2.7 grouping + one-document
join across both sources; `with` group appended to render output; `pre`
group applied ahead through the Applier seam.
Deliverable: CR-to-apply path + tests (group of zero objects, bad ref,
invalid inline entry, mixed ref+inline group, lifecycle enum rows).
Dependencies: T7, T8; `pre` semantics depend on T10/M7. Lifecycle enum
decided: `pre | with` (§2.7) — no longer a blocker.

**T12 — values/valuesRef pipeline.**
Scope: `values.go` (merge half): valuesRef base ⊕ inline RFC-7386 patch →
`#Values` → type-specific application; kustomize post-build substitution
if §2.6's recommendation is confirmed; helm application deferred to T16.
Deliverable: merge + substitution machinery with tests; valuesRef pin
surfaced for future lock work.
Dependencies: T2, T7, T8. Kustomize substitution confirmed (§2.6) — the
full scope applies; no remaining blocker beyond its task dependencies.

**T13 — scaffold, real implementation.**
Scope: `pack new` generates directory + valid pack.cue (uuid v4,
type-appropriate payload skeleton); `--from <ref>` fork mode (fresh uuid,
§2.4).
Deliverable: working scaffold, round-trip test (`pack new` → `pack
render` succeeds for raw/kustomize; helm scaffolds then renders the T4
stub error).
Dependencies: T5, T2; `--from` also needs T7.

**T14 — git ref backend (real).** Gate: design doc for `go-git` adoption.
Dependencies: T7.
**T15 — oci ref backend (real).** Gate: design doc for `oras-go/v2`,
**aligned with the M10 registry milestone's SDK choice** — do not adopt
independently. Dependencies: T7; sequencing near M10.
**T16 — s3 ref backend (real).** Gate: design doc for an AWS SDK;
explicitly demand-driven — stays a stub until pulled. Dependencies: T7.

**T17 — helm rendering, real (its own milestone per the roadmap).**
Scope: replace the T4 stub: client-side template via the helm SDK,
hook/CRD recovery lessons from §1.2 applied (confined file/subpackage),
helm values application per §2.6.
Deliverable: `type: helm` end-to-end; the §1.2 "17k helm.go" is the
anti-benchmark. Gate: design doc for the helm SDK (the heaviest
dependency decision left). Dependencies: T4, T12.

**T18 — pack e2e.**
Scope: `make test-e2e` extension: render + install a fixture pack against
the M3 kind path (worktree-local KUBECONFIG), teardown via the cluster
seam.
Dependencies: T10.

### 3.2 Milestone-sized chunks (one green PR each) and the critical path

| Chunk | Tasks | External gates | Green proof |
| --- | --- | --- | --- |
| **C1 — contract + raw render** | T1, T2, T3, T4, T5 | owner sign-off on T1 (CUE + kustomize adoption recorded; kustomize code lands in C2) | design doc merged; `pack new` (stub), `pack render` on a raw pack; helm stub error |
| **C2 — kustomize render** | T6 | — | `pack render` on a kustomize pack |
| **C3 — CR + resolver + deps** | T7, T8, T9 | — | `config validate` covers `spec.packs`; `internal/ref` lands as a leaf (local+https resolve; git/oci/s3 stubs; `REF` tag row); dependsOn errors fire |
| **C4 — install + e2e** | T10, T18 | **M6, M7 landed** (Applier shape decided, §2.1) | `pack install` against kind, e2e green |
| **C5 — values + externalManifests** | T11, T12 | C4 for `pre` | valuesRef/values on kustomize (post-build substitution); externalManifests both groups, ref + inline |
| **C6 — scaffold real** | T13 | — | `pack new` → `pack render` round-trip |
| **later, each its own PR + gate** | T14 (git), T15 (oci, near M10), T16 (s3, on demand), T17 (helm) | per-task design-doc gates | backend/helm stubs replaced |

Chunk-splitting stays honest to the roadmap's M8a/M8b instinct: C1+C2 are
the old "M8a", C4 the old "M8b"; C3 and C5 are the new-requirements mass
(CR shape, resolver, values, externalManifests) that the owner feedback
added to the unit.

**Critical path.** The spine is T1 → T2 → T3 → T10 → T18 (contract →
loader → render → install → e2e), and its only *external* wait is
**M6+M7 before C4** — everything in C1–C3 is buildable with today's tree.
C2 and C3 are mutually independent and both depend only on C1, so they
can proceed in either order (or in parallel worktrees). With the
2026-08-01 clarifications resolved, C5's only remaining gates are C3 and
C4. The long tail is T17 (helm): last major dependency decision,
deliberately latest.

## Open questions

None — the five clarify items from the 2026-08-01 rework round are all
resolved (owner, 2026-08-01) and folded into the body:

1. externalManifests lifecycle set: **`pre | with`**, no `post` (§2.7).
2. Kustomize values: **recommendation accepted** — post-build `${VAR}`
   substitution, Flux-postBuild-style, schema'd by `#Values` (§2.6).
3. `category` enforcement: **reserved spelling only**, no uniqueness;
   spellings feed later validation, never behavior (§2.5).
4. Applier seam: **error-only `Apply`**; inventory recorded inside the
   M7 implementation; ownership via labels stamped by pack (§2.1). The
   one exported obligation: the M7 design doc must adopt the
   inventory-inside-Apply contract.
5. Same-ref-twice identity: **solved by the `spec.packs[].uuid`
   override** + effective-uuid uniqueness validation (§2.4).

T1 (the design doc) now has every input it needs; nothing in this file
is waiting on an owner call.

---
*Groundwork prepared 2026-08-01 and reworked the same day after owner review,
on branch `RafPe/implement-core-cluster-features`; research from `9a1edd9`
archaeology (incl. re-verification of `#Values`, `valuesRef`, and
`extraManifests` semantics) + pre-reset docs. No other files touched.*
