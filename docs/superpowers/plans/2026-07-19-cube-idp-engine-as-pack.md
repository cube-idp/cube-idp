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

- [ ] **Step 1: Write the failing tests** (append; adapt the locator/fetch
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

- [ ] **Step 2: Run to verify they fail before Tasks 1-2 land / pass after**

```bash
CUBE_IDP_E2E_PACKS_DIR=$PACKS go test ./tests/ -run 'TestCubeEngine' -v
```
Expected: PASS (Tasks 1-2 done). If it fails, fix the PACK (or the chart
pin), not the fence.

- [ ] **Step 3: Commit**

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

- [ ] **Step 1: Write the failing tests** (in `internal/config/load_test.go`;
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

- [ ] **Step 2: Run to verify failure**

```bash
go test ./internal/config/ -run 'TestEngineRefValues|TestEngineTuningRemoved' -v
```
Expected: FAIL (`c.Spec.Engine.Ref undefined`, unknown code).

- [ ] **Step 3: Implement types.go** — replace the `Tuning` field and DELETE
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

- [ ] **Step 4: Quiet the two compile breaks this causes** — `factory.go`
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

- [ ] **Step 5: schema.cue** — replace the whole `engine:` block
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

- [ ] **Step 6: load.go** — (a) insert the migration guard AFTER the
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

- [ ] **Step 7: diag codes** — append to the 0xxx block in
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

- [ ] **Step 8: Run the tests**

```bash
go build ./... && go test ./internal/config/ ./internal/diag/ ./internal/engine/... ./cmd/ -count=1
```
Expected: PASS (diag registry tests enforce code/registry sync — fix any
listing it flags).

- [ ] **Step 9: Commit**

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

- [ ] **Step 1: Failing test**

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

- [ ] **Step 2: Run** `go test ./internal/pack/ -run TestHelmSettings -v` —
Expected: FAIL (`helmSettings` undefined).

- [ ] **Step 3: Implement** — in helm.go add, and replace `settings :=
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

- [ ] **Step 4: Run** the test (PASS) plus a real chart render:
`CUBE_IDP_E2E_PACKS_DIR=$PACKS go test ./tests/ -run TestCubeEngineFluxRenderParity -v`
(exercises LocateChart through the new cache; expect PASS and
`<cache>/helm/repository/` to now exist).

- [ ] **Step 5: Commit**

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

- [ ] **Step 1: Failing test**

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

- [ ] **Step 2: Run** `go test ./internal/lock/ -run TestEngineLockEntry -v` — FAIL.

- [ ] **Step 3: Implement**

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

- [ ] **Step 4: Run** — PASS. **Step 5: Commit**

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

- [ ] **Step 1: Failing tests**

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

- [ ] **Step 2: Run** `go test ./internal/pack/ -run 'TestVerifyEnginePackRef|TestFetchRenderEngine' -v` — FAIL.

- [ ] **Step 3: Implement `internal/pack/enginepack.go`**

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

- [ ] **Step 4: Run** — PASS. **Step 5: Commit**

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

- [ ] **Step 1: Failing test** (append to up_test.go — the engine record
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

- [ ] **Step 2: Run** `go test ./internal/up/ -run TestEnginePackRecordRow -v` —
Expected: PASS already (PackObject passes non-empty delivery through — this
is a pin, not new code; if it fails, PackObject needs nothing but you
misread it: stop and re-check).

- [ ] **Step 3: Rewire the engine install** — in `Run`, hoist the cache
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

- [ ] **Step 4: Fill the lock** — replace the `lf := &lock.File{...}`
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

- [ ] **Step 5: Append the engine record row** — in the D11 record loop
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

- [ ] **Step 6: Verify nothing else regressed**

```bash
go build ./... && go test ./internal/up/ -count=1
```
Expected: PASS — in particular `TestSelfManageSSADecision`,
`TestSelfManageDeliverEngineSelf`,
`TestSelfManageDeliverEngineSelfFailureIsCube3010` unchanged (they feed
installObjs directly; the source swap is invisible to them). If a fake in
up_test.go fails to compile because it implements `InstallManifests`/
`Install`, delete just those two methods from the fake.

- [ ] **Step 7: Commit**

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

- [ ] **Step 1: Rewire desiredState** — hoist the `dir, err :=
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

- [ ] **Step 2: Engine record stub** — next to the existing per-pack
Pack-record identityStub append (~:293-307; copy that line's exact helper
usage), add the engine row:

```go
	orphanOnly = append(orphanOnly, identityStub(
		schema.GroupVersionKind{Group: "cube-idp.dev", Version: "v1alpha1", Kind: "Pack"},
		"", enginePk.Name))
```
(Match the existing stub's GVK expression style — if the file already has
a Pack GVK variable for the per-pack stubs, reuse it instead of a literal.)

- [ ] **Step 3: Fix diff tests** — diff_test.go's fake engines lose
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

- [ ] **Step 4: Run** `go build ./... && go test ./internal/diff/ -count=1` — PASS.

- [ ] **Step 5: Commit**

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

- [ ] **Step 1: Failing test** — add to the vendor test file:

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

- [ ] **Step 2: Run** — FAIL (Vendor currently derives engine images from
the embed and succeeds).

- [ ] **Step 3: Implement** — in `Vendor`, right after the `lf == nil`
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

- [ ] **Step 4: Run** `go build ./... && go test ./internal/bundle/ -count=1` — PASS.

- [ ] **Step 5: Commit**

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

- [ ] **Step 1: Failing test** (the two tuning tests were deleted in
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

- [ ] **Step 2: Run** `go test ./cmd/ -run TestRenderEngineRendersPack -v` — FAIL.

- [ ] **Step 3: Implement** — replace the renderEngine RunE body and Short:

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

- [ ] **Step 4: Golden fence** — run
`go test ./cmd/ -run TestCommandTreeGolden -v`. If the Short change trips
it, follow the failure message's regeneration instruction (the fence
freezes the TREE; a Short-text update is a legitimate fixture refresh —
command add/remove/rename is not).

- [ ] **Step 5: Run** `go test ./cmd/ -count=1` — PASS. **Step 6: Commit**

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

- [ ] **Step 1: Extract the CRD fixtures BEFORE deleting the embeds**
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

- [ ] **Step 2: Contract suite rework** — in `contract.go`:
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

- [ ] **Step 3: Slim the interface** — in `engine.go` delete the `Install`
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

- [ ] **Step 4: Sweep the fakes** — `go build ./... 2>&1` and
`go vet ./...`; every fake engine the compiler flags (up_test.go,
diff_test.go if any survived Task 9) loses its `Install`/`InstallManifests`
methods. Binary size check (the payoff):
`go build -o /tmp/cube-idp . && ls -la /tmp/cube-idp` — expect ~2 MB
smaller than before this task.

- [ ] **Step 5: Full unit run**

```bash
go test ./... -count=1 2>&1 | tail -30
```
Expected: all PASS (envtest suites included).

- [ ] **Step 6: Commit**

```bash
git add -A internal/engine hack && git commit -m "refactor(engine)!: drop Install/InstallManifests + embedded manifests — engines are pure translators (p7 engine-as-pack)" -- internal/engine hack internal/up internal/diff
```

---

### Task 13: docs — contract triad amendment + sweep  `[repo: $ROOT]`

**Files:**
- Modify: `docs/pack-contract-v1.md` (§4 vocabulary triad, ~:164-165),
  `README.md` + any file `grep -rln "engine.tuning\|engine\.Tuning" docs README.md` hits

**Interfaces:** none (docs only).

- [ ] **Step 1:** In `docs/pack-contract-v1.md` §4 replace the triad line
`tuning → engine patches (spec.engine.tuning, not packs)` with
`values → chart render (packs and the engine alike; the engine installs
from the cube-engine-<type> pack — engine-as-pack spec 2026-07-19)` and
bump the doc's revision note (v1 → v1.1, additive per §6: pack-side
contract untouched).

- [ ] **Step 2:** `grep -rn "engine.tuning\|render-engine\|selfManage" docs README.md | grep -v superpowers` —
update every stale mention (tuning → values; render-engine wording;
selfManage docs mention the artifact is now the engine pack render).

- [ ] **Step 3: Commit**

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
T1-R STATUS: UNCLAIMED

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
STATUS: BLOCKED(5c0a16fa-203a-4cf4-9a68-34028389d088, 2026-07-19T11:32:48Z)
Outcome:
- BRANCH: `p7/engine-as-pack` ($ROOT worktree `.claude/worktrees/p7-engine-as-pack`)
- COMMITS:
  - `df6647b` docs: p7 plan — claim T3 (ledger claim only)
  - Fence code (`tests/packs_render_test.go`, both TestCubeEngine* tests +
    `fetchPack`/`marshalObjects` helpers) is WRITTEN and LEFT UNCOMMITTED in the
    worktree (a red task is not committed as done): `TestCubeEngineArgocdRenderGuards`
    PASSES, `TestCubeEngineFluxRenderParity` FAILS against the T1 flux pack — see
    BLOCKERS. The fence is correct; the pack it fences is defective.
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
- BLOCKERS:
  - `TestCubeEngineFluxRenderParity` fails because the T1 `cube-engine-flux` pack renders
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
- HANDOFF: none (task BLOCKED, not DONE). CHART_PIN/MEDIA_TYPES consumed from T1/T2 HANDOFF
  as directed; no re-discovery. The fence code is ready to go green the moment the flux
  pack renders namespaced objects — no fence change needed.

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
STATUS: UNCLAIMED
Outcome: BRANCH · COMMITS · FINDINGS · BLOCKERS · HANDOFF:

### T5 — helm cache pinning [$ROOT]
STATUS: UNCLAIMED
Outcome: BRANCH · COMMITS · FINDINGS · BLOCKERS · HANDOFF:

### T6 — cube.lock engine entry [$ROOT]
STATUS: UNCLAIMED
Outcome: BRANCH · COMMITS · FINDINGS · BLOCKERS · HANDOFF:

### T7 — pack.FetchRenderEngine seam [$ROOT]
STATUS: UNCLAIMED
Outcome: BRANCH · COMMITS · FINDINGS · BLOCKERS · HANDOFF:

### T8 — up.Run engine-pack install [$ROOT]
STATUS: UNCLAIMED
Outcome: BRANCH · COMMITS · FINDINGS · BLOCKERS · HANDOFF:

### T9 — diff renders engine pack [$ROOT]
STATUS: UNCLAIMED
Outcome: BRANCH · COMMITS · FINDINGS · BLOCKERS · HANDOFF:

### T10 — bundle vendor + offline rails [$ROOT]
STATUS: UNCLAIMED
Outcome: BRANCH · COMMITS · FINDINGS · BLOCKERS · HANDOFF:

### T11 — config render-engine [$ROOT]
STATUS: UNCLAIMED
Outcome: BRANCH · COMMITS · FINDINGS · BLOCKERS · HANDOFF:

### T12 — engine interface slimming [$ROOT]
STATUS: UNCLAIMED
Outcome: BRANCH · COMMITS · FINDINGS · BLOCKERS · HANDOFF (binary size delta):

### T13 — docs / contract v1.1 [$ROOT]
STATUS: UNCLAIMED
Outcome: BRANCH · COMMITS · FINDINGS · BLOCKERS · HANDOFF:

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
