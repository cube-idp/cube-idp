package kind

import (
	"errors"
	"os"
	"os/exec"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"

	"github.com/cube-idp/cube-idp/internal/cluster"
	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

// TestValidateSpec is hermetic: construction defers runtime detection,
// so the pure capability must work without Docker — in the green gate.
func TestValidateSpec(t *testing.T) {
	tests := []struct {
		name     string
		raw      string // "" = no forProvider payload
		wantCode cubeerr.Code
	}{
		{"absent payload", "", ""},
		{"empty object", "{}", ""},
		{"valid v1alpha4 payload", `{"nodes":[{"role":"control-plane"}]}`, ""},
		{"unknown field", `{"bogus":1}`, cluster.CodeInvalidForProvider},
		{"non-object payload", `[1]`, cluster.CodeInvalidForProvider},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := New()
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			spec := cluster.Spec{Name: "dev"}
			if tt.raw != "" {
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
