# Domain: pack

Living contract of the pack domain (`internal/pack` + `api/config/v1alpha1`
`spec.packs`) and of the shared-infrastructure leaf `internal/ref` it is
the first consumer of. Cross-cutting rules: `docs/ARCHITECTURE.md`.
Originating design gate: `docs/DECISIONS.md` 2026-08-21 (M8, epic #113).

## Purpose

A **pack** is the self-contained, versioned unit of platform content every
later milestone consumes. This domain **defines, loads, validates, and
renders** packs — and stops there.

It does **not** deliver them. Under delivery-through-engine (DECISIONS
2026-08-06) Flux owns steady state, and packs reach a cluster by being
**written into the source Flux watches**; that write path is M10.
cube-idp renders, never applies. Consequently M8 touches no cluster: the
domain is pure and hermetic, and **has no e2e** — rendering is a function
of its inputs, not of a live API server.

`internal/ref` is the single reference resolver. It exists as a shared leaf
rather than inside `pack` because ref fields are already foreseen for other
consumers (the CLI `-f` edge, cluster/provider refs, registry); the old
tree resolved references three separate times, and that is the sprawl this
placement prevents.

## What a pack is (`pack.cue`)

One artifact directory whose root carries a `pack.cue` metadata file. CUE
is the metadata language (owner-decided 2026-08-01) for exactly one
reason that no cheaper format offers: **`#Values` is a closed definition**,
so an author can lock down, expose, and default the values surface of their
pack. That lockdown is the differentiator; everything else in the file is
ordinary metadata.

| Field | Req? | Semantics |
|---|---|---|
| `name` | required | What the pack *is* (`traefik`, `monitoring`). DNS-label-shaped. Half of artifact identity; the future OCI repo path derives from it. |
| `version` | required | The other half. The recorded OCI push contract keys the tag on it, so it is required from day one. |
| `type` | required — `raw \| helm \| kustomize` | Explicit, **never sniffed**. `raw` ⇒ manifest files under `manifests/` (deterministic sorted walk); `kustomize` ⇒ a kustomization root; `helm` ⇒ a chart. A payload that does not match the declared type is a coded error, not a guess — this is what makes v1's silent-kustomize-takeover cliff unrepresentable. |
| `namespace` | optional | Forces every rendered **namespaced** object into one namespace; cluster-scoped objects are untouched. Absent ⇒ objects keep their own. Conflict rule below. |
| `category` | optional | Open string with well-known spellings (`gateway`, `engine` at minimum). **Identification only** — it feeds status/summary output, discovery, and future doctor/validation checks, and never selects a code path. A grep for `category` inside the domain may only hit parsing, validation, and plumbing-through. It lives in `pack.cue` **only**; there is no `spec.packs[].category` override until a real consumer needs one. |
| `#Values` | optional | The lockdown: a **closed** CUE definition. Undeclared user fields are rejected; `*defaults` fill; the validated, defaulted result feeds type-specific values application. |

**No `dependsOn` in `pack.cue`.** Ordering is an install concern and
instance ids are its only unambiguous target — see *dependsOn* below.

**Deliberately absent** (each returns only with its feature and its own
gate): `uuid` (superseded by the identity model below), `description`,
`expose`, `images`, `gatewayService`, the `${GATEWAY_*}` token engine.

**Helm is a type-stub, not a verb-stub.** The contract — `pack.cue`
schema, CR validation, scaffold — recognizes `type: helm` from day one so
the artifact schema is stable; `Render` on a helm pack returns
`CUBE-PKG-020` with the milestone in its remediation until helm rendering
lands (its own milestone, its own §8 gate). This is different in kind from
registering a user-facing no-op verb, which this domain does not do.

## Identity model

Two identities, cleanly separated (there is **no `uuid`** anywhere):

- **Artifact identity = `name` + `version`** (+ a content digest when
  OCI lands). It is what the pack *is*, authored in `pack.cue`, and it is
  what an expected-name guard checks.
- **Instance identity = `spec.packs[].id`** — a DNS-label, human-readable,
  authored in `cube.yaml` by the operator installing the pack. It is what
  *this copy in this setup* is called.

Rules:

- `id` is **optional**; the **effective id** defaults to the pack's `name`
  when that name occurs exactly once across `spec.packs`.
- `id` is **required** when the same pack name occurs more than once —
  two differently-valued copies of one pack are legal and must be
  individually addressable.
- Effective ids must be unique across the setup.
- `dependsOn` targets ids (id-or-name; see below).

Rationale over the rejected uuid model: under delivery-through-engine each
pack instance becomes a Flux `Kustomization` whose name must be stable and
human-readable. A uuid yields `kustomization/b2c1-…` in every `flux get`,
every event, and every dependency error — opaque exactly where a human is
reading. `name`+`version` already answers "which artifact"; a uuid only
ever answered "which copy", which is what `id` now answers legibly.

## Config surface (`spec.packs`)

A new optional field on `ConfigSpec`, with its defaults and validation
beside it in `api/config/v1alpha1` — the loading machinery never changes:

```go
// Packs are the pack instances in the setup; absent means none are managed.
//   ConfigSpec{ Cluster *ClusterSpec; Engine *EngineSpec; Packs []PackSpec }

// PackSpec is one pack instance in the setup.
type PackSpec struct {
    // ID is this instance's identity within the setup (DNS-label). Empty
    // defaults to the pack's own name, and is required when that name
    // occurs more than once across spec.packs.
    ID string `json:"id,omitempty"`

    // PackRef locates the pack artifact (one ref grammar, one resolver).
    PackRef string `json:"packRef"`

    // ValuesRef locates a base values document: exactly one YAML mapping.
    ValuesRef string `json:"valuesRef,omitempty"`

    // Values are merged over the ValuesRef document as an RFC 7386 merge
    // patch (null deletes, arrays replace). Opaque here; internal/pack
    // merges and validates against the pack's #Values.
    Values *runtime.RawExtension `json:"values,omitempty"`

    // ExternalManifests are delivered beside the pack, grouped by lifecycle.
    ExternalManifests []ExternalManifest `json:"externalManifests,omitempty"`

    // DependsOn entries are the id OR name of another instance in the setup.
    DependsOn []string `json:"dependsOn,omitempty"`
}

// ExternalManifest is one entry: exactly ONE of Ref / Manifest.
type ExternalManifest struct {
    // Exactly one of Ref (resolver, single-document mode) or Manifest
    // (exactly one inline Kubernetes object).
    Ref      string                `json:"ref,omitempty"`
    Manifest *runtime.RawExtension `json:"manifest,omitempty"`

    // Lifecycle is "pre" or "with"; empty defaults to "with".
    Lifecycle Lifecycle `json:"lifecycle,omitempty"`
}

// Lifecycle says when an external manifest is delivered relative to the
// pack: LifecyclePre ("pre") before it, LifecycleWith ("with") alongside it.
type Lifecycle string
```

`Values` and `Manifest` are `runtime.RawExtension` for the same reason
`spec.cluster.forProvider` already is: `api/` stays a pure contract with no
logic, deepcopy generates mechanically, and no new dependency is pulled in
to carry opaque JSON. Interpretation belongs to `internal/pack`.

## Validation in three layers

The layers exist because one of them may not perform I/O, and the identity
and dependency rules **cannot be decided locally** — they need each
`packRef`'s `pack.cue`.

| Layer | Where | Codes | Checks |
|---|---|---|---|
| **Document** (no I/O, ever) | `api/config/v1alpha1` `Validate()` | `CUBE-CFG-*` | required fields; `lifecycle` enum; `ref` XOR `manifest`; inline object carries `apiVersion`+`kind`; `id` is a DNS-label; **duplicate *explicit* ids**; `dependsOn` self-reference by explicit id; each ref is a **well-formed reference token** (non-empty, no whitespace or control characters) |
| **Pack** (after resolution) | `internal/pack` | `CUBE-PKG-*` | `pack.cue` compiles and satisfies the schema; payload matches the declared `type`; values satisfy `#Values`; render output is valid and non-empty |
| **Setup** (after every pack is resolved) | `internal/pack` | `CUBE-PKG-*` | effective-id derivation; "id required because this name repeats"; effective-id uniqueness; unknown / ambiguous `dependsOn`; cycles |

**Scheme grammar is not a document-layer concern.** Which schemes exist and
how each is spelled belongs to `internal/ref`, and `api/` can never import
it — restating a scheme table there would put a second grammar on the far
side of a boundary that cannot be closed, which is exactly the duplicated
reference handling this design set out to remove. So `config validate`
accepts `packRef: oci//typo`; the missing colon surfaces at resolution as
`CUBE-REF-*`. That is the layering working, not a gap: the document layer
is local-only and could not resolve the reference to check it anyway.

`config validate` runs the **document layer only** and never touches the
network or the filesystem beyond the document itself. `pack validate <ref>`
runs the document + pack layers for **one** pack. Codes are never re-tagged
across layers.

**The setup layer is library-only in M8.** It needs every `packRef`
resolved, and M8 exposes no command that resolves a whole setup — `pack
install` is not in M8, and `plan`/`up --dry-run` are M10/M11. So
`CUBE-PKG-015`…`019` are reachable through the domain API and its tests,
not through the CLI, until the command that consumes `ResolvedGraph`
lands. This is deliberate: the graph is built now because M10 and M11
consume it, not because M8 has a verb for it. Adding a whole-setup form to
`pack validate` is a CLI-surface decision for that milestone, not a
drive-by.

## Render: `RenderPlan`

```go
// RenderPlan is the result of rendering one pack instance: the objects the
// pack itself produces, plus the prerequisite objects declared beside it.
type RenderPlan struct {
    // Prerequisites are the lifecycle:pre external manifests. M8 carries
    // them as data only; delivery semantics are M10's.
    Prerequisites []*unstructured.Unstructured

    // Objects are the pack's rendered objects followed by the
    // lifecycle:with external manifests.
    Objects []*unstructured.Unstructured
}
```

`with` is fully handled in M8. `pre` is **carried, not implemented**: real
`pre` semantics under delivery-through-engine need a separate Flux
`Kustomization` for the prerequisite group, a `dependsOn` edge to the pack's
own, a defined health gate, and stable names for both delivery units —
all of which are M10's contract. Joining `pre` documents ahead of the pack
in one YAML stream would *look* like ordering without providing readiness,
and M8 cannot verify readiness because it does not deliver. So the group is
preserved structurally and honestly labelled deferred.

Rendering is deterministic and cluster-independent: sorted walks, stable
object order, no timestamps, no generated identifiers, no environment reads.

## Values pipeline

`valuesRef` (base) ⊕ inline `values` (RFC 7386 merge patch over it — null
deletes, arrays replace) → `#Values` (lockdown + defaults) → type-specific
application.

- `valuesRef` resolves through `internal/ref` in **single-document mode**
  and must be exactly one YAML mapping. Inline `values` must be a mapping
  too: RFC 7386 would have a list or a scalar replace the document
  wholesale, which is not a values map, so it is `CUBE-PKG-013` — raised
  before `#Values` is consulted, since it is not a schema failure and must
  fire on a pack that declares no `#Values` at all.
- Both fields are valid on `type: helm` and `type: kustomize` only. On
  `type: raw` they are a coded error — and because `type` is declared, that
  error fires as soon as `pack.cue` loads, not after a fetch.
- **helm** (when it lands): chart defaults first, the validated map merged
  over them, then template.
- **kustomize**: the validated `#Values` result must be a **flat
  `map[string]string`** — nested maps, arrays, `null`, and non-string
  scalars are a coded error. Kustomize has no values concept; its
  customization axis is overlays and patches, which belong in the payload.
  The one semantic that does not fight the tool is post-build variable
  substitution, which is exactly Flux's `postBuild.substitute` — real prior
  art, and with `#Values` in front of it kustomize packs gain a schema with
  lockdown and defaults that Flux itself does not offer.

### A pack payload is self-contained

**A kustomize pack may not reference anything remote** — no `https://` or
`http://` resource, no `github.com/org/repo` base, no `git@`/`git::`/`oci::`
form. Remote references are rejected before the build starts
(`CUBE-PKG-021`); bases are vendored into the pack instead.

This is not style, it is the hermeticity invariant. **kustomize resolves
remote references over the network unconditionally**, and there is no way
to configure it out: `krusty.Options` exposes only `Reorder`,
`AddManagedbyLabel`, `LoadRestrictions` and `PluginConfig`, none of which
governs remote loading — `LoadRestrictionsRootOnly` restricts *local* path
escape only — and a remote fetch bypasses the in-memory filesystem for the
real one. Left alone, `cube-idp pack render` would silently reach the
network and rendering would stop being a function of its inputs.

So `internal/pack` scans every kustomization in the payload
(`resources`, `components`, `crds`, `configurations`, patch paths, and the
deprecated-but-still-honored `bases`) and rejects remote-looking entries
before invoking kustomize. The scan reimplements kustomize's own unexported
heuristic and therefore **fails closed**: a local path wrongly rejected is a
clear coded error an author fixes by renaming or vendoring, while a remote
reference wrongly allowed would break the invariant silently. Helm and exec
plugins are already off — kustomize disables them by default — so
references are the only hole.

### `${VAR}` substitution grammar

- **Grammar:** `${NAME}` only, `NAME` matching `[A-Za-z_][A-Za-z0-9_]*`.
  The bare `$NAME` form is not recognized (it collides with shell habits
  and with kustomize's own `$(VAR)`).
- **Escaping:** `$${NAME}` renders the literal `${NAME}`. Nothing else is
  special; a `$` not followed by `{` is literal.
- **Missing variable ⇒ coded error** naming every unresolved variable.
  Silently substituting empty is how a deployment gets an empty image tag.
- **No shell-style defaults** (`:=`, `:-`). `#Values` defaults are the one
  defaulting mechanism; a second one in a different syntax is a cliff.
- **Scope:** substitution runs over **scalar values** in the built output,
  never over keys, comments, or raw bytes — the result can therefore never
  be invalid YAML.
- **Result is always a string scalar.** Substituting a whole scalar does
  not retype it: `replicas: ${COUNT}` with `COUNT: "3"` yields `"3"`, not
  `3`. Authors needing an integer keep it out of substitution.
- **Unused values are not an error** — a value may be consumed by one
  overlay and not another.

### No-`#Values` packs

A pack without `#Values` keeps v1 pass-through: supplied values are
accepted as-is (still flat-string-checked for `kustomize`). The sharp edge
is real and documented — a typo in values is silently accepted, because
there is no schema to reject it against. Revisit if it bites; making
`#Values` mandatory is a contract break better made deliberately than now.

### Namespace injection and conflict

`pack.namespace` is applied as a **post-render transform over the rendered
objects**, identical for every pack type. It is not delegated to a render
backend's own namespace transformer: the semantics belong to this contract,
so one implementation serves `raw` and `kustomize` alike.

Per object, when `pack.namespace` is set:

| Object | Result |
|---|---|
| cluster-scoped kind | untouched |
| namespaced, no `metadata.namespace` | `pack.namespace` injected |
| namespaced, same namespace | already correct |
| namespaced, **different** namespace | `CUBE-PKG-008` |

The conflict is an error, not an override: silently replacing an author's
explicit namespace is exactly the silent-takeover class this contract
exists to remove. Absent `pack.namespace`, objects keep their own.

**Scope is decided in three layers, and only the last one is a guess.**
Asking a live API server which kinds are namespaced would need discovery —
cluster access this domain must never have, since rendering is a pure
function of its inputs. So scope is decided offline, in this order:

1. **The static built-in set.** `internal/pack` carries one
   cluster-scoped-kind set, seeded from the well-known list kustomize keeps
   for the same reason (`Namespace`, `CustomResourceDefinition`,
   `ClusterRole`, `ClusterRoleBinding`, `PersistentVolume`, `StorageClass`,
   `APIService`, `PriorityClass`, `CSIDriver`, `CSINode`, the validating and
   mutating webhook configurations, `IngressClass`, `RuntimeClass`,
   `VolumeAttachment`, `Node`, `ComponentStatus`, and the cluster-scoped
   RBAC / certificates / apiregistration / flowcontrol kinds). It stays
   authoritative for core kinds; a kind added to it is an ordinary change,
   not a contract event.
2. **A `CustomResourceDefinition` the pack itself renders.** Every CRD in
   the pack's own output is indexed by `(spec.group, spec.names.kind)` →
   `spec.scope`, and a custom resource matching one takes that definition's
   answer — `Cluster` leaves it untouched, `Namespaced` injects. This is a
   fact, not a heuristic: a self-contained pack ships the definition of its
   own resources, so the authoritative scope is already in the payload and
   reading it needs no cluster. A definition that declares no `spec.scope`,
   or one this contract does not recognise, is skipped rather than guessed
   at — it is not an error, it just leaves the resource on the default.
   Group and kind together are the match: two groups may define one kind,
   and a definition governs only its own group.
3. **The default: namespaced.**

The index is built from the **pack's own rendered objects** and is then used
for the external manifests too, so one instance gives one consistent answer.
A definition delivered *as* an external manifest does not feed it: the pack
payload is the self-contained artifact, and what is delivered beside it is
not part of that artifact.

The consequence that remains, documented rather than hidden: **a
cluster-scoped custom resource whose definition the pack does not bundle —
a *foreign* CR — is treated as namespaced** and gets `pack.namespace`
injected into a field the API server ignores for a cluster-scoped resource.
Nothing offline can know better, since the definition is not here; the
engine resolves it correctly at apply, against a cluster that has the CRD.
Bundling the CRD, which a self-contained pack does anyway, removes the edge
entirely.

## externalManifests

Two axes, both explicit: **where** the manifests come from (a remote `ref`
resolved in single-document mode, XOR an inline `manifest`) and **when**
they apply (`lifecycle: pre | with`, defaulting to `with`).

Inline entries carry **exactly one** Kubernetes object as a typed embedded
value, authored as real nested YAML — not v1's opaque multi-document
string, whose price ADR-0004 itself recorded: mistakes surfaced only as a
parse error, never as schema-level feedback. Several objects mean several
entries, which is also what makes per-object lifecycle expressible at all.
Inline entries never touch the resolver and need no pin — the config
document is their provenance.

A lifecycle group that yields zero objects is an error, exactly like a pack
that renders nothing. Exactly two groups; a `post` group would be its own
recorded decision, at near-zero structural cost.

## dependsOn

**`spec.packs` only.** A pack-level `dependsOn` could only ever speak
names, because an author cannot know installer-side instance ids — so under
the locked identity model it would resolve to a concrete edge in the
single-instance case and punt to the operator otherwise. That is
conditional name-magic, crosswise to the very decision that separated
artifact name from instance id. Ordering is an install concern; ids are its
only unambiguous target. Dropping it also removes the whole
pack.cue-vs-`cube.yaml` union and provenance apparatus: there is now one
source.

Resolution over the instances in the setup:

1. An entry matches an **effective id** first, then a pack **name**.
2. **Unknown** target ⇒ coded error naming the entry and its declarer.
3. **Ambiguous name** (two instances of one pack name) ⇒ coded error
   listing the candidate ids; remediation: depend on an id.
4. **Self-dependency** and **cycles** ⇒ coded errors, the cycle named.
   Kahn topological sort with deterministic declared-order tie-breaking.
5. **No implicit edges, ever.** The graph contains exactly what was
   declared — no category-derived edges, no gateway-derived edges.

Resolution produces data; **executing** the order is the orchestrator's job
(M11):

```go
type InstanceID string

type ResolvedGraph struct {
    Order        []InstanceID
    Dependencies map[InstanceID][]InstanceID
}
```

Stable instance identities rather than integer indices, because M10 and M11
consume this across a package boundary.

*(Parked, its own future decision: a `pack.cue` `requires:` field expressing
a validated capability expectation — "expects a cert-manager" — checked at
plan time and **never** auto-wired into ordering. It recovers the
portability value without the name-expansion behavior. Revisit when M11
makes it concrete.)*

## `internal/ref` — the shared-infrastructure leaf

`ref` resolves a reference string to bytes or a tree, and nothing else. It
imports only `internal/cubeerr` and its per-backend SDKs — **never `api/`,
never a component domain** — and is imported directly by `pack` under the
shared-infrastructure-leaf rule (ARCHITECTURE §2). No exported `Resolver`
driver interface: there is one implementation and one consumer today, and
the repo's own rule is that interfaces arrive with a real second consumer.

Two consumer-oriented entry points rather than one result struct with an
either/or field (which would make invalid states representable):

```go
func ResolveTree(ctx context.Context, ref string) (ResolvedTree, error)
func ResolveFile(ctx context.Context, ref string) (ResolvedFile, error)
```

They share one parser and one backend table internally; the v1 problem was
repeated scheme switches, not consumer-oriented result types.

**Grammar — explicit schemes only**, no bare-git heuristics (which overlap
local paths, host-like directories, and future registry syntax):

| Form | Kind | Status |
|---|---|---|
| `./path`, `../path`, `file:///abs/path` | tree or file | M8 (stdlib) |
| `https://host/path` | file | M8 (stdlib) |
| `git+https://host/org/repo.git?ref=<rev>&path=<sub>` | tree | own gate — `go-git/v5` |
| `oci://host/repo:tag` or `…@sha256:…` | tree | own gate — `oras-go/v2`, aligned with M10 |
| `s3://bucket/key` | tree or file | own gate — AWS SDK, on demand |

Unimplemented backends are recognized by the parser and return their **own
distinct** not-implemented code, so the error names the backend and its
milestone. Resolution records a pin, enforces containment (no path
traversal, no symlink escape), and honors cancellation.

`ref` is kept **OCM-agnostic** — the one obligation the OCM evaluation
exported — so the M10 air-gap door (an optional CTF+signature transport
backend) stays open.

`CUBE-REF-*` is documented here, inside its only consumer's contract,
because `ref` is a leaf and not a component domain — and `internal/pack`
is still its only importer. When a second consumer lands (the CLI `<ref>`
edge, cluster refs, registry), it earns its own `docs/domains/ref.md`:
that move is a docs-map event, not a drive-by, and it is scheduled with
the CLI→`ref` rewiring in **#136**.

## Package shape and import direction

```
internal/cli ──▶ internal/pack ──▶ api/config/v1alpha1
                      │      (+ cubeerr, apimachinery/yaml, cuelang.org/go;
                      │       kustomize confined to one file)
                      └──▶ internal/ref   (shared-infrastructure leaf:
                             cubeerr + per-backend SDKs only)
```

Hard rules restated for this domain: never sideways into `cluster`, `kube`,
or `bootstrap`; no factory inside the domain (composition lives at the CLI
edge); no package whose only job is breaking a cycle — that smell *is* the
finding.

```
internal/pack/
├── pack.go      # Pack, Metadata, RenderPlan; Load (pack.cue + payload check)
├── values.go    # valuesRef ⊕ inline merge; #Values unify/validate/default
├── render.go    # Render() — enum switch over type; raw walk
├── kustomize.go # sole importer of the kustomize library
├── deps.go      # effective ids + dependsOn graph over the setup
├── external.go  # externalManifests → RenderPlan groups
├── new.go       # pack new: scaffold templates, fork, tree write
├── errors.go    # CUBE-PKG-* catalog
└── *_test.go

internal/ref/
├── resolve.go   # grammar, scheme table, ResolveTree/ResolveFile, pins
├── local.go     # local tree/file backend (stdlib)
├── https.go     # https file backend (stdlib)
├── stub.go      # git/oci/s3 — distinct not-implemented codes
├── errors.go    # CUBE-REF-* catalog
└── *_test.go
```

## Interface doctrine applied

**No Kind-B driver seam.** Render types dispatch by an **enum switch**, not
an interface hierarchy — they are three code paths in one package, not
swappable backends with independent implementations. `ref` backends
likewise stay internal. Concrete types are returned; `context.Context` is
first on everything that loads, resolves, or renders; mutable maps and
slices retained at exported boundaries are defensively copied. Functional
options wait for a real optional constructor setting.

## Error codes (`CUBE-PKG-*`, exit 1)

| Code | Meaning |
|---|---|
| `CUBE-PKG-001` | pack source missing or unreadable (no `pack.cue` at the root) |
| `CUBE-PKG-002` | `pack.cue` does not compile |
| `CUBE-PKG-003` | `pack.cue` fails the pack schema (missing/invalid `name`, `version`, `type`, `namespace`, `category`) |
| `CUBE-PKG-004` | payload does not match the declared `type` |
| `CUBE-PKG-005` | manifest parse failure (file:line in the wrapped cause) |
| `CUBE-PKG-006` | kustomize build failed |
| `CUBE-PKG-007` | rendered zero objects (the pack, or an `externalManifests` group) |
| `CUBE-PKG-008` | namespace conflict: `pack.namespace` set, object declares a different one |
| `CUBE-PKG-009` | `values`/`valuesRef` supplied to a `type: raw` pack |
| `CUBE-PKG-010` | values rejected by `#Values` (undeclared field, type mismatch, missing required) |
| `CUBE-PKG-011` | `type: kustomize` values are not a flat `map[string]string` |
| `CUBE-PKG-012` | `${VAR}` in the built output has no value |
| `CUBE-PKG-013` | a values document is not exactly one YAML mapping (`valuesRef`, or inline `values` that decode to a non-mapping) |
| `CUBE-PKG-014` | a resolved `externalManifests` `ref` is not exactly one Kubernetes object (several documents, or no `apiVersion`/`kind`) |
| `CUBE-PKG-015` | instance `id` required — this pack name occurs more than once |
| `CUBE-PKG-016` | duplicate effective instance id |
| `CUBE-PKG-017` | `dependsOn`: unknown target |
| `CUBE-PKG-018` | `dependsOn`: ambiguous name (remediation: depend on an id) |
| `CUBE-PKG-019` | `dependsOn`: cycle or self-dependency |
| `CUBE-PKG-020` | render for this pack type is not implemented in this build (the summary names the type and its milestone) |
| `CUBE-PKG-021` | a kustomize payload references a remote resource; the hermetic renderer rejects it rather than fetching (vendor the base into the pack) |
| `CUBE-PKG-022` | `pack new`: the target directory already exists — a pack is created, never merged into one |
| `CUBE-PKG-023` | `pack new`: the pack could not be created (unwritable target, unreadable `--from` source, an unscaffoldable type, or a forked `pack.cue` whose name cannot be rewritten) |

## Error codes (`CUBE-REF-*`, exit 1)

| Code | Meaning |
|---|---|
| `CUBE-REF-001` | malformed reference (grammar) |
| `CUBE-REF-002` | unsupported scheme |
| `CUBE-REF-003` | fetch failed (wrapped backend cause) |
| `CUBE-REF-004` | reference escapes its root (path traversal / symlink escape) |
| `CUBE-REF-005` | pin/integrity mismatch |
| `CUBE-REF-006` | tree/file mode mismatch (single-document expected, tree resolved, or the reverse) |
| `CUBE-REF-007` | git backend not implemented in this build |
| `CUBE-REF-008` | oci backend not implemented in this build |
| `CUBE-REF-009` | s3 backend not implemented in this build |

`pack` wraps `ref` errors with its own context per the `%w` rule and
**never re-tags them** as `CUBE-PKG-*`. Syntactic `spec.packs` problems
found during document validation stay `CUBE-CFG-*` (exit 2); resolution and
render failures are exit 1.

The tables above are the catalog: the illustrative numbering that circulated
in the pre-M8 shaping notes — including a `CUBE-PKG-014` "ambiguous name"
row — never shipped.

## CLI surface

```
cube-idp pack render   <ref>                    # artifact: the pack as authored
cube-idp pack render   -f <config> --id <id>    # instance: as the setup configures it
cube-idp pack validate <ref>                    # resolve + load + render-check one pack
cube-idp pack new      <dir> [--type raw|kustomize] [--name <n>] [--from <ref>]
```

- **`render` has two forms, and they are mutually exclusive.** A `<ref>`
  renders the pack as its author wrote it, with no setup around it.
  `-f <config> --id <id>` renders one `spec.packs` entry as that document
  configures it: values merged, external manifests attached to their
  groups, the pack's namespace applied to everything it delivers — the
  whole `RenderPlan` an instance means. The forms answer different
  questions and produce different output, so mixing them is an error
  naming which argument to drop rather than a silent preference for one.
  `-f` carries a repo-wide default, so only an explicitly given one asks
  for instance mode; `--id` without `-f` and `-f` without `--id` are both
  errors, because each alone names half of a target.
- **Instance mode resolves real sources; it is not offline-pure.** An
  instance is only defined by what its references resolve to, so rendering
  one reads the pack, its `valuesRef`, and its external refs. It reads
  **every** pack in the setup, not just the requested one: an effective id
  is a property of the whole setup — a pack's name serves as its id only
  while no other entry shares it — so one entry's identity cannot be
  decided without the others. The consequence, accepted rather than
  hidden: **an entry whose `packRef` cannot be read fails the preview of
  any other instance**, and the error names that entry's reference, not
  the one the user asked for. Identity derivation is the domain's
  (`CUBE-PKG-015`/`016` surface here unchanged); a `--id` matching no
  instance is a CLI-level error that lists the ids the document does
  declare, since a defaulted id is the one a user is likeliest to get
  wrong. Artifact mode stays pure, and **`config validate` is unchanged
  and stays local-only, no I/O**.
- The positional is `<ref>` from day one, not `<dir>` — a local directory
  is one reference kind, so `cube-idp pack render ./hello` works
  immediately and the syntax does not change when git or OCI land. C1
  ships the local-path form of exactly this grammar before `internal/ref`
  exists in C3; whichever implementation serves it, the contract is the
  same. **Where that stands today:** `internal/pack` resolves `valuesRef`
  and `externalManifests` refs through the leaf, while the CLI edge still
  carries C1's own local-path resolution for the `packRef`/`<ref>`
  positional. Moving that last one onto the leaf is its own scheduled
  change (**#136**) — it makes the CLI a second `ref` consumer, which is
  the docs-map event this contract records below — and not a drive-by
  inside a feature chunk.
- **`pack validate` renders and discards the output.** Loading alone would
  let it call a pack valid that `render` then refuses — the pack layer's
  checks include "render output is valid and non-empty", so an unparseable
  manifest, an empty result, a namespace conflict, and values the pack
  rejects all surface here. The one exception: a pack whose type this build
  cannot render still validates, because its metadata and payload are sound
  and only the render backend is missing.
- **stdout purity for `render`:** rendered YAML only on stdout, all
  diagnostics on stderr, no success banner, stable object order,
  deterministic document separators, a final newline, and **no partial
  stdout when rendering fails** — so `cube-idp pack render ./d | kubectl
  apply -f -` is safe.
- **`pack new` is real** — it creates a fresh directory (never
  overwriting), a valid `pack.cue`, a type-appropriate payload skeleton,
  and a pack that immediately renders. There is no stub verb: registering
  a command that only returns not-implemented misleads users. (`type:
  helm` remains a recognized *type*, which is a schema-stability concern,
  not a user-facing no-op — and for the same reason it is **not
  scaffoldable**: this build could not render what it wrote.) The target
  must not exist at all (`CUBE-PKG-022`); everything is assembled and
  validated in memory first, so a rejected name leaves no directory
  behind. `--name` defaults to the directory's base name.
- **`--from <ref>` forks, and a fork copies.** The source resolves through
  `internal/ref` as a tree, is loaded to confirm it is a pack, and is then
  copied wholesale. What that does and does not mean:
  - The copy **keeps the type its source declares**, so `--from` and
    `--type` are mutually exclusive: a conversion is not something this
    command could perform, and refusing is clearer than accepting the flag
    when it happens to agree.
  - **Without `--name` the copy keeps the name its source has** — the
    target directory is not a rename request. With `--name`, the rewrite
    touches **the `name` field of `pack.cue`** and nothing else. Names
    inside the payload are the author's content, so a forked manifest
    keeps whatever it said. A `pack.cue`
    that does not spell its name as a plain string cannot be rewritten
    safely; that fork is refused (`CUBE-PKG-023`) rather than delivered
    under the source's name, because two packs sharing a name is an
    identity collision in the setup that installs them.
  - Unimplemented backends surface `internal/ref`'s own codes, naming the
    backend and its milestone — a fork from `oci://` is not a broken path.
- **No `pack install` in M8.** It implies mutation and engine delivery;
  adding it before M10 would mislead users and pressure the design back
  toward direct apply. The retired `apply` verb stays retired.

### `pack render` output when prerequisites exist

`RenderPlan` has two groups but stdout is one YAML stream. Proposal: print
**one deterministic stream — `Prerequisites` first, then `Objects`** — so
nothing declared is ever silently dropped from stdout and the `| kubectl
apply -f -` path keeps working. The group boundary is preserved in the Go
`RenderPlan` type, which is what M10 consumes; it is *not* encoded in the
YAML (a comment marker is not semantics). If a machine-readable plan format
is ever wanted, it arrives as an explicit flag with its own decision.

## Testing

Pure and hermetic — the whole domain is a function of its inputs, so **M8
adds no e2e**. Filesystem access goes through injected `fs.FS`, mocked with
`fstest.MapFS`; the CLI edge converts an OS directory with `os.DirFS`.
Table-driven where cases share a code path, separate functions where they
need branching setup. HTTPS backend tests use `httptest.Server`, never a
mocked HTTP client. Every `ref` backend runs one shared behavioral helper
(a test helper, **not** an exported production interface) covering pins,
containment, mode restrictions, and cancellation. CLI golden tests assert
exact stdout, empty stderr on success, coded stderr and **no partial
stdout** on failure, and that output parses as valid multi-document YAML.

## Dependencies

`cuelang.org/go` joins the closed runtime set at the design gate
(ARCHITECTURE §8), confined to the metadata/values files of
`internal/pack`. `sigs.k8s.io/kustomize/api` + `kyaml` join it with
kustomize rendering, **confined to `kustomize.go`** — that file is the only
importer of the SDK, so the heavy dependency stays out of every other build
path in the domain.

**Owed at their own gates, not now:** `go-git/v5`, `oras-go/v2`, the AWS
SDK, and `helm.sh/helm/v4`, each with its own milestone. Exec-ing
`kubectl kustomize` stays rejected.

## Contracts for future domains

- **M9 (engine)** re-expresses Flux as a conforming pack and consumes this
  contract unchanged. Any pack-contract change from a consumer milestone is
  a design-gate event, never a drive-by edit.
- **M10 (bus)** owns **delivery**: writing rendered content into the source
  Flux watches, and the real `pre` semantics — a separate delivery unit for
  `RenderPlan.Prerequisites`, its `dependsOn` edge, its health gate, and
  stable names for both units. The air-gap answer is due there too.
- **M11 (`up`/`down`)** consumes `ResolvedGraph` as data and executes the
  order; resolution stays here.
- Not in this domain, ever on the current horizon: applying anything to a
  cluster, an `Applier` seam, inventory (bootstrap owns its own), implicit
  dependency edges, category-driven behavior, version constraint solving,
  and engine- or gateway-aware code paths.
