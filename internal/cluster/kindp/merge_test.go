package kindp

import (
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
	out, err := RenderConfig("dev", spec, gw, CertsD{})
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
		ProviderConfig:    filepath.Join("testdata", "user-kind-config.yaml"),
	}
	out, err := RenderConfig("dev", spec, gw, CertsD{})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != golden(t, "merged-with-user.yaml") {
		t.Fatalf("golden mismatch:\n--- got ---\n%s", out)
	}
}

func TestRenderConflictOnGatewayPort(t *testing.T) {
	inline := `
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
  extraPortMappings:
  - containerPort: 9999
    hostPort: 8443
`
	spec := config.ClusterSpec{Provider: "kind", KubernetesVersion: "v1.33.1", ProviderConfig: inline}
	_, err := RenderConfig("dev", spec, gw, CertsD{})
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != "CUBE-1201" {
		t.Fatalf("want CUBE-1201 conflict, got %v", err)
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

func TestRenderConflictOnNodeImage(t *testing.T) {
	inline := `
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
  image: kindest/node:v1.30.0
`
	spec := config.ClusterSpec{Provider: "kind", KubernetesVersion: "v1.33.1", ProviderConfig: inline}
	_, err := RenderConfig("dev", spec, gw, CertsD{})
	wantDiag(t, err, "CUBE-1201")
}

func TestRenderConflictOnDuplicateExtraPort(t *testing.T) {
	inline := `
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
  extraPortMappings:
  - containerPort: 32222
    hostPort: 32222
`
	spec := config.ClusterSpec{
		Provider:          "kind",
		KubernetesVersion: "v1.33.1",
		ProviderConfig:    inline,
		ExtraPorts:        []config.PortMapping{{HostPort: 32222, NodePort: 32222}},
	}
	_, err := RenderConfig("dev", spec, gw, CertsD{})
	de := wantDiag(t, err, "CUBE-1201")
	if !strings.Contains(de.Summary, "providerConfig") {
		t.Fatalf("duplicate against providerConfig should mention providerConfig, got %q", de.Summary)
	}
}

func TestRenderConflictOnExtraPortReservedForGateway(t *testing.T) {
	spec := config.ClusterSpec{
		Provider:          "kind",
		KubernetesVersion: "v1.33.1",
		ExtraPorts:        []config.PortMapping{{HostPort: 8443, NodePort: 30443}},
	}
	_, err := RenderConfig("dev", spec, gw, CertsD{})
	de := wantDiag(t, err, "CUBE-1201")
	if !strings.Contains(de.Summary, "reserves for the gateway") {
		t.Fatalf("gateway-port duplicate should blame extraPorts, not providerConfig; got %q", de.Summary)
	}
}

func TestRenderProviderConfigFileMissing(t *testing.T) {
	spec := config.ClusterSpec{
		Provider:          "kind",
		KubernetesVersion: "v1.33.1",
		ProviderConfig:    filepath.Join("testdata", "does-not-exist.yaml"),
	}
	_, err := RenderConfig("dev", spec, gw, CertsD{})
	wantDiag(t, err, "CUBE-1202")
}

func TestRenderProviderConfigInvalidYAML(t *testing.T) {
	inline := "kind: Cluster\nnodes: {not: [a, valid, kind, document\n"
	spec := config.ClusterSpec{Provider: "kind", KubernetesVersion: "v1.33.1", ProviderConfig: inline}
	_, err := RenderConfig("dev", spec, gw, CertsD{})
	wantDiag(t, err, "CUBE-1202")
}

func TestRenderInjectsOnControlPlaneNotFirstNode(t *testing.T) {
	inline := `
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: worker
- role: control-plane
`
	spec := config.ClusterSpec{
		Provider:          "kind",
		KubernetesVersion: "v1.33.1",
		ExtraPorts:        []config.PortMapping{{HostPort: 32222, NodePort: 32222}},
		Mounts:            []config.Mount{{HostPath: "/tmp/images", NodePath: "/var/lib/images"}},
		ProviderConfig:    inline,
	}
	out, err := RenderConfig("dev", spec, gw, CertsD{})
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
	out, err := RenderConfig("dev", spec, gw, CertsD{})
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
	out, err := RenderConfig("dev", spec, gw, CertsD{Host: "registry.cube-idp.localtest.me", HostDir: "/tmp/certsd"})
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
	inline := `
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: worker
- role: worker
`
	spec := config.ClusterSpec{Provider: "kind", KubernetesVersion: "v1.33.1", ProviderConfig: inline}
	_, err := RenderConfig("dev", spec, gw, CertsD{})
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
	cfg, err := RenderConfig("dev", config.ClusterSpec{Provider: "kind"}, gw, CertsD{})
	if err != nil {
		t.Fatal(err)
	}
	s := string(cfg)
	if !strings.Contains(s, "hostPort: 8080") || !strings.Contains(s, "containerPort: 30080") {
		t.Fatalf("http mapping missing:\n%s", s)
	}
	// And absent → absent (opt-in contract).
	gw.HTTPPort = 0
	cfg, _ = RenderConfig("dev", config.ClusterSpec{Provider: "kind"}, gw, CertsD{})
	if strings.Contains(string(cfg), "30080") {
		t.Fatalf("httpPort must be opt-in:\n%s", cfg)
	}
}
