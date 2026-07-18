# cube-idp Phase 5 roadmap design

Date: 2026-07-18
Status: PROPOSED (owner review pending)
Owner decisions ratified: 2026-07-17 and 2026-07-18 (see §2)
Prior art: `2026-07-13-cube-idp-architecture-design.md` (thesis, pack contract),
`2026-07-15-cube-idp-phase4-first-release-design.md` (v0.1.0, F12),
`docs/superpowers/plans/2026-07-18-openchoreo-spike-plan.md` (branch
`spike/openchoreo-plan`, unmerged).

## 1. Goal

v0.1.0 works only from a repo checkout (F12: the default profile resolves the
gateway pack from the repo-relative `packs/traefik`). Phase 5 makes the
released binary **fully operational standalone** and grows the platform
surface:

1. Packs live in a public monorepo and ship as OCI artifacts with
   GitHub-native provenance attestations — the downloaded binary needs
   nothing else (closes F12).
2. The pack fleet grows from 7 to ~19 (12 confirmed new packs across 11
   Wave A tasks), each with CI conformance and doctor coverage.
3. CLI UX gaps close: visible cluster provisioning, opt-in HTTP gateway
   port, remote pack catalog, engine install knobs.
4. Per-pack Gitea delivery gives users an editable, in-cluster fork of any
   pack.
5. Hub/spoke: cube-idp bootstraps spoke clusters so the hub's engine can
   manage a fleet — registration only; workload delivery to spokes is the
   engine's job, not cube-idp's.
6. Plugins ship from a public attested monorepo mirroring the packs
   platform (decision 15), doctor becomes a tri-state checklist
   (decision 14), and a final CLI-coherence gate freezes and documents
   the whole command surface (decision 16).

Everything is sized for the established execution mode: one
manually-dispatched agent per task, ledger ticks in the plan file.

## 2. Ratified owner decisions

| # | Decision | Choice |
| --- | --- | --- |
| 1 | Packs repo shape | Public monorepo `cube-idp/packs`, **per-pack tags** (`argocd/v0.2.0`) |
| 2 | Main-repo e2e pack source | **Digest-pinned** packs repo (not vendored) |
| 3 | Gateway HTTP (non-TLS) port | **Opt-in** (`spec.gateway.httpPort`), not default |
| 4 | Gitea delivery | **Per-pack** flag/field, not cube-wide mode |
| 5 | Crossplane | **Core-only** pack first; providers as separate packs later |
| 6 | OpenChoreo | **Out of Phase 5 entirely** (owner, 2026-07-18) — research branch `spike/openchoreo-plan` parked; NO spike tasks this phase |
| 7 | Pack signing | **In scope for the repo split** (W0) via **GitHub-native artifact attestations** — keyless, no key custody (owner, 2026-07-18: "make this simple"); verification documented via `gh attestation verify`, in-binary crypto deliberately out |
| 8 | Engine knobs | **Typed knobs → patches** over embedded manifests (option A); field named **`engine.tuning`** per decision 11 (renamed from `engine.values`) |
| 9 | Spokes scope | **Registration only** — bootstrap SA + hub registration, engine takes over; no pack delivery to spokes by cube-idp |
| 10 | Renovate/dependabot in packs repo | **Parked** for a later phase |
| 11 | The values stone | `values:` = **helm values only** (owner, 2026-07-18): chartless `values:` → typed error; extras via **`packs[].extraManifests`** (any pack kind, `${GATEWAY_*}`-substituted, appended); customized installs show **CUSTOMIZED** in `kubectl get packs`. Vocabulary: values→helm, tuning→engine patches, extraManifests→appended objects |
| 12 | Engine self-management | **Opt-in `engine.selfManage: true`, sourced from zot** (never Gitea): render → push `cube-engine` artifact → engine reconciles itself; direct SSA only on first install and unhealthy-recovery (owner-driven, 2026-07-18) |
| 13 | Gitea guarantee | Gitea stays an **optional pack**: `delivery: repo` packs validate gitea presence at load (typed error; gitea itself never repo-delivered), gitea hard-ordered right after the gateway, repo delivery gated on gitea API readiness; "gitea as core" parked as a product question |
| 14 | Doctor UX | **Tri-state checklist** (owner, 2026-07-18): every check renders one green ✔ / yellow ⚠ / red ✗ row — passes shown, not silent; exit 1 iff any red; additive `checks` JSON array |
| 15 | Plugins platform | **`cube-idp/plugins` public monorepo mirroring packs** (owner, 2026-07-18): dedicated folder per plugin, `<name>/vX.Y.Z` tags, per-platform OCI artifacts + discovery index + keyless GitHub attestations; `plugin install` resolves the index by digest and hands off to the unchanged sha256 trust-consent flow |
| 16 | CLI coherence gate | A single **final task** (owner, 2026-07-18): command-tree golden fence (`TestCommandTreeGolden`), conventions audit (-f/--yes/prompt-doctrine), README + machine-readable docs sweep — claimed only when all other main-repo tasks are DONE |

## 3. Workstreams

### W0 — Pack platform foundations (serial gate; blocks Waves A and C)

- **W0.T1 Pack contract v1 freeze.** The moment packs are public, the pack
  format is an API: `pack.cue` fields (name, version, expose, urls,
  authSecretRef, impliedFields), `manifests/` and `chart.yaml` layouts,
  `${GATEWAY_HOST}`/`${GATEWAY_FQDN}` substitution, values merge semantics
  (D15 order: pack defaults ← user values ← substitution). Deliverable: a
  versioned contract doc + CUE schema the conformance harness enforces.
- **W0.T2 `cube-idp/packs` monorepo scaffold + publish CI.** Per-pack tags
  (`<pack>/vX.Y.Z`); CI publishes each pack as an OCI artifact to
  `oci://ghcr.io/cube-idp/packs/<name>`, attests it with GitHub-native
  artifact attestations (decision 7), and publishes a catalog **index
  artifact** (`.../packs/index`) listing name/version/description/ref for
  every pack.
- **W0.T3 Conformance harness.** Packs-repo CI smoke per pack: kind +
  `cube-idp up` + health gate + teardown. This is the multiplier that makes
  Wave A's parallel authoring safe — without it a 16-pack repo rots
  silently.
- **W0.T4 Migrate the 7 existing packs; close F12.** `init` defaults write
  `oci://ghcr.io/cube-idp/packs/...` refs (already the intended shape);
  gateway pack resolves out-of-repo; `init --local` keeps working for
  checkout development; main-repo e2e consumes the packs repo pinned by
  digest (decision 2). Remove the v0.1.0 README caveat.
- **W0.T5 Provenance attestation + verification docs.** CI attests every
  published pack digest with `actions/attest-build-provenance` (keyless
  OIDC — zero repo secrets, zero key custody). Verification is the
  documented `gh attestation verify oci://… --owner cube-idp` path in the
  contract doc and README. In-binary cryptographic verification is a
  deliberate NON-goal (it would require a sigstore Go dependency); pull
  integrity in the binary rests on digest pinning (index digests +
  packs.lock) over TLS.

Ordering inside W0: T1 → T2 → {T3, T4, T5} (the last three can run in
parallel once T2's repo and index format exist).

### Wave A — pack authoring (fully parallel after W0)

One agent per pack. Definition of done for every task: pack + conformance
smoke (W0.T3 harness) + health/diagnosis rules registered (doctor + CUBE
code registry — the `explain` completeness fence enforces the registry) +
README.

Confirmed: `crossplane-core` · `kyverno` · `kyverno-policies` (curated
default policies, separate so they stay optional) · `cloudnativepg` ·
`argo-rollouts` (plain install first; per-gateway traffic-shifting is a
follow-up) · `argo-events` · `argo-workflows` · `prometheus-stack` +
`grafana` · `kargo` · `floci` + `floci-ui` (owner, 2026-07-18: the
cloud-emulator pair — floci is an AWS-compatible local emulator, floci-ui
its console; fills the local-cloud-dev slot the dropped localstack left.
Docker-only upstream, so both are authored-manifest packs; in-cluster the
core services work but container-backed ones — Lambda/RDS/ECS — do not,
since kind nodes have no docker socket).

`kgateway` and `openbao` are **parked** with the OpenChoreo research
(decision 6) — not Wave A candidates in this phase.

### Wave B — CLI UX (parallel with everything; no W0 dependency except B3's index format)

- **B1 Visible provisioning.** Wire kind's `log.Logger` (and k3d's
  equivalent) in `internal/cluster/kindp` / `k3dp` into the existing
  `StepLog` event vocabulary so `up` shows what the provider is doing.
  Same treatment for the engine-install wait, the other long silent
  stretch.
- **B2 Opt-in HTTP gateway port.** `spec.gateway.httpPort` + a second
  `GatewayHTTPNodePort` (30080) mapped by both cluster-creating providers
  and honored by both gateway packs. Cluster-shape field: documented in the
  recreate-caveat table.
- **B3 Remote pack catalog.** Replace the hardcoded `packCatalog`
  (cmd/pack.go) with the W0.T2 index artifact; `pack install` and `init`'s
  wizard discover packs without a binary release. Add `pack list
  --available` / `pack search`. Depends only on the index *format* being
  agreed; can develop against a stub.
- **B4 `engine.tuning` typed knobs.** Design in §4.
- **B5 The values stone** (decision 11): helm-only `values:` enforcement,
  `packs[].extraManifests`, CUSTOMIZED printer column — plan task U4.
- **B6 Engine self-management** (decision 12): `engine.selfManage` from
  zot — design in §4, plan task P8 (needs B4 + C1's plumbing era, see
  plan dependencies).
- **B7 Doctor tri-state checklist** (decision 14) — plan task U5.

### Wave C — Gitea delivery (after W0)

- **C1 Per-pack repo delivery.** `pack install --via repo` (per-pack
  `delivery: repo` field per decision 4): cube-idp creates the Gitea repo,
  pushes the rendered pack, and points the engine at the repo instead of
  the OCI ref — building on the Phase 3 repo/syncer subsystem
  (`deployRepo`, `cube-idp sync`). The payoff: an editable, in-cluster
  fork of any pack (edit in Gitea UI → engine reconciles).

### Wave D — spokes v1 (independent of all other waves)

Design in §5.

### Plugin platform (decision 15 — after the pack platform, mirrors it)

- `cube-idp/plugins` public monorepo: dedicated folder per plugin,
  per-plugin tags, per-platform OCI artifacts + discovery index +
  keyless attestations — plan task P9 (self-contained CI, no secrets).
- `plugin install` resolves the official index, pulls by digest, and
  hands off to the existing sha256 trust-consent flow unchanged — plan
  task P10.

### Final gate (decision 16)

One last task (plan F1), claimable only when every main-repo task is
DONE: a command-tree golden fence freezing the CLI surface, a
conventions audit (`-f` defaults, `--yes` twins, prompt doctrine), and
the README/machine-readable docs sweep — "after our changes, the CLI is
correct" made mechanical.

### Spike (anytime, timeboxed)

- CNOE stacks harvest (owner-confirmed 2026-07-18): try `cnoe import`
  (cmd/cnoe.go) against cnoe-io/stacks and raftechio/cnoe-stacks-custom
  entries as the cheap ingestion path before promoting anything to a
  first-class pack.

No OpenChoreo spikes run in Phase 5 (decision 6) — the research plan on
`spike/openchoreo-plan` stays parked, unexecuted.

## 4. `engine.tuning` — typed knobs → patches, and self-management

(The field was named `engine.values` in early drafts; renamed per
decision 11 — the word *values* is reserved for helm.)

**Ground truth:** both engines install from embedded, pre-rendered plain
manifests (`//go:embed manifests/install.yaml` in
`internal/engine/{flux,argocd}`), parsed to `[]*unstructured.Unstructured`
and server-side-applied. **There is no helm anywhere in the engine install
path** — flux's helm-controller is deliberately never installed; helm
rendering is client-side and pack-only. Plain manifests are therefore the
*easy* case for option A, not the hard one: knobs are applied as in-memory
mutations (or strategic-merge patches) over `InstallManifests()` output
before SSA. A helm-installed engine would have been the expensive path
(re-render charts at up-time); it does not exist here.

Design:

- Schema: `spec.engine.tuning` with a small, documented, *closed* knob set
  (v1 proposal: `components.<name>.replicas`,
  `components.<name>.resources`; component names validated against the
  engine's actual Deployments). Closed schema → typed CUBE error on unknown
  knob, not silent ignore.
- Conflict policy follows the D10 `providerConfig` philosophy: cube-required
  fields always win; a real conflict is a typed error, not a merge
  surprise.
- Inspectability: `cube-idp config render-engine` twin of
  `render-cluster` shows the patched result.
- Config plumbing: schema.cue update; nil-map round-trip discipline same as
  `PackRef.Values` (omitempty, absent key not YAML null).

**Self-management (decision 12).** With `engine.selfManage: true`, after
the engine health gate `up` pushes the rendered (tuned) engine manifests
as `oci://<zot>/cube-engine` and attaches an engine-native self-source
with pruning disabled (flux: OCIRepository + Kustomization in
flux-system; argocd: Application over its own namespace); the engine then
reconciles itself continuously. Rendering ALWAYS happens in cube-idp
before the push — the artifact is finished YAML; the engine never sees
tuning as a concept. Three rules keep it sound: first install is direct
SSA of the rendered manifests (bootstrap); once the self-source exists,
later `up`s render → push → poke and never SSA (single owner); an
unhealthy engine at `up` start gets a direct-SSA fallback of freshly
rendered manifests (self-brick recovery). Because the SSA'd state and the
first pushed artifact are byte-identical renders, enabling selfManage
never causes a restart by itself. Requires nothing beyond zot — no gitea,
offline bundles included. Plan task P8; its four-scenario matrix
(tuning × selfManage) is normative.

## 5. Spokes v1 — registration only

**Scope (decision 9):** cube-idp ensures the spoke cluster exists,
bootstraps credentials, registers the spoke with the hub's engine, and
exits. The engine takes over from there. Delivering workloads to spokes is
user-authored engine content (Applications/Kustomizations, possibly in
Gitea repos) — **not** cube-idp packs. There is no per-spoke pack list and
no cross-cluster registry plumbing in this design.

- **Config:** spokes are first-class, declarative cube.yaml content —
  `cube-idp spoke add` writes the block below, and hand-editing cube.yaml
  directly is equally valid; either way, re-running `up` reconciles spokes
  like everything else and `down` cascades truthfully.

  ```yaml
  spec:
    spokes:
      - name: staging
        cluster: {provider: kind}
      - name: prod-eu
        cluster: {provider: existing, context: eks-prod-eu}
  ```
- **Commands:** `cube-idp spoke add|list|remove` — config-mutating in the
  `pack install` mold (mutate cube.yaml, validate by round-trip, `up`
  applies). `down` deletes cube-created spoke clusters and deregisters
  `existing` ones.
- **Bootstrap on the spoke:** namespace + ServiceAccount
  `cube-idp-{engine}` + ClusterRoleBinding to `cluster-admin` + long-lived
  token secret (`kubernetes.io/service-account-token`). Note: this is the
  standard mechanism for "masters-equivalent" SA power — group membership
  (`system:masters`) is claim-based and not grantable to a SA; the
  `cluster-admin` binding is what Argo CD's own `cluster add` does.
- **Hub registration (both engines, day 1 — recommended):**
  - argocd: cluster secret (`argocd.argoproj.io/secret-type: cluster`) with
    server URL + bearer token + CA.
  - flux: kubeconfig built from the SA token, stored as a hub Secret for
    `Kustomization.spec.kubeConfig.secretRef` use.
- **Networking:** the hub engine's pods must reach the spoke API server.
  kind: all kind clusters share the `kind` docker network, so the server
  URL is rewritten to `https://<name>-control-plane:6443`. k3d: creates a
  per-cluster network by default — spoke creation must join a shared
  network (cluster-shape field, recreate caveat). `existing`: reachability
  is the user's responsibility; `doctor` probes it.
- **Representation once added:** a registered spoke is visible everywhere
  state is shown — its own row in `status` (provider, reachability,
  engine-registration health), `spoke list` output, an inventory record
  driving the `down` cascade, and doctor reachability checks backed by a
  new CUBE code range (registered in the explain registry).

## 6. Adopted gaps and parking lot

Adopted into waves above: pack attestation (W0.T5), remote catalog (B3),
conformance harness (W0.T3), doctor coverage (Wave A DoD).

Parked: Renovate/dependabot in the packs repo (decision 10 — revisit once
the fleet exists); blueprints repository (parked earlier; prerequisite is
the W0.T1 contract freeze); spoke pack-targeting (dropped — engine's job);
OpenChoreo integration in any form, and with it the `kgateway` and
`openbao` packs (decision 6 — the research branch stays parked);
in-binary signature verification (needs a sigstore Go dependency —
revisit only if the threat model demands it); public-release path polish
(Homebrew tap, `go install`) — natural companion to the packs going
public, scheduled when the org flips the main repo public.

## 7. Dispatch map

```text
W0.T1 ──► W0.T2 ──► { W0.T3, W0.T4, W0.T5 }        (serial gate, then 3-wide)
                       │
                       ▼
Wave A: 11 pack tasks, fully parallel               (after W0)
Wave C: C1 Gitea delivery                           (after W0)
Wave B: B1, B2, B4, B5, B7 anytime; B3 after index format; B6 after B4+C1
                                                    (parallel with W0)
Wave D: spokes v1                                   (anytime, independent)
Plugins: P9 repo ──► P10 install                    (after pack platform)
Spike: CNOE stacks harvest                          (anytime, timeboxed)
Final: F1 CLI coherence gate                        (last, all main-repo tasks DONE)
```

Suggested first dispatch batch (max parallelism, no conflicts): W0.T1,
B1, B2, B4, Wave D design/bootstrap, one CNOE harvest spike.

## 8. Open questions — RESOLVED (owner, 2026-07-18)

1. `engine.tuning` v1 knob set: `replicas` + `resources` per component
   only (plan GT1); no args escape hatch. Note: these are NOT helm values
   — the engine install is pre-rendered embedded manifests with no chart
   to merge into (§4); packs keep their helm-values semantics unchanged.
2. Spokes both-engines parity day 1: **yes** (plan GT2).
3. `kgateway` / `openbao`: **parked** with OpenChoreo (decision 6).
4. Spoke bootstrap naming: namespace `cube-idp-system`, SA
   `cube-idp-<engine>`, CRB `cube-idp-<engine>-admin` (plan GT4;
   GT5/GT6/GT7 ratified same day: TokenRequest 10y, kind+existing
   providers only, `<cube>-spoke-<name>` cluster naming).
