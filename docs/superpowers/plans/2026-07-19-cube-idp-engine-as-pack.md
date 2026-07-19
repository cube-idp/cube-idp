# Engine-as-a-Pack Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The engine (flux/argocd) installs from a pack reference with open
chart values (replacing the closed `engine.tuning`), gets a Pack record and
a cube.lock pin, and the CLI drops ~2 MB of embedded manifests.

**Architecture:** Spec: `docs/superpowers/specs/2026-07-19-cube-idp-engine-as-pack-design.md`
(RATIFIED 2026-07-19 — read it first; decisions D1–D7 and §9 resolutions
are binding). Two new chart-based packs in $PACKS
(`cube-engine-flux`, `cube-engine-argocd`); in $ROOT the engine pack is
fetched+rendered before the engine install step, SSA'd by the CLI (GT16
rule 1 unchanged), recorded in cube.lock/Pack records, and the
`engine.Engine` interface loses `Install`/`InstallManifests`.

**Tech Stack:** Go 1.26.2, CUE (config schema), helm v4 SDK (client-side
chart render, already vendored), envtest (contract suites), kind (e2e).

## Global Constraints

- $ROOT = this repo (`github.com/cube-idp/cube-idp`, folder `neocube`).
  $PACKS = the `cube-idp-packs` checkout (sibling dir or
  `.claude/worktrees/cube-idp-packs`). Branches: `p7/engine-as-pack`
  ($ROOT), `p7/engine-packs` ($PACKS).
- CUBE codes are append-only. New: `CUBE-0012` (tuning removed),
  `CUBE-0013` (engine pack mismatch). Retired IN PLACE (comment edit, never
  deleted): `CUBE-3003`, `CUBE-3009`.
- CLI command tree is frozen (`TestCommandTreeGolden`, F1): no command may
  be added/removed/renamed. `config render-engine` keeps its name.
- `docs/pack-contract-v1.md` is additive-only (§6).
- Engine self-artifact name `cube-engine` + tag `latest` are UNCHANGED
  (spec §8.4 defers the tag change).
- Published engine pack pins: `0.1.0` (spec §9.1). Default refs:
  `oci://ghcr.io/cube-idp/packs/cube-engine-<type>:0.1.0`.
- Commit with EXPLICIT pathspecs (`git commit -- <files>`) — this repo has
  a stray-staged-files gotcha.
- Verify with real `go build ./... && go vet ./...` before every commit —
  do not trust editor/LSP diagnostics (known stale-diagnostics gotcha).
- Local e2e needs `CUBE_IDP_E2E_GATEWAY_PORT=18443` (a squatting kind
  cluster owns 8443) and `CUBE_IDP_E2E_PACKS_DIR=<$PACKS path>`.
- Until Task 15 publishes, `oci://…cube-engine-*:0.1.0` does not resolve —
  every test/e2e before that must set `spec.engine.ref` to a local
  `$PACKS/packs/cube-engine-<type>` dir.
- **KNOWN PRE-EXISTING RED (recorded p7 T3, 2026-07-19 — NOT a p7 regression):**
  when `./tests` runs with `CUBE_IDP_E2E_PACKS_DIR` pointed at the $PACKS p7
  worktree, `TestPackManifestsNoAlwaysPull` (tests/packs_airgap_test.go) FAILS on
  three packs that p7 never touched — `argo-events`, `argo-rollouts`,
  `cloudnativepg` pin `imagePullPolicy: Always` (verified present on $PACKS `main`;
  `git log main..p7/engine-packs` for those dirs is empty). It is a $PACKS-content
  matter, out of every $ROOT task's scope. Downstream $ROOT agents: your gate is
  `go build ./... && go vet ./...` CLEAN + your task's own packages green + your
  task's `./tests` subset green — treat THIS SPECIFIC pre-existing
  `TestPackManifestsNoAlwaysPull` failure on those three packs as environmental, NOT
  a blocker and NOT yours. Any NEW `./tests` failure you cause IS yours.

---

### Task 1: `packs/cube-engine-flux` pack  `[repo: $PACKS]`

**Files:**
- Create: `$PACKS/packs/cube-engine-flux/pack.cue`
- Create: `$PACKS/packs/cube-engine-flux/chart.yaml`
- Create: `$PACKS/packs/cube-engine-flux/README.md`

**Interfaces:**
- Produces: a chart pack named `cube-engine-flux` whose render is parity
  with $ROOT's embedded flux blob (source-controller + kustomize-controller
  only, namespace `flux-system`). Task 3 fences it; Task 8 consumes it via
  `spec.engine.ref`. The README documents the REPLICA KNOB (the chart
  values path that sets kustomize-controller replicas) — Task 14 reads it.

- [x] **Step 1: Discover the flux version the embedded blob pins**

Run in $ROOT:
```bash
grep -o 'ghcr.io/fluxcd/[a-z-]*:v[0-9.]*' internal/engine/flux/manifests/install.yaml | sort -u
```
Note the controller image tags (e.g. `source-controller:vX.Y.Z`). The
matching flux distribution version is what the chart pin must ship.

- [x] **Step 2: Discover the chart pin and its values keys**

```bash
helm repo add fluxcd-community https://fluxcd-community.github.io/helm-charts
helm repo update fluxcd-community
helm search repo fluxcd-community/flux2 --versions | head -20
helm show values fluxcd-community/flux2 --version <newest whose appVersion matches step 1's flux version> | grep -n -A3 "sourceController:\|kustomizeController:\|helmController:\|notificationController:\|imageAutomationController:\|imageReflectionController:"
```
Record: (a) the chart version (call it CHART_PIN), (b) the exact boolean
key that disables a controller (expected `<name>.create: false` — verify),
(c) the key that sets kustomize-controller replicas (expected under
`kustomizeController:`; if the chart exposes no replica key, pick its
`resources:` key instead and note that Task 14 will assert resources, not
replicas).

- [x] **Step 3: Write chart.yaml** (substitute CHART_PIN and the verified
disable keys from Step 2 — everything else verbatim)

```yaml
# Flux as cube-idp's engine pack (engine-as-pack spec D1/D2, 2026-07-19).
# Community chart fluxcd-community/flux2 — NOT a fluxcd/fluxcd artifact;
# parity target is the retired embedded blob (`flux install
# --export --components=source-controller,kustomize-controller`), proven by
# the $ROOT e2e engine matrix. Bump deliberately, like packs/traefik.
chart: flux2
repo: https://fluxcd-community.github.io/helm-charts
version: "CHART_PIN"
releaseName: flux
namespace: flux-system
values:
  # Blob parity: only the two controllers cube-idp uses.
  helmController: {create: false}
  notificationController: {create: false}
  imageAutomationController: {create: false}
  imageReflectionController: {create: false}
```

- [x] **Step 4: Write pack.cue**

```cue
name:        "cube-engine-flux"
version:     "0.1.0"
description: "flux GitOps engine (cube-idp engine pack)"
// D3 (engine-as-pack spec): OPEN values — the operator controls the full
// flux2 chart surface. Content validation is helm's; unknown keys are
// silently ignored (the accepted operator-in-control cost).
#Values: {...}
```

- [x] **Step 5: Sanity-render and verify parity**

```bash
helm template flux fluxcd-community/flux2 --version CHART_PIN \
  --namespace flux-system \
  --set helmController.create=false --set notificationController.create=false \
  --set imageAutomationController.create=false --set imageReflectionController.create=false \
  | grep "kind: Deployment" -A0 -B0 | wc -l
```
Expected: exactly 2 Deployments (source-controller, kustomize-controller).
Also verify the replica knob: re-run with the Step 2(c) key overridden to 2
and grep `replicas: 2`.

- [x] **Step 6: Write README.md** — cover: what this pack is (engine pack,
referenced by `spec.engine.ref`/the published default, NOT for
`spec.packs`), the chart pin + bump procedure (replaces
`hack/gen-flux-manifests.sh`), the verified replica knob path from Step
2(c) (state it explicitly — the $ROOT e2e reads this), and the D3
open-values note.

- [x] **Step 7: Commit**

```bash
cd $PACKS && git checkout -b p7/engine-packs
git add packs/cube-engine-flux && git commit -m "feat(pack): cube-engine-flux — flux engine install as a chart pack (engine-as-pack D1/D2)" -- packs/cube-engine-flux
```

---

### Task 2: `packs/cube-engine-argocd` pack  `[repo: $PACKS]`

**Files:**
- Create: `$PACKS/packs/cube-engine-argocd/pack.cue`
- Create: `$PACKS/packs/cube-engine-argocd/chart.yaml`
- Create: `$PACKS/packs/cube-engine-argocd/manifests/10-repo-secret.yaml`
- Create: `$PACKS/packs/cube-engine-argocd/README.md`

**Interfaces:**
- Produces: a chart pack named `cube-engine-argocd` carrying every baked
  hand-edit of the retired embedded install as chart values: OCI
  media-types (load-bearing for pack delivery), `server.insecure`,
  `IfNotPresent`. Consumed like Task 1's.

- [x] **Step 1: Extract the load-bearing values from the embedded blob**
(BEFORE Task 12 deletes it)

Run in $ROOT:
```bash
grep -n -B2 -A2 "reposerver.oci.layer.media.types" internal/engine/argocd/manifests/install.yaml
grep -n "namespace:" internal/engine/argocd/manifests/repo-secret.yaml
head -20 internal/engine/argocd/manifests/repo-secret.yaml
```
Record the exact `reposerver.oci.layer.media.types` VALUE string (call it
MEDIA_TYPES) and note whether repo-secret.yaml carries
`metadata.namespace: argocd`.

- [x] **Step 2: Discover the chart pin**

```bash
helm repo add argo https://argoproj.github.io/argo-helm
helm repo update argo
helm search repo argo/argo-cd --versions | head -30
```
Pick the newest chart version whose `APP VERSION` is `v3.4.5` (the version
the embedded blob vendors — parity). Call it CHART_PIN.

- [x] **Step 3: Write chart.yaml** (substitute CHART_PIN + MEDIA_TYPES)

```yaml
# Argo CD as cube-idp's engine pack (engine-as-pack spec D1/D2, 2026-07-19).
# Community chart argoproj/argo-helm (not argoproj's core install.yaml);
# parity proven by the $ROOT e2e engine matrix. Bump deliberately.
# Every baked value below carries a retired install.yaml hand-edit:
chart: argo-cd
repo: https://argoproj.github.io/argo-helm
version: "CHART_PIN"          # appVersion v3.4.5 — parity with the retired blob
releaseName: argocd
namespace: argocd
values:
  global:
    image:
      imagePullPolicy: IfNotPresent   # airgap: bundle node-loads images; Always would bypass them
  configs:
    params:
      server.insecure: true           # HTTP behind cube-idp's gateway (no self-signed redirect loop)
      # Load-bearing for OCI pack delivery: argocd's repo-server must accept
      # the flux-style artifact media type cube-idp pushes to zot.
      reposerver.oci.layer.media.types: "MEDIA_TYPES"
```

- [x] **Step 4: Copy the repo secret**

```bash
cp $ROOT/internal/engine/argocd/manifests/repo-secret.yaml $PACKS/packs/cube-engine-argocd/manifests/10-repo-secret.yaml
```
If Step 1 showed it lacks `metadata.namespace`, add `namespace: argocd`
under `metadata:` — pack manifests apply exactly as rendered (no implicit
namespace; see packs/argocd README for the precedent).

- [x] **Step 5: Write pack.cue**

```cue
name:        "cube-engine-argocd"
version:     "0.1.0"
description: "Argo CD GitOps engine (cube-idp engine pack)"
// D3 (engine-as-pack spec): OPEN values — full argo-cd chart surface.
#Values: {...}
```

- [x] **Step 6: Sanity-render**

```bash
helm template argocd argo/argo-cd --version CHART_PIN --namespace argocd \
  --set configs.params."server\.insecure"=true \
  | grep -c "imagePullPolicy: Always"
```
Expected: `0` (with the global.image value applied via a `-f` values file if
`--set` escaping fights you — mirror chart.yaml's values in a temp file).
Also `grep "kind: ConfigMap" -A20 | grep media.types` on a render WITH the
params values to confirm they land in `argocd-cmd-params-cm`.

- [x] **Step 7: Write README.md** — engine-pack role, chart pin + bump
procedure (replaces `hack/gen-argocd-manifests.sh` + the awk injector),
why each baked value exists (media-types, insecure, IfNotPresent — one
line each), open-values note, and that the UI HTTPRoute deliberately does
NOT live here (spec D5 — gateway CRDs arrive after the engine).

- [x] **Step 8: Commit**

```bash
cd $PACKS && git add packs/cube-engine-argocd && git commit -m "feat(pack): cube-engine-argocd — argocd engine install as a chart pack (engine-as-pack D1/D2)" -- packs/cube-engine-argocd
```

---

### Task 3: render fences for both engine packs  `[repo: $ROOT]`

**Files:**
- Modify: `tests/packs_render_test.go` (append; reuse its existing
  packs-checkout locator — grep the file for `CUBE_IDP_E2E_PACKS_DIR` and
  use the same helper the existing tests call)

**Interfaces:**
- Consumes: Tasks 1+2 pack dirs; `pack.Fetch(ctx, dir, cacheDir)`,
  `(*pack.Pack).RenderFor(values map[string]any, gw config.GatewaySpec)`.
- Produces: `TestCubeEngineFluxRenderParity`,
  `TestCubeEngineArgocdRenderGuards` — the airgap/media-types fence that
  replaces `internal/engine/argocd/airgap_test.go`.

- [x] **Step 1: Write the failing tests** (append; adapt the locator/fetch
boilerplate from the file's existing tests — same cache-dir and skip
conventions):

```go
// TestCubeEngineFluxRenderParity fences the flux engine pack (engine-as-pack
// D2): exactly the two controllers cube-idp uses, in flux-system.
func TestCubeEngineFluxRenderParity(t *testing.T) {
	pk := fetchPack(t, "cube-engine-flux") // reuse the file's locator+Fetch helper pattern
	r, err := pk.RenderFor(nil, config.GatewaySpec{})
	if err != nil {
		t.Fatal(err)
	}
	var deployments []string
	for _, o := range r.Objects {
		if o.GetKind() == "Deployment" {
			deployments = append(deployments, o.GetName())
			if o.GetNamespace() != "flux-system" {
				t.Fatalf("%s not in flux-system", o.GetName())
			}
		}
	}
	sort.Strings(deployments)
	want := []string{"kustomize-controller", "source-controller"}
	if !reflect.DeepEqual(deployments, want) {
		t.Fatalf("engine parity broken: got Deployments %v, want %v", deployments, want)
	}
}

// TestCubeEngineArgocdRenderGuards fences the argocd engine pack's baked
// hand-edits: no Always pulls (airgap — replaces the retired
// internal/engine/argocd/airgap_test.go) and the OCI media-types param
// (load-bearing for pack delivery).
func TestCubeEngineArgocdRenderGuards(t *testing.T) {
	pk := fetchPack(t, "cube-engine-argocd")
	r, err := pk.RenderFor(nil, config.GatewaySpec{})
	if err != nil {
		t.Fatal(err)
	}
	blob := marshalObjects(t, r.Objects) // small local helper: yaml-marshal + join
	if strings.Contains(blob, "imagePullPolicy: Always") {
		t.Fatal("argocd engine pack renders imagePullPolicy: Always — airgap bundles would be bypassed")
	}
	if !strings.Contains(blob, "reposerver.oci.layer.media.types") {
		t.Fatal("argocd engine pack lost the OCI media-types param — OCI pack delivery will fail")
	}
	if !strings.Contains(blob, "server.insecure") {
		t.Fatal("argocd engine pack lost server.insecure — argocd-server will redirect-loop behind the gateway")
	}
	hasSecret := false
	for _, o := range r.Objects {
		if o.GetKind() == "Secret" && o.GetNamespace() == "argocd" {
			hasSecret = true
		}
	}
	if !hasSecret {
		t.Fatal("argocd engine pack must carry the zot repo secret in ns argocd")
	}
}
```

- [x] **Step 2: Run to verify they fail before Tasks 1-2 land / pass after**

```bash
CUBE_IDP_E2E_PACKS_DIR=$PACKS go test ./tests/ -run 'TestCubeEngine' -v
```
Expected: PASS (Tasks 1-2 done). If it fails, fix the PACK (or the chart
pin), not the fence.

- [x] **Step 3: Commit**

```bash
git add tests/packs_render_test.go && git commit -m "test(packs): render fences for cube-engine-flux/argocd (p7 engine-as-pack)" -- tests/packs_render_test.go
```

---

### Task 4: config surface — `engine.ref`/`engine.values`, tuning removed  `[repo: $ROOT]`

**Files:**
- Modify: `internal/config/types.go:89-127` (EngineSpec; delete
  EngineTuning + ComponentTuning), `internal/config/schema.cue:26-37`,
  `internal/config/load.go` (migration guard + `normalizePackValues`),
  `internal/diag/codes.go`, `internal/diag/registry.go`
- Test: `internal/config/load_test.go`, `internal/config/types_test.go`

**Interfaces:**
- Produces: `config.EngineSpec{Type, Ref string, Values map[string]any,
  SelfManage bool}`, methods `PackName() string` (`"cube-engine-"+Type`)
  and `PackRef() string` (Ref, else published default),
  `diag.CodeEngineTuningRemoved = "CUBE-0012"`,
  `diag.CodeEnginePackMismatch = "CUBE-0013"`. Tasks 7-11 consume all of
  these. NOTE: the tree will NOT compile between this task's type change
  and its same-task fixes to `factory.go`/`flux.go`/`argocd.go` references
  — see Step 4.

- [x] **Step 1: Write the failing tests** (in `internal/config/load_test.go`;
delete `TestEngineTuningRoundTripAndValidation` at :343 in the same edit):

```go
func TestEngineRefValuesRoundTripAndDefaults(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cube.yaml")
	os.WriteFile(p, []byte(`apiVersion: cube-idp.dev/v1alpha1
kind: Cube
metadata: {name: t}
spec:
  engine:
    type: argocd
    ref: /tmp/packs/cube-engine-argocd
    values:
      controller: {replicas: 2}
  gateway: {host: cube-idp.localtest.me, port: 8443, pack: traefik}
`), 0o644)
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Spec.Engine.Ref != "/tmp/packs/cube-engine-argocd" || c.Spec.Engine.PackRef() != c.Spec.Engine.Ref {
		t.Fatalf("explicit ref must win: %+v", c.Spec.Engine)
	}
	if c.Spec.Engine.PackName() != "cube-engine-argocd" {
		t.Fatalf("PackName: %q", c.Spec.Engine.PackName())
	}
	// Values normalized like PackRef.Values: int, never CUE's int64.
	if r := c.Spec.Engine.Values["controller"].(map[string]any)["replicas"]; r != int(2) {
		t.Fatalf("engine values not normalized: %T %v", r, r)
	}
	// Defaults: no ref → the published pin per type.
	if got := (EngineSpec{Type: "flux"}).PackRef(); got != "oci://ghcr.io/cube-idp/packs/cube-engine-flux:0.1.0" {
		t.Fatalf("default flux ref: %q", got)
	}
	if got := (EngineSpec{Type: "argocd"}).PackRef(); got != "oci://ghcr.io/cube-idp/packs/cube-engine-argocd:0.1.0" {
		t.Fatalf("default argocd ref: %q", got)
	}
}

func TestEngineTuningRemovedIsCube0012(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cube.yaml")
	os.WriteFile(p, []byte(`apiVersion: cube-idp.dev/v1alpha1
kind: Cube
metadata: {name: t}
spec:
  engine:
    type: flux
    tuning: {components: {kustomize-controller: {replicas: 2}}}
  gateway: {host: cube-idp.localtest.me, port: 8443, pack: traefik}
`), 0o644)
	_, err := Load(p)
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != diag.CodeEngineTuningRemoved {
		t.Fatalf("want CUBE-0012 migration error, got %v", err)
	}
	if !strings.Contains(de.Remediation, "engine.values") {
		t.Fatalf("remediation must point at engine.values: %q", de.Remediation)
	}
}
```

- [x] **Step 2: Run to verify failure**

```bash
go test ./internal/config/ -run 'TestEngineRefValues|TestEngineTuningRemoved' -v
```
Expected: FAIL (`c.Spec.Engine.Ref undefined`, unknown code).

- [x] **Step 3: Implement types.go** — replace the `Tuning` field and DELETE
`EngineTuning` + `ComponentTuning` (types.go:110-127) entirely:

```go
// EngineSpec selects the GitOps reconciliation engine and its install pack
// (engine-as-pack spec 2026-07-19).
type EngineSpec struct {
	Type string `yaml:"type" json:"type"` // "flux" | "argocd"
	// Ref optionally overrides the engine pack source (any pack ref form:
	// local dir, oci://, git). Unset = the published default for Type
	// (defaultEngineRefs). The fetched pack's declared name must be
	// PackName() — CUBE-0013 at fetch time (pack.VerifyEnginePackRef).
	Ref string `yaml:"ref,omitempty" json:"ref,omitempty"`
	// Values holds the engine pack's chart values — the OPEN,
	// operator-in-control replacement for the retired engine.tuning (D3/D6):
	// consumed exclusively by the pack's chart.yaml render, merged over its
	// baked defaults. Same normalization + omitempty discipline as
	// PackRef.Values.
	Values map[string]any `yaml:"values,omitempty" json:"values,omitempty"`
	// SelfManage — UNCHANGED (GT16): keep the existing field + doc comment
	// verbatim, only s/rendered (tuned) install/rendered engine pack/.
	SelfManage bool `yaml:"selfManage,omitempty" json:"selfManage,omitempty"`
}

// defaultEngineRefs pins the published engine pack per engine type — what
// `up`/`diff` fetch when spec.engine.ref is unset (spec §9.1: 0.1.0).
var defaultEngineRefs = map[string]string{
	"flux":   "oci://ghcr.io/cube-idp/packs/cube-engine-flux:0.1.0",
	"argocd": "oci://ghcr.io/cube-idp/packs/cube-engine-argocd:0.1.0",
}

// PackName returns the pack name engine.type requires: cube-engine-<type>.
func (e EngineSpec) PackName() string { return "cube-engine-" + e.Type }

// PackRef resolves the engine pack source: an explicit e.Ref always wins;
// otherwise the published default for e.Type. (Unknown Type returns "" —
// unreachable past the factory's CUBE-3001.)
func (e EngineSpec) PackRef() string {
	if e.Ref != "" {
		return e.Ref
	}
	return defaultEngineRefs[e.Type]
}
```

- [x] **Step 4: Quiet the two compile breaks this causes** — `factory.go`
and both engines still reference `config.EngineTuning`. Apply the MINIMAL
bridge now (full slimming is Task 12): in
`internal/engine/factory/factory.go` change `flux.NewTuned(spec.Tuning)` →
`flux.New()` and `argocd.NewTuned(spec.Tuning)` → `argocd.New()`; in
`internal/engine/flux/flux.go` and `internal/engine/argocd/argocd.go`
delete the `tuning` struct field, the `NewTuned` constructor, and the
`engine.ApplyTuning(...)` call inside each `InstallManifests` (leave
everything else — embeds, Install — for Task 12); delete
`internal/engine/tune.go` + `internal/engine/tune_test.go` now (nothing
references ApplyTuning after this step). Then:

```bash
go build ./... 2>&1 | head -30
```
Fix any remaining `EngineTuning` reference the compiler names (expected:
`cmd/config_test.go` + `internal/up/up_test.go`/`internal/diff/diff_test.go`
tuning-flavored tests — DELETE `TestRenderEngineAppliesTuning`,
`TestRenderEngineUnknownComponentIsCube3009` (cmd/config_test.go:79,96)
and any test constructing `config.EngineTuning`, adjusting diff_test's
selfManage cases to plain `EngineSpec{Type: ..., SelfManage: true}`).

- [x] **Step 5: schema.cue** — replace the whole `engine:` block
(schema.cue:26-37):

```cue
		engine: {
			type: *"flux" | "argocd"
			// Engine pack source override (engine-as-pack spec §3.1); unset =
			// the published cube-engine-<type> default pinned in Go
			// (config.defaultEngineRefs).
			ref?: string & !=""
			// OPEN chart values (D3) — content validation is helm's, not CUE's.
			values?: {...}
			// GT16 (P8): opt-in engine self-management — unchanged.
			selfManage?: bool
		}
```

- [x] **Step 6: load.go** — (a) insert the migration guard AFTER the
existing `providerConfig` guard (the CUBE-0011 block ending ~line 85),
same probe pattern:

```go
	// Migration guard (engine-as-pack spec D6): engine.tuning was removed.
	// Probed pre-CUE like providerConfig above — the closed schema would
	// otherwise reject the key with a generic CUBE-0002 instead of the
	// migration recipe.
	var legacyTuning struct {
		Spec struct {
			Engine struct {
				Tuning map[string]any `yaml:"tuning"`
			} `yaml:"engine"`
		} `yaml:"spec"`
	}
	if err := yaml.Unmarshal(raw, &legacyTuning); err == nil && len(legacyTuning.Spec.Engine.Tuning) > 0 {
		return nil, diag.New(diag.CodeEngineTuningRemoved,
			"engine.tuning has been removed — the engine now installs from a pack whose chart values replace it",
			"move the knobs to engine.values as chart values of the cube-engine-<type> pack (see its README for the replica/resources value paths); run `cube-idp config schema` for the shape")
	}
```

(b) extend `normalizePackValues` (load.go:139):

```go
func normalizePackValues(c *Cube) {
	for i := range c.Spec.Packs {
		c.Spec.Packs[i].Values = normalizeAny(c.Spec.Packs[i].Values).(map[string]any)
	}
	c.Spec.Engine.Values = normalizeAny(c.Spec.Engine.Values).(map[string]any)
}
```

- [x] **Step 7: diag codes** — append to the 0xxx block in
`internal/diag/codes.go` (after CUBE-0011):

```go
	CodeEngineTuningRemoved Code = "CUBE-0012" // engine.tuning was removed (engine-as-pack) — use engine.values (chart values of the cube-engine-<type> pack)
	CodeEnginePackMismatch  Code = "CUBE-0013" // engine.ref points at a pack whose pack.cue name != cube-engine-<engine.type>
```
Edit the CUBE-3009 line comment in place, appending
` (RETIRED 2026-07-19 by engine-as-pack — never emitted since)`. Add both
new codes to `internal/diag/registry.go` following its existing entry
shape (one-line Summary each, mirroring the remediation texts above), and
append the same RETIRED note to its CUBE-3009 entry.

- [x] **Step 8: Run the tests**

```bash
go build ./... && go test ./internal/config/ ./internal/diag/ ./internal/engine/... ./cmd/ -count=1
```
Expected: PASS (diag registry tests enforce code/registry sync — fix any
listing it flags).

- [x] **Step 9: Commit**

```bash
git checkout -b p7/engine-as-pack
git add internal/config internal/diag internal/engine cmd && git commit -m "feat(config): engine.ref+values replace engine.tuning — CUBE-0012 migration guard, CUBE-0013 reserved (p7 engine-as-pack)" -- internal/config internal/diag internal/engine cmd
```

---

### Task 5: helm chart cache under the cube-idp cache root  `[repo: $ROOT]`

**Files:**
- Modify: `internal/pack/helm.go:100` (the `settings := cli.New()` line in
  `renderChartRef`)
- Test: `internal/pack/helm_test.go` (append)

**Interfaces:**
- Produces: unexported `helmSettings() *cli.EnvSettings` used by
  `renderChartRef`.

- [x] **Step 1: Failing test**

```go
// TestHelmSettingsPinnedUnderCacheRoot pins spec §9.3: helm's chart cache
// lives under cube-idp's own cache root (hermetic, one cache to clean) —
// EnvSettings fields, never process env.
func TestHelmSettingsPinnedUnderCacheRoot(t *testing.T) {
	dir, err := DefaultCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	s := helmSettings()
	if !strings.HasPrefix(s.RepositoryCache, dir) {
		t.Fatalf("RepositoryCache %q not under cache root %q", s.RepositoryCache, dir)
	}
	if !strings.HasPrefix(s.RepositoryConfig, dir) {
		t.Fatalf("RepositoryConfig %q not under cache root %q", s.RepositoryConfig, dir)
	}
}
```

- [x] **Step 2: Run** `go test ./internal/pack/ -run TestHelmSettings -v` —
Expected: FAIL (`helmSettings` undefined).

- [x] **Step 3: Implement** — in helm.go add, and replace `settings :=
cli.New()` in `renderChartRef` with `settings := helmSettings()`:

```go
// helmSettings pins helm's repo cache/config under the cube-idp cache root
// (spec §9.3, applies to ALL chart packs): hermetic renders, no
// interference with the operator's own helm state. Best-effort — on a
// cache-dir failure helm's defaults still work.
func helmSettings() *cli.EnvSettings {
	settings := cli.New()
	if dir, err := DefaultCacheDir(); err == nil {
		settings.RepositoryCache = filepath.Join(dir, "helm", "repository")
		settings.RepositoryConfig = filepath.Join(dir, "helm", "repositories.yaml")
	}
	return settings
}
```

- [x] **Step 4: Run** the test (PASS) plus a real chart render:
`CUBE_IDP_E2E_PACKS_DIR=$PACKS go test ./tests/ -run TestCubeEngineFluxRenderParity -v`
(exercises LocateChart through the new cache; expect PASS and
`<cache>/helm/repository/` to now exist).
  - **PLAN DEVIATION (spec §10, escape hatch):** the flux engine pack is now
    vendored-manifests (CHARTLESS) so `TestCubeEngineFluxRenderParity` does NOT
    exercise LocateChart/the helm cache. Substituted the **argocd** engine fence
    (`TestCubeEngineArgocdRenderGuards`), which still renders a real chart through
    `renderChartRef`. See T5 Ledger FINDINGS.

- [x] **Step 5: Commit**

```bash
git add internal/pack/helm.go internal/pack/helm_test.go && git commit -m "feat(pack): pin helm chart cache under the cube-idp cache root (spec §9.3)" -- internal/pack/helm.go internal/pack/helm_test.go
```

---

### Task 6: cube.lock engine entry  `[repo: $ROOT]`

**Files:**
- Modify: `internal/lock/lock.go:26-28` (EngineLock)
- Test: `internal/lock/lock_test.go` (append)

**Interfaces:**
- Produces: `lock.EngineLock{Type, Ref, Name, Version, Resolved,
  RenderedHash string; Images []string}` + method `Entry() Entry` (the
  projection bundle vendoring/resolution uses). Tasks 8+10 consume.

- [x] **Step 1: Failing test**

```go
// TestEngineLockEntryRoundTrip pins the engine-as-pack lock extension: the
// engine is a first-class reproducibility entry (spec §3.3.6), old locks
// (type-only) still read, and Entry() projects the pack fields for bundle
// vendoring.
func TestEngineLockEntryRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cube.lock")
	f := &File{APIVersion: "cube-idp.dev/v1alpha1", Kind: "CubeLock",
		Engine: EngineLock{Type: "flux", Ref: "oci://ghcr.io/cube-idp/packs/cube-engine-flux:0.1.0",
			Name: "cube-engine-flux", Version: "0.1.0", Resolved: "oci:sha256:abc",
			RenderedHash: "h1", Images: []string{"ghcr.io/fluxcd/source-controller:v1.0.0"}}}
	if err := Write(p, f); err != nil {
		t.Fatal(err)
	}
	got, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Engine, f.Engine) {
		t.Fatalf("engine lock round-trip: %+v != %+v", got.Engine, f.Engine)
	}
	e := got.Engine.Entry()
	if e.Ref != f.Engine.Ref || e.Name != "cube-engine-flux" || e.RenderedHash != "h1" || len(e.Images) != 1 {
		t.Fatalf("Entry projection: %+v", e)
	}
	// Pre-engine-as-pack lock (type only) still reads.
	os.WriteFile(p, []byte("apiVersion: cube-idp.dev/v1alpha1\nkind: CubeLock\nengine:\n  type: argocd\npacks: []\n"), 0o644)
	old, err := Read(p)
	if err != nil || old.Engine.Type != "argocd" || old.Engine.Ref != "" {
		t.Fatalf("old lock compat: %+v, %v", old, err)
	}
}
```

- [x] **Step 2: Run** `go test ./internal/lock/ -run TestEngineLockEntry -v` — FAIL.

- [x] **Step 3: Implement**

```go
// EngineLock records the GitOps engine: its type plus (engine-as-pack,
// 2026-07-19) the same reproducibility fields every pack Entry carries —
// the engine pack is pinnable and vendorable. All pack fields omitempty:
// locks written before engine-as-pack carried only type.
type EngineLock struct {
	Type         string   `yaml:"type" json:"type"`
	Ref          string   `yaml:"ref,omitempty" json:"ref,omitempty"`
	Name         string   `yaml:"name,omitempty" json:"name,omitempty"`
	Version      string   `yaml:"version,omitempty" json:"version,omitempty"`
	Resolved     string   `yaml:"resolved,omitempty" json:"resolved,omitempty"`
	RenderedHash string   `yaml:"renderedHash,omitempty" json:"renderedHash,omitempty"`
	Images       []string `yaml:"images,omitempty" json:"images,omitempty"`
}

// Entry projects the engine's pack fields as a lock.Entry so bundle
// vendoring and ref resolution treat the engine pack like every pack.
func (e EngineLock) Entry() Entry {
	return Entry{Ref: e.Ref, Name: e.Name, Version: e.Version,
		Resolved: e.Resolved, RenderedHash: e.RenderedHash, Images: e.Images}
}
```

- [x] **Step 4: Run** — PASS. **Step 5: Commit**

```bash
git add internal/lock && git commit -m "feat(lock): EngineLock carries the engine pack's reproducibility entry (p7 engine-as-pack)" -- internal/lock
```

---

### Task 7: `pack.FetchRenderEngine` + `pack.VerifyEnginePackRef`  `[repo: $ROOT]`

**Files:**
- Create: `internal/pack/enginepack.go`, `internal/pack/enginepack_test.go`

**Interfaces:**
- Consumes: Task 4's `EngineSpec.PackName()`/`Values`,
  `diag.CodeEnginePackMismatch`.
- Produces: `pack.FetchRenderEngine(ctx context.Context, spec
  config.EngineSpec, gw config.GatewaySpec, ref, cacheDir string) (*Pack,
  *Rendered, error)` and `pack.VerifyEnginePackRef(p *Pack, spec
  config.EngineSpec) error`. Tasks 8, 9, 11 consume both.

- [x] **Step 1: Failing tests**

```go
func TestVerifyEnginePackRef(t *testing.T) {
	ok := &Pack{Name: "cube-engine-flux"}
	if err := VerifyEnginePackRef(ok, config.EngineSpec{Type: "flux"}); err != nil {
		t.Fatal(err)
	}
	err := VerifyEnginePackRef(ok, config.EngineSpec{Type: "argocd", Ref: "/x"})
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != diag.CodeEnginePackMismatch {
		t.Fatalf("want CUBE-0013, got %v", err)
	}
	if !strings.Contains(de.Summary, "cube-engine-argocd") {
		t.Fatalf("summary must name the required pack: %q", de.Summary)
	}
}

// TestFetchRenderEngine drives the whole helper against an on-disk fixture
// pack (manifests-only is fine — values are nil here; chart rendering is
// fenced by tests/packs_render_test.go against the real packs).
func TestFetchRenderEngine(t *testing.T) {
	dir := t.TempDir()
	pd := filepath.Join(dir, "cube-engine-flux")
	os.MkdirAll(filepath.Join(pd, "manifests"), 0o755)
	os.WriteFile(filepath.Join(pd, "pack.cue"),
		[]byte("name: \"cube-engine-flux\"\nversion: \"0.1.0\"\n"), 0o644)
	os.WriteFile(filepath.Join(pd, "manifests", "ns.yaml"),
		[]byte("apiVersion: v1\nkind: Namespace\nmetadata:\n  name: flux-system\n"), 0o644)

	pk, r, err := FetchRenderEngine(context.Background(),
		config.EngineSpec{Type: "flux", Ref: pd}, config.GatewaySpec{}, pd, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if pk.Name != "cube-engine-flux" || len(r.Objects) != 1 || r.Objects[0].GetKind() != "Namespace" {
		t.Fatalf("unexpected render: %+v / %+v", pk, r.Objects)
	}
	// Wrong engine type against the same dir → CUBE-0013, no render.
	_, _, err = FetchRenderEngine(context.Background(),
		config.EngineSpec{Type: "argocd", Ref: pd}, config.GatewaySpec{}, pd, t.TempDir())
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != diag.CodeEnginePackMismatch {
		t.Fatalf("want CUBE-0013, got %v", err)
	}
}
```

- [x] **Step 2: Run** `go test ./internal/pack/ -run 'TestVerifyEnginePackRef|TestFetchRenderEngine' -v` — FAIL.

- [x] **Step 3: Implement `internal/pack/enginepack.go`**

```go
package pack

import (
	"context"
	"fmt"

	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/diag"
)

// FetchRenderEngine fetches the engine pack at ref and renders it with the
// engine's values (engine-as-pack spec §3.3 step 2): the returned objects
// are what `up` SSAs, what the inventory records, and (selfManage) what the
// cube-engine artifact carries. ref is passed explicitly rather than
// derived from spec so offline mode can hand in the bundle-resolved dir.
func FetchRenderEngine(ctx context.Context, spec config.EngineSpec, gw config.GatewaySpec, ref, cacheDir string) (*Pack, *Rendered, error) {
	pk, err := Fetch(ctx, ref, cacheDir)
	if err != nil {
		return nil, nil, err
	}
	if err := VerifyEnginePackRef(pk, spec); err != nil {
		return nil, nil, err
	}
	rendered, err := pk.RenderWith(spec.Values, "", gw)
	if err != nil {
		return nil, nil, err
	}
	return pk, rendered, nil
}

// VerifyEnginePackRef is the engine twin of up's F11 gateway check
// (CUBE-0013): the fetched pack's declared pack.cue name must be exactly
// cube-engine-<engine.type>, so pointing the argocd engine at the flux
// pack (or any ordinary pack) fails before any cluster mutation.
func VerifyEnginePackRef(p *Pack, spec config.EngineSpec) error {
	if p.Name == spec.PackName() {
		return nil
	}
	return diag.New(diag.CodeEnginePackMismatch,
		fmt.Sprintf("spec.engine.ref resolves to the %q pack, but engine.type %q requires pack %q", p.Name, spec.Type, spec.PackName()),
		fmt.Sprintf("point spec.engine.ref at the %s pack or remove it to use the published default", spec.PackName()))
}
```

- [x] **Step 4: Run** — PASS. **Step 5: Commit**

```bash
git add internal/pack/enginepack.go internal/pack/enginepack_test.go && git commit -m "feat(pack): FetchRenderEngine + VerifyEnginePackRef (CUBE-0013) — the engine pack seam (p7)" -- internal/pack/enginepack.go internal/pack/enginepack_test.go
```

---

### Task 8: `up.Run` installs the engine from the pack  `[repo: $ROOT]`

**Files:**
- Modify: `internal/up/up.go` — regions :236-257 (engine install), :262-284
  (cache-dir hoist), :403-408 (lock write), :507-518 (records)
- Test: `internal/up/up_test.go` (append one test; existing selfManage trio
  MUST pass unchanged)

**Interfaces:**
- Consumes: `pack.FetchRenderEngine` (Task 7), `lock.EngineLock` (Task 6).
- Produces: the `engine-pack` progress step; engine row in Pack records
  with `delivery: "engine"`; populated `EngineLock`. `deliverEngineSelf`
  and `installNeedsSSA` are byte-UNCHANGED.

- [x] **Step 1: Failing test** (append to up_test.go — the engine record
row shape, unit-level):

```go
// TestEnginePackRecordRow pins the engine's kubectl-get-packs row
// (engine-as-pack §3.3.7): delivery "engine", CUSTOMIZED tracks
// engine.values, no dependsOn.
func TestEnginePackRecordRow(t *testing.T) {
	pk := &pack.Pack{Name: "cube-engine-flux", Version: "0.1.0"}
	obj := pack.PackObject(pk, config.GatewaySpec{}, true, true, "engine", nil)
	spec := obj.Object["spec"].(map[string]any)
	if spec["delivery"] != "engine" || spec["customized"] != "yes" || spec["ready"] != true {
		t.Fatalf("engine record spec: %+v", spec)
	}
	if _, has := spec["dependsOn"]; has {
		t.Fatalf("engine record must carry no dependsOn: %+v", spec)
	}
}
```

- [x] **Step 2: Run** `go test ./internal/up/ -run TestEnginePackRecordRow -v` —
Expected: PASS already (PackObject passes non-empty delivery through — this
is a pin, not new code; if it fails, PackObject needs nothing but you
misread it: stop and re-check).

- [x] **Step 3: Rewire the engine install** — in `Run`, hoist the cache
dir: move the existing `dir, err := pack.DefaultCacheDir()` block
(currently after the TLS step, ~:270) to just BEFORE the
`pr = con.Progress("engine", ...)` line (:240). Then replace :236-241
(`installObjs, err := eng.InstallManifests()` and its error arm) with:

```go
	// Engine-as-pack (spec §3.3): the engine install is fetched and rendered
	// like any pack — the rendered objects are what SSA applies, what the
	// inventory records, and (selfManage) what the cube-engine artifact
	// carries. Offline: the ref resolves through the bundle like every pack.
	engineRef := cube.Spec.Engine.PackRef()
	if opened != nil {
		eref, err := resolveBundleRefs([]config.PackRef{{Ref: engineRef}}, opened.Lock, opened.PackDirLookup())
		if err != nil {
			return err
		}
		engineRef = eref[0].Ref
	}
	epr := con.Progress("engine-pack", "fetching "+engineRef)
	stepFetchSource(con, engineRef)
	enginePk, engineRendered, err := pack.FetchRenderEngine(ctx, cube.Spec.Engine, cube.Spec.Gateway, engineRef, dir)
	if err != nil {
		epr.Stop()
		return err
	}
	epr.Done("%s@%s rendered", engineRendered.Name, engineRendered.Version)

	pr = con.Progress("engine", fmt.Sprintf("installing %s", cube.Spec.Engine.Type))
	installObjs := engineRendered.Objects
```
Everything from `ssaEngine := installNeedsSSA(...)` down is UNCHANGED.
Update the P8 comment block above the step (:230-235):
s/embedded manifests + U3 tuning/the rendered engine pack (values applied)/.

- [x] **Step 4: Fill the lock** — replace the `lf := &lock.File{...}`
construction (:403-405) with:

```go
	engRH, err := lock.RenderedHash(engineRendered.Objects)
	if err != nil {
		return err
	}
	lf := &lock.File{APIVersion: "cube-idp.dev/v1alpha1", Kind: "CubeLock",
		Engine: lock.EngineLock{Type: cube.Spec.Engine.Type, Ref: cube.Spec.Engine.PackRef(),
			Name: engineRendered.Name, Version: engineRendered.Version, Resolved: enginePk.Pinned,
			RenderedHash: engRH,
			Images:       mergeImages(lock.ImagesFrom(engineRendered.Objects), enginePk.Images)},
		Packs: entries}
```
(Engine.Ref deliberately records the SPEC-level ref, not the bundle-local
rewrite — the lock must be reproducible outside the bundle.)

- [x] **Step 5: Append the engine record row** — in the D11 record loop
(:507-518), after the per-pack `packObjs` loop and before the Apply:

```go
	// Engine-as-pack §3.3.7: the engine's own row. READY is true by
	// construction here (waitHealthy gated above) unless selfManage, where
	// the cube-engine self-source's component health is the honest answer.
	engineReady := true
	if cube.Spec.Engine.SelfManage {
		engineReady = healthByName[engine.SelfArtifactName]
	}
	packObjs = append(packObjs, pack.PackObject(enginePk, cube.Spec.Gateway, engineReady,
		len(cube.Spec.Engine.Values) > 0, "engine", nil))
```

- [x] **Step 6: Verify nothing else regressed**

```bash
go build ./... && go test ./internal/up/ -count=1
```
Expected: PASS — in particular `TestSelfManageSSADecision`,
`TestSelfManageDeliverEngineSelf`,
`TestSelfManageDeliverEngineSelfFailureIsCube3010` unchanged (they feed
installObjs directly; the source swap is invisible to them). If a fake in
up_test.go fails to compile because it implements `InstallManifests`/
`Install`, delete just those two methods from the fake.

- [x] **Step 7: Commit**

```bash
git add internal/up && git commit -m "feat(up): engine installs from the fetched+rendered engine pack — lock entry + record row (p7 engine-as-pack)" -- internal/up
```

---

### Task 9: `diff` renders the engine pack  `[repo: $ROOT]`

**Files:**
- Modify: `internal/diff/diff.go:176-230` (desiredState) and the
  Pack-record orphanOnly region (~:293-307)
- Test: `internal/diff/diff_test.go`

**Interfaces:**
- Consumes: `pack.FetchRenderEngine`. `eng.InstallManifests()` disappears
  from this file.

- [x] **Step 1: Rewire desiredState** — hoist the `dir, err :=
pack.DefaultCacheDir()` block (currently ~:217) to before the engine
section, then replace :191-195 (`installObjs, err :=
eng.InstallManifests()` … append) with:

```go
	// Engine-as-pack: mirror up.Run — the engine install is the rendered
	// engine pack (warm cache in the common case).
	enginePk, engineRendered, err := pack.FetchRenderEngine(ctx, cube.Spec.Engine, cube.Spec.Gateway, cube.Spec.Engine.PackRef(), dir)
	if err != nil {
		return nil, nil, nil, err
	}
	desired = append(desired, engineRendered.Objects...)
```

- [x] **Step 2: Engine record stub** — next to the existing per-pack
Pack-record identityStub append (~:293-307; copy that line's exact helper
usage), add the engine row:

```go
	orphanOnly = append(orphanOnly, identityStub(
		schema.GroupVersionKind{Group: "cube-idp.dev", Version: "v1alpha1", Kind: "Pack"},
		"", enginePk.Name))
```
(Match the existing stub's GVK expression style — if the file already has
a Pack GVK variable for the per-pack stubs, reuse it instead of a literal.)

- [x] **Step 3: Fix diff tests** — diff_test.go's fake engines lose
`InstallManifests`/`Install` methods if present; tests that asserted
engine objects in desired state must now point the cube's
`Spec.Engine.Ref` at an on-disk fixture pack — reuse Task 7's fixture
shape (tempdir pack named `cube-engine-flux` with one Namespace manifest)
via a small local helper in diff_test.go:

```go
func writeEngineFixture(t *testing.T) string {
	t.Helper()
	pd := filepath.Join(t.TempDir(), "cube-engine-flux")
	os.MkdirAll(filepath.Join(pd, "manifests"), 0o755)
	os.WriteFile(filepath.Join(pd, "pack.cue"), []byte("name: \"cube-engine-flux\"\nversion: \"0.1.0\"\n"), 0o644)
	os.WriteFile(filepath.Join(pd, "manifests", "ns.yaml"), []byte("apiVersion: v1\nkind: Namespace\nmetadata:\n  name: flux-system\n"), 0o644)
	return pd
}
```

- [x] **Step 4: Run** `go build ./... && go test ./internal/diff/ -count=1` — PASS.

- [x] **Step 5: Commit**

```bash
git add internal/diff && git commit -m "feat(diff): desiredState renders the engine pack; engine Pack-record stub (p7 engine-as-pack)" -- internal/diff
```

---

### Task 10: bundle vendor + offline rails carry the engine pack  `[repo: $ROOT]`

**Files:**
- Modify: `internal/bundle/vendor.go` (delete
  `defaultEngineInstallImages`/its var wiring ~:189-209; engine images +
  vendor list from the lock)
- Test: `internal/bundle/bundle_test.go` (or vendor_test.go — wherever
  `engineInstallImages` is neutralized today; grep for it)

**Interfaces:**
- Consumes: `lock.EngineLock.Entry()` (Task 6). Up's offline resolve
  (Task 8 Step 3) already routes the engine ref through
  `resolveBundleRefs` — this task makes the bundle actually CONTAIN it.

- [x] **Step 1: Failing test** — add to the vendor test file:

```go
// TestVendorRejectsPreEnginePackLock pins the migration posture: a lock
// with no engine pack entry cannot produce a complete bundle.
func TestVendorRejectsPreEnginePackLock(t *testing.T) {
	dir := t.TempDir()
	lp := filepath.Join(dir, "cube.lock")
	os.WriteFile(lp, []byte("apiVersion: cube-idp.dev/v1alpha1\nkind: CubeLock\nengine:\n  type: flux\npacks: []\n"), 0o644)
	err := Vendor(context.Background(), lp, filepath.Join(dir, "out.tar"), "", testConsole(t)) // reuse the file's existing console/test helpers
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != diag.CodeVendorLockMissing {
		t.Fatalf("want CUBE-7001-family rejection, got %v", err)
	}
	if !strings.Contains(de.Summary, "engine") {
		t.Fatalf("summary must say the engine entry is missing: %q", de.Summary)
	}
}
```

- [x] **Step 2: Run** — FAIL (Vendor currently derives engine images from
the embed and succeeds).

- [x] **Step 3: Implement** — in `Vendor`, right after the `lf == nil`
guard:

```go
	if lf.Engine.Ref == "" {
		return diag.New(diag.CodeVendorLockMissing,
			lockPath+" predates the engine-as-pack change (no engine pack entry)",
			"re-run `cube-idp up` to regenerate cube.lock, then vendor again")
	}
```
Then: (a) DELETE `defaultEngineInstallImages` and the
`engineInstallImages` var indirection; where Vendor unioned
`engineInstallImages(lf.Engine.Type)` into the image set, use
`lf.Engine.Images` directly. (b) Where Vendor iterates `lf.Packs` to
vendor pack sources, iterate
`append([]lock.Entry{lf.Engine.Entry()}, lf.Packs...)` instead, so the
bundle contains the engine pack dir and `resolveBundleRefs` (whose lookup
is built from the same entries — extend its call-site input the same way
if it takes the lock/entries) can rewrite the engine ref at `up --bundle`
time. (c) In the bundle test file, remove the `engineInstallImages`
neutralization from TestMain and fix any test that stubbed it —
lock fixtures in those tests gain an engine entry
(`engine: {type, ref, name, version, resolved, renderedHash, images}`)
mirroring the shape from Task 6's test.

- [x] **Step 4: Run** `go build ./... && go test ./internal/bundle/ -count=1` — PASS.

- [x] **Step 5: Commit**

```bash
git add internal/bundle && git commit -m "feat(bundle): vendor the engine pack from its lock entry; drop embed-derived engine images (p7 engine-as-pack)" -- internal/bundle
```

---

### Task 11: `config render-engine` renders the pack  `[repo: $ROOT]`

**Files:**
- Modify: `cmd/config.go:67-101` (renderEngine command)
- Test: `cmd/config_test.go`; fixture `cmd/testdata/cube-engine-flux/`

**Interfaces:**
- Consumes: `pack.FetchRenderEngine`. Command NAME unchanged (golden
  fence); Short/doc text updated.

- [x] **Step 1: Failing test** (the two tuning tests were deleted in
Task 4):

```go
// TestRenderEngineRendersPack: the command prints the engine pack render —
// same objects `up` would SSA (engine-as-pack §3.3.10).
func TestRenderEngineRendersPack(t *testing.T) {
	dir := t.TempDir()
	pd := filepath.Join(dir, "cube-engine-flux")
	os.MkdirAll(filepath.Join(pd, "manifests"), 0o755)
	os.WriteFile(filepath.Join(pd, "pack.cue"), []byte("name: \"cube-engine-flux\"\nversion: \"0.1.0\"\n"), 0o644)
	os.WriteFile(filepath.Join(pd, "manifests", "ns.yaml"), []byte("apiVersion: v1\nkind: Namespace\nmetadata:\n  name: flux-system\n"), 0o644)
	cy := filepath.Join(dir, "cube.yaml")
	os.WriteFile(cy, []byte(fmt.Sprintf(`apiVersion: cube-idp.dev/v1alpha1
kind: Cube
metadata: {name: t}
spec:
  engine: {type: flux, ref: %s}
  gateway: {host: cube-idp.localtest.me, port: 8443, pack: traefik}
`, pd)), 0o644)
	out := runCommand(t, "config", "render-engine", "-f", cy) // reuse this file's existing command-runner helper (see the deleted tuning tests' pattern in git history / neighboring tests)
	if !strings.Contains(out, "kind: Namespace") || !strings.Contains(out, "flux-system") {
		t.Fatalf("render-engine must print the pack render, got:\n%s", out)
	}
}
```
(Adapt the runner-helper name and the `-f` flag spelling to what the
file's OTHER config tests use — copy their invocation verbatim.)

- [x] **Step 2: Run** `go test ./cmd/ -run TestRenderEngineRendersPack -v` — FAIL.

- [x] **Step 3: Implement** — replace the renderEngine RunE body and Short:

```go
	renderEngine := &cobra.Command{
		Use:   "render-engine",
		Short: "Print the engine install manifests that `up` would apply (rendered from the engine pack)",
		RunE: func(c *cobra.Command, _ []string) error {
			cube, err := config.Load(file)
			if err != nil {
				return err
			}
			dir, err := pack.DefaultCacheDir()
			if err != nil {
				return err
			}
			_, rendered, err := pack.FetchRenderEngine(c.Context(), cube.Spec.Engine, cube.Spec.Gateway, cube.Spec.Engine.PackRef(), dir)
			if err != nil {
				return err
			}
			for i, o := range rendered.Objects {
				b, err := yaml.Marshal(o.Object)
				if err != nil {
					return err
				}
				if i > 0 {
					fmt.Fprintln(c.OutOrStdout(), "---")
				}
				fmt.Fprint(c.OutOrStdout(), string(b))
			}
			return nil
		},
	}
```
Update the comment block above it (GT1/tuning wording → engine pack).

- [x] **Step 4: Golden fence** — run
`go test ./cmd/ -run TestCommandTreeGolden -v`. If the Short change trips
it, follow the failure message's regeneration instruction (the fence
freezes the TREE; a Short-text update is a legitimate fixture refresh —
command add/remove/rename is not).

- [x] **Step 5: Run** `go test ./cmd/ -count=1` — PASS. **Step 6: Commit**

```bash
git add cmd && git commit -m "feat(cmd): config render-engine prints the engine pack render (p7 engine-as-pack)" -- cmd
```

---

### Task 12: engine interface slimming — embeds, Install, contract  `[repo: $ROOT]`

**Files:**
- Modify: `internal/engine/engine.go` (interface),
  `internal/engine/flux/flux.go`, `internal/engine/argocd/argocd.go`,
  `internal/engine/factory/factory.go` (doc), `internal/engine/contract/contract.go`
- Create: `internal/engine/flux/testdata/crds.yaml`,
  `internal/engine/argocd/testdata/crds.yaml`
- Delete: `internal/engine/flux/manifests/`, `internal/engine/argocd/manifests/`,
  `hack/gen-flux-manifests.sh`, `hack/gen-argocd-manifests.sh`,
  `hack/inject-argocd-cmd-params.awk`, `internal/engine/argocd/airgap_test.go`
- Test: `internal/engine/flux/flux_test.go`, `internal/engine/flux/contract_test.go`,
  `internal/engine/flux/uninstall_test.go`, `internal/engine/argocd/argocd_test.go`,
  `internal/engine/argocd/contract_test.go`

**Interfaces:**
- Produces: `engine.Engine` WITHOUT `Install`/`InstallManifests` (now:
  Deliver, DeliverGit, DeliverSelf, Poke, Health, Uninstall,
  OrdersDeliveries); `contract.Impl{Name string; New func() engine.Engine;
  CRDs func() ([]byte, error)}`.

- [x] **Step 1: Extract the CRD fixtures BEFORE deleting the embeds**
(prereq: Tasks 1-3 landed — the packs already carry this content for
production; these fixtures serve only envtest):

```bash
yq eval-all 'select(.kind == "CustomResourceDefinition")' internal/engine/flux/manifests/install.yaml > internal/engine/flux/testdata/crds.yaml
yq eval-all 'select(.kind == "CustomResourceDefinition")' internal/engine/argocd/manifests/install.yaml > internal/engine/argocd/testdata/crds.yaml
grep -c "^kind: CustomResourceDefinition" internal/engine/flux/testdata/crds.yaml    # expect >= 2 (Kustomization, OCIRepository, GitRepository...)
grep -c "^kind: CustomResourceDefinition" internal/engine/argocd/testdata/crds.yaml  # expect >= 2 (Application, AppProject...)
```
(If `yq` is unavailable: `go run` a 10-line ParseMultiDoc filter — same
selection, write the multi-doc YAML with `---` separators.)

- [x] **Step 2: Contract suite rework** — in `contract.go`:
(a) `Impl` gains the CRDs source and loses the InstallManifests comment:

```go
type Impl struct {
	Name string
	New  func() engine.Engine
	// CRDs returns the engine's own CustomResourceDefinitions (testdata —
	// formerly extracted from the embedded install manifests) so envtest
	// can apply the engine's delivered objects.
	CRDs func() ([]byte, error)
}
```
(b) `installEngineCRDs(t, cfg, e engine.Engine)` becomes
`installEngineCRDs(t *testing.T, cfg *rest.Config, impl Impl)`: replace
`e.InstallManifests()` with

```go
	raw, err := impl.CRDs()
	if err != nil {
		t.Fatal(err)
	}
	objs, err := apply.ParseMultiDoc(raw)
```
(rest of the function unchanged); update its two call sites to pass `impl`.
(c) DELETE the `install_manifests_parse` subtest (:188-205) and the
`install_health_uninstall_on_cluster` subtest (:206-228) — install left
the engine seam (up SSAs the pack render). KEEP the health guarantee they
carried as a new leaner subtest:

```go
	t.Run("health_tolerates_fresh_cluster", func(t *testing.T) {
		cfg := startEnvtest(t)
		a, err := apply.New(cfg, "contract-"+impl.Name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := impl.New().Health(ctx, a); err != nil {
			t.Fatalf("Health must not error before the engine is installed: %v", err)
		}
		if err := impl.New().Uninstall(ctx, a, time.Minute); err != nil {
			t.Fatalf("Uninstall must not error on an empty cluster: %v", err)
		}
	})
```
(d) In each impl's `contract_test.go`, embed and wire the fixture:

```go
//go:embed testdata/crds.yaml
var crdsYAML []byte

func TestFluxContract(t *testing.T) { // argocd file mirrors with its name
	contract.Run(t, contract.Impl{
		Name: "flux",
		New:  func() engine.Engine { return flux.New() },
		CRDs: func() ([]byte, error) { return crdsYAML, nil },
	})
}
```
(Match each file's existing package/import shape; flux's
`uninstall_test.go` uses the same CRD bootstrap — point it at the embedded
fixture the same way.)

- [x] **Step 3: Slim the interface** — in `engine.go` delete the `Install`
and `InstallManifests` methods from the `Engine` interface (and their doc
comments); in `flux.go` delete the `//go:embed` + `installYAML`, the
package-level `InstallManifests()`, the method `InstallManifests`, and
`Install`; in `argocd.go` delete both embeds, `defaultNamespace`,
`clusterScopedKinds`, `InstallManifests`, and `Install` (KEEP `Namespace`
const — Deliver/Health/Poke use it). Delete the now-unreferenced
`CodeEngineManifestsInv` usages (the two Install error arms are gone);
edit `codes.go`'s CUBE-3003 line comment in place, appending
` (RETIRED 2026-07-19 by engine-as-pack — install left the engine seam)`,
and mirror in `registry.go`. Update `factory.go`'s doc comment (no more
tuning wording). Delete the whole `manifests/` dirs and the three `hack/`
scripts. Delete `airgap_test.go` (fence moved to Task 3) and the two embed
tests `TestInstallManifestsEmbedAndParse` (flux_test.go:53) /
`TestInstallManifestsIncludeRepoSecret` (argocd_test.go:47).

- [x] **Step 4: Sweep the fakes** — `go build ./... 2>&1` and
`go vet ./...`; every fake engine the compiler flags (up_test.go,
diff_test.go if any survived Task 9) loses its `Install`/`InstallManifests`
methods. Binary size check (the payoff):
`go build -o /tmp/cube-idp . && ls -la /tmp/cube-idp` — expect ~2 MB
smaller than before this task.

- [x] **Step 5: Full unit run**

```bash
go test ./... -count=1 2>&1 | tail -30
```
Expected: all PASS (envtest suites included).

- [x] **Step 6: Commit**

```bash
git add -A internal/engine hack && git commit -m "refactor(engine)!: drop Install/InstallManifests + embedded manifests — engines are pure translators (p7 engine-as-pack)" -- internal/engine hack internal/up internal/diff
```

---

### Task 13: docs — contract triad amendment + sweep  `[repo: $ROOT]`

**Files:**
- Modify: `docs/pack-contract-v1.md` (§4 vocabulary triad, ~:164-165),
  `README.md` + any file `grep -rln "engine.tuning\|engine\.Tuning" docs README.md` hits

**Interfaces:** none (docs only).

- [x] **Step 1:** In `docs/pack-contract-v1.md` §4 replace the triad line
`tuning → engine patches (spec.engine.tuning, not packs)` with
`values → chart render (packs and the engine alike; the engine installs
from the cube-engine-<type> pack — engine-as-pack spec 2026-07-19)` and
bump the doc's revision note (v1 → v1.1, additive per §6: pack-side
contract untouched).

- [x] **Step 2:** `grep -rn "engine.tuning\|render-engine\|selfManage" docs README.md | grep -v superpowers` —
update every stale mention (tuning → values; render-engine wording;
selfManage docs mention the artifact is now the engine pack render).

- [x] **Step 3: Commit**

```bash
git add docs README.md && git commit -m "docs: pack-contract v1.1 — values replace tuning in the vocabulary triad (p7 engine-as-pack)" -- docs README.md
```

---

### Task 14: e2e — selfManage leg on values + engine matrix  `[repo: $ROOT]`

**Files:**
- Modify: `tests/e2e/phase3_test.go` (`TestEngineSelfManage`, :939),
  `tests/e2e/e2e_test.go` (only if its cube.yaml builder needs the
  engine.ref knob)

**Interfaces:**
- Consumes: Task 1's documented replica knob (the flux pack README);
  `CUBE_IDP_E2E_PACKS_DIR`.

- [ ] **Step 1: Point e2e engines at the local packs** — find where the
e2e harness writes cube.yaml (grep `spec:` template strings in
tests/e2e/). Add `engine.ref: <packs-dir>/packs/cube-engine-<type>`
sourced from the same packs-checkout locator the harness already uses for
CUBE_IDP_E2E_PACKS_DIR — published `0.1.0` refs do not exist until
Task 15.

- [ ] **Step 2: Rewrite `TestEngineSelfManage`** — where it currently sets
`engine.tuning` (components replicas) in its cube.yaml and asserts the
tuned replica count converges: set `engine.values` with the REPLICA KNOB
from Task 1's README targeting kustomize-controller replicas: 2 (or, if
Task 1 recorded no replica knob, the resources variant it recorded — apply
whichever the README states, and assert on that field instead), e.g.

```yaml
  engine:
    type: flux
    ref: PACKS_DIR/packs/cube-engine-flux
    selfManage: true
    values:
      REPLICA_KNOB: 2   # exact path from packs/cube-engine-flux/README.md, Task 1 Step 2(c)
```
Keep the test's existing assertions structurally: new cube-engine digest
in zot on re-up, kustomize-controller Deployment converging to
`replicas: 2` (or the recorded resources field), engine healthy after.

- [ ] **Step 3: Run the selfManage leg**

```bash
CUBE_IDP_E2E_ONLINE=1 CUBE_IDP_E2E_GATEWAY_PORT=18443 CUBE_IDP_E2E_PACKS_DIR=$PACKS \
  go test ./tests/e2e/ -run TestEngineSelfManage -v -timeout 30m
```
Expected: PASS. Port 18443 (local squatter on 8443); one e2e at a time
(GT14 queue discipline).

- [ ] **Step 4: Run the engine matrix smoke**

```bash
CUBE_IDP_E2E_ONLINE=1 CUBE_IDP_E2E_GATEWAY_PORT=18443 CUBE_IDP_E2E_PACKS_DIR=$PACKS CUBE_IDP_E2E_ENGINE=argocd \
  go test ./tests/e2e/ -run TestUpStatusDown -v -timeout 30m
```
Expected: PASS — this is the argo-helm parity proof (spec §3.4 risk).
Repeat with `CUBE_IDP_E2E_ENGINE=flux`. Verify in the argocd leg's output:
`kubectl get packs` shows the `cube-engine-argocd` row with DELIVERY
`engine`.

- [ ] **Step 5: Commit**

```bash
git add tests && git commit -m "test(e2e): selfManage leg on engine.values; engine refs point at local engine packs (p7 engine-as-pack)" -- tests
```

---

### Task 15: publish the engine packs at 0.1.0  `[repo: $PACKS + owner]`

**Files:** none (tags + GitHub UI) — release procedure per
`tests/e2e/PACKS.md` + the Phase 5 P4 HANDOFF discipline.

- [ ] **Step 1:** Merge `p7/engine-packs` in $PACKS (owner review).
- [ ] **Step 2:** Tag + push — EACH TAG IN ITS OWN PUSH (>3 tags in one
push = zero workflow runs; and expect the index-rebuild race on multi-pack
waves — rerun red runs until the board settles):

```bash
git tag cube-engine-flux/v0.1.0 && git push origin cube-engine-flux/v0.1.0
# wait for its publish workflow to go green, then:
git tag cube-engine-argocd/v0.1.0 && git push origin cube-engine-argocd/v0.1.0
```
- [ ] **Step 3 (owner):** First publish creates each ghcr package PRIVATE —
flip both `cube-idp/packs/cube-engine-*` packages to PUBLIC in the GitHub
package settings. Verify attestation:
`gh attestation verify oci://ghcr.io/cube-idp/packs/cube-engine-flux:0.1.0 --owner cube-idp`.
- [ ] **Step 4:** Prove the published default end-to-end from $ROOT — a
cube.yaml with NO `engine.ref` (any `spec.engine.type`), live `up`/`down`
on port 18443. Then re-seed `packs.lock` per `tests/e2e/PACKS.md` (post-
publish ritual) and run the online digest leg:
`CUBE_IDP_E2E_ONLINE=1 CUBE_IDP_E2E_GATEWAY_PORT=18443 go test ./tests/e2e/ -run TestPublishedPacksByDigest -v -timeout 30m`.
- [ ] **Step 5:** Merge `p7/engine-as-pack` in $ROOT (owner review; PR per
repo convention).

---

## Agent Execution Protocol

- One agent, one task, then stop. Strict order T1→T15 (T3 needs T1+T2;
  everything after needs its predecessors — no parallel claims).
- Branches (created ONCE, reused across tasks): $ROOT
  `p7/engine-as-pack`, created FROM `2026-07-19-valuesref-remote-config`
  (the branch holding the RATIFIED spec + this plan); $PACKS
  `p7/engine-packs` from `main`. All code AND ledger commits land on the
  feature branch of their repo — never on main, never pushed (T15's
  owner-gated tag pushes are the only outward act in the whole plan).
- CLAIM before any code: set the task's STATUS below to
  `IN_PROGRESS(<session>, <UTC ts>)`, commit
  `docs: p7 plan — claim T<N>` with explicit pathspec. CLOSE after the
  task's gate passes: tick the task's checkboxes, fill EVERY Outcome
  field (evidence = pasted command output, not paraphrase), STATUS →
  DONE / DONE_WITH_CONCERNS / BLOCKED, commit
  `docs: p7 plan — T<N> complete`.
- Gate for every $ROOT task: `go build ./... && go vet ./... &&
  go test ./... -count=1` in the worktree — real runs, never LSP
  diagnostics. $PACKS tasks gate on their own steps' commands.

## Ledger

### T1 — cube-engine-flux pack [$PACKS]
STATUS: DONE
Outcome:
- BRANCH: `p7/engine-packs` ($PACKS worktree `.claude/worktrees/p7-engine-packs`)
- COMMITS:
  - `ca98894` feat(pack): cube-engine-flux — flux engine install as a chart pack (engine-as-pack D1/D2)
    (creates packs/cube-engine-flux/{pack.cue,chart.yaml,README.md}; 3 files, 111 insertions)
- FINDINGS:
  - CHART_PIN determined empirically, NOT from the Step-1 heuristic guess. Step 1
    showed the embedded blob pins `source-controller:v1.9.2` +
    `kustomize-controller:v1.9.2`. Rendering candidate charts and matching those
    exact controller tags identified `fluxcd-community/flux2` chart **2.19.0**
    (app v2.9.1) as the parity pin — 2.18.4 ships v1.8.5, 2.17.2 ships v1.7.x.
    Evidence:
    ```
    === chart 2.19.0 ===  ghcr.io/fluxcd/kustomize-controller:v1.9.2  ghcr.io/fluxcd/source-controller:v1.9.2
    === chart 2.18.4 ===  v1.8.5
    === chart 2.17.2 ===  v1.7.x
    ```
    (The plan's Step-2 "newest whose appVersion matches step 1's flux version"
    wording assumed appVersion == controller version; controllers are versioned
    independently of the flux distribution, so I matched on the controller image
    tags the blob actually pins. §5 escape hatch: verify against the real chart.)
  - REPLICA_KNOB: the flux2 chart models controllers as **singletons** —
    `kustomizeController:` exposes NO replica key (verified against
    `helm show values fluxcd-community/flux2 --version 2.19.0`). Per Step 2(c)'s
    stated escape hatch, the knob becomes a **resources** knob, not replicas:
    `kustomizeController.resources.requests.cpu` (default `100m`). Task 14 must
    assert on this resources field, not `replicas: 2`. Verified overridable:
    `--set kustomizeController.resources.requests.cpu=250m` → `cpu: 250m` lands
    in the rendered kustomize-controller Deployment (grep -c = 1).
  - Disable key confirmed exactly as expected: `<controller>.create: false`.
  - `.claude/worktrees/p7-engine-packs` already existed on branch
    `p7/engine-packs` (orchestrator-created); did NOT re-run `git checkout -b`
    from Step 7 — used the existing worktree/branch.
- BLOCKERS: none
- HANDOFF (for T3, T14, and downstream):
  - **CHART_PIN = `2.19.0`** (app v2.9.1; source/kustomize-controller v1.9.2).
  - **REPLICA_KNOB (resources form) = `kustomizeController.resources.requests.cpu`**
    (default `100m`; override e.g. to `250m`). No replica key exists — Task 14
    asserts resources, not replicas. Documented in the pack README's "Tuning knob"
    section.
  - Parity render (Step 5): exactly **2 Deployments** — `source-controller` +
    `kustomize-controller` — with the four unused controllers disabled:
    ```
    === Deployment count (expect 2) === 2
    (yq) kustomize-controller, source-controller
    ```
  - Baked-disable values in chart.yaml: `helmController/notificationController/
    imageAutomationController/imageReflectionController .create: false`.
  - Pack dir has NO `manifests/` (chart-only pack; cert-manager is the precedent).
    Namespace `flux-system`, releaseName `flux`. Values are OPEN (`#Values: {...}`).
  - T3's `TestCubeEngineFluxRenderParity` should pass against this pack via
    `CUBE_IDP_E2E_PACKS_DIR=$PACKS`; the render fence checks Deployments ==
    {kustomize-controller, source-controller} in ns flux-system.
  - progress.md ledger line: `.superpowers/sdd/progress.md` is on the main
    checkout, not on the p7 branch — noting here instead of appending (per §8).

**REOPENED (owner, 2026-07-19 — task T1-R; spec §10 amendment, see T3 resolution trail):**
the chart-based pack is DEFECTIVE for cube-idp delivery — the flux2-community chart
stamps `metadata.namespace` on NOTHING (0/43 objects, all versions; no alternative
chart exists; helm cannot stamp client-side). The CHART_PIN(flux)=2.19.0 and
REPLICA_KNOB HANDOFF above are **VOID**. T1-R steps (in the $PACKS p7 worktree):
1. `git rm packs/cube-engine-flux/chart.yaml` — the pack becomes manifests-based.
2. COPY (never symlink, CUBE-4001) the retired $ROOT embed:
   `cp $ROOT/internal/engine/flux/manifests/install.yaml packs/cube-engine-flux/manifests/install.yaml`
   ($ROOT p7 worktree copy; byte-identical `flux install --export
   --components=source-controller,kustomize-controller`, v1.9.2 controllers,
   self-stamped flux-system namespaces + Namespace object — verified).
3. `pack.cue`: drop `#Values: {...}` (chartless pack — GT15/CUBE-4016 makes values a
   typed error; D3 no longer applies to flux per spec §10). Keep name/version/description.
4. README rewrite: vendored-manifests engine pack; bump procedure = regenerate via
   `flux install --export --components=source-controller,kustomize-controller`
   (replaces hack/gen-flux-manifests.sh); REMOVE the "Tuning knob" section — state
   plainly: customisations are NOT possible for the flux engine pack in this phase
   (`engine.values` + flux → CUBE-4016); flux engine customization arrives later via
   the self-managed setup (GT16). Namespace-correctness note (why manifests, not chart).
5. Commit (explicit pathspec):
   `git add packs/cube-engine-flux && git commit -m "fix(pack)!: cube-engine-flux — vendored flux install --export manifests replace the chart (spec §10: flux2 chart renders no namespaces)" -- packs/cube-engine-flux`
6. Verify: T3's fence `TestCubeEngineFluxRenderParity` must PASS against the rewritten
   pack (run from the $ROOT p7 worktree with CUBE_IDP_E2E_PACKS_DIR=<$PACKS p7 worktree>/packs).
T1-R STATUS: DONE
  Outcome (T1-R):
  - COMMITS ($PACKS `p7/engine-packs`, worktree `.claude/worktrees/p7-engine-packs`):
    - `a1cf100` fix(pack)!: cube-engine-flux — vendored flux install --export manifests
      replace the chart (spec §10: flux2 chart renders no namespaces)
      (4 files: `chart.yaml` deleted; `manifests/install.yaml` added, 3583 lines;
      `pack.cue` + `README.md` rewritten). Trailer: `Co-Authored-By: Claude Fable 5`.
      (Claim commit `65832d0 docs: p7 plan — claim T1-R` landed on $ROOT p7 earlier.)
  - FINDINGS: none. All 6 T1-R steps executed verbatim. `pack.cue` keeps
    name/version/description, `#Values` removed with the §10 chartless comment.
    README covers: vendored-manifests role, `flux install --export
    --components=source-controller,kustomize-controller` bump procedure (replaces
    `hack/gen-flux-manifests.sh`), Tuning-knob section removed, customisation-not-possible
    note (`engine.values` + flux → CUBE-4016, later via GT16 self-managed setup),
    spec.engine.ref/published-default vs NOT spec.packs, and the why-manifests-not-chart
    namespace-correctness note.
  - HANDOFF:
    - Fence run (from $ROOT p7 worktree,
      `CUBE_IDP_E2E_PACKS_DIR=<$PACKS p7 worktree>/packs go test ./tests/ -run 'TestCubeEngine' -v`):
      ```
      --- PASS: TestCubeEngineFluxRenderParity (0.01s)
      --- PASS: TestCubeEngineArgocdRenderGuards (2.24s)
      PASS
      ok  	github.com/cube-idp/cube-idp/tests	3.778s
      ```
    - `chart.yaml` is GONE (`ls …/cube-engine-flux/chart.yaml` → No such file or directory).
    - `manifests/install.yaml` is a regular file (not symlink), 153058 bytes (~150KB),
      byte-identical to the $ROOT embed:
      `cmp <$ROOT embed> <$PACKS copy>` → exit 0 (no output).
      `grep -o 'ghcr.io/fluxcd/[a-z-]*:v[0-9.]*' | sort -u` →
      `ghcr.io/fluxcd/kustomize-controller:v1.9.2`, `ghcr.io/fluxcd/source-controller:v1.9.2`.
    - The T3 fence code (`tests/packs_render_test.go`) remains UNCOMMITTED in the $ROOT
      p7 worktree — untouched by T1-R; its commit is T3's close.

### T2 — cube-engine-argocd pack [$PACKS]
STATUS: DONE
Outcome:
- BRANCH: `p7/engine-packs` ($PACKS worktree `.claude/worktrees/p7-engine-packs`)
- COMMITS:
  - `edc9c48` feat(pack): cube-engine-argocd — argocd engine install as a chart pack (engine-as-pack D1/D2)
    (creates packs/cube-engine-argocd/{pack.cue,chart.yaml,README.md,manifests/10-repo-secret.yaml};
    4 files, 142 insertions)
- FINDINGS:
  - CHART_PIN = `10.1.4` discovered per Step 2's criterion ("newest chart version
    whose APP VERSION is v3.4.5"). Evidence — `helm search repo argo/argo-cd --versions`:
    ```
    argo/argo-cd  10.1.4  v3.4.5  ...   <- newest v3.4.5 chart → CHART_PIN
    argo/argo-cd  10.1.3  v3.4.5  ...
    argo/argo-cd  10.1.2  v3.4.4  ...
    ```
  - MEDIA_TYPES extracted verbatim from the retired blob (Step 1,
    `internal/engine/argocd/manifests/install.yaml:31301`):
    `application/vnd.oci.image.layer.v1.tar+gzip,application/vnd.oci.image.layer.v1.tar,application/vnd.cncf.helm.chart.content.v1.tar+gzip,application/vnd.cncf.flux.content.v1.tar+gzip`
  - Step 4 deviation (no-op, not a mismatch): the source repo-secret.yaml ALREADY
    carries `metadata.namespace: argocd` (verified `grep -n "namespace:"` → line 26),
    so the "if it lacks metadata.namespace, add it" branch of Step 4 was correctly
    skipped — copied verbatim, no edit. §6c honored: `cp` (regular file), never a
    symlink (`find … -type l` → empty).
  - Step 6 deviation (plan-sanctioned): used a `-f` values file mirroring chart.yaml's
    values rather than `--set` (the plan's Step 6 explicitly offers this "if --set
    escaping fights you"). All three guards passed — see HANDOFF evidence.
  - `.claude/worktrees/p7-engine-packs` already existed on branch `p7/engine-packs`
    (orchestrator-created); used the existing worktree/branch, did not re-run
    `git checkout -b`.
- BLOCKERS: none
- HANDOFF (for T3 and downstream):
  - **CHART_PIN = `10.1.4`** (app v3.4.5 — parity with the retired blob).
  - **MEDIA_TYPES** (baked into chart.yaml `configs.params."reposerver.oci.layer.media.types"`):
    `application/vnd.oci.image.layer.v1.tar+gzip,application/vnd.oci.image.layer.v1.tar,application/vnd.cncf.helm.chart.content.v1.tar+gzip,application/vnd.cncf.flux.content.v1.tar+gzip`
  - Baked values (chart.yaml): `global.image.imagePullPolicy: IfNotPresent` (airgap),
    `configs.params."server.insecure": true`, `configs.params."reposerver.oci.layer.media.types": <MEDIA_TYPES>`.
  - Pack carries `manifests/10-repo-secret.yaml` — a core `v1/Secret` named
    `cube-idp-zot-repo` in ns `argocd`, copied verbatim from the retired blob (already
    namespaced). This is what T3's `TestCubeEngineArgocdRenderGuards` asserts as the
    "Secret in ns argocd" leg.
  - Step 6 sanity-render evidence (`helm template argocd argo/argo-cd --version 10.1.4
    --namespace argocd -f <chart values>`):
    ```
    === (a) imagePullPolicy: Always count (expect 0) === 0
    === (b) media.types in argocd-cmd-params-cm ===
      reposerver.oci.layer.media.types: application/vnd.oci.image.layer.v1.tar+gzip,application/vnd.oci.image.layer.v1.tar,application/vnd.cncf.helm.chart.content.v1.tar+gzip,application/vnd.cncf.flux.content.v1.tar+gzip
    === (b2) server.insecure in params CM === server.insecure: "true"
    ```
  - Pack name `cube-engine-argocd`, version `0.1.0`, releaseName `argocd`, namespace
    `argocd`, values OPEN (`#Values: {...}`). No HTTPRoute/expose (spec D5).
  - T3's `TestCubeEngineArgocdRenderGuards` should pass against this pack via
    `CUBE_IDP_E2E_PACKS_DIR=$PACKS`: no `imagePullPolicy: Always`, media-types param
    present, `server.insecure` present, Secret in ns argocd present.
  - progress.md ledger line: `.superpowers/sdd/progress.md` is on the main checkout,
    not on the p7 branch — noting here instead of appending (per §8, matching T1).

### T3 — render fences [$ROOT]
STATUS: DONE (5c0a16fa-203a-4cf4-9a68-34028389d088, 2026-07-19T12:10Z — closed after T1-R made the flux fence green)
Outcome:
- BRANCH: `p7/engine-as-pack` ($ROOT worktree `.claude/worktrees/p7-engine-as-pack`)
- COMMITS:
  - `3c43850` test(packs): render fences for cube-engine-flux/argocd (p7 engine-as-pack)
    (`tests/packs_render_test.go`, +97 lines: both TestCubeEngine* tests +
    `fetchPack`/`marshalObjects` local helpers). Trailer: `Co-Authored-By: Claude Fable 5`.
  - `df6647b` docs: p7 plan — claim T3 (ledger claim only — earlier in history).
  - The fence code was WRITTEN in the earlier BLOCKED run and LEFT UNCOMMITTED (a red
    task is not committed as done); it is UNCHANGED and now green (T1-R fixed the pack
    it fences). This close commits it verbatim. The earlier ledger-only doc commits are
    already in history: `48db0b5` docs: p7 plan — T3 BLOCKED (flux pack renders no
    metadata.namespace; fix is $PACKS T1); `b52b4fe` docs: p7 plan — T3 owner resolution
    + T3a render-path namespace-stamp task; `a9754e2` docs: p7 spec §10 amendment + plan —
    T3a WITHDRAWN, T1-R vendored-manifests flux pack (owner decision).
- FINDINGS:
  - Helper substitution (plan §5 escape hatch, recorded per instruction): the plan's
    T3 template names `fetchPack(t, name)` and `marshalObjects(t, objs)` which did NOT
    exist in `tests/packs_render_test.go`. The file's real locator is `packsTree(t)`
    (returns the packs/ dir) + `pack.Fetch(ctx, filepath.Join(root,name), t.TempDir())`.
    I added two small local helpers matching the file's conventions: `fetchPack`
    (packsTree + Fetch) and `marshalObjects` (sigs.k8s.io/yaml marshal + join, the
    template's own "small local helper" note). API names VERIFIED against reality and
    used verbatim: `pk.RenderFor(nil, config.GatewaySpec{})` (internal/pack/render.go:34,
    signature matches), `r.Objects []*unstructured.Unstructured` (internal/pack/pack.go:99),
    `o.GetKind()/GetName()/GetNamespace()`. Added imports: reflect, sort,
    sigsyaml "sigs.k8s.io/yaml", internal/config. Also added `if testing.Short()` skip
    to both tests to match the file's existing network-gated `TestStarterPacksRender`
    convention (helm renders hit the network).
  - NOTE on the packs-dir knob: the plan's Step-2 command writes
    `CUBE_IDP_E2E_PACKS_DIR=$PACKS`, but the file's `packsTree` uses that env var as the
    packs/ DIRECTORY (it joins pack NAMES onto it). It must therefore point at the
    packs/ SUBDIR of the $PACKS worktree, i.e.
    `.../cube-idp-packs/.claude/worktrees/p7-engine-packs/packs` — NOT the repo root.
    (Consistent with how the existing tests in this file already read the var.)
  - RESOLVED via T1-R vendored-manifests flux pack per spec §10, T3a withdrawn: the
    original BLOCKER below (flux2 chart renders no `metadata.namespace`) was closed by
    T1-R replacing `cube-engine-flux`'s chart with the `flux install --export` vendored
    manifests (self-stamped `flux-system` namespaces). The fence itself was UNCHANGED —
    it went green the moment T1-R landed. See the OWNER RESOLUTION / FINAL OWNER
    RESOLUTION trail below and T1-R's ledger entry.
- BLOCKERS: none (resolved by T1-R — see below). The trail below preserves the original
  blocker analysis and the owner's resolution path.
  - [HISTORICAL, now resolved] `TestCubeEngineFluxRenderParity` failed because the T1 `cube-engine-flux` pack rendered
    its namespaced objects with NO `metadata.namespace` — they would land in `default`
    under cube-idp's namespace-less GitOps delivery (Flux Kustomization, no
    targetNamespace). This is the exact bug class the pre-existing `TestStarterPacksRender`
    (tests/packs_render_test.go:47-53) guards. The fence is correct; the flux PACK is
    defective. Fix belongs in $PACKS `packs/cube-engine-flux` (T1, DONE) — OUTSIDE this
    $ROOT-only T3's scope (T3 modifies only tests/packs_render_test.go).
  - Command + actual output (run in the $ROOT p7 worktree):
    ```
    $ CUBE_IDP_E2E_PACKS_DIR=<$PACKS p7 worktree>/packs \
        go test ./tests/ -run 'TestCubeEngine' -v
    === RUN   TestCubeEngineFluxRenderParity
        packs_render_test.go:244: kustomize-controller not in flux-system
    --- FAIL: TestCubeEngineFluxRenderParity (2.20s)
    === RUN   TestCubeEngineArgocdRenderGuards
    --- PASS: TestCubeEngineArgocdRenderGuards (2.06s)
    FAIL
    ```
  - Root-cause proof — raw helm render of the T1 flux pack's chart pin (2.19.0), the same
    render cube-idp's helm path produces (internal/pack/helm.go stamps NO namespace on
    objects that lack one; it only injects a standalone Namespace object + sets helm's
    release namespace). yq over every NAMESPACED kind in the render:
    ```
    $ helm template flux fluxcd-community/flux2 --version 2.19.0 --namespace flux-system \
        --set helmController.create=false --set notificationController.create=false \
        --set imageAutomationController.create=false --set imageReflectionController.create=false \
      | yq eval-all 'select(.kind=="ServiceAccount" or .kind=="Service" or .kind=="Deployment") |
          .kind+" "+.metadata.name+" ns="+(.metadata.namespace // "<NONE>")'
    Deployment kustomize-controller ns=<NONE>
    Deployment source-controller   ns=<NONE>
    Service    kustomize-controller ns=<NONE>
    Service    source-controller    ns=<NONE>
    ServiceAccount kustomize-controller ns=<NONE>
    ServiceAccount source-controller    ns=<NONE>
    ```
  - The `fluxcd-community/flux2` chart exposes NO values key to force
    `metadata.namespace` (`helm show values … | grep -iE 'namespace|nameOverride|
    namespaceOverride|createNamespace'` yields nothing relevant) — the chart relies
    entirely on the apply-time `-n` flag, which cube-idp's GitOps delivery does not supply.
  - Contrast (why this is a flux-pack defect, not a fence bug): the cert-manager chart
    (the T1-cited chart-only precedent) and the argo-helm chart BOTH stamp
    `metadata.namespace` on their objects — verified:
    `helm template cm cert-manager --repo https://charts.jetstack.io --version 1.16.3
    --namespace cert-manager | yq … Deployment` → `ns=cert-manager`. And
    `TestCubeEngineArgocdRenderGuards` PASSES (argo objects carry ns=argocd). The flux2
    community chart is the outlier.
  - Parity COUNT is otherwise correct: the render has exactly two Deployments
    (kustomize-controller + source-controller); `flux-flux-check` is a pre-install Job
    (helm.sh/hook: pre-install), not a third Deployment.
  - RESOLUTION REQUIRED (out of T3 scope): $PACKS T1 `cube-engine-flux` must make its
    rendered objects namespace-scoped to flux-system — either a chart value the flux2
    chart honors (none found; may need a kustomization.yaml `namespace:` in the pack, the
    pattern packs that need forced namespaces use), or a $ROOT render-path namespace-stamp
    (a broader change than T3). Until the flux pack renders flux-system-namespaced objects,
    the parity fence (correctly) stays red. NOT weakened to pass — per §7, a red task is
    not closed with a workaround.
- HANDOFF: both fences PASS against the T1-R/T2 engine packs. Fresh run from the $ROOT p7
  worktree (`CUBE_IDP_E2E_PACKS_DIR=<$PACKS p7 worktree>/packs go test ./tests/ -run
  'TestCubeEngine' -v -count=1`):
  ```
  === RUN   TestCubeEngineFluxRenderParity
  --- PASS: TestCubeEngineFluxRenderParity (0.01s)
  === RUN   TestCubeEngineArgocdRenderGuards
  --- PASS: TestCubeEngineArgocdRenderGuards (2.31s)
  PASS
  ok  	github.com/cube-idp/cube-idp/tests	4.295s
  ```
  `go build ./... && go vet ./...` clean. `tests/packs_render_test.go` is now the standing
  render guard for BOTH engine packs: flux parity (exactly source-controller +
  kustomize-controller in `flux-system`) and argocd's baked hand-edits (no `Always` pulls,
  OCI media-types param, `server.insecure`, zot repo Secret in ns `argocd`). CHART_PIN/
  MEDIA_TYPES were consumed from T1/T2 HANDOFF as directed; no re-discovery.
  NOTE (environmental, not T3): the full `./tests` package also runs the pre-existing
  `TestPackManifestsNoAlwaysPull` (packs_airgap_test.go — not touched by T3), which FAILs on
  OTHER packs in the $PACKS p7 worktree (argo-events, argo-rollouts, cloudnativepg pin
  `imagePullPolicy: Always`). That is a $PACKS-content matter outside T3's scope, not a
  regression introduced here (T3's only change is `tests/packs_render_test.go`).

**OWNER RESOLUTION (2026-07-19, orchestrator escalation) → new task T3a, render-path fix:**
The orchestrator investigated the fix options with the owner. Findings (all verified):
- fluxcd ships NO official install helm chart (only `flux install --export` manifests);
  the sole chart is `fluxcd-community/flux2`, and **0 of 43 rendered objects carry
  `metadata.namespace` at any version** — it is built for `helm install --namespace X`
  (apply-time namespace). So "change the chart pin" has no valid target.
- helm has **no client-only render mechanism** that stamps namespace: helm's namespace
  defaulting lives in `KubeClient.Build`, which runs against a live cluster's REST mapper
  and is skipped under `action.DryRunClient` (the mode cube-idp's render path uses,
  helm.go:104, for hermetic offline renders). Verified empirically: both
  `helm template --namespace` and `helm install --dry-run=client` emit `<NONE>`.
- Contrast confirmed: traefik + argocd charts DO stamp `metadata.namespace` on their
  namespaced objects (only cluster-scoped ClusterRole/CRD stay namespace-less, correctly),
  which is why `TestStarterPacksRender` passes for them. flux2-community is the outlier.
~~Owner decision: render-path namespace stamp (T3a)~~ — **SUPERSEDED, see below.**

**FINAL OWNER RESOLUTION (2026-07-19, second review — spec §10 amendment):** the owner
REJECTED the render-path stamp on review (silent shared-render transform; reverses the
content-self-stamps posture T12's deletion rationale relies on). Decision instead:
**`cube-engine-flux` becomes a vendored-manifests pack** — `manifests/install.yaml` is the
`flux install --export --components=source-controller,kustomize-controller` output,
initially the byte-identical retired $ROOT embed
(`internal/engine/flux/manifests/install.yaml`, v1.9.2 controllers): parity by
construction, self-stamped `flux-system` namespaces (verified: every namespaced object
carries it; Namespace object included). No `chart.yaml`. D2/D3 narrow to argocd-only
(spec §10). `engine.values` + flux → the GT15 stone fires naturally at render (typed
CUBE-4016, "values are helm-only") — this IS the "customisations are not possible for
flux (self-managed setup later)" surface; remediation wording reviewed at T7/T8.
Consequences recorded: T1 REOPENED (see its ledger entry — task T1-R); the T1
REPLICA_KNOB HANDOFF is VOID; T5 Step 4 must exercise the chart cache via
`TestCubeEngineArgocdRenderGuards` (flux is chartless now); T14's flux values-convergence
leg is redesigned at T14 (structural selfManage assertions remain); T12's embed deletion
is SAFE only after T1-R's copy (order already guarantees this). T3 fence: UNCHANGED —
goes green once T1-R lands.

### T3a — render path stamps chart namespace [$ROOT] — WITHDRAWN
STATUS: WITHDRAWN (owner, 2026-07-19 — never executed; superseded by the vendored-manifests
resolution above / spec §10. No code was written for this task.)
Outcome: BRANCH: n/a · COMMITS: none · FINDINGS: see T3 OWNER RESOLUTION trail · BLOCKERS: n/a ·
HANDOFF: n/a

### T4 — config surface (ref/values, CUBE-0012/0013) [$ROOT]
STATUS: DONE (5c0a16fa-203a-4cf4-9a68-34028389d088, 2026-07-19T12:45Z claimed → closed same session)
Outcome:
- BRANCH: `p7/engine-as-pack` ($ROOT worktree `.claude/worktrees/p7-engine-as-pack`)
- COMMITS:
  - `3052002` docs: p7 plan — claim T4 (ledger claim only).
  - `e7dea35` feat(config): engine.ref+values replace engine.tuning — CUBE-0012 migration
    guard, CUBE-0013 reserved (p7 engine-as-pack)
    (13 files, +128/-370; trailer `Co-Authored-By: Claude Fable 5`). Touches:
    internal/config/{types.go,schema.cue,load.go,load_test.go},
    internal/diag/{codes.go,registry.go},
    internal/engine/factory/factory.go, internal/engine/flux/flux.go,
    internal/engine/argocd/argocd.go, cmd/config_test.go,
    tests/e2e/phase3_test.go (compile-only, see FINDINGS); DELETES
    internal/engine/{tune.go,tune_test.go}.
- FINDINGS (deviations, all escape-hatched per §5 "verify against real code, minimal correction"):
  - **F1 (Expected-mismatch, Step 8 command):** the plan's Step 8 test command omits
    `internal/pack/` etc. and reads `go build ./... && go test ./internal/config/
    ./internal/diag/ ./internal/engine/... ./cmd/ -count=1`. Ran verbatim → all PASS
    (evidence below). No correction needed; recorded for completeness.
  - **F2 (compile break the plan's Step 4 list did NOT name — the material deviation):**
    removing `config.EngineTuning`/`ComponentTuning` broke `tests/e2e/phase3_test.go:970`
    (`TestEngineSelfManage`'s `setReplicas` constructs `config.EngineTuning`). The e2e
    package has NO build tag — it is gated at RUNTIME (`requireE2E`/`CUBE_IDP_E2E=1`
    t.Skip), so `go vet ./...` and `go test ./...` DO compile it, and the T4 gate
    ("go build/vet/test all green") cannot pass while it references a deleted type. The
    plan assigns the `TestEngineSelfManage` REDESIGN to T14 (+ spec §10: flux
    value-convergence leg reworked at T14). Minimal correction applied: replaced ONLY the
    deleted-type construction — `c.Spec.Engine.Tuning = &config.EngineTuning{...}` →
    `c.Spec.Engine.Values = map[string]any{component: {"replicas": n}}` — with a
    `// TODO(T14, engine-as-pack)` marker stating T14 owns the redesign (flux is chartless
    per §10). No test LOGIC/assertions changed; the leg stays runtime-gated and does not
    run in this gate. `tests/e2e/phase3_test.go` therefore appears in the code commit's
    pathspec (the plan's Step-9 list was `internal/config internal/diag internal/engine
    cmd` — extended by this one file so the tree compiles at the commit).
  - **F3 (new compile break from the map-typed field, mine to fix):**
    `EngineSpec` now carries `Values map[string]any`, so the struct is no longer
    comparable with `!=`. `internal/config/load_test.go`
    `TestDefaultRoundTripsThroughLoad` compared `loaded.Spec.Engine != def.Spec.Engine`
    → switched that one line to `reflect.DeepEqual(...)` (already imported + used two lines
    down for Packs). Minimal, in-scope (load_test.go is a T4 file).
  - **F4 (Step 4 helper deletion, sanctioned by "delete ... any test constructing
    config.EngineTuning"):** `cmd/config_test.go`'s `writeEngineTuningFixture` helper (used
    ONLY by the two deleted tuning tests) was removed with them; its now-unused
    `path/filepath` import was dropped. No other cmd test referenced it.
  - **F5 (import hygiene from Step 4):** flux.go lost its now-unused `internal/config` +
    `internal/engine` imports; argocd.go lost `internal/config` (kept `internal/engine` —
    still used by `Health`'s `[]engine.ComponentHealth` return). `InstallManifests` (the
    method) + `Install` + embeds + argocd's `defaultNamespace`/`clusterScopedKinds` all
    RETAINED per Step 4 ("leave everything else for Task 12").
  - `normalizePackValues` engine leg is nil-safe: a nil `map[string]any` is a typed-nil
    map, matched by `normalizeAny`'s `case map[string]any` (returns the typed-nil map),
    so `.(map[string]any)` never panics — identical to the existing per-pack path.
- BLOCKERS: none.
- GATE EVIDENCE (real commands, $ROOT p7 worktree — never LSP):
  - `go build ./... && go vet ./...` → CLEAN (no output, exit 0).
  - Package set (plan Step 8 / gate): `go test ./internal/config/ ./internal/diag/
    ./internal/engine/... ./cmd/ -count=1` →
    ```
    ok  github.com/cube-idp/cube-idp/internal/config          0.440s
    ok  github.com/cube-idp/cube-idp/internal/diag            0.647s
    ?   github.com/cube-idp/cube-idp/internal/engine          [no test files]
    ok  github.com/cube-idp/cube-idp/internal/engine/argocd   1.826s
    ?   github.com/cube-idp/cube-idp/internal/engine/contract [no test files]
    ok  github.com/cube-idp/cube-idp/internal/engine/factory  0.984s
    ok  github.com/cube-idp/cube-idp/internal/engine/flux     2.489s
    ok  github.com/cube-idp/cube-idp/cmd                      4.183s
    ```
  - New tests (targeted): `go test ./internal/config/ -run
    'TestEngineRefValues|TestEngineTuningRemoved' -v` →
    `--- PASS: TestEngineRefValuesRoundTripAndDefaults` +
    `--- PASS: TestEngineTuningRemovedIsCube0012`.
  - Broader `go test ./... -count=1` (with
    `CUBE_IDP_E2E_PACKS_DIR=<$PACKS p7 worktree>/packs` so the T3 fences + airgap test can
    locate the engine packs) → EVERYTHING green EXCEPT the documented KNOWN PRE-EXISTING
    RED `TestPackManifestsNoAlwaysPull` on `argo-events`/`argo-rollouts`/`cloudnativepg`
    (Global Constraints item — not p7, not mine); T3's `TestCubeEngineFluxRenderParity` +
    `TestCubeEngineArgocdRenderGuards` PASS; e2e legs SKIP (no kind cluster). Proof the
    T3-fence failures seen WITHOUT the env var are purely environmental (empty
    `CUBE_IDP_E2E_PACKS_DIR` → stale default checkout lacking the engine packs):
    ```
    --- PASS: TestCubeEngineFluxRenderParity (0.01s)
    --- PASS: TestCubeEngineArgocdRenderGuards (2.54s)
    ok  github.com/cube-idp/cube-idp/tests   3.633s   (run 'TestCubeEngine' only)
    ```
- HANDOFF (exact signatures T7/T8/T9/T11 consume):
  - **`config.EngineSpec`** (internal/config/types.go) final shape:
    ```go
    type EngineSpec struct {
        Type       string         `yaml:"type" json:"type"`                 // "flux" | "argocd"
        Ref        string         `yaml:"ref,omitempty" json:"ref,omitempty"`
        Values     map[string]any `yaml:"values,omitempty" json:"values,omitempty"`
        SelfManage bool           `yaml:"selfManage,omitempty" json:"selfManage,omitempty"`
    }
    ```
    `EngineTuning`/`ComponentTuning` are GONE. `Values` is normalized by Load
    (int, never CUE int64 — same as PackRef.Values); nil round-trips as an absent key.
  - **`func (e EngineSpec) PackName() string`** → `"cube-engine-" + e.Type`
    (e.g. `cube-engine-argocd`). Used by T7 `VerifyEnginePackRef` (CUBE-0013).
  - **`func (e EngineSpec) PackRef() string`** → `e.Ref` if non-empty, else
    `defaultEngineRefs[e.Type]`. Unknown Type → `""` (unreachable past factory CUBE-3001).
    Consumed by T8/T9/T11 to source the engine pack.
  - **`defaultEngineRefs` (unexported, internal/config/types.go)** — the two published pins:
    `flux → oci://ghcr.io/cube-idp/packs/cube-engine-flux:0.1.0`,
    `argocd → oci://ghcr.io/cube-idp/packs/cube-engine-argocd:0.1.0`.
  - **Diag codes** (internal/diag/codes.go + registry.go, both in sync):
    `diag.CodeEngineTuningRemoved = "CUBE-0012"` (load-time migration reject, remediation
    names `engine.values`); `diag.CodeEnginePackMismatch = "CUBE-0013"` (RESERVED here —
    declared + registered; T7 `pack.VerifyEnginePackRef` is the emitter).
    `CUBE-3009` (CodeEngineTuningUnknown) retained-in-place, comment + registry entry
    marked `(RETIRED 2026-07-19 by engine-as-pack — never emitted since)`.
  - **schema.cue** `engine:` block now: `type` (default flux|argocd), `ref?: string & !=""`,
    `values?: {...}` (OPEN, D3), `selfManage?: bool`. `tuning` removed — a present
    `engine.tuning` key is caught pre-CUE by the load.go migration guard (CUBE-0012),
    not by the schema.
  - **Engines**: `flux.New()` / `argocd.New()` take NO config now (factory calls them
    directly). `InstallManifests()` method still exists on both (T12 removes it from the
    interface); it no longer applies tuning.

### T5 — helm cache pinning [$ROOT]
STATUS: DONE (5c0a16fa-203a-4cf4-9a68-34028389d088, 2026-07-19T13:00Z claimed → closed same session)
Outcome:
- BRANCH: `p7/engine-as-pack` ($ROOT worktree `.claude/worktrees/p7-engine-as-pack`)
- COMMITS:
  - `c157194` docs: p7 plan — claim T5 (ledger claim only).
  - `d7d2ae4` feat(pack): pin helm chart cache under the cube-idp cache root (spec §9.3)
    (2 files, +32/-1; trailer `Co-Authored-By: Claude Fable 5`). Touches:
    internal/pack/helm.go (adds `helmSettings()`; `settings := cli.New()` →
    `settings := helmSettings()` in `renderChartRef`),
    internal/pack/helm_test.go (adds `TestHelmSettingsPinnedUnderCacheRoot` +
    `strings` import). go.mod UNCHANGED (no new module — `cli.EnvSettings`,
    `filepath`, `DefaultCacheDir` all already present).
- FINDINGS:
  - **Step-4 flux→argocd substitution (PLAN DEVIATION, spec §10 escape hatch —
    §5 "verify against real code, minimal correction"):** the plan's Step 4 says
    exercise LocateChart-through-the-new-cache by running
    `TestCubeEngineFluxRenderParity`. Per spec §10 the flux engine pack became a
    **vendored-manifests / CHARTLESS** pack (no `chart.yaml`), so its render path
    never touches `renderChartRef`/`LocateChart`/the helm cache — the flux fence
    would exercise nothing of T5's change. Substituted the **argocd** engine fence
    `TestCubeEngineArgocdRenderGuards`, which still renders the real argo-cd chart
    through `renderChartRef` (hence through `helmSettings()` + `LocateChart`).
    Verified: fence PASS AND the pinned `<DefaultCacheDir>/helm/repository/` dir
    was created by the render (absent before, present at 16:02 after — evidence
    below). No code change from this deviation — verification-target only.
  - `RepositoryConfig` (`<cache>/helm/repositories.yaml`) is set on the
    EnvSettings but the file is not materialized by this render: the argo-cd
    chart is located via `ChartPathOptions.RepoURL`, not a named repo entry, so
    helm writes no repositories.yaml. The plan's Step 4 only requires verifying
    `<DefaultCacheDir>/helm/repository/` exists (it does); the test asserts the
    EnvSettings *field prefixes*, which pass. Not a defect, recorded for clarity.
- BLOCKERS: none.
- GATE EVIDENCE (real commands, $ROOT p7 worktree — never LSP):
  - Step 2 (RED via real `go test`, not LSP):
    ```
    internal/pack/helm_test.go:191:7: undefined: helmSettings
    FAIL	github.com/cube-idp/cube-idp/internal/pack [build failed]
    ```
  - Step 3 → `go build ./...` exit 0; `go test ./internal/pack/ -run TestHelmSettings -v`:
    ```
    --- PASS: TestHelmSettingsPinnedUnderCacheRoot (0.00s)
    ok  github.com/cube-idp/cube-idp/internal/pack   1.558s
    ```
  - Step 4 argocd fence (substituted; `CUBE_IDP_E2E_PACKS_DIR=<$PACKS p7 worktree>/packs`):
    ```
    --- PASS: TestCubeEngineArgocdRenderGuards (1.81s)
    ok  github.com/cube-idp/cube-idp/tests   2.989s
    ```
  - Pinned cache dir created by the render (`<DefaultCacheDir>` =
    `$HOME/.cache/cube-idp/packs`): BEFORE →
    `.../helm/repository/: No such file or directory`; AFTER →
    `.../helm/repository/` exists (drwxr-xr-x, created Jul 19 16:02).
  - Gate: `go build ./...` exit 0, `go vet ./...` exit 0,
    `go test ./internal/pack/ -count=1` → `ok  …/internal/pack  4.832s`.
  - Broad `go test ./... -count=1` (with `CUBE_IDP_E2E_PACKS_DIR=<$PACKS p7
    worktree>/packs`): everything green EXCEPT the documented KNOWN PRE-EXISTING
    RED `TestPackManifestsNoAlwaysPull` on `argo-events`/`argo-rollouts`/
    `cloudnativepg` (Global Constraints item — NOT p7, NOT mine); e2e legs
    runtime-gated. Failure tally over the whole run: exactly one FAIL test
    (`TestPackManifestsNoAlwaysPull`) in exactly one FAIL package (`./tests`),
    no other failures. My fence `TestCubeEngineArgocdRenderGuards` PASS in the
    same run. (The `cert-manager` "unable to find exact version … falling back
    to closest available version … selected=v1.16.3" line is a benign helm WARN,
    not a failure.)
- HANDOFF (for T7/T8/T9 downstream):
  - **`helmSettings() *cli.EnvSettings`** (unexported, internal/pack/helm.go) now
    exists and is the ONLY constructor of EnvSettings used by `renderChartRef`.
    It pins `RepositoryCache = <DefaultCacheDir>/helm/repository` and
    `RepositoryConfig = <DefaultCacheDir>/helm/repositories.yaml`; best-effort —
    on a `DefaultCacheDir()` error it falls back to `cli.New()` defaults. Applies
    uniformly to ALL chart packs (traefik, cube-engine-argocd, cnoe RenderChart).
  - Cache dir layout under `$HOME/.cache/cube-idp/packs/` (= `DefaultCacheDir()`):
    `helm/repository/` (chart tarball cache, created on first chart render) and
    `helm/repositories.yaml` (RepositoryConfig path; only materialized if helm
    adds a named repo entry — RepoURL-located charts don't write it).
  - `cli` import in helm.go is still used (inside `helmSettings`); no import
    churn beyond `strings` added to helm_test.go.

### T6 — cube.lock engine entry [$ROOT]
STATUS: DONE (5c0a16fa-203a-4cf4-9a68-34028389d088, 2026-07-19T17:30Z claimed → closed same session)
Outcome:
- BRANCH: `p7/engine-as-pack` ($ROOT worktree `.claude/worktrees/p7-engine-as-pack`)
- COMMITS:
  - `be0434c` docs: p7 plan — claim T6 (ledger claim only).
  - `9dfb3d7` feat(lock): EngineLock carries the engine pack's reproducibility entry (p7 engine-as-pack)
    (2 files, +51/-2; trailer `Co-Authored-By: Claude Fable 5`). Touches:
    internal/lock/lock.go (EngineLock grows omitempty pack fields
    Ref/Name/Version/Resolved/RenderedHash/Images + `Entry() Entry` method),
    internal/lock/lock_test.go (adds `TestEngineLockEntryRoundTrip`). go.mod/go.sum
    UNCHANGED — no new module (all fields are string/[]string; Entry already existed).
- FINDINGS: none. The plan's Task 6 code was verified against the real
  `internal/lock/lock.go` before writing: the real `Entry` struct fields
  (Ref, Name, Version, Resolved, RenderedHash string; Images []string) and the
  `File` struct (Engine EngineLock; Packs []Entry) matched the plan's template
  exactly — Steps 1 and 3 were applied verbatim, no correction needed.
- BLOCKERS: none.
- GATE EVIDENCE (real commands, $ROOT p7 worktree — never LSP):
  - Step 2 (RED via real `go test`, not LSP):
    ```
    internal/lock/lock_test.go:101:36: unknown field Ref in struct literal of type EngineLock
    ... (Name/Version/Resolved/RenderedHash/Images unknown; Entry undefined) ...
    FAIL	github.com/cube-idp/cube-idp/internal/lock [build failed]
    ```
  - Step 4 → `go build ./...` exit 0; `go test ./internal/lock/ -run TestEngineLockEntry -v`:
    ```
    --- PASS: TestEngineLockEntryRoundTrip (0.00s)
    ok  github.com/cube-idp/cube-idp/internal/lock	0.953s
    ```
    (The single test body exercises BOTH the new-lock round-trip incl. Entry()
    projection AND the old-lock-compat leg — a `engine:\n  type: argocd` lock with
    no pack fields still Reads: `old.Engine.Type=="argocd"`, `old.Engine.Ref==""`.)
  - Gate: `go build ./...` exit 0, `go vet ./...` exit 0,
    `go test ./internal/lock/ -count=1` → `ok  …/internal/lock  0.355s`.
  - Broad `go test ./... -count=1` (with `CUBE_IDP_E2E_PACKS_DIR=<$PACKS p7
    worktree>/packs`): everything green EXCEPT the documented KNOWN PRE-EXISTING
    RED `TestPackManifestsNoAlwaysPull` — verified it fails on ONLY the three
    untouched packs `argo-events`/`argo-rollouts`/`cloudnativepg` (Global
    Constraints item — NOT p7, NOT mine, no engine pack among them). Failure tally:
    exactly one FAIL test in exactly one FAIL package (`./tests`), no other
    failures; e2e/live legs runtime-gated (skip, not fail).
- HANDOFF (for T8/T10 downstream):
  - **`lock.EngineLock`** (internal/lock/lock.go) now has, in addition to
    `Type string` (yaml `type`, NOT omitempty): `Ref`, `Name`, `Version`,
    `Resolved`, `RenderedHash` (all `string`, yaml/json `<field>,omitempty`) and
    `Images []string` (yaml/json `images,omitempty`). These mirror `lock.Entry`'s
    fields 1:1. T8 (`up.Run`) populates them when it installs the engine from the
    pack; because every pack field is omitempty, a type-only lock still Reads
    (old-lock compat verified above — `Read` returns `Ref==""`).
  - **`func (e EngineLock) Entry() lock.Entry`** (value receiver) projects the six
    pack fields into a `lock.Entry{Ref,Name,Version,Resolved,RenderedHash,Images}`
    (Type is engine-only, not part of Entry). T10 (bundle vendor + offline rails)
    calls `f.Engine.Entry()` to treat the engine pack like every other pack for
    vendoring / ref resolution. Signature: `Entry() Entry`, no args, no error.
  - `File.Engine` remains `EngineLock` (yaml `engine`); `File.Packs` remains
    `[]Entry` (yaml `packs`). Read/Write are unchanged — the new fields ride the
    existing sigs.k8s.io/yaml (JSON-via-YAML, sorted keys) marshalling for free.

### T7 — pack.FetchRenderEngine seam [$ROOT]
STATUS: DONE (5c0a16fa-203a-4cf4-9a68-34028389d088, 2026-07-19T19:40Z claimed → closed same session)
Outcome:
- BRANCH: `p7/engine-as-pack` ($ROOT worktree `.claude/worktrees/p7-engine-as-pack`)
- COMMITS:
  - `94476ef` docs: p7 plan — claim T7 (ledger claim only).
  - `b6ed0b8` feat(pack): FetchRenderEngine + VerifyEnginePackRef (CUBE-0013) — the
    engine pack seam (p7)
    (2 files, +99/-0; trailer `Co-Authored-By: Claude Fable 5`). Creates
    internal/pack/enginepack.go (FetchRenderEngine + VerifyEnginePackRef) and
    internal/pack/enginepack_test.go (TestVerifyEnginePackRef +
    TestFetchRenderEngine). go.mod/go.sum UNCHANGED — no new module (uses existing
    context, fmt, config, diag; Fetch/RenderWith/DefaultCacheDir already in pkg).
- FINDINGS: The plan's Task 7 code + fixture were verified against the REAL
  signatures before writing — `pack.Fetch(ctx, ref, cacheDir) (*Pack, error)`
  (source.go:33), `(*Pack).RenderWith(values, extraManifests, gw) (*Rendered, error)`
  (render.go:129), `Rendered{Name,Version,Objects}` (pack.go:99), `Pack.Name`
  (pack.go:48), `EngineSpec.PackName()` (types.go:122), `diag.CodeEnginePackMismatch =
  "CUBE-0013"` (codes.go:18, registered registry.go:37), `diag.New(code,summary,rem)
  *Error` (diag.go:23). All matched the plan's template EXACTLY — Steps 1 and 3 applied
  verbatim, no correction needed. One environmental note (NOT mine, NOT a blocker): the
  broader `go test ./... -count=1` shows, beyond the plan's documented pre-existing
  `TestPackManifestsNoAlwaysPull` red, TWO Task-3 render fences failing —
  `TestCubeEngineFluxRenderParity` + `TestCubeEngineArgocdRenderGuards`
  (tests/packs_render_test.go) — with `CUBE-4001: pack path .../cube-engine-{flux,argocd}
  is not a directory`. ROOT CAUSE: the default `CUBE_IDP_E2E_PACKS_DIR` resolves to the
  `.claude/worktrees/cube-idp-packs` worktree, which is checked out to `main` and lacks the
  T1/T2 engine packs (those live on the `$PACKS` `p7/engine-packs` branch). PROVEN not
  mine: both failures reproduce with my two T7 files stashed out (`mv`-tested pre-commit).
  A $PACKS-checkout matter, out of every $ROOT task's scope — same class as the plan's
  documented `TestPackManifestsNoAlwaysPull`.
- BLOCKERS: none.
- GATE EVIDENCE (real commands, $ROOT p7 worktree — never LSP):
  - Step 2 (RED via real `go test`, not LSP):
    `go test ./internal/pack/ -run 'TestVerifyEnginePackRef|TestFetchRenderEngine' -v`
    → `undefined: VerifyEnginePackRef` / `undefined: FetchRenderEngine`, `FAIL [build failed]`.
  - Step 4 (GREEN): same command → `--- PASS: TestVerifyEnginePackRef` +
    `--- PASS: TestFetchRenderEngine`, `ok  github.com/cube-idp/cube-idp/internal/pack`.
  - GATE: `go build ./... && go vet ./... && go test ./internal/pack/ -count=1` →
    all clean, `ok  github.com/cube-idp/cube-idp/internal/pack 4.441s`.
- HANDOFF (exact signatures T8/T9/T11 consume — both in internal/pack/enginepack.go):
  - `pack.FetchRenderEngine(ctx context.Context, spec config.EngineSpec, gw
    config.GatewaySpec, ref, cacheDir string) (*pack.Pack, *pack.Rendered, error)`
    — fetches `ref` (explicit, NOT `spec.PackRef()`: callers pass the bundle-resolved
    dir in offline mode), verifies pack name via VerifyEnginePackRef (CUBE-0013), then
    `pk.RenderWith(spec.Values, "", gw)`. Returns the fetched `*Pack` and its
    `*Rendered` (Name/Version/Objects). T8 `up.Run` uses `engineRendered.Objects` as
    installObjs + Name/Version for the lock/records; T9 `diff.desiredState` the same;
    T11 `config render-engine` for the printed render.
  - `pack.VerifyEnginePackRef(p *pack.Pack, spec config.EngineSpec) error` — nil iff
    `p.Name == spec.PackName()` (`"cube-engine-"+Type`), else `diag.New(CUBE-0013, …)`
    whose Summary names both the resolved pack and the required `cube-engine-<type>`.
  - BY-DESIGN (spec §10): FetchRenderEngine calls `RenderWith(spec.Values, "", gw)`;
    for the CHARTLESS flux engine pack, NON-EMPTY `engine.values` returns typed
    **CUBE-4016** (GT15 "values are helm-only", render.go:130) — the intended
    "flux customisation not supported this phase" surface, emitted THROUGH RenderWith.
    T7's fixture uses nil values so it does not trip this; T8 surfaces it wrapped in the
    `[engine-pack]` step context. NOT special-cased in FetchRenderEngine — correct as-is.

### T8 — up.Run engine-pack install [$ROOT]
STATUS: DONE (5c0a16fa-203a-4cf4-9a68-34028389d088, 2026-07-19T13:23Z claimed → closed same session)
Outcome:
- BRANCH: `p7/engine-as-pack` ($ROOT worktree `.claude/worktrees/p7-engine-as-pack`)
- COMMITS:
  - `531e3fb` docs: p7 plan — claim T8
  - `fa29230` feat(up): engine installs from the fetched+rendered engine pack — lock entry + record row (p7 engine-as-pack)
    (internal/up/up.go + internal/up/up_test.go; 2 files, 59 insertions, 12 deletions)
  - (this entry) docs: p7 plan — T8 complete
- FINDINGS (line-drift corrections + real-name substitutions):
  - LINE DRIFT — every plan-cited region had drifted (T4 already re-wired engine
    plumbing). Grepped the anchors the plan describes, not the numbers, and edited
    those. Actual post-T7 locations: engine install / P8 comment `:226-262`
    (plan said :236-257 + P8 :230-235); cache-dir block was at `:273-276` (plan
    said ~:270); offline `resolveBundleRefs` pack call at `:289`; `lf := &lock.File{`
    at `:418` (plan said :403-405); D11 record loop `packObjs := make(...)` at
    `:530`, `for i, pk := range packs` at `:531` (plan said :507-518).
  - REAL-NAME CONFIRMATIONS (all matched the plan verbatim — no substitution
    needed): `pack.Pack.Pinned` (the resolved pin field — plan's `enginePk.Pinned`
    is correct, NOT Resolved/Digest); `pack.Pack.Images`; `lock.RenderedHash(objs)
    (string,error)` @internal/lock/images.go:54; `lock.ImagesFrom(objs) []string`
    @images.go:15; `mergeImages(rendered, declared []string) []string` @up.go:762;
    `engine.SelfArtifactName` = "cube-engine" @internal/engine/engine.go:31;
    `pack.PackObject(p,gw,ready,customized bool,delivery string,dependsOn []string)`
    @internal/pack/expose.go:97; `resolveBundleRefs(refs, lk *lock.File, lookup
    func(string)(string,bool))` @internal/up/bundle.go:25; `stepFetchSource(con
    *ui.Console, ref string)` @up.go:752; `pack.FetchRenderEngine(ctx,spec,gw,ref,
    cacheDir)` @internal/pack/enginepack.go:16.
  - FAKE ENGINE METHODS KEPT (deviation from Step 6's conditional note): the plan's
    Step 6 says "if a fake fails to compile because it implements InstallManifests/
    Install, delete just those two methods." At T8 time the `engine.Engine`
    interface STILL declares Install + InstallManifests (their removal is T12), so
    the up_test.go fakes (`stubUnhealthyEngine`, `fakeHealthEngine`) MUST retain
    those two methods to satisfy the interface — deleting them now would BREAK
    compilation. Left both fakes byte-unchanged; no fake failed to compile. The
    conditional never triggered. (T12 will delete them then.)
  - `installObjs` is now `installObjs := engineRendered.Objects` (was
    `installObjs, err := eng.InstallManifests()`); `eng.InstallManifests()` call
    removed from up.go. `eng` is still used (installNeedsSSA, deliverDeps, Health).
  - No new go.mod module. `engine`, `pack`, `lock`, `config` were all already
    imported in up.go — no import edits.
- BLOCKERS: none
- HANDOFF (for T9/T10 to mirror):
  - selfManage trio (TestSelfManageSSADecision, TestSelfManageDeliverEngineSelf,
    TestSelfManageDeliverEngineSelfFailureIsCube3010) PASS byte-UNCHANGED — the
    install-source swap is invisible to them (they feed installObjs directly).
    Evidence pasted below.
  - ENGINE-PACK PROGRESS STEP NAME: `engine-pack` (distinct from the existing
    `engine` step which still names the install). Emitted between `packs-crd` and
    `engine`: `con.Progress("engine-pack", "fetching "+engineRef)` +
    `stepFetchSource(con, engineRef)` → `epr.Done("%s@%s rendered", Name, Version)`.
  - OFFLINE ENGINE REF: resolved through `resolveBundleRefs([]config.PackRef{{Ref:
    engineRef}}, opened.Lock, opened.PackDirLookup())` when `opened != nil`, BEFORE
    the pack loop's own resolveBundleRefs — T10 (bundle) must make the bundle
    actually CONTAIN the engine dir so this lookup resolves.
  - LOCK SHAPE T10 mirrors: `lock.EngineLock{Type, Ref: spec.PackRef(), Name:
    rendered.Name, Version: rendered.Version, Resolved: enginePk.Pinned,
    RenderedHash: lock.RenderedHash(rendered.Objects), Images:
    mergeImages(lock.ImagesFrom(rendered.Objects), enginePk.Images)}`. Ref records
    the SPEC-level ref (`cube.Spec.Engine.PackRef()`), NOT the bundle-local rewrite
    — reproducible outside the bundle. `.Entry()` (T6) projects these for vendoring.
  - RECORD-ROW SHAPE T9 mirrors: `pack.PackObject(enginePk, gw, engineReady,
    len(spec.Values)>0, "engine", nil)` appended AFTER the per-pack loop, BEFORE the
    Apply. `engineReady := true`, overridden to `healthByName[engine.SelfArtifactName]`
    only when `spec.SelfManage`. delivery "engine", no dependsOn (nil last arg).
  - GATE EVIDENCE (worktree, CUBE_IDP_E2E_PACKS_DIR=$PACKS/.claude/worktrees/
    p7-engine-packs/packs):
    - `go build ./...` → exit 0 (clean).
    - `go vet ./...` → exit 0 (clean).
    - `go test ./internal/up/ -count=1` → `ok  ...internal/up  1.913s`.
    - selfManage trio + record row `-v`:
      ```
      --- PASS: TestSelfManageSSADecision (0.00s)
      --- PASS: TestSelfManageDeliverEngineSelf (0.00s)
      --- PASS: TestSelfManageDeliverEngineSelfFailureIsCube3010 (0.00s)
      --- PASS: TestEnginePackRecordRow (0.00s)
      ```
    - `go test ./... -count=1` → every package `ok` EXCEPT the KNOWN PRE-EXISTING
      `FAIL github.com/cube-idp/cube-idp/tests` (TestPackManifestsNoAlwaysPull on
      argo-events/argo-rollouts/cloudnativepg — the three non-p7 packs the Global
      Constraints name as environmental; verified the failing files are those three
      packs only, none touched by T8). All packages T8 could affect green: cmd,
      internal/{up,lock,pack,diff,bundle,config,diag,engine/argocd,engine/flux,
      engine/factory,upgrade}, tests/e2e.

### T9 — diff renders engine pack [$ROOT]
STATUS: DONE
Outcome:
- BRANCH: `p7/engine-as-pack` ($ROOT worktree `.claude/worktrees/p7-engine-as-pack`)
- COMMITS:
  - `10fe5a7` docs: p7 plan — claim T9
  - `ec75c26` feat(diff): desiredState renders the engine pack; engine Pack-record
    stub (p7 engine-as-pack) — trailer `Co-Authored-By: Claude Fable 5`;
    2 files changed, 54 insertions(+), 12 deletions(-)
    (`internal/diff/diff.go`, `internal/diff/diff_test.go`)
- FINDINGS (line-drift corrections + real-name substitutions):
  - **Line drift.** The plan's cited regions had all shifted. Real anchors used
    instead: `eng.InstallManifests()` was at diff.go:191 (plan :191-195 — matched);
    the `pack.DefaultCacheDir()` block was at diff.go:215 (plan said ~:217 and to
    hoist it to *before* the engine section — in reality it sat AFTER the
    selfManage block, so the hoist moved it from :215 up to replace the
    InstallManifests slot at :191, then the old :215 copy was deleted as a
    duplicate `:=` redeclaration); the per-pack Pack-record stub was at diff.go:295
    (plan ~:293-307).
  - **Real-name substitution — Pack GVK.** The plan Step 2 showed a GVK *literal*
    (`schema.GroupVersionKind{Group: "cube-idp.dev", ...}`) but told me to reuse a
    Pack GVK var if the file had one. It does: `packGVK` (diff.go var block, was
    :313). Used `identityStub(packGVK, "", enginePk.Name)` — exact mirror of the
    per-pack stub, not the literal.
  - **desiredState signature/returns confirmed as plan expected.** Real signature
    `desiredState(ctx context.Context, cube *config.Cube, eng engine.Engine)
    (desired, orphanOnly []*unstructured.Unstructured, entries []lock.Entry, err
    error)` — a 4-value return; error arms return `nil, nil, nil, err` (plan's
    "4-tuple (nil,nil,nil,err)" — matched). Append var is `desired` (matched).
    `eng` parameter STAYS (still used by DeliverSelf/DeliverGit/Deliver; interface
    slimming is T12).
  - **Ref arg.** Mirrored T8's FetchRenderEngine shape but with diff's ref: diff
    has no bundle-resolution path, so the ref arg is `cube.Spec.Engine.PackRef()`
    directly (exactly as plan Step 1 code shows), vs up.Run's bundle-resolved
    `engineRef`.
  - **diff_test.go — all four desiredState tests needed the fixture, not just the
    engine-object one.** Because desiredState now fetches the engine pack via
    `PackRef()`, and the unpublished `oci://…cube-engine-flux:0.1.0` default does
    not resolve until T15, EVERY test constructing `EngineSpec{Type:"flux"}` (incl.
    `TestDesiredStateRepoDeliveredPack`, `TestDesiredStateSelfManagedEngine`,
    `TestDesiredStateFailsOnDepCycle`) got `Ref: writeEngineFixture(t)`. The
    dep-cycle test still reaches ResolveOrder because the fixture fetch (before the
    pack loop) succeeds, then CUBE-4019 surfaces as before.
  - **`writeEngineFixture` added** verbatim from the plan (tempdir pack
    `cube-engine-flux` + one `flux-system` Namespace manifest); imports `os` +
    `path/filepath` added.
  - **`TestDesiredStateMatchesUpAppliedSet` want-set reworked.** Its old want-set
    used `eng.InstallManifests()` (fakeEngine's `engine-controller` Deployment);
    that no longer feeds desiredState. Replaced with a real
    `pack.FetchRenderEngine(...)` of the same fixture (the Namespace) AND added the
    engine's own D11 Pack record row (`pack.PackObject(enginePk, gw, false,
    len(Engine.Values)>0, "engine", nil)`) so `wantApplied` covers the new engine
    identityStub desiredState emits in orphanOnly. Size-parity assertion passes.
  - **fakeEngine methods left intact.** `Install`/`InstallManifests` were NOT
    removed — the `engine.Engine` interface still declares them until T12, so the
    compiler does not flag them. Its doc comment "InstallManifests and Deliver are
    the only methods desiredState calls" is now stale but left untouched (T12
    rewrites this fake) to avoid scope creep.
- BLOCKERS: none
- HANDOFF (for T10+ and downstream):
  - `internal/diff/diff.go` desiredState now: (1) hoists `dir, err :=
    pack.DefaultCacheDir()` to before the engine section; (2) fetches+renders the
    engine pack via `pack.FetchRenderEngine(ctx, cube.Spec.Engine,
    cube.Spec.Gateway, cube.Spec.Engine.PackRef(), dir)` → `desired = append(...,
    engineRendered.Objects...)`; (3) appends `identityStub(packGVK, "",
    enginePk.Name)` to orphanOnly (engine's D11 Pack record). `eng.InstallManifests`
    no longer appears in diff.go.
  - GATE evidence (real runs, $ROOT p7 worktree):
    ```
    go build ./...   → BUILD_EXIT=0
    go vet ./...     → VET_EXIT=0
    go test ./internal/diff/ -count=1 -v:
      --- PASS: TestDesiredStateMatchesUpAppliedSet (0.01s)
      --- PASS: TestDesiredStateRepoDeliveredPack (0.00s)
      --- PASS: TestDesiredStateSelfManagedEngine (0.00s)
      --- PASS: TestDesiredStateFailsOnDepCycle (2.10s)
      ok  github.com/cube-idp/cube-idp/internal/diff  3.308s
    ```
  - Broader `go test ./... -count=1`
    (CUBE_IDP_E2E_PACKS_DIR=<$PACKS p7 worktree>/packs): all packages `ok`
    (incl. `internal/diff`, `internal/up`, `cmd`, `tests/e2e`) EXCEPT the single
    KNOWN PRE-EXISTING RED:
    ```
    --- FAIL: TestPackManifestsNoAlwaysPull (0.02s)
        argo-events/.../20-install.yaml:431   imagePullPolicy: Always
        argo-events/.../30-webhook.yaml:135   imagePullPolicy: Always
        argo-rollouts/.../20-install.yaml:18786  imagePullPolicy: Always
        cloudnativepg/.../10-cnpg.yaml:20606  imagePullPolicy: Always
    FAIL  github.com/cube-idp/cube-idp/tests
    ```
    Those three packs are the exact ones the Global Constraints (plan §"KNOWN
    PRE-EXISTING RED", 2026-07-19) flag as $PACKS-content, out of every $ROOT
    task's scope — NOT a T9 regression. `TestCubeEngineFluxRenderParity` +
    `TestCubeEngineArgocdRenderGuards` (the fences T9's engine render rides on)
    both PASS.

### T10 — bundle vendor + offline rails [$ROOT]
STATUS: DONE (5c0a16fa-203a-4cf4-9a68-34028389d088, 2026-07-19T21:05Z claimed → closed same session)
Outcome:
- BRANCH: `p7/engine-as-pack` ($ROOT worktree `.claude/worktrees/p7-engine-as-pack`)
- COMMITS:
  - `c4aa614` docs: p7 plan — claim T10 (ledger claim only).
  - `53d4bad` feat(bundle): vendor the engine pack from its lock entry; drop
    embed-derived engine images (p7 engine-as-pack) — trailer
    `Co-Authored-By: Claude Fable 5`. 3 files, +112/-70:
    `internal/bundle/vendor.go`, `internal/bundle/bundle_test.go`,
    `internal/bundle/vendor_pipeline_test.go`. go.mod/go.sum UNCHANGED — no new
    module (in fact DROPS the internal `config` + `engine/factory` imports from
    vendor.go, now unused after the embed-derivation deletion).
  - (this entry) docs: p7 plan — T10 complete.
- FINDINGS (line-drift corrections + real-name substitutions):
  - REAL TEST HELPER (plan Step 1 substitution). The plan's failing test used
    `Vendor(ctx, lp, out, "", testConsole(t))`, but `internal/bundle` has NO
    `testConsole` helper. The real sanctioned test-construction path is
    `vendorForTest(t, lockPath, outPath, platform)` (bundle_test.go:35), which
    wraps `Vendor` in `ui.RunPipeline` with ModePlain. Used it verbatim — the
    plan's own parenthetical "reuse the file's existing console/test helpers"
    authorises this. Test added to `internal/bundle/bundle_test.go` (the file
    that neutralized `engineInstallImages`), asserting `diag.CodeVendorLockMissing`
    (= CUBE-7001, confirmed codes.go:138 — the plan's name is correct).
  - UNION POINT IS `vendorImages`, NOT `Vendor` (plan's :189-209/:199 line hints
    were stale). The real shape: `engineInstallImages` was a package var
    (`= defaultEngineInstallImages`, vendor.go:190) unioned inside `vendorImages`
    via `engImgs, err := engineInstallImages(lf.Engine.Type)` (vendor.go:237).
    Deleted `defaultEngineInstallImages` + the `engineInstallImages` var
    (kept `registryInstallImages` — NOT in T10's scope); replaced the union with
    a direct `for _, img := range lf.Engine.Images` loop. Removed the now-unused
    `config` + `enginefactory` imports.
  - VENDOR-SOURCES ITERATION IS `vendorPacks`, NOT `Vendor` (plan said "where
    Vendor iterates lf.Packs"). `vendorPacks` (vendor.go:141) iterates `lf.Packs`;
    prepended the engine entry exactly as the plan directs:
    `entries := append([]lock.Entry{lf.Engine.Entry()}, lf.Packs...)` and iterate
    `entries`. This stages the engine pack under `stage/packs/<engine.Name>`
    (e.g. `packs/cube-engine-flux`).
  - `resolveBundleRefs` NEEDED NO CHANGE (plan's "extend its call-site input the
    same way if it takes the lock/entries" — verified it does NOT need it).
    `resolveBundleRefs` lives on the UP side (internal/up/bundle.go:25), consumes
    `bundle.Opened.PackDirLookup()`, and resolves the engine ref via
    `bundlePackName` → `refBaseName("oci://…/cube-engine-flux:0.1.0")` =
    `cube-engine-flux`, then `lookup("cube-engine-flux")`. `PackDirLookup`
    (internal/bundle/bundle.go:158) keys on `packs/<name>/pack.cue` — which now
    exists because vendorPacks staged it under `entry.Name`. So the offline
    rewrite resolves with zero signature changes on either side; T10 only had to
    make the bundle CONTAIN the dir (T8 already wired up's engine-ref resolve).
  - TEST-FILE FIXTURES (plan Step 3(c), extended reach). Removed the
    `engineInstallImages` neutralization from bundle_test.go TestMain (kept the
    `registryInstallImages` one — still a live seam). Added shared helper
    `writeEngineLockEntry(t, host, images)` that pushes a minimal
    `cube-engine-flux` pack to the fixture's in-process registry and returns a
    fully-pinned `lock.EngineLock` mirroring T6's shape (Type + Ref/Name/Version/
    Resolved/RenderedHash/Images). All three `lock.File{...}` fixtures
    (`writeLockFixture`, `writeLockFixtureWithImage`,
    `TestVendorImagesIncludesEngineAndRegistry`) now carry a fetchable engine
    entry instead of the bare `EngineLock{Type:"flux"}` — required because
    vendorPacks now fetches the engine pack too and Vendor rejects `engine.ref==""`.
    `TestVendorImagesIncludesEngineAndRegistry` (which stubbed `engineInstallImages`)
    now sets the engine's images via `writeEngineLockEntry(t, host, []string{engImgRef})`.
  - PIPELINE GOLDEN TESTS (plan named bundle_test.go; the fixture reach also hits
    `vendor_pipeline_test.go` — recorded as a FINDING). Because the engine pack is
    vendored FIRST, the plain-output line counts grew by the engine pack's
    start+done pair: `TestVendorPlainByteStable` 3→5 lines (leading line is now
    `pack cube-engine-flux`, demo pack line still asserted via Contains);
    `TestVendorImagePlainByteStable` 5→7 lines. Updated both counts + comments.
    `TestVendorJSONStreamEmitsExpectedEventTypes` uses substring Contains + only
    first/last-line ordering (no exact count) — its `pack demo` assertions still
    hold, no change needed.
  - No new go.mod module. `go build`/`go vet` clean via REAL runs (LSP squiggles
    during mid-edit were the documented stale-diagnostics gotcha; ignored).
- BLOCKERS: none.
- GATE EVIDENCE (real commands, $ROOT p7 worktree — never LSP):
  - Step 2 (RED via real `go test`): `go test ./internal/bundle/ -run
    TestVendorRejectsPreEnginePackLock -v` →
    ```
    bundle_test.go:223: want CUBE-7001-family rejection, got <nil>
    --- FAIL: TestVendorRejectsPreEnginePackLock (0.00s)
    ```
    (Vendor derived engine images from the embed — with TestMain's neutralized
    seam it built a complete bundle and returned nil.)
  - Step 4 (GREEN): `go build ./...` exit 0; `go test ./internal/bundle/ -count=1`
    → `ok  github.com/cube-idp/cube-idp/internal/bundle`. Key tests `-v`:
    ```
    --- PASS: TestVendorThenOpenRoundTrip (0.02s)          (now vendors the engine pack too)
    --- PASS: TestVendorRejectsPreEnginePackLock (0.00s)
    --- PASS: TestVendorBundlesImages (0.09s)
    --- PASS: TestVendorImagesIncludesEngineAndRegistry (0.12s)
    --- PASS: TestVendorPlainByteStable (0.01s)            (5-line golden)
    --- PASS: TestVendorImagePlainByteStable (0.06s)       (7-line golden)
    --- PASS: TestVendorJSONStreamEmitsExpectedEventTypes (0.06s)
    ```
  - GATE: `go build ./...` exit 0, `go vet ./...` exit 0,
    `go test ./internal/bundle/ -count=1` → `ok`.
  - Broad `go test ./... -count=1`
    (CUBE_IDP_E2E_PACKS_DIR=<$PACKS p7 worktree>/packs): EVERY package `ok`
    (incl. `internal/bundle`, `internal/up`, `internal/diff`, `cmd`, `tests/e2e`)
    EXCEPT the single KNOWN PRE-EXISTING RED:
    ```
    --- FAIL: TestPackManifestsNoAlwaysPull (0.02s)
        argo-events/.../20-install.yaml:431   imagePullPolicy: Always
        argo-events/.../30-webhook.yaml:135   imagePullPolicy: Always
        argo-rollouts/.../20-install.yaml:18786  imagePullPolicy: Always
        cloudnativepg/.../10-cnpg.yaml:20606  imagePullPolicy: Always
    FAIL  github.com/cube-idp/cube-idp/tests
    ```
    Verified it is the SOLE failing test in `./tests` (`--- FAIL` count = 1) and
    fires only on the three non-p7 packs the Global Constraints name as
    environmental — NOT a T10 regression. The engine render fences
    `TestCubeEngineFluxRenderParity` + `TestCubeEngineArgocdRenderGuards` PASS.
- HANDOFF (for T11+ and downstream):
  - `internal/bundle/vendor.go`: `Vendor` now rejects a lock with
    `lf.Engine.Ref == ""` up front (CUBE-7001, summary names "engine pack
    entry" — migration posture: re-run `up` to regenerate cube.lock).
    `vendorPacks` vendors `append([]lock.Entry{lf.Engine.Entry()}, lf.Packs...)`
    so the bundle carries `packs/<engine.Name>`. `vendorImages` unions
    `lf.Engine.Images` directly (engine/factory embed derivation deleted).
    The engine pack rides the SAME offline rails as every chart pack — up's
    `resolveBundleRefs` (unchanged) rewrites the engine ref to the bundle-local
    dir via `PackDirLookup(refBaseName(engineRef))`. No signature changes.
  - CAVEAT (not a defect, out of T10 scope): `Opened.Verify` (bundle.go:196)
    iterates `o.Lock.Packs` for its pack-hash floor — the engine pack is in
    `PackHashes` (vendorPacks hashes every staged entry incl. the engine) but is
    NOT re-checked against the lock by Verify, because the engine is `Lock.Engine`,
    not a `Lock.Packs` member. This matches the pre-T10 posture (engine was never
    a Verify-anchored pack) and the plan does not touch Verify.
  - Test seam now: `internal/bundle` fixtures MUST carry a fetchable engine entry
    (helper `writeEngineLockEntry`); the `engineInstallImages` var seam is GONE
    (only `registryInstallImages` remains). Any future bundle test that builds a
    bundle must include an engine entry or hit the CUBE-7001 reject guard.

### T11 — config render-engine [$ROOT]
STATUS: DONE
Outcome:
- BRANCH: `p7/engine-as-pack` ($ROOT worktree `.claude/worktrees/p7-engine-as-pack`).
  Executed INLINE by the orchestrator after two dispatched subagents returned with
  0 tool uses (a transient non-start — each emitted only a skills system-reminder and
  did no work; state was left clean/UNCLAIMED, verified before re-executing).
- COMMITS:
  - `85df323` docs: p7 plan — claim T11
  - `78f4508` feat(cmd): config render-engine prints the engine pack render (p7 engine-as-pack)
    (cmd/config.go RunE + Short rewritten; cmd/config_test.go +TestRenderEngineRendersPack;
    cmd/testdata/clitree.golden refreshed — 3 files, +62/-12)
  - `<this>` docs: p7 plan — T11 complete
- FINDINGS:
  - Line drift (plan cited cmd/config.go:67-101): the real renderEngine command sat at
    :73-101; anchored on `render-engine`/`renderEngine` and edited there.
  - Test-runner helper: the plan's template used `runCommand(t, ...)` + `-f`, which does
    NOT exist in cmd/config_test.go. The file's real convention is
    `root := NewRootCmd(); root.SetOut/SetErr(&bytes.Buffer{}); root.SetArgs([]string{...});
    root.Execute()`. Used that verbatim (matching TestRenderCluster* neighbors).
  - TEST STRENGTHENED (recorded deviation): the plan's fixture Namespace name `flux-system`
    also appears in the retired embedded flux blob, so the test PASSED even against the OLD
    InstallManifests() source — not a discriminating RED. Changed the fixture Namespace to
    a distinctive `enginepack-fixture-ns` (absent from the embed) so the test genuinely
    fails on the old path and passes only when render-engine renders the pack at
    spec.engine.ref. Verified RED→GREEN: RED printed the embed's kustomize-controller
    stream (no fixture ns); GREEN prints `kind: Namespace` / `enginepack-fixture-ns`.
  - Import swap: `enginefactory` (used only by the old RunE) → `internal/pack`. `context`
    not needed (c.Context() supplies it). No new go.mod module.
  - Golden fence (F1): the Short-text change tripped TestCommandTreeGolden; regenerated via
    `go test ./cmd/ -run TestCommandTreeGolden -update` (the fence's own documented refresh
    for an edited Short). The diff is EXACTLY ONE LINE — the render-engine Short — the
    command TREE, all other commands, and all flags are byte-identical (F1 tree freeze
    honored; only the description refreshed). Diff:
    ```
    -cube-idp config render-engine | Print the tuned engine install manifests that `up` would apply (GT1) |
    +cube-idp config render-engine | Print the engine install manifests that `up` would apply (rendered from the engine pack) |
    ```
- BLOCKERS: none.
- HANDOFF: `config render-engine` now prints `pack.FetchRenderEngine(...).Objects` (same
  objects `up` SSAs), source = the engine pack at `spec.engine.ref` / the published
  `cube-engine-<type>` default, `spec.engine.values` applied. Command name/Use/tree
  UNCHANGED (F1 intact). Gate: `go build ./...` + `go vet ./...` clean; `go test ./cmd/
  -count=1` → ok (TestRenderEngineRendersPack PASS, TestCommandTreeGolden PASS).

### T12 — engine interface slimming [$ROOT]
STATUS: DONE
Outcome:
- BRANCH: `p7/engine-as-pack` ($ROOT worktree `.claude/worktrees/p7-engine-as-pack`).
  Executed INLINE by the orchestrator (a third dispatched subagent also returned with
  0 tool uses — same transient non-start pattern as the two T11 attempts; state was
  UNCLAIMED/clean, verified before executing).
- COMMITS:
  - `<claim>` docs: p7 plan — claim T12
  - `277d39a` refactor(engine)!: drop Install/InstallManifests + embedded manifests —
    engines are pure translators (p7 engine-as-pack) — 21 files: interface slimmed;
    flux.go/argocd.go embeds+Install+InstallManifests+defaultNamespace/clusterScopedKinds
    deleted; contract.go reworked; both manifests/ dirs + 3 hack/ scripts + airgap_test.go
    deleted; testdata/crds.yaml fixtures added; CUBE-3003 retired-in-place.
  - `<this>` docs: p7 plan — T12 complete
- FINDINGS:
  - Step 1 CRD counts: flux **7** CRDs (buckets, externalartifacts, gitrepositories,
    helmcharts, helmrepositories, kustomizations, ocirepositories), argocd **3**
    (applications, applicationsets, appprojects) — both ≥2. Fixtures verified valid
    multi-doc YAML via yq.
  - Step 4 fake sweep: NO fakes needed sweeping. `go vet ./...` compiles all tests and
    passed clean — the up_test.go / diff_test.go fake engines did NOT implement
    Install/InstallManifests (T8/T9 already handled the up/diff seam; nothing to delete).
    The plan's conditional "if a fake fails to compile" did not trigger. Consequently the
    commit pathspec is `internal/engine internal/diag hack` (NO internal/up / internal/diff
    changes — the plan's Step 6 listed them defensively; none were needed).
  - git detected argocd's `manifests/install.yaml → testdata/crds.yaml` as a RENAME (R)
    because the extracted CRD subset is >50% similar to the source; flux's is an ADD (A).
    Both are the correct CRD-extraction content — cosmetic git classification only.
  - factory.go doc comment already read "pure translator/operator" (T4 updated it) — no
    edit needed there beyond confirming.
  - envtest legs (contract/poke/uninstall) SKIP: KUBEBUILDER_ASSETS is unset in this
    environment, so the on-cluster subtests skip (documented; not a failure). The NON-envtest
    engine tests (Deliver shapes, factory, flux/argocd unit) all PASS, and the CRD fixtures
    parse cleanly — the health_tolerates_fresh_cluster subtest that replaced
    install_health_uninstall_on_cluster compiles and is gated on envtest like its predecessor.
- BLOCKERS: none.
- HANDOFF (binary size delta):
  - **BINARY SIZE PAYOFF: 2,065,680 bytes = 1.96 MB smaller** (dev build:
    171,846,562 → 169,780,882 bytes). Matches the plan's ~2MB target. Evidence:
    `go build -o /tmp/cube-idp-{pre,post}-t12 .` then `wc -c`.
  - `engine.Engine` interface surviving method set (verified): **Deliver, DeliverGit,
    DeliverSelf, Poke, Health, Uninstall, OrdersDeliveries** — Install + InstallManifests
    removed (spec §3.5). Engines are pure translators + operators.
  - `contract.Impl` now `{Name string; New func() engine.Engine; CRDs func() ([]byte, error)}`
    (T13 does not touch this; it's the final contract shape). CUBE-3003 retired-in-place
    (code kept, comment appended — codes.go + registry.go).
  - Gate: `go build ./...` + `go vet ./...` clean; `go test ./internal/engine/... ./internal/diag/
    ./internal/up/ ./internal/diff/ -count=1` all `ok`. Full `go test ./...` green except the
    KNOWN PRE-EXISTING TestPackManifestsNoAlwaysPull red (argo-events/argo-rollouts/cloudnativepg,
    not p7) and envtest SKIPs; `tests/e2e` builds `ok`.

### T13 — docs / contract v1.1 [$ROOT]
STATUS: DONE
Outcome:
- BRANCH: `p7/engine-as-pack` ($ROOT worktree). Executed INLINE by the orchestrator
  (continuing inline after the T11/T12 subagent non-start pattern).
- COMMITS:
  - `<claim>` docs: p7 plan — claim T13
  - `178bb4f` docs: pack-contract v1.1 — values replace tuning in the vocabulary triad
    (p7 engine-as-pack) — docs/pack-contract-v1.md + README.md
  - `<this>` docs: p7 plan — T13 complete
- FINDINGS:
  - Step 1: replaced the §4 triad line (`tuning → engine patches` → `values → chart
    render (packs and the engine alike; … cube-engine-<type> pack)`) and added a v1.1
    additive revision note (next to the "changes additively (§6)" line + inline in the
    triad). `<type>` backtick-wrapped to avoid the markdown inline-HTML lint.
  - Step 2 sweep: replaced README's two `spec.engine.tuning.components.<name>.{replicas,
    resources}` config-table rows with `spec.engine.ref` (CUBE-0013 mismatch note) +
    `spec.engine.values` (D3 open values; **argocd-only per spec §10** — flux is chartless
    so engine.values+flux is CUBE-4016; flux customization deferred to selfManage). Updated
    the selfManage row (rendered "tuned install manifests" → "rendered engine pack (values
    applied)") and the render-engine prose ("`spec.engine.tuning` already patched in" →
    "rendered from the cube-engine-<type> pack with spec.engine.values applied").
  - Remaining `engine.tuning` mentions in docs/README are INTENTIONAL retirement references
    (the v1.1 note "`spec.engine.tuning` is gone" + the README "the retired `engine.tuning`")
    — describing removal, not documenting a live feature. Verified via the sweep grep.
- BLOCKERS: none.
- HANDOFF: docs-only, no code/gate. pack-contract now v1.1 (additive; pack-side contract
  untouched — engine packs are ordinary packs). Only T14 (e2e) + T15 (publish, owner-gated)
  remain.

### T14 — e2e selfManage on values + matrix [$ROOT]
STATUS: UNCLAIMED
Outcome: BRANCH · COMMITS · FINDINGS · BLOCKERS · HANDOFF (leg durations, engine-record row evidence):

### T15 — publish 0.1.0 [$PACKS + owner, OWNER-GATED]
STATUS: UNCLAIMED
Outcome: BRANCH · COMMITS · FINDINGS · BLOCKERS · HANDOFF (tag push run URLs, visibility, attestation verify output):

## Plan Self-Review (performed at write time)

- **Spec coverage:** D1/D2 → Tasks 1-2; D3 → Tasks 1-2 pack.cue + Task 4
  values; D4 → Task 8 (selfManage untouched, fenced by existing trio);
  D5 → Tasks 1-2 (no route/expose); D6 → Task 4 (CUBE-0012); §3.1 →
  Tasks 4+7; §3.3 steps 1-11 → Tasks 8 (1-7), 9 (9), 10 (11), 11 (10),
  12 (interface); §3.5 → Task 12; §4 airgap → Task 10; §5 codes → Tasks
  4+12; §6 amendments → Task 13; §7 tests → Tasks 3,4,12,14; §9.1 → Task
  15; §9.2 → no guard (nothing to build — posture documented in Task 2
  README); §9.3 → Task 5. `down`/status: zero changes by design (§3.3.8).
- **Placeholder scan:** CHART_PIN / MEDIA_TYPES / REPLICA_KNOB are
  execution-time-discovered values with their discovery command and
  selection criterion stated in the same task — not deferred decisions.
- **Type consistency:** `EngineSpec.PackName/PackRef` (T4) ==
  consumers (T7/T8/T9/T11); `EngineLock.Entry()` (T6) == T10;
  `FetchRenderEngine(ctx, spec, gw, ref, cacheDir)` uniform at all four
  call sites; `Impl.CRDs func() ([]byte, error)` == both contract_test
  wirings.
