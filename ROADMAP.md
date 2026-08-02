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

## Queue

- **M5 — cluster lifecycle completion**: no new domain; close out
  `internal/cluster` per cluster design §9 — `delete` (or `down`) exposing
  the seam's `Delete`, `status`, kubeconfig cleanup. Command naming and
  exact §9 scope decided at plan time (direction doc Q7). Likely checkbox
  plan only; design doc only if scope grows.

## Default continuation after M5 (directional, not committed)

Re-evaluated once M5 lands. Decision record, alternatives, risk table, and
open questions: `docs/archived/plans/2026-08-01-roadmap-direction.md`.

kube (client access, client-go → design gate) → apply (SSA + inventory) →
pack (the spine, made solid — contract designed against recorded consumer
requirements; consumers conform to pack, never the reverse) → engine
(gitops driver seam + Flux, installs as an ordinary pack) → registry (OCI
bus) → orchestrator (`up`/`down` phase runner, last) → periphery in pull
order (doctor, diff, trust, lock/vendor, spokes, …).

**Prepared work:** the pack unit is fully pre-shaped in
`docs/work/pack-groundwork.md` — owner-decided contract direction
(pack.cue/CUE, packRef, uuid/category, values, externalManifests,
dependsOn), an 18-task breakdown grouped into 6 one-PR chunks with
dependencies, and 5 open owner questions that gate its design task (T1).
