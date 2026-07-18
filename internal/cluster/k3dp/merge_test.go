package k3dp

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	out, err := RenderConfig("dev", spec, gw, ZotMirror{})
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
		ProviderConfig:    filepath.Join("testdata", "user-k3d-config.yaml"),
	}
	out, err := RenderConfig("dev", spec, gw, ZotMirror{})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != golden(t, "merged-with-user.yaml") {
		t.Fatalf("golden mismatch:\n--- got ---\n%s", out)
	}
}

func TestRenderConflictOnGatewayPort(t *testing.T) {
	inline := `
apiVersion: k3d.io/v1alpha5
kind: Simple
ports:
  - port: "8443:9999"
    nodeFilters: ["server:0"]
`
	spec := config.ClusterSpec{Provider: "k3d", KubernetesVersion: "v1.33.1", ProviderConfig: inline}
	_, err := RenderConfig("dev", spec, gw, ZotMirror{})
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-1301" {
		t.Fatalf("want CUBE-1301 conflict, got %v", err)
	}
}

func TestRenderConflictOnImage(t *testing.T) {
	inline := `
apiVersion: k3d.io/v1alpha5
kind: Simple
image: rancher/k3s:v1.30.0-k3s1
`
	spec := config.ClusterSpec{Provider: "k3d", KubernetesVersion: "v1.33.1", ProviderConfig: inline}
	_, err := RenderConfig("dev", spec, gw, ZotMirror{})
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-1301" {
		t.Fatalf("want CUBE-1301 image conflict, got %v", err)
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

// TestRenderConflictOnTraefikReenable covers the third of the five documented
// conflict cases: a providerConfig that explicitly re-enables (or otherwise
// touches) k3s's bundled traefik conflicts with cube-idp's gateway pack,
// which owns ingress (D3).
func TestRenderConflictOnTraefikReenable(t *testing.T) {
	inline := `
apiVersion: k3d.io/v1alpha5
kind: Simple
options:
  k3s:
    extraArgs:
      - arg: "--disable=metrics-server"
        nodeFilters: ["server:0"]
`
	// Not actually a traefik arg — this must NOT conflict (only traefik-related
	// args do); establishes the baseline before the real conflict case below.
	spec := config.ClusterSpec{Provider: "k3d", KubernetesVersion: "v1.33.1", ProviderConfig: inline}
	if _, err := RenderConfig("dev", spec, gw, ZotMirror{}); err != nil {
		t.Fatalf("unrelated k3s extraArgs must not conflict: %v", err)
	}

	inline = `
apiVersion: k3d.io/v1alpha5
kind: Simple
options:
  k3s:
    extraArgs:
      - arg: "--disable=coredns,traefik"
        nodeFilters: ["server:0"]
`
	spec = config.ClusterSpec{Provider: "k3d", KubernetesVersion: "v1.33.1", ProviderConfig: inline}
	_, err := RenderConfig("dev", spec, gw, ZotMirror{})
	de := wantDiag(t, err, "CUBE-1301")
	if !strings.Contains(de.Summary, "traefik") {
		t.Fatalf("summary should mention traefik, got %q", de.Summary)
	}
}

// TestRenderConflictOnDuplicateExtraPort covers the fourth documented
// conflict case: the same host port mapped both in providerConfig and in
// spec.cluster.extraPorts.
func TestRenderConflictOnDuplicateExtraPort(t *testing.T) {
	inline := `
apiVersion: k3d.io/v1alpha5
kind: Simple
ports:
  - port: "32222:32222"
    nodeFilters: ["server:0"]
`
	spec := config.ClusterSpec{
		Provider:          "k3d",
		KubernetesVersion: "v1.33.1",
		ProviderConfig:    inline,
		ExtraPorts:        []config.PortMapping{{HostPort: 32222, NodePort: 32222}},
	}
	_, err := RenderConfig("dev", spec, gw, ZotMirror{})
	de := wantDiag(t, err, "CUBE-1301")
	if !strings.Contains(de.Summary, "providerConfig") {
		t.Fatalf("duplicate against providerConfig should mention providerConfig, got %q", de.Summary)
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
	_, err := RenderConfig("dev", spec, gw, ZotMirror{})
	de := wantDiag(t, err, "CUBE-1301")
	if !strings.Contains(de.Summary, "reserves for the gateway") {
		t.Fatalf("gateway-port duplicate should blame extraPorts, not providerConfig; got %q", de.Summary)
	}
}

// TestRenderConflictOnDualRegistryConfig covers the fifth documented conflict
// case: registries.yaml set both via providerConfig (registries.config) and
// via spec.cluster.registry.
func TestRenderConflictOnDualRegistryConfig(t *testing.T) {
	inline := `
apiVersion: k3d.io/v1alpha5
kind: Simple
registries:
  config: |
    mirrors:
      docker.io:
        endpoint: ["https://mirror.other.example"]
`
	spec := config.ClusterSpec{
		Provider:          "k3d",
		KubernetesVersion: "v1.33.1",
		ProviderConfig:    inline,
		Registry:          config.RegistrySpec{Mirrors: map[string]string{"docker.io": "https://mirror.corp.example"}},
	}
	_, err := RenderConfig("dev", spec, gw, ZotMirror{})
	de := wantDiag(t, err, "CUBE-1301")
	if !strings.Contains(de.Summary, "registries.config") {
		t.Fatalf("dual registry config conflict should mention registries.config, got %q", de.Summary)
	}
}

// TestRenderConflictOnUserRegistriesWithZotMirror pins the Ensure-path
// diagnostic: zot.Host is ALWAYS non-empty on Ensure, so a user
// providerConfig carrying registries.config collides with the injected zot
// mirror even when spec.cluster.registry is completely empty. The conflict
// must still be rejected (registries.config is an opaque blob we can't merge
// the zot entry into without parsing), but the message must name the injected
// zot mirror requirement — NOT misdiagnose it as "set both in providerConfig
// and spec.cluster.registry", pointing at a field the user never set.
func TestRenderConflictOnUserRegistriesWithZotMirror(t *testing.T) {
	inline := `
apiVersion: k3d.io/v1alpha5
kind: Simple
registries:
  config: |
    mirrors:
      docker.io:
        endpoint: ["https://mirror.other.example"]
`
	spec := config.ClusterSpec{Provider: "k3d", KubernetesVersion: "v1.33.1", ProviderConfig: inline}
	_, err := RenderConfig("dev", spec, gw, ZotMirror{Host: "registry.cube-idp.localtest.me"})
	de := wantDiag(t, err, "CUBE-1301")
	if !strings.Contains(de.Summary, "zot") {
		t.Fatalf("summary must name the injected zot mirror as the conflicting requirement, got %q", de.Summary)
	}
	if strings.Contains(de.Summary, "spec.cluster.registry") {
		t.Fatalf("summary must not blame spec.cluster.registry (the user never set it), got %q", de.Summary)
	}
}

// TestRenderKeepsUserRegistriesWithoutZotOrSpecRegistry: on the pure
// render-cluster path (zero ZotMirror) with no spec.cluster.registry, a user
// providerConfig registries.config has nothing to conflict with and must
// survive the merge untouched.
func TestRenderKeepsUserRegistriesWithoutZotOrSpecRegistry(t *testing.T) {
	inline := `
apiVersion: k3d.io/v1alpha5
kind: Simple
registries:
  config: |
    mirrors:
      docker.io:
        endpoint: ["https://mirror.other.example"]
`
	spec := config.ClusterSpec{Provider: "k3d", KubernetesVersion: "v1.33.1", ProviderConfig: inline}
	out, err := RenderConfig("dev", spec, gw, ZotMirror{})
	if err != nil {
		t.Fatal(err)
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
	out, err := RenderConfig("dev", spec, gw, ZotMirror{Host: "registry.cube-idp.localtest.me"})
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
	out, err := RenderConfig("dev", spec, gw, ZotMirror{})
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
// 8443:30443 mapping); absent → absent, byte-identical output (decision 3).
func TestRenderConfigMapsHTTPPortWhenSet(t *testing.T) {
	gw := config.GatewaySpec{Pack: "traefik", Host: "cube-idp.localtest.me", Port: 8443, HTTPPort: 8080}
	cfg, err := RenderConfig("dev", config.ClusterSpec{Provider: "k3d"}, gw, ZotMirror{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cfg), "8080:30080") {
		t.Fatalf("http mapping missing:\n%s", cfg)
	}
	// And absent → absent (opt-in contract).
	gw.HTTPPort = 0
	cfg, _ = RenderConfig("dev", config.ClusterSpec{Provider: "k3d"}, gw, ZotMirror{})
	if strings.Contains(string(cfg), "30080") {
		t.Fatalf("httpPort must be opt-in:\n%s", cfg)
	}
}
