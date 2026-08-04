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

## Queue

- **M6 — kube: client access** (epic #81): new leaf domain `internal/kube`
  (`CUBE-KUB-*`) — clients constructed from injected kubeconfig bytes +
  context name (REST config, discovery, RESTMapper, dynamic, `Ping`);
  `k8s.io/client-go` joins the closed set with construction-scoped
  confinement; no driver seam. Thin user-visible proof: `status` gains an
  API-reachability line. Design gate: `docs/DECISIONS.md` 2026-08-04;
  living contract: `docs/domains/kube.md`.

## Default continuation after M6 (directional, not committed)

Re-evaluated after M5 (2026-08-04): M6 = kube confirmed per the default;
the remainder stays directional, re-checked as milestones land. Decision
record, alternatives, risk table, and open questions:
`docs/archived/plans/2026-08-01-roadmap-direction.md`.

apply (SSA + inventory) → pack (the spine, made solid — contract designed
against recorded consumer requirements; consumers conform to pack, never
the reverse) → engine (gitops driver seam + Flux, installs as an ordinary
pack) → registry (OCI bus) → orchestrator (`up`/`down` phase runner, last)
→ periphery in pull order (doctor, diff, trust, lock/vendor, spokes, …).

**Prepared work:** the pack unit is fully pre-shaped in
`docs/work/pack-groundwork.md` — owner-decided contract direction
(pack.cue/CUE, packRef, uuid/category, values, externalManifests,
dependsOn) and an 18-task breakdown grouped into 6 one-PR chunks with
dependencies. All five 2026-08-01 owner questions are resolved and folded
in; its design task (T1) is fully unblocked, and only the install chunk
waits on M6/M7. Still open for the M8/M9 design gates: Argo CD scope and
the air-gap commitment (direction doc §5 Q1/Q3).
