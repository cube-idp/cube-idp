package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
	"github.com/cube-idp/cube-idp/internal/cluster"
)

// TestConfigValidateSkipsWithoutCapability pins SpecValidator down as
// optional: a driver that does not implement it validates nothing, so a
// payload only the driver could reject passes `config validate`.
func TestConfigValidateSkipsWithoutCapability(t *testing.T) {
	t.Parallel() // no package state mutated: the factory is injected
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "cube.yaml")
	doc := `apiVersion: cube-idp.dev/v1alpha1
kind: Config
metadata:
  name: dev
spec:
  cluster:
    provider: kind
    forProvider:
      notAKindField: true
`
	if err := os.WriteFile(cfgPath, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}

	// mockProvisioner implements Provisioner only — not SpecValidator.
	root := newRootCmd(func(v1alpha1.ClusterProvider) (cluster.Provisioner, error) {
		return mockProvisioner{}, nil
	})
	var stdout, stderr bytes.Buffer
	code := execute(t.Context(), root, []string{"config", "validate", "-f", cfgPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 — a driver without SpecValidator must not fail validate (stderr: %s)", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "valid") {
		t.Errorf("stdout %q should confirm validity", stdout.String())
	}
}
