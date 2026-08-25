package cli

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
	"github.com/cube-idp/cube-idp/internal/cluster"
)

type mockProvisioner struct{}

func (mockProvisioner) Ensure(context.Context, cluster.Spec) error   { return nil }
func (mockProvisioner) Exists(context.Context, string) (bool, error) { return true, nil }
func (mockProvisioner) Delete(context.Context, string) error         { return nil }
func (mockProvisioner) Kubeconfig(_ context.Context, name string) ([]byte, error) {
	kc := `apiVersion: v1
kind: Config
clusters:
  - name: kind-NAME
    cluster:
      server: https://127.0.0.1:6443
contexts:
  - name: kind-NAME
    context:
      cluster: kind-NAME
      user: kind-NAME
users:
  - name: kind-NAME
    user:
      token: fake
current-context: kind-NAME
`
	return []byte(strings.ReplaceAll(kc, "NAME", name)), nil
}

const createClusterYAML = `apiVersion: cube-idp.dev/v1alpha1
kind: Config
metadata:
  name: dev
spec:
  cluster:
    provider: kind
`

// execCreate executes create against the injected mock provisioner,
// pointing --kubeconfig inside dir so nothing touches the user's file.
func execCreate(t *testing.T, dir string) (code int, stdout, stderr string) {
	t.Helper()
	root := newRootCmd(func(v1alpha1.ClusterProvider) (cluster.Provisioner, error) {
		return mockProvisioner{}, nil
	}, defaultEngine)
	var out, errBuf bytes.Buffer
	code = execute(t.Context(), root, []string{
		"create", "-f", filepath.Join(dir, "cube.yaml"),
		"--kubeconfig", filepath.Join(dir, "kubeconfig"),
	}, &out, &errBuf)
	return code, out.String(), errBuf.String()
}

func TestCreateWritesKubeconfig(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cube.yaml"), []byte(createClusterYAML), 0o600); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := execCreate(t, dir)
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	if want := `cluster "dev" ready — kubeconfig context "cube-idp.dev/dev" installed`; !strings.Contains(stdout, want) {
		t.Fatalf("stdout missing %q:\n%s", want, stdout)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "kubeconfig"))
	if err != nil {
		t.Fatalf("kubeconfig not written: %v", err)
	}
	if !strings.Contains(string(raw), "cube-idp.dev/dev") {
		t.Fatalf("kubeconfig missing cube context:\n%s", raw)
	}
}

// TestCreateMissingConfigDoesNotScaffold: unlike init, create never
// creates a config — a missing file is the loader's coded error.
func TestCreateMissingConfigDoesNotScaffold(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	code, _, stderr := execCreate(t, dir)
	if code != 2 {
		t.Fatalf("exit = %d, want 2; stderr: %s", code, stderr)
	}
	if !strings.Contains(stderr, "CUBE-CFG-004") {
		t.Fatalf("stderr missing CUBE-CFG-004:\n%s", stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "cube.yaml")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("create scaffolded a config file")
	}
}

func TestCreateNoClusterConfigured(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cube.yaml"), []byte(`apiVersion: cube-idp.dev/v1alpha1
kind: Config
metadata:
  name: dev
`), 0o600); err != nil {
		t.Fatal(err)
	}

	code, _, stderr := execCreate(t, dir)
	if code != 1 {
		t.Fatalf("exit = %d, want 1; stderr: %s", code, stderr)
	}
	if !strings.Contains(stderr, "CUBE-CLU-001") {
		t.Fatalf("stderr missing CUBE-CLU-001:\n%s", stderr)
	}
}
