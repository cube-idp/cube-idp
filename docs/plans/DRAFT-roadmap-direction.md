# DRAFT — Roadmap direction (for owner review)

Status: **proposal, not binding**. Nothing here reorders `ROADMAP.md`; that
happens only in the PR that completes or reorders a milestone. Inputs: the
old codebase at `9a1edd9` (~20.4k LOC, 33 internal packages, 45 ADRs), the
back-to-basics design (`docs/design/2026-07-27-back-to-basics-structure.md`),
the cluster-domain design (`docs/design/2026-07-29-cluster-domain.md`), and
the current queue in `docs/plans/ROADMAP.md` (M0–M3 done, M4 queued).

## 1. Capability gap: what the old product could do that v0+M3 cannot

The old CLI stood up a full local IDP: `up` ensured a cluster (kind/k3d/
existing), installed an in-cluster zot OCI registry, installed a GitOps
engine (Flux or Argo CD) as an ordinary pack, rendered data-only packs
client-side (raw/kustomize/helm), pushed them as OCI artifacts, delivered
them via the engine, and tracked everything in an SSA inventory that powered
`down`, `diff`, and `upgrade --plan`. Around that core: local CA/TLS trust,
hub/spoke registration, `doctor`/`explain` diagnostics, `cube.lock` +
`vendor` air-gap bundles, live `sync`, CNOE/idpbuilder import, and a
krew-style exec-plugin tier.

v0+M3 has: a validated `Config` (`cube-idp.dev/v1alpha1`, `spec.cluster`
sub-struct), `config validate|show`, and `init` (kind driver behind a
conformance-tested seam, kubeconfig rebrand/merge). Everything after "the
cluster exists" is a gap.

Verdict on the old capabilities:

| Capability | Verdict |
| --- | --- |
| render→push→deliver→apply pipeline (pack/oci/registry/engine/apply) | **Earned its keep** — this is the product. Rebuild, in slices. |
| Cluster provisioning + kubeconfig handling | Earned its keep — already rebuilt (M3). |
| SSA apply + inventory (powers `down`/`diff`/prune) | Earned its keep — small, high leverage. |
| Typed error codes + remediation | Earned its keep — but the *central* catalog (fan-in 30, 107 codes) caused rot; per-domain `errors.go` replaces it. |
| Engine seam (Flux/Argo behind one interface, pure translators) | The seam design earned its keep (carried into §6 of the structure doc); the `engine/factory` package and alias re-exports it forced were rot. |
| `up` orchestration | The *capability* is essential; the 1,504-line god file with a 615-line `Run` importing ~20 packages was the primary rot site. Rebuild as the phase runner. |
| lock/vendor/bundle (air-gap), diff/upgrade previews | Real differentiators, but heavy; defer to periphery, re-justify then. |
| trust/CA, doctor, spokes, cnoe import | Genuine features; none are prerequisites for the core loop. Periphery. |
| Exec-plugin tier (krew model, trust, indexes) | Premature for the product's stage; caused surface area, not adoption. Do not rebuild until pulled by users. |
| 27-file UI subsystem (4 render backends) | Disproportionate; plain output + `-o json` later is enough for now. |
| Cycle-breaker packages (`cfgload`, `refval`, factories, aliases) | Pure rot — structural tax from factories importing implementations. The greenfield rules exist to prevent recurrence. |

## 2. Proposed milestone units (M4–M10)

Each milestone: one green PR, one new `internal/<domain>` package at most,
one `ConfigSpec` sub-struct per component, design doc first where a new
domain / seam / dependency arrives. The numbering below follows Path A
(design §8 order); §3 proposes alternative orderings of these same units —
the unit *scopes* hold regardless of which path is chosen.

**M4 — init bootstrap** *(already queued; keep as-is).* When the config file
is absent, `init` scaffolds it (`--name` or generated docker-style name) and
provisions. Needs its short design doc (scaffold semantics, name generator,
which domain writes the file) per the ROADMAP note. Rationale: completes the
zero-to-cluster story before any new domain lands; small, self-contained.
Fold the queued **`forProvider` validation follow-up** into this PR if it
stays small (both touch the `init`/validate edge), otherwise land it as its
own chore-sized PR first — either way it must not make `internal/config`
import `internal/cluster` (cluster design §9).

**M5 — kube: client access.** New leaf domain `internal/kube` (`CUBE-KUB-*`):
construct a client from injected kubeconfig bytes (per the cluster design §9
injection contract — consumers never import `internal/cluster`), plus the
minimal helpers later domains need (REST config, discovery/mapper). Brings
`k8s.io/client-go` into the closed dependency set → **design doc required**.
Rationale: everything downstream (apply, engine, doctor) needs a client;
keeping it a leaf with a tiny surface avoids the old `kube`+`apply`+`up`
entanglement. No user-visible command yet; that is acceptable for one
milestone because M6 cashes it in.

**M6 — apply: SSA + inventory.** New domain `internal/apply` (`CUBE-APP-*`):
server-side apply with field manager `cube-idp`, kstatus-style readiness
wait, and a ConfigMap inventory recording what we own. Decide in its design
doc whether to depend on `fluxcd/pkg/ssa` (old choice) or hand-roll on
client-go — either way the doc gates it. Rationale: inventory is the seed of
`down`, `diff`, and prune later; building it early means every subsequent
domain writes through one audited apply path instead of growing its own
(the old tree had apply fan-in 19).

**M7 — pack: fetch + render, minimal contract.** New domain `internal/pack`
(`CUBE-PKG-*`), deliberately scoped to: local-directory packs, `pack.cue`-or-
simpler metadata (design doc decides whether CUE returns at all), raw
manifests + kustomize rendering. Explicitly excluded from this milestone:
helm rendering, OCI fetch, catalogs/indexes, dependency graph. User-visible:
`pack render` (pure, no cluster) and `pack install <dir>` (render → apply via
M6). Rationale: pack was the largest old package (59 files) and the biggest
scope-creep magnet; landing a small frozen core first, with helm/OCI as
their own later milestones, is the main defense. The old pack-contract-v1
reference doc is input, not authority.

**M8 — engine: gitops driver seam + Flux.** New domain `internal/engine`
(`CUBE-ENG-*`), the second Kind-B driver seam: pure-translator interface
(engines return engine-native objects; caller applies via M6) + conformance
suite in the domain package, `engine/flux` as the only implementation.
Design doc required (new seam + whatever Flux manifests/deps it needs).
Carry the old hard rule forward: Argo CD support, if it ever returns, must
never become a compile-time dependency (`unstructured` only). Rationale:
the seam pattern is proven by M3; a second driver seam is where the old
factory-import-cycle rot started, so this milestone is the test of the
"factories at the edge" rule.

**M9 — registry: OCI delivery bus.** New domain `internal/registry`
(`CUBE-REG-*`): install zot (or the doc's chosen registry) in-cluster via
the apply path, push rendered packs as OCI artifacts (oras-go v2 → design
doc), point the engine's OCI source at it. Rationale: this converts M7+M8
from "CLI applies manifests" into the real product loop (engine pulls from
the in-cluster bus). Sequenced after engine so the engine's source
requirements drive the registry contract, not the reverse.

**M10 — orchestrator: `up`/`down` as a phase runner.** New domain
`internal/orchestrator` (`CUBE-ORC-*`): kubeadm-style `[]Phase{Name, Run}`
sequencing cluster → registry → engine → packs, depending only on
interfaces; composition/factories stay in `internal/cli`. `down` walks the
M6 inventory. Rationale: this is the god-file ghost (§4 risk #1) — it lands
last, after every phase it sequences already exists and is tested in
isolation, so it can genuinely be "ordering and timeouts" this time.

**After M10 (not sequenced here):** periphery in pull order — doctor, diff,
trust/CA, lock/vendor, spokes, cnoe — each its own milestone with its own
justification; none should land before the core loop is real.

## 3. Alternative paths after M4

Three orderings of the §2 units, differing in *when pack lands relative to
its prerequisites and consumers*. The governing constraint (owner input):
pack must not land until everything it needs is already in place — the open
question is whether "needs" means only its upstream dependencies (kube,
apply) or also its downstream consumers (engine, registry).

**Path A — dependency ladder (design §8 order).**
`kube → apply → pack → engine → registry → orchestrator`.
Pack lands once its upstream prerequisites exist: `pack install` = render →
SSA via apply (the ADR-0045 insight that SSA-with-wait is a legitimate
delivery path). Engine and registry then attach as consumers. *Strength:*
shortest route to a user-visible pack workflow; each milestone immediately
consumes the previous one. *Weakness:* pack's delivery contract is designed
before its real consumers exist — when engine (source formats) and registry
(push contract) arrive, pack may need revision, which is exactly the churn
the owner's constraint is meant to avoid.

**Path B — consumers-first: pack lands last before the orchestrator
(recommended).**
`kube → apply → engine → registry → pack → orchestrator`.
Pack arrives with *everything* in place: client, apply path, a live engine
defining what a delivery source looks like, and a registry defining the
push contract. Pack's design doc is then written against two real consumers
instead of speculation — the strongest guard against rebuilding the old
59-file sprawl. *Cost, stated honestly:* the engine milestone must install
Flux without pack — from a local vendored-manifests directory applied via
the apply path. That temporarily echoes the pre-ADR-0007 "engine baked into
the tool" pain (not pinnable/vendorable as a pack); mitigations: manifests
live on disk rather than compiled in, and the engine install migrates to an
ordinary pack once pack lands. Engine and registry milestones also ship no
pack-shaped user feature — their green proof is `init` + engine healthy +
one artifact pushed and delivered end-to-end.

**Path C — cluster-domain completion first, then the ladder.**
`cluster lifecycle (delete/down, status, forProvider validation per cluster
design §9) → kube → apply → …` continuing as A or B. Finishes the domain we
just opened before broadening: every §9 follow-up lands while M3 context is
fresh, each PR is small and user-visible (`cube-idp delete`, `status`), and
the client-go dependency decision is deferred one milestone. *Strength:* no
half-finished domain left behind when the delivery core starts; `down`
exists before anything installs software worth tearing down. *Weakness:*
the core loop arrives one or two milestones later, and `delete` at this
stage only removes a kind cluster — cheap but thin.

**Recommendation.** Path **B**, optionally prefixed with C's first
milestone if the owner wants `delete`/`down` to exist before anything gets
installed — the two compose cleanly (`cluster-complete → kube → apply →
engine → registry → pack → orchestrator`, 7 milestones, one green PR each).
Path A remains the documented fallback if B's vendored-manifests engine
bootstrap feels like too much of an ADR-0007 regression.

**Independent of path — `forProvider` validation:** fold into M4 if the
edge-validation mechanism is obvious once the M4 design doc is written;
standalone chore PR first if it needs its own seam discussion. Either is
one small green PR; the only hard constraint is the import direction
(`internal/config` never imports `internal/cluster`).

## 4. Risks / watch-items from the old-codebase autopsy

| Risk (old evidence) | Becomes live at | Guard |
| --- | --- | --- |
| God-file orchestrator (`up.go` 1,504 lines, 615-line `Run`, ~20 imports) | **orchestrator milestone** | Phase-runner contract (design §7); orchestrator imports interfaces only; funlen/filelen gates are already CI-enforced. |
| Factory import cycles (`engine/factory`, `cluster` alias re-exports, `cfgload`/`refval` split-packages) | **engine milestone** (second driver seam), then every seam after | Factories live only in `internal/cli`; domain packages never import their own subpackage implementations; flag any new package whose only job is cycle-breaking — that is the smell itself. |
| Central registries (diag catalog: 107 codes, fan-in 30/69 files; pack catalog/index) | **Every milestone**, acutely the pack milestone (catalog temptation) and any future `explain` command | `cubeerr` stays machinery-only (rule already binding); codes live in per-domain `errors.go`; no cross-domain code catalog, no pack index until a design doc justifies one. |
| Dependency creep (old tree: Helm SDK, controller-runtime, TUI stack, forked go-getter, cloud SDKs) | **kube** (client-go), **apply** (ssa lib?), **pack** (CUE? kustomize-api?), **engine/registry** (flux manifests, oras) | Closed set + design-doc gate per dependency; carry the old single-file import-boundary trick (only one file may import a heavy SDK) into any milestone that adopts one. |
| Business logic leaking into `cmd/`/`cli` (old `cmd` imported 25 packages) | **Every milestone**; pressure spikes at M4 (scaffold logic) and the orchestrator milestone (composition) | `internal/cli` stays flag-mapping + factories; scaffold/name-generation logic must live in a domain, decided in the M4 design doc. |
| Pack domain sprawl (59 files: fetch+render+helm+catalog+depgraph+OCI in one package) | **pack milestone** onward | The pack design doc draws the exclusion list (no helm, no OCI, no graph, no catalog); each exclusion returns only as its own milestone. |
| Seam ceremony without a second implementation | **engine milestone** (single-impl seam) | Acceptable only because gitops engines are a designated Kind-B swappable backend (design §6); conformance suite + fake keep it honest, as in M3. |

## 5. Open questions for the owner

1. **Is Argo CD in scope for the rebuild horizon at all?** If yes, the engine
   conformance suite should be written against two mental implementations
   from day one; if no, we note it as a non-goal and keep the seam anyway.
2. **Does CUE return as the pack metadata/values language**, or does the
   rebuilt pack contract use plain YAML + apimachinery validation? This is
   the single biggest dependency/complexity decision in the pack unit.
3. **Is air-gap (`lock`/`vendor`/bundle) a differentiator we are committed
   to?** It shaped many old decisions (embedded manifests, pinning,
   `IsLocalRegistryHost`); knowing early keeps the pack/registry contracts
   bundle-compatible without building the bundle.
4. **How much of frozen `pack-contract-v1.md` is a compatibility promise**
   to existing pack repos vs history we are free to break?
5. **Second cluster provider (k3d or `existing`)**: is there near-term pull?
   `existing` in particular changes the kube unit's kubeconfig-injection contract and
   would be cheap to reserve room for now.
6. **Milestone granularity check:** the kube unit has no user-visible
   command. Comfortable with one "plumbing-only" green PR, or should
   kube+apply be one milestone ("apply a manifest to the init'd cluster") at the cost of a
   bigger PR?

7. **Path choice (§3):** does "pack needs everything in place" mean
   upstream only (Path A) or consumers too (Path B, recommended)? And should
   the cluster-lifecycle completion milestone (Path C prefix) run first?

---
*Draft prepared 2026-07-29, revised 2026-07-31 on branch `RafPe/roadmap-direction`; no other
files touched.*
