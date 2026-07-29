# cube-idp roadmap

Small milestones; every milestone lands as one green PR to `main`
(flow: CLAUDE.md §6). This file is the queue — update it in the PR that
completes or reorders a milestone.

## Done

- **M0 — v0 config baseline** (PR #49, 2026-07-27): teardown of the old
  implementation + `Config` API, strict loader, `cubeerr` machinery, CLI
  `config validate|show`, CI gates.
  Design: `docs/design/2026-07-27-back-to-basics-structure.md`.
- **M1 — docs reset** (PR #50, 2026-07-29): CLAUDE.md rewritten for the
  v0 world, short v0 README, CHANGELOG wipe, this ROADMAP.
- **M2 — error-handling polish** (PR #51, 2026-07-29): `LoadFile` path
  context, a `CUBE-CFG-*` code for an unreadable config file, malformed-tag
  test row in `cubeerr`, golden-file test for `config show`.

## Queue

- **M3 — cluster** (in progress, this PR): `spec.cluster` sub-struct
  (`provider` + opaque `forProvider`) + `internal/cluster` driver seam with
  conformance suite + kind provider (kind as a Go library) + `init` command
  with cube-owned kubeconfig contexts.
  Design: `docs/design/2026-07-29-cluster-domain.md`.
- **Follow-up — `config validate` covers `forProvider`**: provider-side
  validation surfaced at the CLI edge without breaking import direction
  (`internal/config` never imports `internal/cluster`). Recorded in the
  cluster design §9.
- **M4 — init bootstrap**: when the config file does not exist, `init`
  scaffolds it (`metadata.name` from `--name`, else a generated
  docker-style name constrained to the name regex) and provisions from it.
  `--name` never mutates an existing document — coded error with "edit
  metadata.name" remediation. Contract change → short design doc first
  (owns: scaffold semantics, name generator, which domain writes config).

## After M3 (tentative — order revisited after cluster delivery)

kube (client access) · apply (SSA/inventory) · pack (fetch/render) ·
engine (gitops, driver seam) · registry (OCI) · orchestrator (`up`, phase
runner) · periphery (trust, spokes, doctor, diff, …)

Reference build sequence from the design doc §8: config → kube → apply →
cluster → pack → engine → registry → orchestrator → periphery.
