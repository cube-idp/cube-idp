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

**2026-08-23 — Helm packs render to a Flux `HelmRelease`, not via local
templating (M9, epic #139).** A `type: helm` pack is **thin**: chart
coordinates + a closed `#Values` overlay in `pack.cue`, and **no bundled
chart content** — bundled chart files are a payload mismatch
(`CUBE-PKG-004`), rejected rather than ignored. Rendering emits a Flux
`HelmRelease` (`helm.toolkit.fluxcd.io/v2`, the validated `#Values` result
nested verbatim into `spec.values`) plus its source CR
(`source.toolkit.fluxcd.io/v1` — `HelmRepository` + `spec.chart.spec` for a
classic repo, `OCIRepository` + `spec.chartRef` for `oci://`); the engine's
helm-controller pulls and templates the chart in cluster. cube-idp never
runs Helm.

Consequences, each deliberate:

1. **`helm.sh/helm/v4` is not adopted** — removed from the ARCHITECTURE §8
   deferred set rather than exercised. Emitting a CR is `unstructured` plus
   apimachinery, so **M9 adds no runtime dependency**, the same outcome M7
   reached by embedding Flux instead of importing it.
2. **Rendering stays hermetic** — no `helm template`, no chart, subchart, or
   repository fetch at render time. Render remains a pure function of its
   inputs.
3. **The `#Values` lockdown applies unchanged**, and helm packs are its
   clearest showcase: values are the only thing the pack contributes. The
   kustomize-only rules do not apply — nested values are emitted verbatim,
   so the flat-`map[string]string` restriction (`CUBE-PKG-011`) and the
   `${VAR}` substitution grammar are not in this path.
4. **`pack render` on a helm pack shows the `HelmRelease`, not the expanded
   chart** — a "delegated pack". Accepted and documented rather than
   worked around: expanding the chart is precisely what is delegated, and
   `pack validate` therefore cannot tell you the chart exists or that
   `#Values` matches the chart's own values.
5. **`pack.namespace` maps to `HelmRelease.spec.targetNamespace`**, because
   the workload objects do not exist at render time; the post-render
   namespace transform is structurally unrepresentable for helm and
   `CUBE-PKG-008` can never fire on one. It still runs over that pack's
   `externalManifests`. `install.createNamespace: true` is emitted with it:
   a thin pack has no payload to bundle a `Namespace` into,
   `externalManifests` is an operator-side surface whose `pre` lifecycle is
   deferred to M11, so refusing would leave `pack.namespace` silently
   requiring an out-of-band `kubectl create ns`. helm-controller does the
   creating; this domain still only renders.
6. **No new error codes.** Every helm failure lands on `CUBE-PKG-003`
   (schema, including the `chart` block), `004` (bundled chart content), or
   `010` (values). `CUBE-PKG-020` "render not implemented" is **retired**,
   its row kept and its number never reused.
7. **`pack new --from-chart <dir>`** scaffolds a thin helm pack from a
   **local chart directory** — a metadata read of `Chart.yaml` +
   `values.yaml`, no fetch, no new dependency. It is a separate flag from
   `--from`, which already means *fork another pack*: one flag meaning both
   would require sniffing pack-vs-chart, the guess `type` exists to
   abolish. Repo-index and OCI chart reads defer to the milestone that
   brings the backend.
8. **`chart.version` is validated by a real SemVer parser, not by the CUE
   regex.** `#ExactSemVer` is a shape check that fails obviously-malformed
   input at compile time; the Go loader is authoritative, applying the
   canonical round-trip `semver.Canonical("v"+v) == "v"+v` with
   `golang.org/x/mod/semver` — **already in the module graph** (indirect via
   k8s/cue), so this is an indirect→direct promotion with no new module and
   no `go.sum` change, not an §8 dependency event. (`Masterminds/semver/v3`,
   Helm's own, is the fallback if the round-trip proves too strict, and
   **that** would be an §8 gate.) Verified against `x/mod v0.37.0`: it
   accepts `6.5.4` and `1.2.3-rc.1` and rejects ranges, partial versions, a
   leading `v`, and SemVer build metadata. The last two are deliberate and
   reversible — accepting a spelling later is additive.

**Scope limit, stated rather than implied: M9 supports public chart sources
and non-sensitive values only.** There is no way to authenticate to a
private chart repository or registry (no `secretRef` on the emitted source
CR) and no way to supply a secret value (no `valuesFrom`). Everything in
`#Values` and in an instance's `values` is plaintext in four places — the
operator's `cube.yaml`, `pack render` stdout, the M11 delivery artifact,
and the `HelmRelease` CR. Private-source authentication and secret-backed
values are deferred to **#142 — Trust & credential bindings**, which also
owns source verification and the `lock`/`mirror` question. Credentials cut
across the source CR, the values surface, and the delivery artifact at
once: one milestone, one design gate. Relatedly, the **pack/instance
boundary** is now explicit in the contract — a publishable pack carries
chart coordinates, safe default `#Values`, and metadata, and never
environment specifics, credentials, or secret names. `pack.namespace` is
**unchanged from M8**; whether a fuller boundary eventually moves it is
#142's remit.

**Reproducibility, corrected (independent review of PR #141, 2026-08-23).**
The conclusion "no `pack.lock` in M9" stands; the reasoning first given for
it did not, and is replaced here. **An exact `version` on a `kind: repo`
chart does not prevent drift and does not identify identical software** —
a Helm repository index is the repository owner's to rewrite, so the same
chart version can serve different bytes over time. A repo-backed pack is
therefore a **mutable reference**, and exactness buys legibility and intent
(one pack identity names one *intended* release) rather than integrity.
Only an **OCI digest** pins content, so a digest is **recommended for any
published or production pack** — recommended, not required, because
requiring it would ban repo-only charts and every development workflow;
"required for published packs" is a production-profile rule, and profiles
do not exist yet. The real remedy is a `lock`/`mirror` operation resolving
a mutable reference to verified content, which is **#142**, not this
milestone.

**Requires extending the embedded Flux install** (currently
`--components=source-controller,kustomize-controller`) with
**helm-controller** and the `helmreleases.helm.toolkit.fluxcd.io` CRD:
regenerate `internal/bootstrap/assets/flux.yaml` at the pinned v2.9.2 and
update the recorded sha256. **The bootstrap kind-set and readiness wait
need no change** — they filter by *kind*, so the new Deployment and CRD are
waited on as soon as they are in the asset. This corrects the "the
readiness wait must cover it" framing in #139/#140.

Supersedes the "helm rendering via `helm.sh/helm/v4`" assumption in
`docs/domains/pack.md` and the M8 design gate (2026-08-21). **Also
re-sequences the roadmap**: helm is M9, engine → M10, bus → M11,
`up`/`down` → M12. Living documents are renumbered; the earlier entries in
this append-only log and the shipped `CHANGELOG.md` release notes are left
as written, and `/ROADMAP.md` carries the old→new map for anyone who
follows one — the same treatment the 2026-08-22 closeout gave to references
to a deleted path.

**Twenty-five detail-level proposals carry a lean pending confirmation**
and are recorded in a delimited "Proposals — M9 helm packs" section of
`docs/domains/pack.md`, deleted by the M9 closeout. Seven of them (the
lettered rows) and the correction to row 5 came from the independent design
review of PR #141. Living contract: `docs/domains/pack.md`, "Helm packs
(`type: helm`) — a delegated pack".

**2026-08-23 — M9 closeout: the "Proposals — M9 helm packs" section is
deleted.** All twenty-five leans were confirmed by the operator and are
written into the contract they proposed, so the delimited section in
`docs/domains/pack.md` is removed with the milestone that consumed it —
the same `docs/work/`-style discipline the M8 closeout applied to its
groundwork file. **The 2026-08-23 design-gate entry above still points at
that section**, and is left as written because this log is append-only;
this entry is the answer for anyone who follows the pointer. Nothing was
dropped: each lean now reads as ordinary contract text under "Helm packs
(`type: helm`) — a delegated pack", and the two leans that were still
labelled proposals inside the contract prose (the SemVer spellings, and
M8's single-stream `pack render` output) are restated as settled
behaviour.

M9 shipped: `type: helm` rendering to a Flux `HelmRelease` plus its
source CR, a type-discriminated `#Pack` schema, exact-SemVer validation
via `golang.org/x/mod/semver` (a promotion to a direct import — no new
module, `go.sum` unchanged, recorded as an `ARCHITECTURE.md` §8 table
row), helm-controller and the `helmreleases` CRD in the embedded Flux
asset, and `pack new --from-chart <dir>` / `--type helm`. **No new error
codes and no new tag** — helm reused `CUBE-PKG-*`, and `CUBE-PKG-020`
retired unreachable. Deferred by design and stated in the contract rather
than implied: private chart-source authentication, secret-backed
`valuesFrom`, and the `lock`/`mirror` operation that would turn a mutable
repository reference into verified content — all **#142**.

**2026-08-24 — M10 engine design gate (two-tier model: invariant
substrate + tier-2 driver seam; the gateway milestone inserted).** New
domain `internal/engine` (tag `ENG`, gated — activates with the code),
structured by the founding vision, recorded verbatim: *"we use Flux to
deliver the prerequisites and the engine itself (that being Flux or
ArgoCD) — from there the engine takes over installation and coordination
of packs."* Two tiers: **tier 1**, the invariant Flux substrate
(`internal/engine/substrate` — embedded, pinned, never driver-selected);
**tier 2**, the engine (`engine.Provider`, a Kind-B seam;
`internal/engine/flux` the only driver today — the degenerate case where
the substrate doubles as the engine). Positions, in the ranked order of
the post-M9 architecture review's eleven gate questions (epic #152),
reworked to the two-tier model by operator decision:

1. **Boundaries: substrate is invariant; the seam covers the engine
   only; bootstrap keeps tier-agnostic machinery.**
   `internal/engine/substrate` owns the embedded Flux asset (re-homed as
   a *pack*: version constant + sha256 + `make` regeneration unchanged)
   and the substrate-namespace fact (`flux-system` — where bootstrap's
   inventory records, injected as a string at the edge and
   conformance-tied to a `Namespace` object in the payload).
   `internal/engine/flux` owns the sync-wiring shapes and the reconciled
   predicates. `internal/bootstrap` keeps SSA, the by-kind kind-set
   wait, the inventory, sequencing — and executes the new phased waits —
   applying only injected content; it no longer embeds or knows Flux,
   and its retained codes' Flux-specific text goes engine-neutral.
   `CUBE-BST-001/002/007/008` — every code raised by moving content —
   are superseded by `ENG` codes (rows kept, numbers never reused); the
   machinery codes `003..006` plus the new `-009` and `-010` stay. Rejected: the
   substrate behind the seam (it would make helm packs conditional on
   provider choice — the exact inertness PR 161's first draft
   manufactured); the seam subsuming bootstrap; a hollow seam leaving
   engine content in bootstrap.
2. **Cross-domain contract types: a new shared-infrastructure leaf,
   `internal/plan` — "domains never import each other" stands with no
   exported-types escape hatch, and the leaf is how sharing happens
   instead.** Naming another domain's type in a signature is an import,
   and stays banned domain-to-domain. The delivery vocabulary —
   `RenderPlan`, `ResolvedGraph`, instance identity, and the single
   `EffectiveIDs` derivation — moves to `internal/plan`, third in the
   §2 listed leaf set (operator decision): pure data plus that minimal
   derivation logic, importing only `internal/cubeerr` and apimachinery
   (the neutral KRM vocabulary its types carry). The listing also
   sharpens the leaf qualification: "no domain concepts" reads as **no
   concept owned by a single component domain** — `plan` carries
   contract vocabulary belonging to several domains at once, which is
   exactly why none of them may own it. `pack` imports the leaf and
   produces its types; the M12 bus and the M13 orchestrator import it
   and consume them. **Timing: listed at this gate, instantiated by the
   M12 bus PR that lands the first real cross-domain consumer** — M10
   code is unchanged (bootstrap consumes injected function values,
   already neutral vocabulary). The leaf carries a **closed content
   rule**: every type or function added is gate-approved by name, never
   inferred, so it cannot drift into a general types bucket. The
   M10-approved member set, by name: `RenderPlan`, `ResolvedGraph`,
   `InstanceID`, `EffectiveIDs`, and `InstanceIdentity` — the neutral
   derivation input (a pack name plus an optional explicit id), required
   because today's `EffectiveIDs` takes `[]pack.Instance` wrapping
   `v1alpha1.PackSpec`, neither of which the leaf may see; `pack` maps
   its instances into it. `ResolveOrder` stays in `pack` (dependency
   resolution is pack contract) and returns the leaf's `ResolvedGraph`.
   The two guardrails thereby become **properties of the shared types
   rather than promises every consumer repeats**: `RenderPlan`'s
   `Prerequisites`/`Objects` group boundary is preserved because there
   is only one delivery type and it has the boundary; effective-id
   derivation is single-sourced because `EffectiveIDs` lives in the leaf
   and is the only implementation anywhere. Rejected: consumer-side
   mirror types mapped at the edge (operator: no duplicated shapes — the
   originally drafted answer, reversed at review); promoting the types
   to `api/` (not config surface); and an *uncurated* types-only
   package, which remains the import-cycle-workaround smell §2 bans —
   `internal/plan` differs precisely in being a gate-listed leaf with
   by-name curated content and real derivation logic, not a bucket
   anyone can grow.
3. **Argo: a legitimate future tier-2 driver; the replace-vs-layer
   dichotomy is dissolved, not answered.** What M9 actually forces is
   that **tier 1 is permanently Flux**: helm packs render Flux CRs
   (`HelmRelease` + source CR) that tier-1 controllers reconcile
   whatever coordinates packs — so "replace the substrate" stays
   foreclosed, and under an invariant substrate helm packs are **never
   inert under any tier-2 engine** (the first draft's inertness argument
   was manufactured by putting the substrate behind provider selection,
   and is withdrawn with that model). The engine choice — Flux or, in
   the future, Argo — is the user's at day 0 via
   `spec.engine.provider`, **immutable for the cube's lifetime: no
   handover, migration, or engine-switching semantics exist, ever.** An
   Argo driver arrives via **its own design gate**, whose recorded
   first-class design inputs are the two-tier analyses' findings:
   false-green Application health over CR kinds with no registered
   health check (the driver must ship checks for the Flux CR kinds);
   prune/finalizer coupling to tier-1 liveness and the
   finalizer-window orphan race; the per-driver sync-topology mapping
   at the M12 bus; the two-bundle delivery/provenance story; the day-0
   bundle path (requires the bus); source topology beyond the one
   configured source; tier-1 self-management under a non-Flux engine;
   and an Argo-vocabulary staleness rule (no Flux-style
   `observedGeneration` contract on Application CRs). "Argo is never a
   compile-time dependency" stands unchanged. Every categorical
   exclusion ("not an Argo invitation", "not a second driver on any
   current horizon") from the earlier draft of this entry is removed.
4. **Substrate-as-pack, operationally: the embedded asset becomes an
   embedded pack; day-0 stays cube-idp-applied.** The substrate's asset
   is a pack directory (`name: "flux"`, `version` = the pinned Flux
   version in clean SemVer — `2.9.2`, never the upstream `v`-prefixed
   tag spelling; `spec.engine.version` accepts the clean spelling and
   the substrate alone maps to `v2.9.2` where the vendored asset
   requires it — `type: raw`, `category: "engine"`, no `#Values`, no
   `namespace`) whose payload is the vendored manifests. The substrate
   parses its own payload (a raw, values-free pack renders as a sorted
   manifest parse) — it does **not** import `internal/pack`; conformance
   is enforced by a green-gate test at the composition edge asserting
   `pack.Load`+`Render` over the embedded directory yields exactly the
   substrate's parsed objects — deep equality of the ordered object
   lists after both parse paths, not membership. The pack carries no
   instance state — sync wiring is config-derived and driver-emitted,
   never pack content (the M9 pack/instance boundary). **Day-0 direct
   apply is sanctioned for the substrate because nothing exists yet to
   deliver it; a tier-2 engine's bundle is not circular to deliver
   through the running substrate — that is the vision's normal path.**
   Whether the running system later reconciles the substrate's own
   upgrades through the source is left open for the bus and beyond.
5. **Delivery-target ownership: the M12 bus's contract; M10 records one
   reservation.** Every fact needed to locate the delivery target (URL,
   ref, path) is and must remain fully derivable from
   `spec.engine.source` in `api/`; the seam may never make engine-domain
   private state necessary to locate it. The bus therefore lands without
   reopening the seam. Source-topology questions beyond the one
   configured source (substrate source vs engine revision vs
   pack-delivery target) are the second-driver gate's. The M7 air-gap
   local-manifest override lands with the substrate, which owns the
   asset.
6. **Seam method set: four pure methods plus one optional capability,
   covering tier 2 only.** `SourceObjects` (the engine's sync wiring
   from `spec.engine.source`; nil source → none; flux: the
   GitRepository|OCIRepository + Kustomization pair — substrate
   vocabulary, because the substrate doubles as the engine),
   `EngineObjects` (the engine's own install bundle, pack-shaped,
   delivered **through tier 1**, never by bootstrap's SSA; flux:
   empty — no second install occurs), `Reconciled` (per-object readiness
   judgment over handed-in `unstructured` status covering the driver's
   declared objects; its reason string feeds `CUBE-BST-009`
   diagnostics; the driver never fetches), and `EngineNamespace` (where
   tier-2 engine content lives; flux: the substrate namespace).
   Optional `SpecValidator` capability mirrors cluster's (M4 pattern).
   **Every method is pure** — §4's "interfaces stay pure where possible"
   holds completely, because the only I/O an engine needs is bootstrap's
   machinery, which is not duplicated (no-second-applier, 2026-08-06) —
   and purity makes the seam **apply-path-agnostic**: it does not care
   whether its objects are SSA-applied at day 0 or delivered through
   the source, which is what lets a second driver land without
   reopening it. The previous draft's `InstallObjects` (substrate
   content) leaves the seam — the substrate is not selectable — and its
   `InstallNamespace` **splits**: inventory placement is the invariant
   substrate-namespace fact; engine placement is the driver's
   `EngineNamespace`. Exact signatures fix at implementation within the
   contract (`docs/domains/engine.md`).
7. **`spec.engine`: the existing `provider` field is re-scoped to select
   the tier-2 engine only; the opaque-`forProvider` migration is still
   not taken — for a new reason.** The substrate is never selectable.
   `"flux"` (the default) remains the only accepted value in M10; adding
   `"argo"` is the second driver's gate event, additive. **The choice is
   immutable per cube** — recorded as contract now; mechanical
   enforcement lands with the second driver, since today there is
   nothing to switch to. `version` asserts the *engine* version (flux:
   the substrate version — degenerate). The old no-migration rationale
   ("no second provider on the horizon") is **void** under the recorded
   vision of user engine choice; the standing reason is concrete: the
   typed `EngineSource` is shared sync-wiring vocabulary every driver
   consumes (the source through which tier 1 delivers is not
   driver-private), and no driver-specific knob exists yet — an empty
   `forProvider` is ceremony. The second-driver gate, which knows Argo's
   actual knobs and source-topology needs, migrates the shape if and as
   needed; `EngineSource` (M7-finalized) must cross any migration
   losslessly or be superseded on the record.
8. **Readiness: three phases, one budget, two codes.** Bootstrap
   executes (1) the kind-set wait over what its SSA applied; (2) the
   reconciliation wait over the driver's `SourceObjects`; (3) the
   engine-readiness wait over the driver's declared `EngineObjects` —
   content bootstrap did **not** apply, polled by declared identity with
   the same predicate machinery; **empty and skipped for flux**, and the
   phase contract exists now precisely so a second driver's gate fills
   it without a new seam method. All phases share the existing
   `--timeout` as one **total** budget, never per phase. **In phases
   2–3, transient discovery is pending, never terminal**: declared
   content may not exist yet by design (a tier-1-delivered engine CRD
   still establishing when phase 3 first polls), so *no REST mapping
   yet* and *NotFound* are pending states retried until the shared
   deadline, while permanent errors (forbidden, malformed declared
   identity, any non-transient retrieval failure) fail immediately as a
   new machinery code, **`CUBE-BST-010`** — readiness polling failed on
   a permanent error, wrapped cause — coded at the point of failure so
   it is already-coded before the pass-through boundary and neither
   timeout code ever retags it; a stated departure from the apply path's one-shot
   mapper reset-and-retry, which stays one-shot where it serves
   applying; the wait path re-consults discovery per poll rather than
   converting a mapping miss into a terminal `CUBE-BST-003`. Phases 2–3
   time out as the new machinery code **`CUBE-BST-009`** — `-005` keeps
   meaning exactly "kind-set wait timed out"; the two codes cut along
   the polling contract, kind-set rollout polling vs driver-declared
   reconciliation polling — carrying the pending objects with the
   driver's pending reasons, which is what `Reconciled`'s reason string
   exists for. The wait-code pass-through rule (landed with PR #159)
   extends to both unchanged: an already-coded **permanent** cause
   (including an `ENG`-coded predicate error) keeps its code; transient
   discovery conditions are pending states, not causes; the wait code
   wraps only a deadline with objects still pending. The flux driver's predicates:
   for `GitRepository`/`OCIRepository`/`Kustomization`, `Ready`
   condition true **and** `status.observedGeneration` equal to
   `metadata.generation` — and the driver-neutral seam principle behind
   it, a documented conformance semantic: **no stale success may count
   as reconciled; each driver rejects staleness in its own CRs'
   freshness vocabulary.** Whether `status` gains an engine line is an
   implementation-breakdown decision, not gate-fixed.
9. **Conformance shape: hermetic against the real driver, because the
   seam is pure.** `RunEngineConformance(t, factory)` in
   `internal/engine`; no stateful fake is written (a fake of a pure seam
   tests the fake). Fixtures are driver-supplied (the cluster suite's
   lesson: no hardcoded "universally invalid" payloads); coded-error
   identity is asserted via `errors.As` + code equality; documented
   semantics — including the no-stale-success principle and
   `EngineObjects`/`EngineNamespace` consistency (empty for the
   degenerate driver, or a bundle in which the namespace names a
   `Namespace` object) — are enforced rather than non-nil-checked. The
   substrate carries its own green-gate checks (sha256, parse, the
   namespace-fact-to-content tie, the edge dogfood test). The real
   round-trip extends `make test-e2e`; never the gate.
10. **#142 hook points, reserved not implemented:** a future `secretRef`
    is an additive `EngineSource` field (`api/`) flowing through
    `SourceObjects` unchanged; #142 designs it together with the pack
    domain's helm source-CR authentication — two emitters, one gate.
11. **M10 adds no runtime dependency** — client-go interface types,
    apimachinery, function values, embedded data; the asset move (to the
    substrate home) is an import-path change, not a module-graph change.
    Recorded in §8 so the closed set stays closed by decision.

**The gateway milestone is inserted into the queue** (operator decision
at this gate): the trust-fabric prerequisite — ingress gateway,
certificates, hostnames, trust, internal DNS — delivered through tier 1
ahead of ordinary packs, per the founding vision. It becomes **M11**;
bus → **M12**, `up`/`down` → **M13** (renumbering table extended in
`/ROADMAP.md`, the same treatment as 2026-08-23; living documents use the
new numbers, this append-only log and shipped release notes are not
rewritten). Only the queue position is decided here — the gateway gets
its own epic and design gate.

Folded into the same diff, from the post-M9 architecture review's drift
list: the §5 `CLI` row now states its status honestly (active, no codes
yet); the §8 `cuelang.org/go` confinement cell names the scaffold
formatter (`new_chart.go`) the M9 `--from-chart` work added. Living
contracts: `docs/domains/engine.md` (new — both tiers),
`docs/domains/bootstrap.md` (delimited M10 section),
`docs/domains/pack.md` (M10 bullet).

**2026-08-27 — M11 design gate: gateway + ca domains, the ordered
prerequisite-pack list, the trust fabric.** Epic #177; scoping and the
operator decision round are #178 (its closing comment is the decision
record this entry anchors to, held 2026-08-25/27 over `M11-SCOPING.md`
and its codex rounds). Founding rationale (operator, 2026-08-24,
verbatim anchors): the gateway belongs at bootstrap because of "certs /
hostnames / trust / internal dns redirect and name resolutions" — the
identity fabric steady state, including the engine's own endpoints,
presumes. **Gated ahead of code**: this diff is the approved design; no
M11 code before the operator approves it and the implementation
breakdown is aligned. The decisions:

1. **Prerequisites are an ordered list of prerequisite packs**
   (operator-reshaped from the scoped single hybrid) **that bootstrap
   delivers through the tier-1 substrate before the engine.** The
   Gateway API CRDs are their **own** prerequisite pack, never folded
   into the gateway pack — a future engine may ship its own Gateway
   API CRDs, so separation avoids conflict inside one pack and lets
   the prerequisite list vary per setup; the list may hold more than
   one prerequisite pack. The Traefik gateway pack is **thin-helm**
   via the substrate's helm-controller (deliberate M9 dogfooding;
   chart digest-pinned — OCI publication verified). The
   **embedded-raw** variant is the documented air-gap fallback,
   **deferred to the M12 bus gate** with the network-dependent-day-0
   consequence recorded as its input. Each list member is applied and
   waited ready before the next applies.
2. **Traefik + Gateway API (standard channel) is the routing
   contract — footprint verified at the gate, not asserted**
   (2026-08-27): Traefik chart 41.3.0 (Traefik v3.7.11, OCI at
   `ghcr.io/traefik/helm/traefik`) renders **8 objects with exactly
   one Deployment** (controller = data plane) under the Gateway API
   provider; Envoy Gateway runs a controller **plus** a separately
   provisioned per-Gateway Envoy Deployment and its own config CRDs;
   ingress-nginx is disqualified (retirement announced, best-effort
   maintenance ended March 2026). Gateway API v1.6.1 standard channel
   measured: 10 CRDs, 1,170,953 bytes — the scoping's 100–300 KB
   estimate is corrected on the record (the Flux asset is 232 KB; this
   becomes the largest embedded asset). Reproducible evidence: chart
   OCI digest at verification time
   `sha256:dcae2d586d7fbda6a08150eaeeca4132e9dd042d8a4d16ada287e8c40f6ff17a`
   (`ghcr.io/traefik/helm/traefik:41.3.0`), rendered via `helm
   template t oci://ghcr.io/traefik/helm/traefik --version 41.3.0
   --set providers.kubernetesGateway.enabled=true --set
   gateway.enabled=true`; CRD bundle from the upstream v1.6.1
   `standard-install.yaml` release asset. Gate-time evidence, not the
   final pins — the implementation re-pins under the embedded-pack
   sha256 discipline at the breakdown. Ingress stays tolerated for content that
   needs it. The implementation is pack content, not a seam.
3. **Two new component domains** (operator-expanded from the scoped
   one): **`internal/gateway`** (`CUBE-GWY-*`,
   `docs/domains/gateway.md`, `spec.gateway`) and **`internal/ca`**
   (`CUBE-CA-*`, `docs/domains/ca.md`, `spec.ca`). The ca contract is
   written **provider-seam-READY**: the cube-owned stdlib CA is v0's
   only implementation; **user-provided CA, cert-manager, and the
   native Kubernetes pod-certificate signer (`PodCertificateRequest`,
   KEP-4317 — verified GA in v1.37)** are named future backends whose
   seam **activates at a later gate** under the second-implementation
   doctrine — the config surface is designed now
   (`spec.ca.provider`, `cube` default and only value), the Go
   interface is not. The §5 `TLS` row is re-scoped to #142's
   credential bindings (certificates/CA move to `CA`).
4. **CA key custody: the in-cluster Secret only** — never the repo,
   never a vault (#142 adjacency recorded); `down` destroying the
   cluster destroys the CA, correct for a local cube. **Mint-if-absent
   requires the named new edge behavior of reading the existing
   Secret** (the edge's first cluster read outside a domain
   operation, with the dynamic client it already constructs — the
   exported bootstrap machinery has no read operation and grows
   none); without it every re-bootstrap would rotate the CA. Long
   v0 validity (CA ~10y, leaf ~2y), no rotation machinery. Every
   minted CA carries a marker CN/OU.
5. **Hostnames, trust distribution, ports, exposure:**
   - **(a)** `spec.gateway.domain` is the configuration point; the
     fallback default is `<metadata.name>.` + the **single
     compile-time const `cube.test`** — recompilable by design (an
     operator building their own binary rebases every default at one
     constant; that intent is part of the decision).
   - **(b)** Trust: a CLI **`trust install|list|remove` verb group**
     backed by a **minimal ledger** — one file
     (`~/.cube-idp/trust.yaml`; cube name + fingerprint + store +
     date) — plus the marker CN/OU on every minted CA. Operator
     directive: do **not** over-complicate; no orphan-scan machinery.
     The gate sizes the verbs at user-scope stores only (macOS login
     keychain via the OS `security` tool, Linux p11-kit user anchors),
     sudo never invoked; the per-cube CA certificate syncs
     idempotently to `~/.cube-idp/<cube>/ca.crt` on every bootstrap.
     **Sanctioned descope if the effort balloons: emit-only CA +
     ledger now, verbs deferred to the #142 gate.** **No `/etc/hosts`
     entries in M11** — no gateway-owned application endpoints exist
     in v0; route-host emission is M12's.
   - **(c, operator override of the scoped recommendation)** The kind
     driver defaults to **high host ports: `extraPortMappings`
     8080→80 and 8443→443** (unprivileged; URLs carry ports) plus the
     ingress-ready node label, whenever the user supplies no explicit
     `forProvider`; **explicit `forProvider` wins**, never merged.
     Recorded boundaries: the **driver cannot see `spec.gateway`**
     (it receives `{Name, ForProvider}` only — the default is
     unconditional, coherent with D8), and the
     **create-before-bootstrap coupling** (mappings exist only at
     create; a collision fails `create` loudly; a gateway on a
     cluster created without them needs a recreate).
   - **(d)** Exposure inside the cluster is kind's documented
     **ingress-ready pattern**: labeled node, hostPorts 80/443,
     `nodeSelector` — no LoadBalancer, no NodePort translation.
6. **The CoreDNS marker-block read-modify-write is approved** — the
   first sanctioned mutation of an object cube-idp does not own, with
   the safety envelope recorded as binding: marker-delimited block,
   idempotent splice preserving all unmarked content, **optimistic
   concurrency** (update with the read `resourceVersion`, retry on
   conflict — never a read-derived blind SSA write), the ConfigMap
   **never inventory-recorded** (the inventory is a deletion seed; a
   system object must never be seeded for deletion), and
   **restore-not-delete published as an M13 `down` requirement**.
7. *(numbering kept aligned with the scoping document's D-numbers;
   trust distribution was folded into 5b by the operator round.)*
8. **Absent `spec.gateway` means installed with defaults** — the
   gateway is fundamental fabric (the engine precedent: not opt-in);
   `spec.ca` likewise defaults to the `cube` provider. The loudest
   default consequence is version-(c)-softened: default cubes bind
   high ports 8080/8443, not 80/443.
9. **Machinery: bootstrap's install sequencing generalizes to the
   ordered prerequisite list** — per unit, record-before-apply
   extends (re-record inventory, apply, wait ready before the next
   member), with CRDs waited `Established` before dependents; raw
   units use the kind-set wait, CR units the reconciliation wait
   under injected predicates; one `--timeout` budget total.
   **`CUBE-BST-005/009/010` keep numbers and mechanics; their
   contract text generalizes** from phase-/engine-flavored wording to
   any bootstrap-executed kind-set / reconciliation wait over
   declared objects. **Zero new Go runtime dependencies** — CA
   minting is stdlib crypto; the gateway is content (§8 records the
   adjacent platform-tool note: the trust verbs shell out to the OS's
   own trust tooling, which has no in-process alternative — distinct
   from the rejected `kubectl kustomize` exec).
10. **Inheritance, recorded now.** M12 inherits: the prerequisite
    list as `Prerequisites` prior art, the air-gap answer (embedded-
    raw fallback), route-host discovery and host-resolution emission,
    and the steady-state ownership migration question (one answer
    with substrate self-management). M13 inherits: CoreDNS
    restore-not-delete and the trust-artifact/teardown semantics.
    **Frozen — must not be designed in M11**: the bus write path and
    `RenderPlan.Prerequisites` semantics; certificate
    rotation/ACME; custody backends beyond the in-cluster Secret;
    multi-gateway and non-kind port topologies; OS resolver/hosts
    mutation; a gateway driver seam; the ca provider interface.

Living contracts: `docs/domains/gateway.md` + `docs/domains/ca.md`
(new, gated ahead of code), `docs/domains/bootstrap.md` +
`docs/domains/cluster.md` (delimited M11 amendments),
`docs/domains/engine.md` + `docs/domains/pack.md` (touchpoint bullets;
`category: "gateway"` becomes the second used well-known spelling),
`docs/ARCHITECTURE.md` §2/§5/§8. The C4 model (`docs/architecture/`)
renders the implemented binary and is trued at the M11 closeout, the
M10 precedent — gated-ahead-of-code domains are deliberately not drawn
before their packages exist.
