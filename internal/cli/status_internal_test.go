package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
	"github.com/cube-idp/cube-idp/internal/cluster"
)

// absentClusterProvisioner reports the cluster as not existing; the
// embedded mock answers everything else.
type absentClusterProvisioner struct{ mockProvisioner }

func (absentClusterProvisioner) Exists(context.Context, string) (bool, error) { return false, nil }

const statusClusterYAML = `apiVersion: cube-idp.dev/v1alpha1
kind: Config
metadata:
  name: dev
spec:
  cluster:
    provider: kind
`

// execStatus executes status with an injected provisioner, pointing
// --kubeconfig inside dir so nothing touches the user's file.
func execStatus(t *testing.T, dir string, p cluster.Provisioner) (code int, stdout, stderr string) {
	t.Helper()
	root := newRootCmd(func(v1alpha1.ClusterProvider) (cluster.Provisioner, error) {
		return p, nil
	})
	var out, errBuf bytes.Buffer
	code = execute(t.Context(), root, []string{
		"status", "-f", filepath.Join(dir, "cube.yaml"),
		"--kubeconfig", filepath.Join(dir, "kubeconfig"),
	}, &out, &errBuf)
	return code, out.String(), errBuf.String()
}

func TestStatusReportsInstalled(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cube.yaml"), []byte(statusClusterYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := execCreate(t, dir); code != 0 {
		t.Fatalf("create exit = %d, stderr: %s", code, stderr)
	}

	code, stdout, stderr := execStatus(t, dir, mockProvisioner{})
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	for _, want := range []string{
		`cluster "dev": exists`,
		`kubeconfig context "cube-idp.dev/dev": installed in ` + filepath.Join(dir, "kubeconfig"),
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout)
		}
	}
}

// TestStatusReportsAbsent: an absent cluster and uninstalled context
// are findings — exit 0, not an error.
func TestStatusReportsAbsent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cube.yaml"), []byte(statusClusterYAML), 0o600); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := execStatus(t, dir, absentClusterProvisioner{})
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	for _, want := range []string{
		`cluster "dev": not found`,
		`kubeconfig context "cube-idp.dev/dev": not installed`,
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout)
		}
	}
}

func TestStatusMissingConfig(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	code, _, stderr := execStatus(t, dir, mockProvisioner{})
	if code != 2 {
		t.Fatalf("exit = %d, want 2; stderr: %s", code, stderr)
	}
	if !strings.Contains(stderr, "CUBE-CFG-004") {
		t.Fatalf("stderr missing CUBE-CFG-004:\n%s", stderr)
	}
}

func TestStatusNoClusterConfigured(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cube.yaml"), []byte(`apiVersion: cube-idp.dev/v1alpha1
kind: Config
metadata:
  name: dev
`), 0o600); err != nil {
		t.Fatal(err)
	}

	code, _, stderr := execStatus(t, dir, mockProvisioner{})
	if code != 1 {
		t.Fatalf("exit = %d, want 1; stderr: %s", code, stderr)
	}
	if !strings.Contains(stderr, "CUBE-CLU-001") {
		t.Fatalf("stderr missing CUBE-CLU-001:\n%s", stderr)
	}
}
