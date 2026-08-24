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
  source Flux watches (M12), so M8 touches no cluster and has **no e2e**.
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
  deterministic order for M13. Two runtime dependencies at their own
  gates: `cuelang.org/go` and `sigs.k8s.io/kustomize/api`+`kyaml`. Design
  gate: `docs/DECISIONS.md` 2026-08-21 (plus 2026-08-22, bundled-CRD
  scope); living contract: `docs/domains/pack.md`.

- **M9 — helm packs** (epic #139; PR stack #147/#149/#150, 2026-08-23):
  `type: helm` became real by **delegating** it. A helm pack is **thin** —
  chart coordinates (`repo|oci`, exact SemVer, optional OCI digest) plus a
  closed `#Values` in `pack.cue`, and **no bundled chart content**, which is
  a payload mismatch (`CUBE-PKG-004`) rather than something ignored. `Render`
  emits a Flux `HelmRelease` plus its `HelmRepository`/`OCIRepository` source
  CR, both named for the effective instance id and carrying no namespace, and
  the engine's helm-controller pulls and templates the chart in cluster.
  cube-idp never runs Helm, so rendering stays hermetic and
  `helm.sh/helm/v4` was **dropped** from the deferred set rather than
  adopted. `#Pack` became a **type-discriminated disjunction**, so "chart
  required for helm, forbidden otherwise" is schema-enforced; `chart.version`
  is validated by `golang.org/x/mod/semver` (a promotion to a direct import,
  no new module, `go.sum` unchanged). The embedded Flux install gained
  **helm-controller** + the `helmreleases` CRD — an asset regeneration and a
  new sha256, with **no readiness-wait change**, because the bootstrap
  kind-set filters by kind. `pack new` gained `--type helm` and
  `--from-chart <dir>` (a local `Chart.yaml`+`values.yaml` metadata read that
  scaffolds a thin pack with a reserved-host placeholder url and a lossy,
  clearly-labelled `#Values`). `CUBE-PKG-020` retired with **no new error
  codes**. Trade-off recorded in the contract: `pack render` on a helm pack
  shows the `HelmRelease`, not the expanded chart — a "delegated pack".
  **Scoped to public chart sources and non-sensitive values**: no
  private-source auth, no secret-backed `valuesFrom`, and a `kind: repo`
  chart is an honestly-labelled *mutable reference* (only an OCI digest pins
  content) — all of that is #142. Design gate: `docs/DECISIONS.md`
  2026-08-23; living contract: `docs/domains/pack.md`, "Helm packs".

## Queue

- **M10 — engine**: the two-tier model (design gate 2026-08-24, PR #161):
  the invariant Flux **substrate** (tier 1, re-expressed as a conforming
  pack) and the tier-2 **engine driver seam** (`engine.Provider`;
  user-selected via `spec.engine.provider`, immutable per cube; flux is
  the only — degenerate — driver). Consumes the M8 pack contract
  unchanged. The former Argo replace-vs-layer question is dissolved by
  the model: the substrate is never replaceable (M9 pins it), and Argo is
  a legitimate future tier-2 driver behind its own design gate.

- **M11 — gateway** (inserted at the M10 design gate by operator
  decision, 2026-08-24, per the founding vision: "we use Flux to deliver
  the prerequisites and the engine itself … from there the engine takes
  over"): the trust-fabric prerequisite delivered through tier 1 ahead of
  ordinary packs — ingress gateway, certificates, hostnames, trust,
  internal DNS. Own epic and design gate when picked up; its delivery
  semantics feed the bus milestone's `Prerequisites` handling. No design
  here — only the queue position.

- **#142 — trust & credential bindings** (opened from the independent
  review of the M9 design gate, 2026-08-23): private chart-source
  authentication (`secretRef` on the source CR), a constrained
  instance-level `valuesFrom` for secret-backed values, source
  verification, and the `lock`/`mirror` operation that resolves a mutable
  repository reference to verified content. Its own design gate; **not**
  sequenced into the M9–M13 chain until it is picked up, because every
  milestone after M9 can proceed without it.

### Milestone renumbering (2026-08-23)

Helm was re-sequenced ahead of the engine milestone by operator direction
(2026-08-22, epic #139), so everything behind it shifts by one:

| Milestone | Was | Now |
|---|---|---|
| helm packs | — (unscheduled, "its own milestone") | **M9** |
| engine (seam + Flux-as-pack) | M9 | **M10** |
| bus (delivery, `pre` semantics, air-gap) | M10 | **M11** |
| `up`/`down` finisher | M11 | **M12** |

### Milestone renumbering (2026-08-24)

The gateway milestone was inserted after M10 by operator decision at the
M10 design gate (PR #161), so everything behind it shifts by one again:

| Milestone | Was | Now |
|---|---|---|
| gateway (trust fabric) | — (new) | **M11** |
| bus (delivery, `pre` semantics, air-gap) | M11 | **M12** |
| `up`/`down` finisher | M12 | **M13** |

Living documents (this file, `docs/ARCHITECTURE.md`, `docs/domains/`) use
the new numbers. **`docs/DECISIONS.md` and the shipped `CHANGELOG.md`
release notes are not rewritten** — both record what was true when written,
and `DECISIONS.md` is append-only. These tables are the answer for anyone
who follows a reference from one of them (chain them for pre-2026-08-23
references). `docs/archived/` is read-only and untouched.

Deferred from M8 with issues of their own: the CLI edge resolving
`<ref>`/`packRef` through `internal/ref` (**#136**, carrying the
`docs/domains/ref.md` move), and the git/oci/s3 `ref` backends, which land
with the milestones that need them.

## Continuation after M10 (directional, not committed)

Re-sequenced by operator direction (2026-08-05, recorded in the M7 design
gate; helm inserted ahead of the engine 2026-08-22, see the renumbering
table above). Decision record, alternatives, risk table, and prior open
questions: `docs/archived/plans/2026-08-01-roadmap-direction.md`. (The pack
unit's pre-M8 shaping notes were absorbed into `docs/domains/pack.md` and
`docs/DECISIONS.md`, and deleted with the M8 closeout.)

M10 engine is the committed next milestone (Queue above). After it: M11
gateway (trust fabric, inserted 2026-08-24) → M12 bus (OCI/git delivery;
the real `pre` semantics and the air-gap answer are due by then) → M13
thin `up`/`down` finisher (it consumes M8's `ResolvedGraph` as data and
executes the order) → periphery in pull order (doctor, diff, trust,
lock/vendor, spokes, …). #142 is queued but unsequenced: every milestone
after M9 can proceed without it.

Rationale: M7 makes the product demo-able (up → gitops-managed cluster) and
M8 gives the later milestones the content unit they deliver. M9 went ahead
of the engine because helm charts are the bulk of real platform content,
and delegating them to a `HelmRelease` turned out to be *lighter* than the
"integrate `helm.sh/helm/v4`" plan it replaced — it removed a deferred
dependency instead of adopting one.
Carried-forward hard rules: Argo CD never a compile-time dependency; any
post-M8 pack-contract change is a design-gate event, never a drive-by edit
inside a consumer milestone (M9 is exactly such a gate, taken before its
code). The Argo scope was settled at the M10 gate (two-tier: legitimate
future tier-2 driver, own design gate); still open for the M12 bus gate:
the air-gap commitment (direction doc §5).
