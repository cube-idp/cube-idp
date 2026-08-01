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

**2026-08-01 — Documentation system reset.** ROADMAP moved to repo root;
all dated/pre-reset docs frozen under `docs/archived/`. Living set:
`docs/ARCHITECTURE.md` (cross-cutting, updated in place),
`docs/domains/<name>.md` (exactly one per domain), `docs/DECISIONS.md`
(this log), `docs/work/` (ephemeral — milestone plans/groundwork, deleted
in the milestone's closing PR; git history is the archive). Dated files
are banned outside `archived/`; a milestone's design gate is now an
ARCHITECTURE/domain diff + a DECISIONS entry, still owner-approved before
code.
