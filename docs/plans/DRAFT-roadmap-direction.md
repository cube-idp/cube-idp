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

## 2. Proposed milestone sequence (M4–M10)

Each milestone: one green PR, one new `internal/<domain>` package at most,
one `ConfigSpec` sub-struct per component, design doc first where a new
domain / seam / dependency arrives. Follows the design §8 build order
(`config → kube → apply → cluster → pack → engine → registry →
orchestrator → periphery`) with one deviation noted in §3.

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

## 3. Sequencing alternatives considered

**kube-before-pack (recommended) vs pack-first-with-stub.** Pack rendering
is pure (no cluster I/O), so M7 could run before M5/M6 and ship `pack
render` earlier without new heavy deps. Rejected as the default because:
(a) `pack install` without an apply path either stubs the interesting half
or prints YAML, delivering little; (b) building pack while its consumer
contract (what apply needs) is unknown invites the old speculative sprawl;
(c) design §8 already records the dependency-ordered sequence and deviating
needs a reason, not a preference. Pack-first is worth revisiting only if
client-go/design work stalls M5.

**registry-before-engine vs engine-before-registry (recommended: engine
first, matching both §8 and the ROADMAP's tentative order).** The engine
defines what source it consumes; the registry exists to serve it. Building zot first would freeze a
push contract with no consumer. Additionally, M7+M6 already give a working
CLI-applied delivery (the ADR-0045 "prerequisites" insight: SSA-with-wait is
a legitimate delivery path), so there is no pressure to rush the bus.

**Fold `forProvider` validation into M4 vs standalone chore PR.** Recommend
folding if the edge-validation mechanism is obvious once the M4 design doc
is written; standalone first if it needs its own seam discussion. Either is
one small green PR; the only hard constraint is the import direction.

## 4. Risks / watch-items from the old-codebase autopsy

| Risk (old evidence) | Becomes live at | Guard |
| --- | --- | --- |
| God-file orchestrator (`up.go` 1,504 lines, 615-line `Run`, ~20 imports) | **M10** | Phase-runner contract (design §7); orchestrator imports interfaces only; funlen/filelen gates are already CI-enforced. |
| Factory import cycles (`engine/factory`, `cluster` alias re-exports, `cfgload`/`refval` split-packages) | **M8** (second driver seam), then every seam after | Factories live only in `internal/cli`; domain packages never import their own subpackage implementations; flag any new package whose only job is cycle-breaking — that is the smell itself. |
| Central registries (diag catalog: 107 codes, fan-in 30/69 files; pack catalog/index) | **Every milestone**, acutely M7 (pack catalog temptation) and any future `explain` command | `cubeerr` stays machinery-only (rule already binding); codes live in per-domain `errors.go`; no cross-domain code catalog, no pack index until a design doc justifies one. |
| Dependency creep (old tree: Helm SDK, controller-runtime, TUI stack, forked go-getter, cloud SDKs) | **M5** (client-go), **M6** (ssa lib?), **M7** (CUE? kustomize-api?), **M8/M9** (flux manifests, oras) | Closed set + design-doc gate per dependency; carry the old single-file import-boundary trick (only one file may import a heavy SDK) into any milestone that adopts one. |
| Business logic leaking into `cmd/`/`cli` (old `cmd` imported 25 packages) | **Every milestone**; pressure spikes at M4 (scaffold logic) and M10 (composition) | `internal/cli` stays flag-mapping + factories; scaffold/name-generation logic must live in a domain, decided in the M4 design doc. |
| Pack domain sprawl (59 files: fetch+render+helm+catalog+depgraph+OCI in one package) | **M7** onward | M7's design doc draws the exclusion list (no helm, no OCI, no graph, no catalog); each exclusion returns only as its own milestone. |
| Seam ceremony without a second implementation | **M8** (single-impl engine seam) | Acceptable only because gitops engines are a designated Kind-B swappable backend (design §6); conformance suite + fake keep it honest, as in M3. |

## 5. Open questions for the owner

1. **Is Argo CD in scope for the rebuild horizon at all?** If yes, the M8
   conformance suite should be written against two mental implementations
   from day one; if no, we note it as a non-goal and keep the seam anyway.
2. **Does CUE return as the pack metadata/values language**, or does the
   rebuilt pack contract use plain YAML + apimachinery validation? This is
   the single biggest dependency/complexity decision in M7.
3. **Is air-gap (`lock`/`vendor`/bundle) a differentiator we are committed
   to?** It shaped many old decisions (embedded manifests, pinning,
   `IsLocalRegistryHost`); knowing early keeps M7/M9 contracts
   bundle-compatible without building the bundle.
4. **How much of frozen `pack-contract-v1.md` is a compatibility promise**
   to existing pack repos vs history we are free to break?
5. **Second cluster provider (k3d or `existing`)**: is there near-term pull?
   `existing` in particular changes the M5 kubeconfig-injection contract and
   would be cheap to reserve room for now.
6. **Milestone granularity check:** M5 (kube) has no user-visible command.
   Comfortable with one "plumbing-only" green PR, or should M5+M6 be one
   milestone ("apply a manifest to the init'd cluster") at the cost of a
   bigger PR?

---
*Draft prepared 2026-07-29 on branch `RafPe/roadmap-direction`; no other
files touched.*
