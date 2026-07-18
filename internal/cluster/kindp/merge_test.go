package kindp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
	v1alpha4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"

	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/diag"
)

var gw = config.GatewaySpec{Pack: "traefik", Host: "cube-idp.localtest.me", Port: 8443}

func golden(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestRenderTypedFieldsOnly(t *testing.T) {
	spec := config.ClusterSpec{
		Provider:          "kind",
		KubernetesVersion: "v1.33.1",
		ExtraPorts:        []config.PortMapping{{HostPort: 32222, NodePort: 32222}},
		Registry: config.RegistrySpec{
			Mirrors:  map[string]string{"docker.io": "https://mirror.corp.example"},
			Insecure: []string{"registry.corp.example:5000"},
		},
		Mounts: []config.Mount{{HostPath: "/tmp/images", NodePath: "/var/lib/images"}},
	}
	out, _, err := RenderConfig(context.Background(), "dev", spec, gw, CertsD{})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != golden(t, "merged-typed.yaml") {
		t.Fatalf("golden mismatch:\n--- got ---\n%s\n--- want ---\n%s", out, golden(t, "merged-typed.yaml"))
	}
}

func TestRenderMergesUserProviderConfig(t *testing.T) {
	spec := config.ClusterSpec{
		Provider:          "kind",
		KubernetesVersion: "v1.33.1",
		ProviderConfigRef: filepath.Join("testdata", "user-kind-config.yaml"),
	}
	out, _, err := RenderConfig(context.Background(), "dev", spec, gw, CertsD{})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != golden(t, "merged-with-user.yaml") {
		t.Fatalf("golden mismatch:\n--- got ---\n%s", out)
	}
}

// TestRenderConflictOnGatewayPort pins decision 1: a providerConfigRef/
// forProvider mapping that collides with the gateway's required hostPort no
// longer hard-errors — core wins and the override is surfaced as a
// CUBE-1206 warning.
func TestRenderConflictOnGatewayPort(t *testing.T) {
	spec := config.ClusterSpec{
		Provider: "kind", KubernetesVersion: "v1.33.1",
		ForProvider: map[string]any{"nodes": []any{map[string]any{
			"role": "control-plane",
			"extraPortMappings": []any{map[string]any{
				"containerPort": float64(9999), "hostPort": float64(8443)}}}}},
	}
	out, warns, err := RenderConfig(context.Background(), "dev", spec, gw, CertsD{})
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 1 || warns[0].Code != diag.CodeKindCoreOverride || warns[0].Severity != diag.SeverityWarning {
		t.Fatalf("want one CUBE-1206 warning, got %v", warns)
	}
	var cfg v1alpha4.Cluster
	if err := yaml.Unmarshal(out, &cfg); err != nil {
		t.Fatal(err)
	}
	cp := cfg.Nodes[0]
	if len(cp.ExtraPortMappings) != 1 || cp.ExtraPortMappings[0].ContainerPort != 30443 {
		t.Fatalf("gateway mapping must be rewritten to 30443: %s", out)
	}
}

// wantDiag asserts err is a *diag.Error with the given code.
func wantDiag(t *testing.T, err error, code diag.Code) *diag.Error {
	t.Helper()
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != code {
		t.Fatalf("want %s, got %v", code, err)
	}
	return de
}

// TestRenderConflictOnNodeImage pins decision 1: a providerConfigRef/
// forProvider node image conflicting with spec.cluster.kubernetesVersion no
// longer hard-errors — core wins and the override is a CUBE-1206 warning.
func TestRenderConflictOnNodeImage(t *testing.T) {
	spec := config.ClusterSpec{
		Provider: "kind", KubernetesVersion: "v1.33.1",
		ForProvider: map[string]any{"nodes": []any{map[string]any{
			"role": "control-plane", "image": "kindest/node:v1.30.0"}}},
	}
	out, warns, err := RenderConfig(context.Background(), "dev", spec, gw, CertsD{})
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 1 || warns[0].Code != diag.CodeKindCoreOverride || warns[0].Severity != diag.SeverityWarning {
		t.Fatalf("want one CUBE-1206 warning, got %v", warns)
	}
	if !strings.Contains(string(out), "kindest/node:v1.33.1") || strings.Contains(string(out), "v1.30.0") {
		t.Fatalf("core image must win: %s", out)
	}
}

func TestRenderConflictOnDuplicateExtraPort(t *testing.T) {
	spec := config.ClusterSpec{
		Provider:          "kind",
		KubernetesVersion: "v1.33.1",
		ForProvider: map[string]any{"nodes": []any{map[string]any{
			"role": "control-plane",
			"extraPortMappings": []any{map[string]any{
				"containerPort": float64(32222), "hostPort": float64(32222)}}}}},
		ExtraPorts: []config.PortMapping{{HostPort: 32222, NodePort: 32222}},
	}
	_, _, err := RenderConfig(context.Background(), "dev", spec, gw, CertsD{})
	de := wantDiag(t, err, "CUBE-1201")
	if !strings.Contains(de.Summary, "providerConfigRef/forProvider") {
		t.Fatalf("duplicate against providerConfigRef/forProvider should mention it, got %q", de.Summary)
	}
}

func TestRenderConflictOnExtraPortReservedForGateway(t *testing.T) {
	spec := config.ClusterSpec{
		Provider:          "kind",
		KubernetesVersion: "v1.33.1",
		ExtraPorts:        []config.PortMapping{{HostPort: 8443, NodePort: 30443}},
	}
	_, _, err := RenderConfig(context.Background(), "dev", spec, gw, CertsD{})
	de := wantDiag(t, err, "CUBE-1201")
	if !strings.Contains(de.Summary, "reserves for the gateway") {
		t.Fatalf("gateway-port duplicate should blame extraPorts, not providerConfig; got %q", de.Summary)
	}
}

// TestRenderProviderConfigFileMissing pins the new fetch layer: a
// providerConfigRef that cannot be fetched is a CUBE-1005 (compose.Resolve),
// not a kindp-level CUBE-1202 — the failure happens before strict decode.
func TestRenderProviderConfigFileMissing(t *testing.T) {
	spec := config.ClusterSpec{
		Provider:          "kind",
		KubernetesVersion: "v1.33.1",
		ProviderConfigRef: filepath.Join("testdata", "does-not-exist.yaml"),
	}
	_, _, err := RenderConfig(context.Background(), "dev", spec, gw, CertsD{})
	wantDiag(t, err, diag.CodeProviderConfigRefFetch)
}

// TestRenderProviderConfigInvalidYAML pins the new fetch layer: invalid YAML
// content behind providerConfigRef fails at compose.Resolve (CUBE-1005),
// same reasoning as TestRenderProviderConfigFileMissing — the inline-blob
// channel that used to hit kindp's own YAML unmarshal is gone.
func TestRenderProviderConfigInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(p, []byte("kind: Cluster\nnodes: {not: [a, valid, kind, document\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	spec := config.ClusterSpec{Provider: "kind", KubernetesVersion: "v1.33.1", ProviderConfigRef: p}
	_, _, err := RenderConfig(context.Background(), "dev", spec, gw, CertsD{})
	wantDiag(t, err, diag.CodeProviderConfigRefFetch)
}

func TestRenderInjectsOnControlPlaneNotFirstNode(t *testing.T) {
	spec := config.ClusterSpec{
		Provider:          "kind",
		KubernetesVersion: "v1.33.1",
		ExtraPorts:        []config.PortMapping{{HostPort: 32222, NodePort: 32222}},
		Mounts:            []config.Mount{{HostPath: "/tmp/images", NodePath: "/var/lib/images"}},
		ForProvider: map[string]any{"nodes": []any{
			map[string]any{"role": "worker"},
			map[string]any{"role": "control-plane"}}},
	}
	out, _, err := RenderConfig(context.Background(), "dev", spec, gw, CertsD{})
	if err != nil {
		t.Fatal(err)
	}
	var got v1alpha4.Cluster
	if err := yaml.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Nodes) != 2 {
		t.Fatalf("want 2 nodes, got %d", len(got.Nodes))
	}
	worker, cp := got.Nodes[0], got.Nodes[1]
	if worker.Role != v1alpha4.WorkerRole || cp.Role != v1alpha4.ControlPlaneRole {
		t.Fatalf("node order changed: got roles %q, %q", worker.Role, cp.Role)
	}
	if worker.Image != "" || len(worker.ExtraPortMappings) != 0 || len(worker.ExtraMounts) != 0 {
		t.Fatalf("injections leaked onto the worker node: %+v", worker)
	}
	if cp.Image != "kindest/node:v1.33.1" {
		t.Fatalf("control-plane image = %q, want kindest/node:v1.33.1", cp.Image)
	}
	var gwMapped, extraMapped bool
	for _, pm := range cp.ExtraPortMappings {
		if pm.HostPort == 8443 && pm.ContainerPort == 30443 {
			gwMapped = true
		}
		if pm.HostPort == 32222 && pm.ContainerPort == 32222 {
			extraMapped = true
		}
	}
	if !gwMapped || !extraMapped {
		t.Fatalf("control-plane missing injected port mappings (gateway=%v, extra=%v): %+v", gwMapped, extraMapped, cp.ExtraPortMappings)
	}
	if len(cp.ExtraMounts) != 1 || cp.ExtraMounts[0].HostPath != "/tmp/images" || cp.ExtraMounts[0].ContainerPath != "/var/lib/images" {
		t.Fatalf("control-plane missing injected mount: %+v", cp.ExtraMounts)
	}
}

func TestRenderMapsGatewayPortToWebsecure(t *testing.T) {
	spec := config.ClusterSpec{Provider: "kind", KubernetesVersion: "v1.33.1"}
	out, _, err := RenderConfig(context.Background(), "dev", spec, gw, CertsD{})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "hostPort: 8443") || !strings.Contains(s, "containerPort: 30443") {
		t.Fatalf("gateway must map host %d to websecure NodePort 30443:\n%s", gw.Port, s)
	}
}

func TestRenderConfigInjectsCertsD(t *testing.T) {
	spec := config.ClusterSpec{Provider: "kind", KubernetesVersion: "v1.33.1"}
	out, _, err := RenderConfig(context.Background(), "dev", spec, gw, CertsD{Host: "registry.cube-idp.localtest.me", HostDir: "/tmp/certsd"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "/etc/containerd/certs.d/registry.cube-idp.localtest.me") {
		t.Fatalf("certs.d mount missing:\n%s", s)
	}
	if !strings.Contains(s, `config_path = "/etc/containerd/certs.d"`) {
		t.Fatalf("containerd config_path patch missing:\n%s", s)
	}
}

func TestRenderNoControlPlaneNode(t *testing.T) {
	spec := config.ClusterSpec{
		Provider: "kind", KubernetesVersion: "v1.33.1",
		ForProvider: map[string]any{"nodes": []any{
			map[string]any{"role": "worker"},
			map[string]any{"role": "worker"}}},
	}
	_, _, err := RenderConfig(context.Background(), "dev", spec, gw, CertsD{})
	de := wantDiag(t, err, "CUBE-1202")
	if !strings.Contains(de.Summary, "no control-plane node") {
		t.Fatalf("summary should say no control-plane node, got %q", de.Summary)
	}
}

// TestRenderConfigMapsHTTPPortWhenSet pins U2's opt-in gateway.httpPort:
// set → a second extraPortMapping host httpPort -> the plain-HTTP NodePort
// (config.GatewayHTTPNodePort) both gateway packs pin; absent → absent,
// byte-identical output to today (decision 3).
func TestRenderConfigMapsHTTPPortWhenSet(t *testing.T) {
	gw := config.GatewaySpec{Pack: "traefik", Host: "cube-idp.localtest.me", Port: 8443, HTTPPort: 8080}
	cfg, _, err := RenderConfig(context.Background(), "dev", config.ClusterSpec{Provider: "kind"}, gw, CertsD{})
	if err != nil {
		t.Fatal(err)
	}
	s := string(cfg)
	if !strings.Contains(s, "hostPort: 8080") || !strings.Contains(s, "containerPort: 30080") {
		t.Fatalf("http mapping missing:\n%s", s)
	}
	// And absent → absent (opt-in contract).
	gw.HTTPPort = 0
	cfg, _, _ = RenderConfig(context.Background(), "dev", config.ClusterSpec{Provider: "kind"}, gw, CertsD{})
	if strings.Contains(string(cfg), "30080") {
		t.Fatalf("httpPort must be opt-in:\n%s", cfg)
	}
}

// TestRenderConfigZeroGatewaySkipsHostPorts pins the S3 spoke contract: a
// zero GatewaySpec (spoke clusters — the hub owns the host ports) renders a
// kind config with no host port mapping at all.
func TestRenderConfigZeroGatewaySkipsHostPorts(t *testing.T) {
	cfg, _, err := RenderConfig(context.Background(), "dev-spoke-staging", config.ClusterSpec{Provider: "kind"}, config.GatewaySpec{}, CertsD{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(cfg), "hostPort") {
		t.Fatalf("spoke render must not map host ports (hub owns them):\n%s", cfg)
	}
}

func TestRenderForProviderFeatureGates(t *testing.T) {
	spec := config.ClusterSpec{
		Provider: "kind", KubernetesVersion: "v1.33.1",
		ForProvider: map[string]any{
			"featureGates": map[string]any{"MyFeature": true},
			"networking":   map[string]any{"kubeProxyMode": "nftables"},
		},
	}
	out, warns, err := RenderConfig(context.Background(), "dev", spec, gw, CertsD{})
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 0 {
		t.Fatalf("unexpected warnings: %v", warns)
	}
	var cfg v1alpha4.Cluster
	if err := yaml.Unmarshal(out, &cfg); err != nil {
		t.Fatal(err)
	}
	if !cfg.FeatureGates["MyFeature"] || cfg.Networking.KubeProxyMode != "nftables" {
		t.Fatalf("forProvider fields missing: %s", out)
	}
}

func TestRenderForProviderUnknownFieldStrict(t *testing.T) {
	spec := config.ClusterSpec{
		Provider: "kind", KubernetesVersion: "v1.33.1",
		ForProvider: map[string]any{"featureGatez": map[string]any{"Oops": true}},
	}
	_, _, err := RenderConfig(context.Background(), "dev", spec, gw, CertsD{})
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != diag.CodeKindConfigInvalid {
		t.Fatalf("want CUBE-1202, got %v", err)
	}
	if !strings.Contains(err.Error(), "featureGatez") {
		t.Fatalf("error must name the bad field: %v", err)
	}
}

func TestRenderRefPlusForProviderListReplace(t *testing.T) {
	// providerConfigRef declares 2 nodes; forProvider's nodes list replaces
	// wholesale (RFC 7386, decision 4) with a single labeled control-plane.
	dir := t.TempDir()
	ref := filepath.Join(dir, "base.yaml")
	os.WriteFile(ref, []byte("nodes:\n- role: control-plane\n- role: worker\n"), 0o644)
	spec := config.ClusterSpec{
		Provider: "kind", KubernetesVersion: "v1.33.1",
		ProviderConfigRef: ref,
		ForProvider: map[string]any{"nodes": []any{
			map[string]any{"role": "control-plane", "labels": map[string]any{"tier": "system"}}}},
	}
	out, _, err := RenderConfig(context.Background(), "dev", spec, gw, CertsD{})
	if err != nil {
		t.Fatal(err)
	}
	var cfg v1alpha4.Cluster
	if err := yaml.Unmarshal(out, &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Nodes) != 1 || cfg.Nodes[0].Labels["tier"] != "system" {
		t.Fatalf("list-replace failed: %s", out)
	}
}

func TestRenderImageConflictWarnsAndWins(t *testing.T) {
	spec := config.ClusterSpec{
		Provider: "kind", KubernetesVersion: "v1.33.1",
		ForProvider: map[string]any{"nodes": []any{
			map[string]any{"role": "control-plane", "image": "kindest/node:v1.99.0"}}},
	}
	out, warns, err := RenderConfig(context.Background(), "dev", spec, gw, CertsD{})
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 1 || warns[0].Code != diag.CodeKindCoreOverride || warns[0].Severity != diag.SeverityWarning {
		t.Fatalf("warns = %v", warns)
	}
	if !strings.Contains(string(out), "kindest/node:v1.33.1") || strings.Contains(string(out), "v1.99.0") {
		t.Fatalf("core image must win: %s", out)
	}
}

func TestRenderGatewayPortConflictWarnsAndWins(t *testing.T) {
	spec := config.ClusterSpec{
		Provider: "kind", KubernetesVersion: "v1.33.1",
		ForProvider: map[string]any{"nodes": []any{map[string]any{
			"role": "control-plane",
			"extraPortMappings": []any{map[string]any{
				"containerPort": float64(31000), "hostPort": float64(8443)}}}}},
	}
	out, warns, err := RenderConfig(context.Background(), "dev", spec, gw, CertsD{})
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 1 || warns[0].Code != diag.CodeKindCoreOverride {
		t.Fatalf("warns = %v", warns)
	}
	var cfg v1alpha4.Cluster
	if err := yaml.Unmarshal(out, &cfg); err != nil {
		t.Fatal(err)
	}
	cp := cfg.Nodes[0]
	if len(cp.ExtraPortMappings) != 1 || cp.ExtraPortMappings[0].ContainerPort != 30443 {
		t.Fatalf("gateway mapping must be rewritten to 30443: %s", out)
	}
}
