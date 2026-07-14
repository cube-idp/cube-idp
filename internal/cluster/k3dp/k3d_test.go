package k3dp

import (
	"strconv"
	"testing"

	v1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
)

// TestEnsureExposedAPIPort guards the k3d kubeconfig-port bug (Phase 3 e2e,
// TestK3dUpDown): our library-direct create path skips the k3d CLI's
// random-free-port assignment, so ExposeAPI.HostPort stays "", the server
// node's k3d.server.api.port label is baked empty, and KubeconfigGet emits
// https://0.0.0.0 with NO port (dialing 443 → connection refused). The fix
// assigns a free host port when the user left it unset, mirroring
// cmd/cluster/clusterCreate.go's "Set to random port if port is empty string".
func TestEnsureExposedAPIPort(t *testing.T) {
	t.Run("assigns a free port when unset", func(t *testing.T) {
		var cfg v1alpha5.SimpleConfig
		if err := ensureExposedAPIPort(&cfg); err != nil {
			t.Fatalf("ensureExposedAPIPort: %v", err)
		}
		if cfg.ExposeAPI.HostPort == "" {
			t.Fatal("HostPort still empty: kubeconfig would carry https://0.0.0.0 with no port")
		}
		p, err := strconv.Atoi(cfg.ExposeAPI.HostPort)
		if err != nil || p <= 0 || p > 65535 {
			t.Fatalf("HostPort %q is not a valid TCP port", cfg.ExposeAPI.HostPort)
		}
	})

	t.Run("preserves a user-provided port", func(t *testing.T) {
		cfg := v1alpha5.SimpleConfig{}
		cfg.ExposeAPI.HostPort = "6550"
		if err := ensureExposedAPIPort(&cfg); err != nil {
			t.Fatalf("ensureExposedAPIPort: %v", err)
		}
		if cfg.ExposeAPI.HostPort != "6550" {
			t.Fatalf("user-provided HostPort overwritten: got %q, want 6550", cfg.ExposeAPI.HostPort)
		}
	})
}
