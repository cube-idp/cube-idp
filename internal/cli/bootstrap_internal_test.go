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

const bootstrapConfigYAML = `apiVersion: cube-idp.dev/v1alpha1
kind: Config
metadata:
  name: dev
spec:
  cluster:
    provider: kind
`

// execBootstrap runs the bootstrap verb with an injected provisioner, keeping
// --kubeconfig inside dir so nothing touches the user's file. The full apply +
// wait against a real API server is exercised by make test-e2e (T8); these
// unit rows cover the pre-cluster edge failures.
func execBootstrap(t *testing.T, dir string, p cluster.Provisioner, extraArgs ...string) (code int, stdout, stderr string) {
	t.Helper()
	root := newRootCmd(func(v1alpha1.ClusterProvider) (cluster.Provisioner, error) {
		return p, nil
	})
	args := append([]string{
		"bootstrap", "-f", filepath.Join(dir, "cube.yaml"),
		"--kubeconfig", filepath.Join(dir, "kubeconfig"),
	}, extraArgs...)
	var out, errBuf bytes.Buffer
	code = execute(t.Context(), root, args, &out, &errBuf)
	return code, out.String(), errBuf.String()
}

func TestBootstrapMissingConfig(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	code, _, stderr := execBootstrap(t, dir, mockProvisioner{})
	if code != 2 {
		t.Fatalf("exit = %d, want 2; stderr: %s", code, stderr)
	}
	if !strings.Contains(stderr, "CUBE-CFG-004") {
		t.Fatalf("stderr missing CUBE-CFG-004:\n%s", stderr)
	}
}

func TestBootstrapNoClusterConfigured(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cube.yaml"), []byte(`apiVersion: cube-idp.dev/v1alpha1
kind: Config
metadata:
  name: dev
`), 0o600); err != nil {
		t.Fatal(err)
	}

	code, _, stderr := execBootstrap(t, dir, mockProvisioner{})
	if code != 1 {
		t.Fatalf("exit = %d, want 1; stderr: %s", code, stderr)
	}
	if !strings.Contains(stderr, "CUBE-CLU-001") {
		t.Fatalf("stderr missing CUBE-CLU-001:\n%s", stderr)
	}
}

// TestBootstrapKubeconfigMissing: a configured cluster whose kubeconfig target
// cannot be read fails at the edge before any apply — CUBE-CLU-005, exit 1.
func TestBootstrapKubeconfigMissing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cube.yaml"), []byte(bootstrapConfigYAML), 0o600); err != nil {
		t.Fatal(err)
	}

	// mockProvisioner reports the cluster as existing, but no kubeconfig file
	// was written into dir, so the edge read fails.
	code, _, stderr := execBootstrap(t, dir, mockProvisioner{})
	if code != 1 {
		t.Fatalf("exit = %d, want 1; stderr: %s", code, stderr)
	}
	if !strings.Contains(stderr, "CUBE-CLU-005") {
		t.Fatalf("stderr missing CUBE-CLU-005:\n%s", stderr)
	}
}
