# Decisions (append-only)

Dated, owner-approved decisions with rationale. Newest last. Entries are
short; the living state they produced is in `docs/ARCHITECTURE.md` and
`docs/domains/`. Full pre-reset ADR corpus: `docs/archived/adr/`
(read-only history, superseded wholesale by the 2026-07-27 reset).

---

**2026-07-27 — Greenfield v0 reset.** The previous implementation
(~20.4k LOC, 33 packages, 45 ADRs) degraded structurally (god-file `up`,
central diag catalog with fan-in 30, factory import cycles). Rebuilt from
zero: standard Go skeleton, one package per domain, consumer-side
interfaces + driver-seam exception, per-domain error catalogs, CI-enforced
size limits. Old code stays browsable in git history (`9a1edd9`).
Record: `docs/archived/design/2026-07-27-back-to-basics-structure.md`.

**2026-07-27 — Config is KRM-shaped and CRD-ready.** Real apimachinery
TypeMeta/ObjectMeta, root API group `cube-idp.dev`, strict decode →
Default → Validate, controller-gen deepcopy committed. Rejected:
hand-rolled metadata (defeats mechanical CRD promotion).

**2026-07-29 — M3 cluster domain.** kind driven as a Go library
(`sigs.k8s.io/kind`, confined to `internal/cluster/kind`); Crossplane-shaped
`spec.cluster` (`provider` const + opaque `forProvider` RawExtension,
provider-validated at init time); full-lifecycle `Provisioner` driver seam
(conformance + e2e cleanup need Delete before any CLI exposes it);
cube-owned kubeconfig contexts `cube-idp.dev/<name>` via own minimal
kubeconfig structs (no client-go); hermetic test split (fake in gate, kind
behind `make test-e2e`). Record:
`docs/archived/design/2026-07-29-cluster-domain.md`.

**2026-07-29 — `init` has no `--name` override of a loaded config.** The
document is the single source of truth; flags only steer local side
effects (kubeconfig placement/context name). A future `--name` may seed a
*new* scaffolded document (M4), never mutate an existing one.

**2026-08-01 — Roadmap direction: committed through M5.** M4 = init
bootstrap (scaffold-on-absent-config, generated docker-style names);
M5 = cluster lifecycle completion (finish the domain before broadening).
M6+ is a directional default, re-evaluated after M5: kube → apply → pack
(the spine, made solid — consumers conform to pack, never the reverse) →
engine → registry → orchestrator-last → periphery in pull order. Paths
considered and rationale:
`docs/archived/plans/2026-08-01-roadmap-direction.md`.

**2026-08-01 — Pack direction (owner feedback on groundwork).** CUE stays
as the pack metadata language (`pack.cue`), preserving owner value
lockdown with explicit expose/defaults. Pack CR carries `packRef`
(https/s3/git/oci… backends via ONE shared resolver — the old codebase
implemented resolution three times), explicit `type: raw|helm|kustomize`
(no silent kustomize takeover), `uuid` identity (differently-customized
copies may coexist), restricted `category` values `gateway`/`engine`
(identification metadata, never behavior — engine and gateway are ordinary
packs), `values`/`valuesRef`, `externalManifests` with lifecycle grouping,
`dependsOn[]` by uuid-or-name, and a scaffold command (stub-first).
Groundwork: `docs/work/pack-groundwork.md`.

**2026-08-01 — Epic-driven milestone flow.** Supersedes "one green PR per
milestone": each milestone is a GitHub epic issue; tasks are individual
issues aligned with the operator BEFORE work starts and ticked off as PRs
close them; a milestone may span several small green PRs, each
referencing the epic. Scope change mid-milestone = new/closed task issues
+ operator alignment + doc updates, never silent. Every epic's final task
is "Docs & architecture consistent and updated" — the epic cannot close
before it. Labels: `epic`, `task`, `docs`, `scope-change`, `wontfix`,
`domain:<name>` as domains land. Pre-reset issues were closed wontfix the
same day (the tracker restarts with this flow).

**2026-08-01 — Documentation system reset.** ROADMAP moved to repo root;
all dated/pre-reset docs frozen under `docs/archived/`. Living set:
`docs/ARCHITECTURE.md` (cross-cutting, updated in place),
`docs/domains/<name>.md` (exactly one per domain), `docs/DECISIONS.md`
(this log), `docs/work/` (ephemeral — milestone plans/groundwork, deleted
in the milestone's closing PR; git history is the archive). Dated files
are banned outside `archived/`; a milestone's design gate is now an
ARCHITECTURE/domain diff + a DECISIONS entry, still owner-approved before
code.

**2026-08-01 — M4 init bootstrap design gate.** `init` scaffolds a
missing config file, then provisions from it; the `forProvider`
validation follow-up folds in. Positions: (1) scaffolding/serialization
is config-domain machinery (`ScaffoldFile` — fixed template with
`metadata.name` + `spec.cluster: {}`, validated through the standard load
pipeline in memory before writing, created `O_EXCL`); cluster never
writes config; the CLI edge composes scaffold-if-absent → load →
provision. (2) The docker-style `<adjective>-<noun>` generator lives in
`internal/config` (NOT `api/`, which stays logic-free); every wordlist
combination must match the api name regex. (3) `--name` never mutates an
existing document — a mismatch with `metadata.name` is new
`CUBE-CFG-005` ("edit metadata.name" remediation, exported constructor
for the edge); a *matching* `--name` proceeds, preserving `init`
idempotency (the 2026-07-29 no-mutation decision governs the mismatch
case only). (4) Provider-side validation is an optional type-asserted
capability beside the seam (`SpecValidator.ValidateSpec`, pure); kind
implements it with the strict decode already in `Ensure`, and its runtime
detection moves from `New()` to first provisioning call so `config
validate` works without Docker. Import direction untouched:
`internal/config` never imports `internal/cluster`; composition stays at
the CLI edge. Provider-payload failures keep the provider's code
(`CUBE-CLU-003`, exit 1) — codes are never re-tagged across domains. No
ARCHITECTURE change: no new tag, no new dependency, capability pattern
already sanctioned (§4). *Amended 2026-08-02:* after the go-skills
review the owner reversed the uncoded-scaffold-errors call —
already-exists and scaffold I/O failures are coded (`CUBE-CFG-006`,
`CUBE-CFG-007`) so the scaffold path carries remediation and exit 2
like every other config error.

**2026-08-02 — Go conventions aligned with the go-skills review (rules
audit).** Owner-approved amendments to CLAUDE.md §2/§3/§5 and
ARCHITECTURE §4/§5/§7: test seams are injected, mutable package-level
state banned outside `main`; formatting joins the lint gate
(gofmt/goimports formatters, `gofmt -l .` silent); tests and conformance
suites use `t.Context()`; stdlib error checks use
`errors.Is(fs.ErrNotExist)`-style, never the legacy `os.IsNotExist`
family; coded-error constructors are named
`new<Thing>Error`/`New<Thing>Error` — the `Err` prefix is reserved for
`errors.Is`-comparable sentinels (which this repo has none of), and the
existing `Err*`/`err*` constructors are renamed in one mechanical PR
after the in-flight M4 branches merge; table tests only where cases
share one code path, separate functions otherwise; funlen excludes
`_test.go`; the string-matching ban narrowed to error *identity*
(substring checks on rendered CLI output and message context stay
sanctioned); every exported identifier carries a doc comment; error
handling is handle-once (return wrapped or render at the edge, never
both). Rules the skills endorse (consumer-side interfaces, hand-rolled
mocks, driver-seam conformance suites, the load pipeline, type-asserted
capabilities, code-equality assertions) are recorded as unchanged.

**2026-08-02 — C4 architecture docs adopted.** The C4 model lives in
`docs/architecture/` with `workspace.dsl` as the source of truth
(regeneration commands in its header comment) beside the rendered SVGs;
`docs/ARCHITECTURE.md` §9 embeds the same views as mermaid. DSL, SVGs,
and mermaid embeds are regenerated together from the DSL — never
hand-edited.

**2026-08-02 — Cluster deletion command named `delete` (M5).** Operator
decision: the CLI verb exposing the seam's `Delete` is `delete` —
matching the seam method and kubectl-style verbs; `down` stays reserved
for the future up/down orchestrator. Cleanup semantics fixed at plan
time: `delete` resolves the cube from the config document (no `--name`,
never scaffolds), targets the same kubeconfig file init writes
(`--kubeconfig`, else `$KUBECONFIG`/`~/.kube/config`), removes only the
cube-owned entries losslessly via the map-based machinery, unsets
`current-context` only when it pointed at the removed context, skips the
write when nothing matched, and never unlinks a file — an emptied
kubeconfig stays on disk. `status` (same milestone) exits 0 whenever its
report succeeds; cluster-absent is a finding, not a failure.

**2026-08-03 — init split: scaffold-only `init`, new `create` provisions
(M5 scope change, #78).** Operator decision from testing feedback:
`init` must not spin up a cluster. It becomes config-only —
scaffold-if-absent (unchanged `--name` semantics), load+validate,
report, exit 0 idempotent — and drops its `--kubeconfig*` flags. The new
`create` command owns load → provision → kubeconfig context install and
never scaffolds (missing config is the loader's coded error, the same
doctrine as `delete`). Verb: `create`, pairing with `delete`; `up`/`down`
stay reserved for the future orchestrator, `apply` is reserved for the
SSA milestone, and a flag-modal `init` was rejected.

**2026-08-04 — M6 kube design gate (roadmap re-evaluation + new domain).**
The post-M5 re-evaluation confirms the default continuation: M6 = kube
(epic #81); the remainder (apply → pack → engine → registry →
orchestrator → periphery) stays directional. The pull-pack-forward reorder
was weighed and rejected — pack lands only once its prerequisites (client,
apply path) are real, per the 2026-08-01 rationale. Positions: (1)
`k8s.io/client-go` joins the closed runtime set, pinned to the
apimachinery minor, with **construction-scoped confinement**: only
`internal/kube` constructs clients from kubeconfig bytes; consumers may
reference client-go's stable interface types in signatures (closer to
apimachinery's status than kind's — mirror-wrapping rejected as ceremony).
(2) `internal/kube` is a shared leaf importing only `cubeerr` + client-go;
kubeconfig **bytes + context name are injected** at the CLI/orchestrator
edge — the domain never reads files, never derives the context name, never
imports `internal/cluster`. (3) **No driver seam**: one Kubernetes API,
nothing swappable; consumer-side doctrine applies (M7 defines what it
needs where it uses it). (4) Operator answers folded in: the milestone is
kube-only plus a thin user-visible proof (`status` gains one
API-reachability line composed at the edge via `kube.Ping`; unreachable is
a finding, not a failure); no near-term second-provider pull — the
bytes+context contract already accommodates `existing`/k3d, no extra room
reserved. Argo CD scope and air-gap commitment stay open, due before the
M8/M9 design gates. Living contract: `docs/domains/kube.md`.

**2026-08-06 — M7 bootstrap design gate (new domain + gitops-engine
direction).** The gitops **engine is a mandatory component; Flux is the
default, installed before all packs** as the bootstrap primitive.
Everything else — prerequisites included — is an ordinary pack reconciled
by the engine, ordered by engine dependencies. **This supersedes ADR-0045's
"prerequisites before the engine" ordering** (engine first now; recorded
explicitly). Positions:

1. **New domain `internal/bootstrap`, tag `BST`** (epic #92). It is a
   *micro-bootstrap applier only*: install Flux + its source/sync wiring,
   wait ready, hand over permanently. Steady-state ownership of all
   packs/manifests is the engine's; **no-engine operation is not a supported
   mode.** The reserved `APP` (`internal/apply`) tag row is **retired /
   superseded** — there is no standalone apply domain; SSA lives privately
   inside `bootstrap`.
2. **SSA is hand-rolled on `k8s.io/client-go`** via `internal/kube`'s
   clients, injected at the CLI edge as interface types (`dynamic.Interface`,
   `meta.RESTMapper`) — `bootstrap` never imports `internal/kube`. Decided on
   measured evidence: `fluxcd/pkg/ssa` v0.77.0 would add **+37 modules**
   (164→201 in the module graph), **+72 `go.sum` lines**, **+3.6 MB binary
   (+7.5 %, measured with `ResourceManager` linked)**, and back-door
   `sigs.k8s.io/controller-runtime` past the M6 gate's explicit rejection —
   for a job reduced to applying one known manifest set. kstatus/`cli-utils`
   are likewise excluded; readiness predicates read off `unstructured`
   status. **No new §8 runtime dependency** — the Flux manifests are embedded
   data, and client-go/apimachinery are already in the closed set.
3. **Wait scope = the bootstrap kind-set only:** CRD Established,
   Deployment/StatefulSet ready, Job complete, Namespace Active. Engine-CR
   readiness (GitRepository/Kustomization reconciled) belongs to the future
   M9 seam, not M7.
4. **Applier-seam supersession (Q1).** `docs/work/pack-groundwork.md` §2.1's
   exported obligation — a reusable `Applier` / "inventory-inside-Apply"
   seam that `pack.Install` injects — is **superseded**: `bootstrap`'s SSA
   stays private and M8 delivers packs by writing to the Flux source
   (delivery-through-engine). The bootstrap **inventory** (seed of `down`) is
   kept, in-domain. This design-gate PR reconciles pack-groundwork.md's stale
   `internal/apply` / "M7 apply implementation" references.
5. **Flux acquisition = embed.** `go:embed` of vendored `flux install
   --export` output (source-controller + kustomize-controller at minimum),
   pinned version constant + recorded sha256 provenance + a `make` regen
   target. Fetch-and-verify at runtime rejected (breaks the hermetic gate +
   air-gap). The air-gap local-manifest override is **deferred to the M10
   air-gap decision**.
6. **Config surface `spec.engine`** (`EngineSpec`): minimal-typed now —
   `provider` (defaulted `flux`), `version`, `source` — validated in `api/`.
   M9 may migrate it to cluster-style `provider` + opaque `forProvider` as a
   design-gate event (noted in the domain doc). **CLI verb `cube-idp
   bootstrap`**; the 2026-08-03 reservation of `apply` is superseded (the
   `apply` verb stays retired).
7. **Demo source (git vs OCI) deferred (Q6).** In-cluster Flux cannot read a
   manifest directory on the operator's machine, so the pre-M8 source is a
   reachable git URL or an OCIRepository (the latter pulls an M10 concern
   forward). The call is deferred: tasks T5 (source/sync CRs) and T8 (e2e)
   are **sequenced last**, gated on a `M7-demo-source` checkpoint.
8. **ArgoCD** returns later only as an engine *pack*; replace-vs-layer
   (Argo-at-bootstrap vs Flux-deploys-Argo) is parked for the M9 design gate.
   Argo never becomes a compile-time dependency.

ROADMAP resequenced (directional continuation): M7 bootstrap → M8 pack
(delivery-through-engine, groundwork's 18 tasks) → M9 engine seam +
Flux-as-conforming-pack + the Argo question → M10 bus (git vs OCI; air-gap
answer due) → M11 thin `up`/`down` finisher. Rationale: M7 makes the product
demo-able (up → gitops-managed cluster) and M8 iterates against real
substrate. Living contract: `docs/domains/bootstrap.md`.

**2026-08-16 — M7 demo-source resolved: `spec.engine.source` is git|oci,
explicit `kind`.** The deferred Q6 (git-vs-OCI pre-M8 sync source) is
settled: bootstrap supports **both**, discriminated by an **explicit
`kind: git|oci` field** rather than URL scheme-sniffing — k8s-idiomatic and
mirroring `spec.cluster.provider`; the URL scheme still guards the kind
(`oci` requires `oci://`, `git` rejects it), so the two can never disagree.
The `EngineSource` shape (`kind`/`url`/`ref`/`path`/`interval`) is now
**finalized**, not provisional. git ⇒ `GitRepository` + `Kustomization`,
oci ⇒ `OCIRepository` (`provider: generic`) + `Kustomization`, both
`…/v1`. **Public URLs only** in M7; credential Secrets return with a real
consumer. Bootstrap applies + records the source CRs but does not wait on
their reconciliation (M9). Recorded in `docs/domains/bootstrap.md`.

**2026-08-21 — M8 pack design gate (new domain + new shared-infrastructure
leaf).** `internal/pack` (tag `PKG`) **defines, loads, validates, and
renders** packs; `internal/ref` (tag `REF`) resolves references. Under the
2026-08-06 delivery-through-engine reshape, **M8 renders and M10 delivers**
— cube-idp writes rendered content into the source Flux watches and never
applies. Corollary: M8 touches no cluster, so it is pure, hermetic, and
**has no e2e**. `cuelang.org/go` joins the closed runtime set, confined to
`internal/pack`'s metadata/values files; every other candidate (kustomize,
go-git, oras-go, AWS SDK, helm) is explicitly deferred to its own gate.
Positions:

1. **Identity: no `uuid`.** Artifact identity is `name`+`version` (plus a
   content digest when OCI lands); instance identity is an optional
   human-readable `spec.packs[].id` (DNS-label) that defaults to the pack
   name when unique and is **required when a name repeats**. `dependsOn`
   targets ids. This reverses the 2026-08-01 pack-direction call, which
   conflated artifact/lineage identity with installed-instance identity and
   let the CR override the artifact's own uuid — making it non-authoritative
   exactly where it mattered. Under delivery-through-engine each instance
   becomes a Flux `Kustomization` whose name must be stable and legible; a
   uuid yields `kustomization/b2c1-…` in every event and every error.
2. **Third package category: shared-infrastructure leaf.** `internal/ref`
   and `internal/kube` are a **closed, listed** set. A component domain MAY
   import a listed leaf; a leaf MAY NOT import a component domain or
   `api/config`. `pack` imports `ref` directly. Rejected: parking the
   resolver under `pack` (making one domain the accidental home of shared
   machinery — the `authclient`/`IsLocalRegistryHost` misplacement class),
   re-implementing per domain (the v1 triplication, now *across* domains),
   and an exported `Resolver` interface injected at the edge (premature by
   this repo's own "interface only at a real second consumer" rule).
   **MAY is not SHOULD:** injection at the edge stays preferred wherever
   the crossing value is already an interface, and the M7 rule that
   `bootstrap` does not import `kube` is unchanged.
3. **`pre` lifecycle: contract now, delivery later.** The CR keeps
   `lifecycle: pre|with`; `Render` returns `RenderPlan{Prerequisites,
   Objects}`; `with` is fully handled in M8, and `pre` is carried as data
   only. Real `pre` semantics need a separate Flux `Kustomization`, a
   `dependsOn` edge, a health gate, and stable names for both delivery
   units — all M10's contract. Joining `pre` documents ahead of the pack
   would look like ordering while providing no readiness, and M8 cannot
   verify readiness because it does not deliver. Rejected: dropping `pre`
   from the contract (churns the CR later) and faking it in document order.
4. **`pack new` is real when it lands; no stub verb.** The verb registers
   only once it creates a fresh directory (never overwriting), a valid
   `pack.cue`, a type-appropriate payload skeleton, and a pack that
   immediately renders. The internal `type: helm` **type**-stub stays —
   recognizing a type stabilizes the artifact schema, which is a different
   thing from shipping a user-facing command that only says
   not-implemented. `pack install` is likewise **not** added in M8.
5. **`dependsOn` lives at `spec.packs` only** — dropped from `pack.cue`.
   A pack-level `dependsOn` can only speak names, because an author cannot
   know installer-side ids; under position 1 it would resolve to a concrete
   edge only in the single-instance case and punt otherwise — conditional
   name-magic, crosswise to the identity decision itself. Entries are
   id-or-name; unknown, ambiguous, self, and cycle are each coded errors;
   **no implicit edges, ever**; executing the resolved order is M11's.
   This also removes the pack.cue-vs-`cube.yaml` union and its provenance
   apparatus — there is one source now. *Parked for its own future
   decision:* a `pack.cue` `requires:` capability expectation, checked at
   plan time and never auto-wired into ordering.

Adopted without reversal, from the independent design review: explicit ref
schemes only (no bare-git heuristics) with split `ResolveTree`/`ResolveFile`
(no invalid either/or result); three validation layers with `config
validate` staying **local-only, no I/O**, a new `pack validate <ref>`, and
a setup layer for everything that needs resolution; `pack render <ref>`
positional from day one with pure stdout (YAML only, diagnostics to stderr,
deterministic order, no partial output on failure); `category` in
`pack.cue` only, an open string with well-known spellings, identification
never behavior; distinct not-implemented codes per backend; `ResolvedGraph`
keyed by human instance ids; and kustomize values restricted to a flat
`map[string]string` feeding a defined `${VAR}` post-build substitution.

Four detail-level proposals carry a lean pending confirmation and are
recorded in `docs/domains/pack.md`: the strict `${VAR}` grammar (escape
`$${…}`, missing variable is an error, no shell-style defaults, scalar
values only); namespace conflict is an error rather than a silent
override; no-`#Values` packs keep pass-through; and `pack render` prints
prerequisites and objects as one deterministic stream, with the group
boundary living in the Go type rather than the YAML.

`docs/work/pack-groundwork.md` is **partially superseded** by this entry —
its `uuid` model (§2.4), its `pack.cue` `dependsOn` union (§2.8), its
`internal/apply`/`Applier` assumptions (already superseded 2026-08-06), and
its illustrative error numbering (§2.10). It stays until the M8 closeout PR
deletes it. Living contract: `docs/domains/pack.md`.

**2026-08-22 — Namespace scope reads a pack's bundled CRDs (M8, epic #113).**
The namespace transform decides scope in three layers: the static built-in
cluster-scoped set (unchanged, authoritative for core kinds), then the
`spec.scope` of any `CustomResourceDefinition` the pack itself renders,
matched by `(spec.group, spec.names.kind)`, then the namespaced default. A
self-contained pack already ships the definition of its own resources, so
the authoritative scope is in the payload — reading it is a fact, not a
heuristic, and it needs no cluster, which keeps rendering a pure function of
its inputs. The index is built from the pack's own rendered objects and used
for the external-manifest groups too, so one instance gives one consistent
answer; a definition delivered *as* an external manifest does not feed it,
because the payload is the artifact and what is delivered beside it is not.
A CRD with no `spec.scope`, or one this contract does not recognise, is
skipped rather than guessed at — no new error code. The sharp edge narrows
to **foreign** cluster-scoped CRs, whose definition the pack does not
bundle: nothing offline can know their scope, and the engine resolves them
correctly at apply. Contract: `docs/domains/pack.md`, "Namespace injection
and conflict".

**2026-08-22 — M8 closeout: `docs/work/pack-groundwork.md` deleted.** The
pack unit's pre-M8 shaping notes have been absorbed and are removed with
the milestone that consumed them, per the `docs/work/` rule (CLAUDE.md §1:
ephemeral, deleted in the closing PR). Where its content now lives: the
pack contract in `docs/domains/pack.md`, and everything it got wrong or
that was later overruled — the `uuid` identity model, the `pack.cue`
`dependsOn` union, the `internal/apply`/`Applier` assumptions, and its
illustrative `CUBE-PKG-*` numbering — in the 2026-08-06 and 2026-08-21
entries above, which already recorded those supersessions. **Earlier
entries in this file still point at the deleted path**; they are left as
written because this log is append-only, and this entry is the answer for
anyone who follows one. M8 shipped: `internal/pack` (`PKG`) and the
shared-infrastructure leaf `internal/ref` (`REF`), `spec.packs` with
instance identity and a `dependsOn` graph, and the `pack
render|validate|new` verbs. `internal/ref` remains **single-consumer**
(only `internal/pack` imports it), so `CUBE-REF-*` stays documented inside
`docs/domains/pack.md`; the `docs/domains/ref.md` split is scheduled with
the CLI→`ref` rewiring in #136.
