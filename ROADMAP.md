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

- **M8 — pack** (epic #113; PR stack #132/#134/#137, 2026-08-22): the
  **pack** domain (`internal/pack`, `CUBE-PKG-*`) — the self-contained,
  versioned unit of platform content every later milestone consumes. It
  **defines, loads, validates, and renders** packs and stops there: under
  delivery-through-engine, packs reach a cluster by being written into the
  source Flux watches (M10), so M8 touches no cluster and has **no e2e**.
  A pack is a directory with a `pack.cue` carrying `name`/`version`/an
  explicit `type` (`raw|helm|kustomize`, never sniffed) and the
  differentiator, a **closed `#Values` definition** that locks down,
  exposes, and defaults its values surface. `Render` returns a
  `RenderPlan{Prerequisites, Objects}`; namespace injection is a
  post-render transform whose scope reads the **`spec.scope` of CRDs the
  pack itself bundles** before falling back to a static kind set;
  kustomize builds hermetically, rejecting remote references
  (`CUBE-PKG-021`) rather than fetching. New verbs: `pack render <ref>`
  (pure, stdout-clean), `pack render -f <config> --id <id>` (the
  configured instance), `pack validate <ref>` (renders and discards), and
  `pack new` (a real scaffold that renders as written, with `--from`
  forking). New shared-infrastructure leaf `internal/ref` (`CUBE-REF-*`) —
  one reference grammar resolving to a tree or a file (local + https now;
  git/oci/s3 recognized with their own not-implemented codes). New
  `spec.packs` sub-struct with instance identity (`id`, defaulted from the
  pack name when unambiguous) and a `dependsOn` graph resolved to a
  deterministic order for M11. Two runtime dependencies at their own
  gates: `cuelang.org/go` and `sigs.k8s.io/kustomize/api`+`kyaml`. Design
  gate: `docs/DECISIONS.md` 2026-08-21 (plus 2026-08-22, bundled-CRD
  scope); living contract: `docs/domains/pack.md`.

## Queue

- **M9 — engine**: formalize the gitops driver seam and re-express Flux as
  a conforming pack, consuming the M8 pack contract unchanged; the
  Argo replace-vs-layer question is due at its design gate.

Deferred from M8 with issues of their own: the CLI edge resolving
`<ref>`/`packRef` through `internal/ref` (**#136**, carrying the
`docs/domains/ref.md` move), and the git/oci/s3 `ref` backends, which land
with the milestones that need them.

## Continuation after M9 (directional, not committed)

Re-sequenced by operator direction (2026-08-05, recorded in the M7 design
gate). Decision record, alternatives, risk table, and prior open questions:
`docs/archived/plans/2026-08-01-roadmap-direction.md`. (The pack unit's
pre-M8 shaping notes were absorbed into `docs/domains/pack.md` and
`docs/DECISIONS.md`, and deleted with the M8 closeout.)

M9 is the committed next milestone (Queue above). After it: M10 bus
(OCI/git delivery; Flux does both; the real `pre` semantics and the
air-gap answer are due by then) → M11 thin `up`/`down` finisher (it
consumes M8's `ResolvedGraph` as data and executes the order) → periphery
in pull order (doctor, diff, trust, lock/vendor, spokes, …).

Rationale: M7 makes the product demo-able (up → gitops-managed cluster) and
M8 gives the later milestones the content unit they deliver.
Carried-forward hard rules: Argo CD never a compile-time dependency; any
post-M8 pack-contract change is a design-gate event, never a drive-by edit
inside a consumer milestone. Still open for the M9/M10 design gates: the
Argo scope and the air-gap commitment (direction doc §5).
