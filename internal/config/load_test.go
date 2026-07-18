package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	sigyaml "sigs.k8s.io/yaml"

	"github.com/cube-idp/cube-idp/internal/diag"
)

func codeOf(t *testing.T, err error) diag.Code {
	t.Helper()
	var de *diag.Error
	if !errors.As(err, &de) {
		t.Fatalf("want *diag.Error, got %T: %v", err, err)
	}
	return de.Code
}

func TestLoadMinimalAppliesDefaults(t *testing.T) {
	c, err := Load("testdata/minimal.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if c.Spec.Cluster.Provider != "kind" || c.Spec.Engine.Type != "flux" {
		t.Fatalf("defaults not applied: %+v", c.Spec)
	}
	if c.Spec.Gateway.Host != "cube-idp.localtest.me" || c.Spec.Gateway.Port != 8443 || c.Spec.Gateway.Pack != "traefik" {
		t.Fatalf("gateway defaults: %+v", c.Spec.Gateway)
	}
	if c.Spec.Cluster.KubernetesVersion != "v1.33.1" {
		t.Fatalf("kubernetesVersion default not applied: %+v", c.Spec.Cluster)
	}
}

func TestLoadFullRoundTrips(t *testing.T) {
	c, err := Load("testdata/full.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Spec.Cluster.ExtraPorts) != 1 || c.Spec.Cluster.ExtraPorts[0].HostPort != 32222 {
		t.Fatalf("extraPorts: %+v", c.Spec.Cluster.ExtraPorts)
	}
	if c.Spec.Cluster.Registry.Mirrors["docker.io"] != "https://mirror.corp.example" {
		t.Fatalf("mirrors: %+v", c.Spec.Cluster.Registry)
	}
	if len(c.Spec.Packs) != 2 || c.Spec.Packs[1].Values["replicas"] != 2 {
		t.Fatalf("packs: %+v", c.Spec.Packs)
	}
	if c.Spec.Gateway.Ref != "/repo/packs/traefik" {
		t.Fatalf("gateway.ref did not round-trip: %+v", c.Spec.Gateway)
	}
}

func TestLoadGatewayRefDefaultsEmpty(t *testing.T) {
	c, err := Load("testdata/minimal.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if c.Spec.Gateway.Ref != "" {
		t.Fatalf("gateway.ref should default to empty (falls back to packs/<pack> in `up`), got %q", c.Spec.Gateway.Ref)
	}
}

func TestLoadAcceptsK3dProvider(t *testing.T) {
	c, err := Load("testdata/k3d.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if c.Spec.Cluster.Provider != "k3d" {
		t.Fatalf("provider: %q", c.Spec.Cluster.Provider)
	}
}

func TestLoadRejectsBadProvider(t *testing.T) {
	_, err := Load("testdata/bad-provider.yaml")
	if codeOf(t, err) != "CUBE-0002" {
		t.Fatalf("want CUBE-0002, got %v", err)
	}
}

func TestLoadRejectsNodeFieldsOnExisting(t *testing.T) {
	_, err := Load("testdata/existing-with-ports.yaml")
	if codeOf(t, err) != "CUBE-1003" {
		t.Fatalf("want CUBE-1003, got %v", err)
	}
}

func TestLoadRejectsKubernetesVersionOnExisting(t *testing.T) { // D10, spec §4.1
	dir := t.TempDir()
	path := filepath.Join(dir, "cube.yaml")
	doc := `apiVersion: cube-idp.dev/v1alpha1
kind: Cube
metadata:
  name: remote
spec:
  cluster:
    provider: existing
    context: my-eks
    kubernetesVersion: v1.30.0
`
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if codeOf(t, err) != "CUBE-1003" {
		t.Fatalf("want CUBE-1003, got %v", err)
	}
}

func TestLoadRejectsArgoPackWithArgoEngine(t *testing.T) { // CUBE-0005
	_, err := Load("testdata/argocd-engine-with-pack.yaml")
	if codeOf(t, err) != "CUBE-0005" {
		t.Fatalf("want CUBE-0005, got %v", err)
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load("testdata/nope.yaml")
	if codeOf(t, err) != "CUBE-0001" {
		t.Fatalf("want CUBE-0001, got %v", err)
	}
}

// TestDefaultRoundTripsThroughLoad pins the bug class fixed by the omitempty
// tags in types.go: `cube-idp init` marshals config.Default with
// sigs.k8s.io/yaml, and any optional (`?` in schema.cue) slice/map field
// WITHOUT omitempty serializes its nil zero value as an explicit YAML null,
// which CUE re-validation rejects (mismatched types list/map and null) —
// making every init-generated cube.yaml unloadable. A future optional field
// added without omitempty fails this test.
func TestDefaultRoundTripsThroughLoad(t *testing.T) {
	writeAndLoad := func(t *testing.T, c *Cube) *Cube {
		t.Helper()
		raw, err := sigyaml.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "cube.yaml")
		if err := os.WriteFile(path, raw, 0o644); err != nil {
			t.Fatal(err)
		}
		loaded, err := Load(path)
		if err != nil {
			t.Fatalf("Load rejected marshaled config:\n%s\nerror: %v", raw, err)
		}
		return loaded
	}

	t.Run("default profile", func(t *testing.T) {
		def := Default("dev")
		loaded := writeAndLoad(t, def)
		if loaded.Spec.Cluster.Provider != def.Spec.Cluster.Provider {
			t.Fatalf("provider: got %q, want %q", loaded.Spec.Cluster.Provider, def.Spec.Cluster.Provider)
		}
		if loaded.Spec.Engine != def.Spec.Engine {
			t.Fatalf("engine: got %+v, want %+v", loaded.Spec.Engine, def.Spec.Engine)
		}
		if loaded.Spec.Gateway != def.Spec.Gateway {
			t.Fatalf("gateway: got %+v, want %+v", loaded.Spec.Gateway, def.Spec.Gateway)
		}
		if !reflect.DeepEqual(loaded.Spec.Packs, def.Spec.Packs) {
			t.Fatalf("packs: got %+v, want %+v", loaded.Spec.Packs, def.Spec.Packs)
		}
	})

	// packs? is optional in schema.cue, so a Cube without any packs (nil or
	// explicitly empty slice) must also round-trip — omitempty on Spec.Packs
	// keeps both out of the output instead of emitting `packs: null`.
	t.Run("empty packs slice", func(t *testing.T) {
		c := Default("dev")
		c.Spec.Packs = []PackRef{}
		loaded := writeAndLoad(t, c)
		if len(loaded.Spec.Packs) != 0 {
			t.Fatalf("packs should be absent, got %+v", loaded.Spec.Packs)
		}
	})

	t.Run("nil packs", func(t *testing.T) {
		c := Default("dev")
		c.Spec.Packs = nil
		loaded := writeAndLoad(t, c)
		if len(loaded.Spec.Packs) != 0 {
			t.Fatalf("packs should be absent, got %+v", loaded.Spec.Packs)
		}
	})
}

func TestDefaultProfileIncludesGitea(t *testing.T) { // D9
	c := Default("dev")
	found := false
	for _, p := range c.Spec.Packs {
		if p.Ref == "oci://ghcr.io/cube-idp/packs/gitea:0.2.0" {
			found = true
		}
	}
	if !found {
		t.Fatalf("default profile must include gitea (D9): %+v", c.Spec.Packs)
	}
}

// TestDefaultGatewayRefIsPublishedOCI pins the P4 standalone-binary contract
// (F12 CLOSED): the default profile's gateway pack resolves from the
// published packs monorepo, never from a repo-relative checkout path.
func TestDefaultGatewayRefIsPublishedOCI(t *testing.T) {
	c := Default("dev")
	want := "oci://ghcr.io/cube-idp/packs/traefik:0.2.0"
	if c.Spec.Gateway.Ref != want {
		t.Fatalf("default gateway.ref = %q, want %q (F12)", c.Spec.Gateway.Ref, want)
	}
	// The fallback for a hand-written cube.yaml WITHOUT ref stays the
	// documented checkout-only last resort — unchanged by P4.
	if got := (GatewaySpec{Pack: "traefik"}).PackRef(); got != "packs/traefik" {
		t.Fatalf("empty-ref fallback must stay packs/<pack>, got %q", got)
	}
}

func TestSpokesRoundTripAndValidation(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cube.yaml")
	base := `apiVersion: cube-idp.dev/v1alpha1
kind: Cube
metadata: {name: dev}
spec:
  engine: {type: flux}
  gateway: {pack: traefik, host: cube-idp.localtest.me, port: 8443}
  spokes:
    - name: staging
      cluster: {provider: kind}
    - name: prod-eu
      cluster: {provider: existing, context: eks-prod-eu}
`
	if err := os.WriteFile(p, []byte(base), 0o644); err != nil {
		t.Fatal(err)
	}
	cube, err := Load(p)
	if err != nil {
		t.Fatalf("valid spokes rejected: %v", err)
	}
	if len(cube.Spec.Spokes) != 2 || cube.Spec.Spokes[0].Name != "staging" {
		t.Fatalf("spokes not decoded: %+v", cube.Spec.Spokes)
	}

	// k3d spokes are deferred (GT6): must fail with CUBE-8001.
	bad := strings.Replace(base, "provider: kind", "provider: k3d", 1)
	if err := os.WriteFile(p, []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = Load(p)
	if err == nil || !strings.Contains(err.Error(), "CUBE-8001") {
		t.Fatalf("k3d spoke must be CUBE-8001, got: %v", err)
	}

	// existing spoke without context must fail (CUBE-8001 family).
	bad2 := strings.Replace(base, "context: eks-prod-eu", "", 1)
	if err := os.WriteFile(p, []byte(bad2), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err = Load(p); err == nil {
		t.Fatal("existing spoke without context must be rejected")
	}

	// duplicate spoke names must fail.
	dup := strings.Replace(base, "prod-eu", "staging", 1)
	if err := os.WriteFile(p, []byte(dup), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err = Load(p); err == nil {
		t.Fatal("duplicate spoke names must be rejected")
	}
}

// TestLoadGatewayHTTPPortRoundTripAndCollisions covers U2's opt-in
// spec.gateway.httpPort (decision 3): set → decoded and round-tripped
// through SaveValidated; equal to gateway.port or colliding with a typed
// extraPorts hostPort → CUBE-0002; omitted → zero (no host mapping at all).
func TestLoadGatewayHTTPPortRoundTripAndCollisions(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cube.yaml")
	base := `apiVersion: cube-idp.dev/v1alpha1
kind: Cube
metadata: {name: dev}
spec:
  engine: {type: flux}
  gateway: {pack: traefik, host: cube-idp.localtest.me, port: 8443, httpPort: 8080}
`
	if err := os.WriteFile(p, []byte(base), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatalf("valid httpPort rejected: %v", err)
	}
	if c.Spec.Gateway.HTTPPort != 8080 {
		t.Fatalf("httpPort not decoded: %+v", c.Spec.Gateway)
	}
	if err := SaveValidated(p, c); err != nil {
		t.Fatalf("httpPort does not round-trip through SaveValidated: %v", err)
	}
	c, err = Load(p)
	if err != nil || c.Spec.Gateway.HTTPPort != 8080 {
		t.Fatalf("httpPort lost on round-trip: %v %+v", err, c.Spec.Gateway)
	}

	// httpPort equal to gateway.port must fail validation (CUBE-0002 family).
	bad := strings.Replace(base, "httpPort: 8080", "httpPort: 8443", 1)
	if err := os.WriteFile(p, []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil || codeOf(t, err) != "CUBE-0002" {
		t.Fatalf("httpPort == port must be CUBE-0002, got: %v", err)
	}

	// httpPort colliding with a typed extraPorts hostPort must fail too.
	collide := base + `  cluster: {provider: kind, extraPorts: [{hostPort: 8080, nodePort: 31000}]}
`
	if err := os.WriteFile(p, []byte(collide), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil || codeOf(t, err) != "CUBE-0002" {
		t.Fatalf("httpPort colliding with extraPorts must be CUBE-0002, got: %v", err)
	}

	// Omitted → zero: the opt-in default maps nothing (byte-identical to today).
	mc, err := Load("testdata/minimal.yaml")
	if err != nil || mc.Spec.Gateway.HTTPPort != 0 {
		t.Fatalf("omitted httpPort must be zero, got %v %+v", err, mc.Spec.Gateway)
	}
}

// TestEngineTuningRoundTripAndValidation covers U3's spec.engine.tuning
// (GT1): set → decoded (typed *int replicas, int64-leaved resources — CUE's
// decode type, deliberately NOT normalized to int like PackRef.Values,
// because the consumer is unstructured SSA) and round-tripped through
// SaveValidated; a knob outside the closed set (replicas: 0) → CUBE-0002;
// omitted → nil pointer, an absent key on re-marshal (PackRef.Values
// discipline).
func TestEngineTuningRoundTripAndValidation(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cube.yaml")
	base := `apiVersion: cube-idp.dev/v1alpha1
kind: Cube
metadata: {name: dev}
spec:
  engine:
    type: flux
    tuning:
      components:
        source-controller:
          replicas: 2
          resources: {limits: {memory: 512Mi, cpu: 1}}
  gateway: {pack: traefik, host: cube-idp.localtest.me, port: 8443}
`
	if err := os.WriteFile(p, []byte(base), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatalf("valid tuning rejected: %v", err)
	}
	ct, ok := c.Spec.Engine.Tuning.Components["source-controller"]
	if !ok || ct.Replicas == nil || *ct.Replicas != 2 {
		t.Fatalf("tuning not decoded: %+v", c.Spec.Engine.Tuning)
	}
	limits, _ := ct.Resources["limits"].(map[string]any)
	if limits["memory"] != "512Mi" {
		t.Fatalf("resources not decoded: %+v", ct.Resources)
	}
	if cpu, isInt64 := limits["cpu"].(int64); !isInt64 || cpu != 1 {
		t.Fatalf("tuning numbers must stay int64 (unstructured-safe), got %T %v", limits["cpu"], limits["cpu"])
	}
	if err := SaveValidated(p, c); err != nil {
		t.Fatalf("tuning does not round-trip through SaveValidated: %v", err)
	}
	c, err = Load(p)
	if err != nil || c.Spec.Engine.Tuning == nil || *c.Spec.Engine.Tuning.Components["source-controller"].Replicas != 2 {
		t.Fatalf("tuning lost on round-trip: %v %+v", err, c.Spec.Engine)
	}

	// The knob set is closed: replicas must be > 0 per schema.cue.
	bad := strings.Replace(base, "replicas: 2", "replicas: 0", 1)
	if err := os.WriteFile(p, []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil || codeOf(t, err) != "CUBE-0002" {
		t.Fatalf("replicas: 0 must be CUBE-0002, got: %v", err)
	}

	// Omitted → nil: no tuning block, no patch, and re-marshal writes no
	// explicit `tuning: null` (omitempty discipline).
	mc, err := Load("testdata/minimal.yaml")
	if err != nil || mc.Spec.Engine.Tuning != nil {
		t.Fatalf("omitted tuning must be nil, got %v %+v", err, mc.Spec.Engine)
	}
	raw, err := sigyaml.Marshal(mc)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "tuning") {
		t.Fatalf("nil tuning must marshal as an absent key:\n%s", raw)
	}
}

// TestPackExtraManifestsRoundTrip pins GT15's config surface (U4):
// packs[].extraManifests decodes, survives a SaveValidated round-trip, an
// explicit empty string is rejected by schema.cue (`string & !=""`), and a
// cleared field re-marshals as an ABSENT key (omitempty discipline — same
// nil-round-trip rule as PackRef.Values; an emitted `extraManifests: ""`
// would make the file unwritable against that same schema).
func TestPackExtraManifestsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cube.yaml")
	base := `apiVersion: cube-idp.dev/v1alpha1
kind: Cube
metadata: {name: dev}
spec:
  engine: {type: flux}
  gateway: {pack: traefik, host: cube-idp.localtest.me, port: 8443}
  packs:
    - ref: oci://ghcr.io/cube-idp/packs/gitea:0.2.0
      extraManifests: |
        apiVersion: v1
        kind: ConfigMap
        metadata: {name: seed, namespace: gitea}
`
	if err := os.WriteFile(p, []byte(base), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatalf("valid extraManifests rejected: %v", err)
	}
	if !strings.Contains(c.Spec.Packs[0].ExtraManifests, "kind: ConfigMap") {
		t.Fatalf("extraManifests not decoded: %+v", c.Spec.Packs[0])
	}
	if err := SaveValidated(p, c); err != nil {
		t.Fatalf("extraManifests does not round-trip through SaveValidated: %v", err)
	}
	c, err = Load(p)
	if err != nil || !strings.Contains(c.Spec.Packs[0].ExtraManifests, "kind: ConfigMap") {
		t.Fatalf("extraManifests lost on round-trip: %v %+v", err, c.Spec.Packs)
	}

	// Explicit empty string is rejected (schema.cue: string & !="").
	bad := strings.Replace(base,
		`      extraManifests: |
        apiVersion: v1
        kind: ConfigMap
        metadata: {name: seed, namespace: gitea}
`, `      extraManifests: ""
`, 1)
	if err := os.WriteFile(p, []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil {
		t.Fatal("empty extraManifests must be rejected by schema.cue")
	}

	// Cleared field saves as an absent key, not an explicit "".
	c.Spec.Packs[0].ExtraManifests = ""
	if err := SaveValidated(p, c); err != nil {
		t.Fatalf("cleared extraManifests must save (absent key): %v", err)
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "extraManifests") {
		t.Fatalf("empty ExtraManifests must marshal as an absent key:\n%s", raw)
	}
}

func TestPackDeliveryRoundTripAndGiteaGuarantee(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cube.yaml")
	base := `apiVersion: cube-idp.dev/v1alpha1
kind: Cube
metadata: {name: dev}
spec:
  engine: {type: flux}
  gateway: {pack: traefik, host: cube-idp.localtest.me, port: 8443}
  packs:
    - ref: oci://ghcr.io/cube-idp/packs/gitea:0.2.0
    - ref: oci://ghcr.io/cube-idp/packs/backstage:0.2.0
      delivery: repo
`
	if err := os.WriteFile(p, []byte(base), 0o644); err != nil {
		t.Fatal(err)
	}

	// (a) delivery: repo round-trips; the absent field stays absent.
	c, err := Load(p)
	if err != nil {
		t.Fatalf("valid delivery: repo rejected: %v", err)
	}
	if c.Spec.Packs[0].Delivery != "" || c.Spec.Packs[1].Delivery != "repo" {
		t.Fatalf("delivery not decoded: %+v", c.Spec.Packs)
	}
	if err := SaveValidated(p, c); err != nil {
		t.Fatalf("delivery does not round-trip through SaveValidated: %v", err)
	}
	c, err = Load(p)
	if err != nil || c.Spec.Packs[1].Delivery != "repo" {
		t.Fatalf("delivery lost on round-trip: %v %+v", err, c.Spec.Packs)
	}
	raw, _ := os.ReadFile(p)
	if got := strings.Count(string(raw), "delivery:"); got != 1 {
		t.Fatalf("empty Delivery must marshal as an absent key (want exactly 1 delivery: line, got %d):\n%s", got, raw)
	}

	// (b) delivery: bogus is rejected by CUE (the enum is oci|repo).
	bad := strings.Replace(base, "delivery: repo", "delivery: bogus", 1)
	if err := os.WriteFile(p, []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil || !strings.Contains(err.Error(), "CUBE-0002") {
		t.Fatalf("delivery: bogus must fail CUE validation with CUBE-0002, got: %v", err)
	}

	// (c) The gitea guarantee: a delivery: repo pack with no gitea pack in
	// spec.packs is a typed error naming the fix.
	noGitea := strings.Replace(base, "    - ref: oci://ghcr.io/cube-idp/packs/gitea:0.2.0\n", "", 1)
	if err := os.WriteFile(p, []byte(noGitea), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = Load(p)
	if err == nil || !strings.Contains(err.Error(), "CUBE-7304") {
		t.Fatalf("repo delivery without the gitea pack must be CUBE-7304, got: %v", err)
	}
	var de *diag.Error
	if !errors.As(err, &de) || !strings.Contains(de.Remediation, "add the gitea pack or use delivery: oci") {
		t.Fatalf("CUBE-7304 must name the fix in its remediation, got: %+v", err)
	}

	// (d) gitea itself can never be repo-delivered (self-reference).
	selfRef := `apiVersion: cube-idp.dev/v1alpha1
kind: Cube
metadata: {name: dev}
spec:
  engine: {type: flux}
  gateway: {pack: traefik, host: cube-idp.localtest.me, port: 8443}
  packs:
    - ref: oci://ghcr.io/cube-idp/packs/gitea:0.2.0
      delivery: repo
`
	if err := os.WriteFile(p, []byte(selfRef), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil || !strings.Contains(err.Error(), "CUBE-7304") {
		t.Fatalf("gitea with delivery: repo must be CUBE-7304 (self-reference), got: %v", err)
	}
}

func TestLoadForProviderRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cube.yaml")
	doc := `apiVersion: cube-idp.dev/v1alpha1
kind: Cube
metadata:
  name: dev
spec:
  cluster:
    provider: kind
    providerConfigRef: ./base.yaml
    forProvider:
      featureGates:
        MyFeature: true
      networking:
        kubeProxyMode: nftables
  engine:
    type: flux
  gateway:
    pack: traefik
    host: cube-idp.localtest.me
    port: 8443
`
	if err := os.WriteFile(p, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Spec.Cluster.ProviderConfigRef != "./base.yaml" {
		t.Fatalf("ProviderConfigRef = %q", c.Spec.Cluster.ProviderConfigRef)
	}
	fg, ok := c.Spec.Cluster.ForProvider["featureGates"].(map[string]any)
	if !ok || fg["MyFeature"] != true {
		t.Fatalf("ForProvider = %#v", c.Spec.Cluster.ForProvider)
	}
	// Round-trip discipline: absent forProvider must stay an absent key.
	if err := SaveValidated(p, c); err != nil {
		t.Fatalf("SaveValidated round-trip: %v", err)
	}
}

func TestLoadForProviderRejectedForExisting(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cube.yaml")
	doc := `apiVersion: cube-idp.dev/v1alpha1
kind: Cube
metadata:
  name: dev
spec:
  cluster:
    provider: existing
    context: my-ctx
    forProvider:
      featureGates: {MyFeature: true}
  engine:
    type: flux
  gateway:
    pack: traefik
    host: cube-idp.localtest.me
    port: 8443
`
	if err := os.WriteFile(p, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(p)
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != diag.CodeClusterFieldsConflict {
		t.Fatalf("want CUBE-1003, got %v", err)
	}
}

func TestLoadSpokeForProvider(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cube.yaml")
	doc := `apiVersion: cube-idp.dev/v1alpha1
kind: Cube
metadata:
  name: dev
spec:
  cluster:
    provider: kind
  engine:
    type: flux
  gateway:
    pack: traefik
    host: cube-idp.localtest.me
    port: 8443
  spokes:
  - name: staging
    cluster:
      provider: kind
      providerConfigRef: ./spoke-base.yaml
      forProvider:
        featureGates: {MyFeature: true}
`
	if err := os.WriteFile(p, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Spec.Spokes[0].Cluster.ProviderConfigRef != "./spoke-base.yaml" {
		t.Fatalf("spoke ProviderConfigRef = %q", c.Spec.Spokes[0].Cluster.ProviderConfigRef)
	}
}

func TestLoadProviderConfigMigrationError(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cube.yaml")
	doc := `apiVersion: cube-idp.dev/v1alpha1
kind: Cube
metadata:
  name: dev
spec:
  cluster:
    provider: kind
    providerConfig: ./my-kind.yaml
  engine:
    type: flux
  gateway:
    pack: traefik
    host: cube-idp.localtest.me
    port: 8443
`
	if err := os.WriteFile(p, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(p)
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != diag.CodeProviderConfigRemoved {
		t.Fatalf("want CUBE-0011, got %v", err)
	}
	if !strings.Contains(de.Remediation, "providerConfigRef") || !strings.Contains(de.Remediation, "forProvider") {
		t.Fatalf("remediation must name both replacement fields: %q", de.Remediation)
	}
}

// TestEngineSelfManageRoundTrip pins P8's config surface (GT16):
// spec.engine.selfManage: true decodes and survives a SaveValidated
// round-trip; omitted → false with NO selfManage key on re-marshal
// (omitempty discipline — a false bool is the zero value and must not
// materialize a key the user never wrote).
func TestEngineSelfManageRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cube.yaml")
	base := `apiVersion: cube-idp.dev/v1alpha1
kind: Cube
metadata: {name: dev}
spec:
  engine:
    type: flux
    selfManage: true
  gateway: {pack: traefik, host: cube-idp.localtest.me, port: 8443}
`
	if err := os.WriteFile(p, []byte(base), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatalf("valid selfManage rejected: %v", err)
	}
	if !c.Spec.Engine.SelfManage {
		t.Fatalf("selfManage: true not decoded: %+v", c.Spec.Engine)
	}
	if err := SaveValidated(p, c); err != nil {
		t.Fatalf("selfManage does not round-trip through SaveValidated: %v", err)
	}
	if c, err = Load(p); err != nil || !c.Spec.Engine.SelfManage {
		t.Fatalf("selfManage lost on round-trip: %v %+v", err, c.Spec.Engine)
	}

	// Omitted → false, and re-marshal writes no selfManage key.
	mc, err := Load("testdata/minimal.yaml")
	if err != nil || mc.Spec.Engine.SelfManage {
		t.Fatalf("omitted selfManage must be false, got %v %+v", err, mc.Spec.Engine)
	}
	raw, err := sigyaml.Marshal(mc)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "selfManage") {
		t.Fatalf("false selfManage must marshal as an absent key:\n%s", raw)
	}
}
