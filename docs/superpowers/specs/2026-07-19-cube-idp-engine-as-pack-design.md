# cube-idp engine-as-a-pack: engine install from a pack reference, values replace tuning

Date: 2026-07-19
Status: PROPOSED (owner review pending). Plan: not yet written
(writing-plans follows owner ratification of this spec).
Prior art: `2026-07-18-cube-idp-phase5-roadmap-design.md` (GT1/GT15 tuning
vocabulary, GT16 self-management — this spec amends all three),
`2026-07-19-cube-idp-pack-depends-and-cubelock-crd-design.md` (p6 depgraph
this spec's deferred follow-up builds on), `docs/pack-contract-v1.md`
(§4 vocabulary triad — amended, §6 additive compatibility — obeyed).

Owner directive (2026-07-19): "Our engine must become a pack — installed
from reference; instead of tuning it would have values — then the engine
becomes visible in the packs as well."

---

## 1. Motivation — evidence from the tree

### 1.1 The engine is the last component outside its own platform

Every workload cube-idp installs is a pack — including the gateway, which
`orderPackRefs` (internal/up/up.go:1136) prepends into the same
`[]config.PackRef` slice as `spec.packs`, delivers through the identical
engine path, and records via `pack.PackObject`. The engine alone installs
from ~2 MB of YAML embedded in the binary
(internal/engine/flux/manifests/install.yaml, 150 KB;
internal/engine/argocd/manifests/, 1.8 MB), is invisible in
`kubectl get packs`, and is pinned nowhere: `cube.lock`'s `EngineLock`
records only `{Type}` (internal/lock/lock.go:17-45) — the `cube-engine`
self-artifact GT16 pushes to zot has no recorded version, digest, or hash.

### 1.2 tuning is a closed knob the owner has outgrown

`engine.tuning` (GT1/GT15) allows exactly
`components.<name>.{replicas,resources}` (internal/engine/tune.go). The
owner's requirement is full operator control of the engine install —
demonstrated concretely by the argocd UI-exposure request
(argo-helm `server.*` values), which the closed knob set cannot express
and was *designed* not to express. The knob set is the wrong shape, not
merely too small.

### 1.3 The gateway precedent proves the model

The gateway pack already demonstrates every property the engine needs:
ref-addressable source (local dir / `oci://` / git via `pack.Fetch`),
published-OCI default (`config.Default()` sets
`oci://ghcr.io/cube-idp/packs/traefik:0.2.0`), chart values under operator
control merged over baked defaults (packs/traefik/chart.yaml +
internal/pack/helm.go), a Pack record row, and bundle vendoring for
offline. The engine differs in exactly one way — the bootstrap asymmetry
of §3.2.

---

## 2. Decisions (owner-answered 2026-07-19)

- **D1 — Dedicated engine packs**, not reuse of the UI-oriented
  `packs/argocd`: new `packs/cube-engine-flux` and
  `packs/cube-engine-argocd` in $PACKS. Engine install stays headless;
  the UI pack remains a flux-engine-cube concern.
- **D2 — Chart-based packs (Approach A)**, traefik-style: `chart.yaml`
  pinning a community chart with baked opinionated values, rendered by the
  existing helm path. Rejected: data-only vendored `install.yaml` packs
  (plain-manifest packs cannot consume values — GT15/CUBE-4016 — which
  contradicts D3) and a new patch mechanism (reinvents the closed-knob
  dead end).
- **D3 — Open values ("everything — operator in control")**: `#Values` in
  both engine packs is an open CUE struct (`...`). Content validation is
  helm's; the accepted cost is that helm silently ignores unknown keys.
- **D4 — `selfManage` unchanged**: still an opt-in bool; GT16's four
  rules survive verbatim. The self artifact simply *becomes* the rendered
  engine pack. Rejected: always-on (removes the CLI-owned recovery mode),
  default-flip (silently changes existing cube.yaml meaning).
- **D5 — Engine packs are install-only**: no HTTPRoute, no `expose`
  block. The engine is SSA'd on a fresh cluster *before* the gateway pack
  delivers the Gateway API CRDs (traefik ships them as static manifests;
  envoy-gateway's controller installs them at runtime; `up` only waits —
  waitCRDEstablished, up.go:885). A gateway-dependent object in the
  engine pack would fail the bootstrap SSA dry-run. UI exposure is
  deferred (§8.1).
- **D6 — tuning is removed, not deprecated**: config load rejects
  `engine.tuning` with a typed migration error pointing at
  `engine.values`. Pre-1.0, one release note, no dual code path.
- **D7 — Capability inference is a separate follow-up** (§8.2): this
  phase does not generalize the depgraph's implicit edges.

---

## 3. Design

### 3.1 Config surface

```yaml
spec:
  engine:
    type: argocd            # unchanged — selects Deliver/Poke/Health behavior
    ref: <pack ref>         # optional; local dir, oci://, or git ref
    values: { ... }         # open chart values, merged over the pack's baked defaults
    selfManage: true        # unchanged opt-in (GT16)
```

- No `pack` name field (unlike `GatewaySpec`): the pack name is a
  function of `type`. `EngineSpec.PackRef()` returns `ref` if set, else
  the published default from a per-type constant map:
  `oci://ghcr.io/cube-idp/packs/cube-engine-<type>:<pin>`.
- `verifyEnginePackRef` (mirror of the gateway's CUBE-0008 check,
  up.go:806): the fetched pack's `name` must equal `cube-engine-<type>`
  — pointing the argocd engine at the flux pack is a typed error.
- Schema: `engine.tuning` block deleted from schema.cue; `values?: {...}`
  added open; load.go rejects a present `tuning` key with the §5 migration
  code.

### 3.2 The bootstrap asymmetry — the one way the engine is not a pack

The gateway pack is delivered *by the engine*; the engine cannot be.
Rule: the engine pack is **fetched and rendered like any pack, but its
first install is direct SSA by the CLI** — which is GT16 rule 1 verbatim,
already implemented (`installNeedsSSA`, up.go:1188). With `selfManage`,
post-bootstrap ownership transfers to the engine itself via the existing
`deliverEngineSelf` (up.go:1217), which already accepts pre-rendered
objects — the `cube-engine` artifact becomes the rendered engine pack
with zero signature change. selfManage and engine-as-pack thereby unify:
the former is "the engine pack, delivered to itself."

### 3.3 up.Run flow (everything unlisted is byte-identical)

1. `enginefactory.New` builds tuning-less engines (`New()` only).
2. **New step** between packs-crd and engine install:
   `[engine-pack] fetching cube-engine-argocd@<ver>` —
   `pack.Fetch(PackRef())` → `verifyEnginePackRef` →
   `RenderWith(engine.Values, "", gw)` → `installObjs`. Replaces
   `eng.InstallManifests()` (up.go:237).
3. `installNeedsSSA` decision, SSA + `RecordInventory` — unchanged code,
   pack-rendered objects.
4. …unchanged through `waitHealthy`…
5. selfManage block calls `deliverEngineSelf(installObjs)` — unchanged.
6. `cube.lock`: `EngineLock` grows from `{Type}` to `{Type}` + the
   standard `lock.Entry` fields (Ref, Name, Version, Resolved pin,
   RenderedHash, Images). The engine becomes reproducible.
7. Pack records: an engine row is appended to the records write —
   `delivery: "engine"` (new marker), READY aggregated from
   `eng.Health`, CUSTOMIZED = "yes" iff `engine.values` is set, empty
   URL/DEPENDS-ON. `kubectl get packs` shows the engine.
8. `down`: zero changes — engine objects leave via the inventory cascade.
9. `diff.desiredState`: fetch+render the engine pack (warm cache)
   instead of `InstallManifests()` (diff.go:191); selfManage orphan
   stubs unchanged.
10. `cmd config render-engine`: same command (F1 golden fence intact),
    source becomes the rendered engine pack.
11. Bundles: engine ref vendored like every pack
    (`vendorPacks`/`resolveBundleRefs`); the special-cased
    `defaultEngineInstallImages` (vendor.go:199) is deleted — engine
    images derive from the pack render + `images:` list.

### 3.4 The two engine packs ($PACKS)

**`packs/cube-engine-flux`** — `chart.yaml` → `fluxcd-community/flux2`
chart, pinned; baked values enable only `sourceController` +
`kustomizeController` (parity with today's
`flux install --components=source-controller,kustomize-controller` blob).
`pack.cue` with open `#Values`. No manifests dir.

**`packs/cube-engine-argocd`** — `chart.yaml` → `argo-helm/argo-cd`
chart, pinned; baked values carry every current hand-edit as a chart
value: `configs.params."reposerver.oci.layer.media.types"` (load-bearing
for OCI pack delivery — argocd.go:14-35 documents why),
`server.insecure: true`, `imagePullPolicy: IfNotPresent` (airgap guard),
non-HA sizing. `manifests/10-repo-secret.yaml` carries today's
repo-secret.yaml (a core Secret — bootstrap-safe). `pack.cue` with open
`#Values`.

Parity risk, named honestly: both charts are community-maintained
(argo-helm is not argoproj's core `install.yaml`; flux2 is
fluxcd-community). The e2e `{flux,argocd}` engine matrix is the parity
proof; chart pins bump deliberately, like traefik's.

Re-vendoring tooling: `hack/gen-flux-manifests.sh`,
`hack/gen-argocd-manifests.sh`, `inject-argocd-cmd-params.awk` retire
from $ROOT; their pinned-version-bump duty moves to the packs' READMEs
(chart pin bumps, not manifest regeneration).

### 3.5 Engine interface slimming (the Go win)

`Install()` and `InstallManifests()` leave the `engine.Engine` interface
— `up` applies rendered objects the same way for every pack. Engines
become pure translators + operators:
`Deliver / DeliverGit / DeliverSelf / Poke / Health / Uninstall /
OrdersDeliveries`. Implementations lose `embed.FS`, `tuning` fields,
`NewTuned`, and argocd's `defaultNamespace()`/`clusterScopedKinds`
transform (the chart renders explicit namespaces). The factory takes no
config beyond `Type`.

Deleted outright: internal/engine/tune.go (+ its int64 deep-copy
helpers), `EngineTuning`/`ComponentTuning` (config/types.go:110-127),
schema.cue tuning block, both embedded manifest trees, three tuning test
suites, `install_manifests_parse` from the engine contract suite
(internal/engine/contract/contract.go:188 — it encodes the
embedded-and-self-contained assumption).

---

## 4. Airgap / offline

The engine install today works offline for free (embedded blob). After
this change it rides the same rails as every chart pack: vendored into
the bundle (`vendorPacks`), ref rewritten to the bundle-local dir
(`resolveBundleRefs`), images node-loaded from the pack's derived image
set — the path `TestVendorBundleOffline` already exercises for traefik.
The plain-`up` network posture is unchanged in kind: the *gateway*
default is already a network-requiring `oci://` ref, so "online by
default, bundles for offline" is the established contract. The argocd
`IfNotPresent` guard (internal/engine/argocd/airgap_test.go) moves to
$PACKS as an assertion on `cube-engine-argocd`'s rendered output.

---

## 5. Diagnostics

- **CUBE-3009 retires** (tuning unknown component); registry entry kept
  and marked retired — the codes surface is append-only.
- **New (config family): engine.tuning removed** — load-time rejection
  with remediation "move engine.tuning to engine.values (chart values);
  see cube-engine-<type> pack README".
- **New (config family): engine pack ref mismatch** — the CUBE-0008
  analog from §3.1.
- Engine-pack fetch/render failures reuse the existing CUBE-4xxx pack
  codes, wrapped with the `[engine-pack]` step context.
- CUBE-3010 (selfManage) unchanged.

---

## 6. Ratified-decision amendments (explicit, not silent)

- **GT1/GT15**: the tuning vocabulary stone is amended — the `tuning`
  noun retires; the engine joins the `values` vocabulary. The stone's
  *motivation* (no ad-hoc patch DSLs) survives: values are real chart
  values, not a patch language.
- **GT16**: rules 1-3 and the four-scenario matrix survive verbatim; the
  self artifact's source changes from embedded blob to rendered pack.
  The matrix's "tuning" axis becomes "values".
- **`docs/pack-contract-v1.md` §4 vocabulary triad**: `tuning → engine
  patches (spec.engine.tuning, not packs)` is replaced by `values →
  chart render (packs and the engine alike)`. Additive doc revision per
  §6 (v1.1); the pack-side contract is untouched — engine packs are
  ordinary packs.
- **CUBE-0005** (argocd pack rejected with argocd engine) survives
  unchanged: D1's dedicated packs mean the UI pack and the engine pack
  remain distinct artifacts, so the guard keeps its meaning.

---

## 7. Testing

- Delete: three tuning suites (engine/tune_test.go,
  config load_test.go:343, cmd/config_test.go:79,96), flux/argocd
  `TestInstallManifests*` embed tests, argocd airgap_test.go (moves to
  $PACKS), contract `install_manifests_parse`.
- Rewrite: e2e `TestEngineSelfManage` (phase3_test.go:939) asserts a
  values-driven replica count converges (was: tuning-driven);
  up_test.go selfManage trio feeds pack-rendered objects.
- Add: config round-trip for `engine.ref`/`engine.values` + the two new
  typed errors; `verifyEnginePackRef` units; render-fixture tests for
  both engine packs ($PACKS `TestReposPacksSatisfyContractV1` picks them
  up automatically); a bundle-vendor leg proving the engine pack rides
  `vendorPacks`.
- Parity proof: the existing e2e `{flux,argocd}` matrix
  (`CUBE_IDP_E2E_ENGINE`) runs unchanged against the chart-based
  installs.

---

## 8. Deferred (recorded, not designed here)

### 8.1 ArgoCD UI exposure

Out of scope per D5. The clean future shape *enabled by* this phase: a
route-only pack listed in `spec.packs`, delivered through the engine,
auto-ordered behind the gateway pack by the existing implicit
HTTPRoute→gateway depgraph edge (depgraph.go:66). Opt-in, zero new
machinery. Needs its own small design (route + expose + auth posture).

### 8.2 Capability inference in the depgraph

The general solution to the CRD-ordering class (gateway CRDs today;
cert-manager/external-secrets/kyverno CRs tomorrow): provides/consumes
analysis over rendered objects — packs rendering CRDs *provide* those
group/kinds; packs rendering foreign CRs *consume* them; consumer →
provider becomes an implicit dependsOn edge flowing into the existing p6
translation and diagnostics. Runtime-installed CRDs (envoy-gateway) are
covered by an additive `provides:` pack.cue field; the index-0 gateway
edge survives as backstop. Owner-scoped as the follow-up phase
(2026-07-19); deletes the two hardcoded implicit edges instead of adding
a third mechanism.

### 8.3 `spec.prerequisites` — CLI-delivered bootstrap packs

Owner-proposed 2026-07-19, shape settled, spun out as its own DRAFT spec:
`2026-07-19-cube-idp-prerequisites-packs-design.md`. Generalizes this
spec's `[engine-pack]` step into a loop over `[prerequisites…, engine
pack]` — operator-declared packs CLI-SSA'd before the engine (canonical
case: Gateway API CRDs, which would relax D5 and degrade the
registry-route CRD wait to a backstop). Hard-depends on this phase's
plumbing; carries its own due-diligence list (SSA ownership collision
with the traefik pack's vendored CRDs being the big one). Interacts with
§8.1 (may supersede the route-only-pack shape) and §8.2 (prereq-provided
GVKs must count as satisfied).

### 8.4 cube-engine artifact tag

`engineSelfTag` stays `"latest"` (up.go:1180) in this phase; switching
the self-artifact tag to the engine pack's version is a candidate
follow-up once CubeLock-CRD (p6) and this spec have both landed.

---

## 9. Open questions for owner review

1. **Published pack pins**: first release version for the two engine
   packs (`0.1.0`?) and whether they enter the existing catalog index.
2. **`existing`-cluster mode**: an engine already installed by other
   means (pre-cube argocd) — is `verifyEnginePackRef` + health preflight
   sufficient, or does first-SSA-onto-existing-install deserve a guard?
3. **Chart pull cache**: helm's default cache dir vs cube-idp's pack
   cache — acceptable to inherit helm's, or should the plan pin
   `HELM_CACHE_HOME` under the cube-idp cache root?
