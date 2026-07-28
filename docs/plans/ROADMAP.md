# cube-idp roadmap

Small milestones; every milestone lands as one green PR to `main`
(flow: CLAUDE.md §6). This file is the queue — update it in the PR that
completes or reorders a milestone.

## Done

- **M0 — v0 config baseline** (PR #49, 2026-07-27): teardown of the old
  implementation + `Config` API, strict loader, `cubeerr` machinery, CLI
  `config validate|show`, CI gates.
  Design: `docs/design/2026-07-27-back-to-basics-structure.md`.

## Queue

- **M1 — docs reset** (in progress, this PR): CLAUDE.md rewritten for the
  v0 world, short v0 README, CHANGELOG wipe, this ROADMAP.
- **M2 — error-handling polish** (chore, direct PR): `LoadFile` path
  context, a `CUBE-CFG-*` code for an unreadable config file, malformed-tag
  test row in `cubeerr`, golden-file test for `config show`.
- **M3 — cluster**: `spec.cluster` sub-struct + `internal/cluster` driver
  seam with conformance suite + kind provider + `init` command and all
  related parts. Needs a design doc first (new domain, new seam, new
  dependency).

## After M3 (tentative — order revisited after cluster delivery)

kube (client access) · apply (SSA/inventory) · pack (fetch/render) ·
engine (gitops, driver seam) · registry (OCI) · orchestrator (`up`, phase
runner) · periphery (trust, spokes, doctor, diff, …)

Reference build sequence from the design doc §8: config → kube → apply →
cluster → pack → engine → registry → orchestrator → periphery.
