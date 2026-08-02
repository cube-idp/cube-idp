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

func TestInitWritesKubeconfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "cube.yaml")
	cfgYAML := `apiVersion: cube-idp.dev/v1alpha1
kind: Config
metadata:
  name: dev
spec:
  cluster:
    provider: kind
`
	if err := os.WriteFile(cfgPath, []byte(cfgYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	kubeconfigPath := filepath.Join(dir, "kubeconfig")

	restore := newProvisioner
	newProvisioner = func(v1alpha1.ClusterProvider) (cluster.Provisioner, error) {
		return mockProvisioner{}, nil
	}
	defer func() { newProvisioner = restore }()

	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(),
		[]string{"init", "-f", cfgPath, "--kubeconfig", kubeconfigPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr.String())
	}
	raw, err := os.ReadFile(kubeconfigPath)
	if err != nil {
		t.Fatalf("kubeconfig not written: %v", err)
	}
	if !strings.Contains(string(raw), "cube-idp.dev/dev") {
		t.Fatalf("kubeconfig missing cube context:\n%s", raw)
	}
}
