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
