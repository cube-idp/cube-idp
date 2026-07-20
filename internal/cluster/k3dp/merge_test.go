package k3dp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	v1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"sigs.k8s.io/yaml"

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
		Provider:          "k3d",
		KubernetesVersion: "v1.33.1",
		ExtraPorts:        []config.PortMapping{{HostPort: 32222, NodePort: 32222}},
		Registry: config.RegistrySpec{
			Mirrors:  map[string]string{"docker.io": "https://mirror.corp.example"},
			Insecure: []string{"registry.corp.example:5000"},
		},
		Mounts: []config.Mount{{HostPath: "/tmp/images", NodePath: "/var/lib/images"}},
	}
	out, _, err := RenderConfig(context.Background(), "dev", spec, gw, ZotMirror{})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != golden(t, "merged-typed.yaml") {
		t.Fatalf("golden mismatch:\n--- got ---\n%s\n--- want ---\n%s", out, golden(t, "merged-typed.yaml"))
	}
}

func TestRenderMergesUserProviderConfig(t *testing.T) {
	spec := config.ClusterSpec{
		Provider:          "k3d",
		KubernetesVersion: "v1.33.1",
		ProviderConfigRef: filepath.Join("testdata", "user-k3d-config.yaml"),
	}
	out, _, err := RenderConfig(context.Background(), "dev", spec, gw, ZotMirror{})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != golden(t, "merged-with-user.yaml") {
		t.Fatalf("golden mismatch:\n--- got ---\n%s", out)
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

// TestRenderImageConflictWarnsAndWins pins the core-injection warn-and-win rule:
// a providerConfigRef/forProvider image conflicting with
// spec.cluster.kubernetesVersion no longer hard-errors — core wins and the
// override is a CUBE-1306 warning.
func TestRenderImageConflictWarnsAndWins(t *testing.T) {
	spec := config.ClusterSpec{
		Provider: "k3d", KubernetesVersion: "v1.33.1",
		ForProvider: map[string]any{"image": "rancher/k3s:v1.99.0-k3s1"},
	}
	out, warns, err := RenderConfig(context.Background(), "dev", spec, gw, ZotMirror{})
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 1 || warns[0].Code != diag.CodeK3dCoreOverride || warns[0].Severity != diag.SeverityWarning {
		t.Fatalf("warns = %v", warns)
	}
	var cfg v1alpha5.SimpleConfig
	if err := yaml.Unmarshal(out, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Image != "rancher/k3s:v1.33.1-k3s1" {
		t.Fatalf("core image must win, got %q", cfg.Image)
	}
}

func TestRenderForProviderUnknownFieldStrict(t *testing.T) {
	spec := config.ClusterSpec{
		Provider: "k3d", KubernetesVersion: "v1.33.1",
		ForProvider: map[string]any{"serverz": float64(3)},
	}
	_, _, err := RenderConfig(context.Background(), "dev", spec, gw, ZotMirror{})
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != diag.CodeK3dConfigInvalid {
		t.Fatalf("want CUBE-1302, got %v", err)
	}
	if !strings.Contains(err.Error(), "serverz") {
		t.Fatalf("error must name the bad field: %v", err)
	}
}

func TestRenderForProviderHappyPath(t *testing.T) {
	spec := config.ClusterSpec{
		Provider: "k3d", KubernetesVersion: "v1.33.1",
		ForProvider: map[string]any{"servers": float64(3)},
	}
	out, warns, err := RenderConfig(context.Background(), "dev", spec, gw, ZotMirror{})
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 0 {
		t.Fatalf("unexpected warnings: %v", warns)
	}
	var cfg v1alpha5.SimpleConfig
	if err := yaml.Unmarshal(out, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Servers != 3 {
		t.Fatalf("forProvider servers field missing: %s", out)
	}
}

// TestRenderGatewayPortConflictWarnsAndWins pins conflict-table row 2: a
// ports entry that maps gw.Port to the wrong node port no longer
// hard-errors — the entry is rewritten to gw.Port:GatewayNodePort with a
// CUBE-1306 warning.
func TestRenderGatewayPortConflictWarnsAndWins(t *testing.T) {
	spec := config.ClusterSpec{
		Provider: "k3d", KubernetesVersion: "v1.33.1",
		ForProvider: map[string]any{"ports": []any{map[string]any{
			"port": "8443:9999", "nodeFilters": []any{"server:0"}}}},
	}
	out, warns, err := RenderConfig(context.Background(), "dev", spec, gw, ZotMirror{})
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 1 || warns[0].Code != diag.CodeK3dCoreOverride {
		t.Fatalf("warns = %v", warns)
	}
	if !strings.Contains(string(out), "8443:30443") || strings.Contains(string(out), "8443:9999") {
		t.Fatalf("gateway mapping must be rewritten to 8443:30443: %s", out)
	}
}

// TestRenderHTTPPortConflictWarnsAndWins pins conflict-table row 3: the same
// warn-and-win rewrite for gw.HTTPPort, mapped to GatewayHTTPNodePort.
func TestRenderHTTPPortConflictWarnsAndWins(t *testing.T) {
	gwHTTP := config.GatewaySpec{Pack: "traefik", Host: "cube-idp.localtest.me", Port: 8443, HTTPPort: 8080}
	spec := config.ClusterSpec{
		Provider: "k3d", KubernetesVersion: "v1.33.1",
		ForProvider: map[string]any{"ports": []any{map[string]any{
			"port": "8080:9999", "nodeFilters": []any{"server:0"}}}},
	}
	out, warns, err := RenderConfig(context.Background(), "dev", spec, gwHTTP, ZotMirror{})
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 1 || warns[0].Code != diag.CodeK3dCoreOverride {
		t.Fatalf("warns = %v", warns)
	}
	if !strings.Contains(string(out), "8080:30080") || strings.Contains(string(out), "8080:9999") {
		t.Fatalf("http gateway mapping must be rewritten to 8080:30080: %s", out)
	}
}

// TestRenderTraefikArgConflictWarnsAndWins pins conflict-table row 4: a user
// k3s arg containing "traefik" that isn't exactly --disable=traefik is
// replaced verbatim by --disable=traefik, with the warning quoting the
// discarded arg.
func TestRenderTraefikArgConflictWarnsAndWins(t *testing.T) {
	// Baseline: an unrelated k3s extraArg must not warn at all.
	spec := config.ClusterSpec{
		Provider: "k3d", KubernetesVersion: "v1.33.1",
		ForProvider: map[string]any{"options": map[string]any{
			"k3s": map[string]any{"extraArgs": []any{
				map[string]any{"arg": "--disable=metrics-server", "nodeFilters": []any{"server:0"}}}}}},
	}
	if _, warns, err := RenderConfig(context.Background(), "dev", spec, gw, ZotMirror{}); err != nil || len(warns) != 0 {
		t.Fatalf("unrelated k3s extraArgs must not warn: warns=%v err=%v", warns, err)
	}

	spec = config.ClusterSpec{
		Provider: "k3d", KubernetesVersion: "v1.33.1",
		ForProvider: map[string]any{"options": map[string]any{
			"k3s": map[string]any{"extraArgs": []any{
				map[string]any{"arg": "--disable=coredns,traefik", "nodeFilters": []any{"server:0"}}}}}},
	}
	out, warns, err := RenderConfig(context.Background(), "dev", spec, gw, ZotMirror{})
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 1 || warns[0].Code != diag.CodeK3dCoreOverride {
		t.Fatalf("warns = %v", warns)
	}
	if !strings.Contains(warns[0].Message, "coredns,traefik") {
		t.Fatalf("warning must quote the discarded arg, got %q", warns[0].Message)
	}
	var cfg v1alpha5.SimpleConfig
	if err := yaml.Unmarshal(out, &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Options.K3sOptions.ExtraArgs) != 1 || cfg.Options.K3sOptions.ExtraArgs[0].Arg != "--disable=traefik" {
		t.Fatalf("traefik arg must be replaced by --disable=traefik: %+v", cfg.Options.K3sOptions.ExtraArgs)
	}
}

// TestRenderConflictOnDuplicateExtraPort covers conflict-table row 6 (stays
// a hard error): the same host port mapped both in providerConfigRef/
// forProvider and in spec.cluster.extraPorts.
func TestRenderConflictOnDuplicateExtraPort(t *testing.T) {
	spec := config.ClusterSpec{
		Provider:          "k3d",
		KubernetesVersion: "v1.33.1",
		ForProvider: map[string]any{"ports": []any{map[string]any{
			"port": "32222:32222", "nodeFilters": []any{"server:0"}}}},
		ExtraPorts: []config.PortMapping{{HostPort: 32222, NodePort: 32222}},
	}
	_, _, err := RenderConfig(context.Background(), "dev", spec, gw, ZotMirror{})
	de := wantDiag(t, err, "CUBE-1301")
	if !strings.Contains(de.Summary, "providerConfigRef/forProvider") {
		t.Fatalf("duplicate against providerConfigRef/forProvider should mention it, got %q", de.Summary)
	}
}

// TestRenderConflictOnExtraPortReservedForGateway: spec.cluster.extraPorts
// must not re-map the host port cube-idp reserves for the gateway.
func TestRenderConflictOnExtraPortReservedForGateway(t *testing.T) {
	spec := config.ClusterSpec{
		Provider:          "k3d",
		KubernetesVersion: "v1.33.1",
		ExtraPorts:        []config.PortMapping{{HostPort: 8443, NodePort: 30443}},
	}
	_, _, err := RenderConfig(context.Background(), "dev", spec, gw, ZotMirror{})
	de := wantDiag(t, err, "CUBE-1301")
	if !strings.Contains(de.Summary, "reserves for the gateway") {
		t.Fatalf("gateway-port duplicate should blame extraPorts, not providerConfigRef/forProvider; got %q", de.Summary)
	}
}

// TestRenderConflictOnDualRegistryConfig covers conflict-table row 5 (stays
// a hard error): registries.yaml set both via providerConfigRef/forProvider
// (registries.config) and via spec.cluster.registry.
func TestRenderConflictOnDualRegistryConfig(t *testing.T) {
	spec := config.ClusterSpec{
		Provider:          "k3d",
		KubernetesVersion: "v1.33.1",
		ForProvider: map[string]any{"registries": map[string]any{"config": "mirrors:\n" +
			"  docker.io:\n" +
			"    endpoint: [\"https://mirror.other.example\"]\n"}},
		Registry: config.RegistrySpec{Mirrors: map[string]string{"docker.io": "https://mirror.corp.example"}},
	}
	_, _, err := RenderConfig(context.Background(), "dev", spec, gw, ZotMirror{})
	de := wantDiag(t, err, "CUBE-1301")
	if !strings.Contains(de.Summary, "registries.config") {
		t.Fatalf("dual registry config conflict should mention registries.config, got %q", de.Summary)
	}
}

// TestRenderRegistriesConfigZotConflictWarnsAndWins pins conflict-table row
// 5's warning half — the Ensure-path diagnostic: zot.Host is ALWAYS
// non-empty on Ensure, so a user providerConfigRef/forProvider carrying
// registries.config collides with the injected zot mirror even when
// spec.cluster.registry is completely empty. That is a user-vs-core
// conflict: core wins, registries.config is replaced by
// cube-idp's own registriesYAML output, and the warning names the
// discarded block.
func TestRenderRegistriesConfigZotConflictWarnsAndWins(t *testing.T) {
	spec := config.ClusterSpec{
		Provider: "k3d", KubernetesVersion: "v1.33.1",
		ForProvider: map[string]any{"registries": map[string]any{"config": "mirrors:\n" +
			"  docker.io:\n" +
			"    endpoint: [\"https://mirror.other.example\"]\n"}},
	}
	out, warns, err := RenderConfig(context.Background(), "dev", spec, gw, ZotMirror{Host: "registry.cube-idp.localtest.me"})
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 1 || warns[0].Code != diag.CodeK3dCoreOverride {
		t.Fatalf("warns = %v", warns)
	}
	if !strings.Contains(warns[0].Message, "mirror.other.example") {
		t.Fatalf("warning must name the discarded providerConfigRef/forProvider block, got %q", warns[0].Message)
	}
	s := string(out)
	if strings.Contains(s, "mirror.other.example") {
		t.Fatalf("discarded user registries.config must not survive into the output:\n%s", s)
	}
	if !strings.Contains(s, "registry.cube-idp.localtest.me") || !strings.Contains(s, "http://localhost:30500") {
		t.Fatalf("zot mirror entry missing:\n%s", s)
	}
}

// TestRenderKeepsUserRegistriesWithoutZotOrSpecRegistry: on the pure
// render-cluster path (zero ZotMirror) with no spec.cluster.registry, a user
// providerConfigRef/forProvider registries.config has nothing to conflict
// with and must survive the merge untouched.
func TestRenderKeepsUserRegistriesWithoutZotOrSpecRegistry(t *testing.T) {
	spec := config.ClusterSpec{
		Provider: "k3d", KubernetesVersion: "v1.33.1",
		ForProvider: map[string]any{"registries": map[string]any{"config": "mirrors:\n" +
			"  docker.io:\n" +
			"    endpoint: [\"https://mirror.other.example\"]\n"}},
	}
	out, warns, err := RenderConfig(context.Background(), "dev", spec, gw, ZotMirror{})
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 0 {
		t.Fatalf("unexpected warnings: %v", warns)
	}
	if !strings.Contains(string(out), "mirror.other.example") {
		t.Fatalf("user registries.config must be preserved when nothing conflicts:\n%s", out)
	}
}

// TestRenderConfigInjectsZotMirror covers the D12 zot-mirror wiring: a
// non-zero ZotMirror adds a registries.yaml mirror entry for it even when
// spec.cluster.registry is unset.
func TestRenderConfigInjectsZotMirror(t *testing.T) {
	spec := config.ClusterSpec{Provider: "k3d", KubernetesVersion: "v1.33.1"}
	out, _, err := RenderConfig(context.Background(), "dev", spec, gw, ZotMirror{Host: "registry.cube-idp.localtest.me"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "registry.cube-idp.localtest.me") || !strings.Contains(s, "http://localhost:30500") {
		t.Fatalf("zot mirror entry missing:\n%s", s)
	}
}

// TestRenderConfigOmitsZotMirrorWhenZero covers cmd/config.go's render path
// (zero-value ZotMirror): no registries block is injected when neither the
// zot mirror nor spec.cluster.registry is set (mirrors kindp's zero-CertsD
// file-free render).
func TestRenderConfigOmitsZotMirrorWhenZero(t *testing.T) {
	spec := config.ClusterSpec{Provider: "k3d", KubernetesVersion: "v1.33.1"}
	out, _, err := RenderConfig(context.Background(), "dev", spec, gw, ZotMirror{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "registry.cube-idp.localtest.me") {
		t.Fatalf("zero-value ZotMirror must not inject a mirror entry:\n%s", out)
	}
}

// TestRenderConfigMapsHTTPPortWhenSet pins U2's opt-in gateway.httpPort for
// k3d: set → a second ports entry host httpPort -> the plain-HTTP NodePort
// (config.GatewayHTTPNodePort, same host:node syntax as the gateway's
// 8443:30443 mapping); absent → absent, byte-identical output.
func TestRenderConfigMapsHTTPPortWhenSet(t *testing.T) {
	gw := config.GatewaySpec{Pack: "traefik", Host: "cube-idp.localtest.me", Port: 8443, HTTPPort: 8080}
	cfg, _, err := RenderConfig(context.Background(), "dev", config.ClusterSpec{Provider: "k3d"}, gw, ZotMirror{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cfg), "8080:30080") {
		t.Fatalf("http mapping missing:\n%s", cfg)
	}
	// And absent → absent (opt-in contract).
	gw.HTTPPort = 0
	cfg, _, _ = RenderConfig(context.Background(), "dev", config.ClusterSpec{Provider: "k3d"}, gw, ZotMirror{})
	if strings.Contains(string(cfg), "30080") {
		t.Fatalf("httpPort must be opt-in:\n%s", cfg)
	}
}
