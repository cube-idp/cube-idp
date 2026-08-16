# cube-idp roadmap

Small milestones; each milestone runs as a GitHub epic issue with
operator-aligned task issues, delivered through small green PRs that
reference the epic (flow: CLAUDE.md §6). This file is the queue — update
it in the PR that completes or reorders a milestone.

## Done

- **M0 — v0 config baseline** (PR #49, 2026-07-27): teardown of the old
  implementation + `Config` API, strict loader, `cubeerr` machinery, CLI
  `config validate|show`, CI gates.
  Design: `docs/archived/design/2026-07-27-back-to-basics-structure.md`.
- **M1 — docs reset** (PR #50, 2026-07-29): CLAUDE.md rewritten for the
  v0 world, short v0 README, CHANGELOG wipe, this ROADMAP.
- **M2 — error-handling polish** (PR #51, 2026-07-29): `LoadFile` path
  context, a `CUBE-CFG-*` code for an unreadable config file, malformed-tag
  test row in `cubeerr`, golden-file test for `config show`.
- **M3 — cluster** (PR #52): `spec.cluster` sub-struct (`provider` +
  opaque `forProvider`) + `internal/cluster` driver seam with conformance
  suite + kind provider (kind as a Go library) + `init` command with
  cube-owned kubeconfig contexts.
  Design: `docs/archived/design/2026-07-29-cluster-domain.md`; living
  contract: `docs/domains/cluster.md`.
- **M4 — init bootstrap** (epic #53; PRs #60–#63, #68–#70, 2026-08-02):
  `init` composes scaffold-if-absent → load → provision at the CLI edge —
  a missing config file is scaffolded (`metadata.name` from `--name`,
  else a generated docker-style name constrained to the name regex),
  validated through the standard pipeline before an `O_EXCL` write, with
  a stdout notice naming the created file and cube. `--name` never
  mutates an existing document (`CUBE-CFG-005` on mismatch; a match
  proceeds — idempotent). The `forProvider`-validation follow-up folded
  in: `config validate` surfaces provider-side validation via the seam's
  optional `SpecValidator` capability (kind's runtime detection now lazy,
  so no Docker needed) — exit 0 valid / 2 document errors / 1 provider
  payload. Design gate: `docs/DECISIONS.md` 2026-08-01; living contracts:
  `docs/domains/config.md`, `docs/domains/cluster.md`.

- **M5 — cluster lifecycle completion** (epic #72; PRs #76, #77, #79,
  2026-08-04): `delete` exposes the seam's `Delete` and losslessly removes
  the cube-owned kubeconfig context (map-based removal mirroring the
  merge, atomic write, `current-context` unset only when it pointed at the
  removed context, file never unlinked, write skipped when nothing
  matched); `status` reports cluster existence + context installation,
  read-only, exit 0 whenever the report succeeds. Mid-milestone scope
  change (#78, operator testing feedback): `init` split — `init` is
  config-only (scaffold-if-absent, load+validate, report, exit-0
  idempotent), the new `create` command owns provision + context install
  and never scaffolds. Operator decisions: `docs/DECISIONS.md` 2026-08-02
  and 2026-08-03; living contract: `docs/domains/cluster.md`.

- **M6 — kube: client access** (epic #81; PRs #87–#90, 2026-08-05): new
  leaf domain `internal/kube` (`CUBE-KUB-*`) — clients constructed from
  injected kubeconfig bytes + context name (REST config, discovery,
  memory-cached RESTMapper, dynamic client, bounded `Ping`);
  `k8s.io/client-go v0.36.2` joins the closed set with
  construction-scoped confinement; no driver seam (one Kubernetes API).
  `status` gains a third line — api server reachable/unreachable/not
  checked — composed at the CLI edge; opt-in e2e round-trip in the new
  `tests/e2e` package. Design gate: `docs/DECISIONS.md` 2026-08-04;
  living contract: `docs/domains/kube.md`.

- **M7 — bootstrap** (epic #92; PR stack #108, 2026-08-16): new domain
  `internal/bootstrap` (`CUBE-BST-*`) — the **micro-bootstrap applier**.
  It hand-rolls SSA on `k8s.io/client-go` to install the embedded, pinned
  **Flux** manifests (`go:embed` of `flux install --export` v2.9.2,
  provenance-pinned by sha256), waits the bootstrap kind-set (CRD
  Established, Deployment/StatefulSet ready, Job complete, Namespace
  Active), records a ConfigMap inventory (seed of `down`), then applies the
  source + sync CRs from `spec.engine.source` and hands over — the engine
  owns steady state. New `spec.engine` sub-struct: `provider` (defaulted
  flux) + a finalized **git|oci** discriminated `EngineSource`
  (`kind`/`url`/`ref`/`path`/`interval`), validated in `api/`. New verb
  `cube-idp bootstrap`; the reserved `apply` domain/verb retired. **No new
  runtime dependency** (embedded data + client-go/apimachinery already in
  the set). Real Flux round-trip behind `make test-e2e`. Design gate:
  `docs/DECISIONS.md` 2026-08-06; living contract:
  `docs/domains/bootstrap.md`.

## Queue

- *(empty — the next milestone is **M8 pack**, picked from the continuation
  below; the git-vs-OCI demo source landed as the explicit `spec.engine.source`
  `kind` in M7.)*

## Continuation after M7 (directional, not committed)

Re-sequenced by operator direction (2026-08-05, recorded in the M7 design
gate). Decision record, alternatives, risk table, and prior open questions:
`docs/archived/plans/2026-08-01-roadmap-direction.md`; the pack unit's
shaping and 18-task breakdown: `docs/work/pack-groundwork.md`.

M8 pack (done properly against the **live Flux loop** —
**delivery-through-engine**: packs are delivered by writing to the Flux
source, not through a cube-idp applier; groundwork's 18 tasks, contract
designed against recorded consumer requirements) → M9 engine (gitops driver
seam formalized + **Flux re-expressed as a conforming pack** + the Argo
replace-vs-layer question) → M10 bus (OCI/git delivery; Flux does both; the
air-gap answer is due by then) → M11 thin `up`/`down` finisher (ordering
lives in the engine) → periphery in pull order (doctor, diff, trust,
lock/vendor, spokes, …).

Rationale: M7 makes the product demo-able (up → gitops-managed cluster) and
M8 iterates against real substrate. Carried-forward hard rules: Argo CD
never a compile-time dependency; any post-M8 pack-contract change is a
design-gate event, never a drive-by edit inside a consumer milestone. Still
open for the M9/M10 design gates: the Argo scope and the air-gap commitment
(direction doc §5).
