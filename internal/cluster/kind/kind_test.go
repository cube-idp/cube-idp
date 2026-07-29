package kind

import (
	"os"
	"os/exec"
	"testing"

	"github.com/cube-idp/cube-idp/internal/cluster"
)

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
