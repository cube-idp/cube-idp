package kind

import (
	"errors"
	"os"
	"os/exec"
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	v1alpha4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"

	"github.com/cube-idp/cube-idp/internal/cluster"
	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

// TestValidateSpec is hermetic: construction defers runtime detection,
// so the pure capability must work without Docker — in the green gate.
func TestValidateSpec(t *testing.T) {
	tests := []struct {
		name     string
		raw      string // "" = no forProvider payload
		emptyRaw bool   // present RawExtension with zero-length Raw
		wantCode cubeerr.Code
	}{
		{name: "absent payload", wantCode: ""},
		{name: "present payload, empty raw", emptyRaw: true, wantCode: ""},
		{name: "empty object", raw: "{}", wantCode: ""},
		{name: "valid v1alpha4 payload", raw: `{"nodes":[{"role":"control-plane"}]}`, wantCode: ""},
		{name: "unknown field", raw: `{"bogus":1}`, wantCode: cluster.CodeInvalidForProvider},
		{name: "non-object payload", raw: `[1]`, wantCode: cluster.CodeInvalidForProvider},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := New()
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			spec := cluster.Spec{Name: "dev"}
			switch {
			case tt.emptyRaw:
				spec.ForProvider = &runtime.RawExtension{}
			case tt.raw != "":
				spec.ForProvider = &runtime.RawExtension{Raw: []byte(tt.raw)}
			}
			err = p.ValidateSpec(spec)

			if tt.wantCode == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			var coded *cubeerr.Coded
			if !errors.As(err, &coded) {
				t.Fatalf("error %v is not a *cubeerr.Coded", err)
			}
			if coded.Code != tt.wantCode {
				t.Errorf("code = %s, want %s", coded.Code, tt.wantCode)
			}
		})
	}
}

// TestClusterConfig pins the three create-time branches of the config
// selection — absent payload defaults, `{}` opts out, non-empty is used
// unmerged — hermetically: no container runtime is touched.
func TestClusterConfig(t *testing.T) {
	tests := []struct {
		name        string
		forProvider *runtime.RawExtension
		wantNodes   int
		wantDefault bool // ingress-ready label + 8080/8443 mappings present
	}{
		{"absent: nil forProvider", nil, 1, true},
		{"absent: empty raw", &runtime.RawExtension{}, 1, true},
		{"explicit opt-out: {}", &runtime.RawExtension{Raw: []byte("{}")}, 0, false},
		{
			"explicit custom payload",
			&runtime.RawExtension{Raw: []byte(`{"nodes":[{"role":"control-plane"},{"role":"worker"}]}`)},
			2, false,
		},
		{
			"explicit custom port mapping",
			&runtime.RawExtension{Raw: []byte(`{"nodes":[{"role":"control-plane","extraPortMappings":[{"containerPort":80,"hostPort":9090}]}]}`)},
			1, false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := clusterConfig(cluster.Spec{Name: "dev", ForProvider: tt.forProvider})
			if err != nil {
				t.Fatalf("clusterConfig: %v", err)
			}
			if len(cfg.Nodes) != tt.wantNodes {
				t.Fatalf("nodes = %d, want %d", len(cfg.Nodes), tt.wantNodes)
			}
			if !tt.wantDefault {
				assertNoIngressDefaults(t, cfg)
				return
			}
			assertIngressReadyDefault(t, cfg)
		})
	}
}

// assertIngressReadyDefault checks the pinned generated shape: a single
// control-plane node labeled ingress-ready=true with both TCP mappings.
func assertIngressReadyDefault(t *testing.T, cfg *v1alpha4.Cluster) {
	t.Helper()
	n := cfg.Nodes[0]
	if n.Role != v1alpha4.ControlPlaneRole {
		t.Errorf("role = %q, want %q", n.Role, v1alpha4.ControlPlaneRole)
	}
	if got := n.Labels["ingress-ready"]; got != "true" {
		t.Errorf("ingress-ready label = %q, want \"true\"", got)
	}
	want := []v1alpha4.PortMapping{
		{ContainerPort: 80, HostPort: 8080, Protocol: v1alpha4.PortMappingProtocolTCP},
		{ContainerPort: 443, HostPort: 8443, Protocol: v1alpha4.PortMappingProtocolTCP},
	}
	if !reflect.DeepEqual(n.ExtraPortMappings, want) {
		t.Errorf("extraPortMappings = %+v, want %+v", n.ExtraPortMappings, want)
	}
}

// assertNoIngressDefaults checks that nothing from the default was
// merged into an explicit payload: no ingress-ready label anywhere, and
// no 8080/8443 host binding the user did not ask for.
func assertNoIngressDefaults(t *testing.T, cfg *v1alpha4.Cluster) {
	t.Helper()
	for i, n := range cfg.Nodes {
		if _, ok := n.Labels["ingress-ready"]; ok {
			t.Errorf("node %d: ingress-ready label was merged into an explicit payload", i)
		}
		for _, pm := range n.ExtraPortMappings {
			if pm.HostPort == 8080 || pm.HostPort == 8443 {
				t.Errorf("node %d: default host port %d was merged into an explicit payload", i, pm.HostPort)
			}
		}
	}
}

// TestClusterConfigInvalidPayload keeps the decode error path on the
// create-time helper, not only on ValidateSpec.
func TestClusterConfigInvalidPayload(t *testing.T) {
	spec := cluster.Spec{Name: "dev", ForProvider: &runtime.RawExtension{Raw: []byte(`{"bogus":1}`)}}
	_, err := clusterConfig(spec)

	var coded *cubeerr.Coded
	if !errors.As(err, &coded) {
		t.Fatalf("error %v is not a *cubeerr.Coded", err)
	}
	if coded.Code != cluster.CodeInvalidForProvider {
		t.Errorf("code = %s, want %s", coded.Code, cluster.CodeInvalidForProvider)
	}
}

// TestConformance runs the seam suite against real Docker. Since the
// ingress-ready default landed, each created cluster binds host 8080 and
// 8443 — a create failure on an occupied port is the environment, not a
// driver regression.
func TestConformance(t *testing.T) {
	if os.Getenv("CUBE_E2E") != "1" {
		t.Skip("kind e2e is opt-in: run via `make test-e2e` (sets CUBE_E2E=1)")
	}
	if !runtimeAvailable() {
		t.Skip("no container runtime reachable (docker/podman) — skipping kind e2e")
	}
	cluster.RunClusterConformance(t, func() cluster.Provisioner {
		p, err := New()
		if err != nil {
			t.Fatalf("kind.New: %v", err)
		}
		return p
	})
}

func runtimeAvailable() bool {
	for _, rt := range []string{"docker", "podman"} {
		if exec.Command(rt, "info").Run() == nil {
			return true
		}
	}
	return false
}
