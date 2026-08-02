package cli

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
	"github.com/cube-idp/cube-idp/internal/cluster"
)

// execDelete executes delete against the injected mock provisioner,
// pointing --kubeconfig inside dir so nothing touches the user's file.
func execDelete(t *testing.T, dir string) (code int, stdout, stderr string) {
	t.Helper()
	root := newRootCmd(func(v1alpha1.ClusterProvider) (cluster.Provisioner, error) {
		return mockProvisioner{}, nil
	})
	var out, errBuf bytes.Buffer
	code = execute(t.Context(), root, []string{
		"delete", "-f", filepath.Join(dir, "cube.yaml"),
		"--kubeconfig", filepath.Join(dir, "kubeconfig"),
	}, &out, &errBuf)
	return code, out.String(), errBuf.String()
}

func writeConfig(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "cube.yaml"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

const clusterConfigYAML = `apiVersion: cube-idp.dev/v1alpha1
kind: Config
metadata:
  name: dev
spec:
  cluster:
    provider: kind
`

func TestDeleteRemovesKubeconfigContext(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeConfig(t, dir, clusterConfigYAML)
	if code, _, stderr := execInit(t, dir); code != 0 {
		t.Fatalf("init exit = %d, stderr: %s", code, stderr)
	}

	code, stdout, stderr := execDelete(t, dir)
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	if want := `kubeconfig context "cube-idp.dev/dev" removed`; !strings.Contains(stdout, want) {
		t.Fatalf("stdout missing %q:\n%s", want, stdout)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "kubeconfig"))
	if err != nil {
		t.Fatalf("kubeconfig must survive delete — never unlinked: %v", err)
	}
	if strings.Contains(string(raw), "cube-idp.dev/dev") {
		t.Fatalf("kubeconfig still contains cube context:\n%s", raw)
	}
}

func TestDeleteWithoutInstalledKubeconfig(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeConfig(t, dir, clusterConfigYAML)

	code, stdout, stderr := execDelete(t, dir)
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	if want := "no kubeconfig changes needed"; !strings.Contains(stdout, want) {
		t.Fatalf("stdout missing %q:\n%s", want, stdout)
	}
	if _, err := os.Stat(filepath.Join(dir, "kubeconfig")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("delete created a kubeconfig file")
	}
}

// TestDeleteMissingConfigDoesNotScaffold: unlike init, delete never
// creates a config — a missing file is the loader's coded error.
func TestDeleteMissingConfigDoesNotScaffold(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	code, _, stderr := execDelete(t, dir)
	if code != 2 {
		t.Fatalf("exit = %d, want 2; stderr: %s", code, stderr)
	}
	if !strings.Contains(stderr, "CUBE-CFG-004") {
		t.Fatalf("stderr missing CUBE-CFG-004:\n%s", stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "cube.yaml")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("delete scaffolded a config file")
	}
}

func TestDeleteNoClusterConfigured(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeConfig(t, dir, `apiVersion: cube-idp.dev/v1alpha1
kind: Config
metadata:
  name: dev
`)

	code, _, stderr := execDelete(t, dir)
	if code != 1 {
		t.Fatalf("exit = %d, want 1; stderr: %s", code, stderr)
	}
	if !strings.Contains(stderr, "CUBE-CLU-001") {
		t.Fatalf("stderr missing CUBE-CLU-001:\n%s", stderr)
	}
}
