package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
	"github.com/cube-idp/cube-idp/internal/cluster"
	"github.com/cube-idp/cube-idp/internal/config"
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
	t.Parallel() // no package state mutated: the factory is injected
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

	root := newRootCmd(func(v1alpha1.ClusterProvider) (cluster.Provisioner, error) {
		return mockProvisioner{}, nil
	})

	var stdout, stderr bytes.Buffer
	code := execute(t.Context(), root,
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

// execInit executes init against an injected mock provisioner, pointing
// --kubeconfig inside dir so nothing touches the user's kubeconfig.
func execInit(t *testing.T, dir string, extraArgs ...string) (code int, stdout, stderr string) {
	t.Helper()
	root := newRootCmd(func(v1alpha1.ClusterProvider) (cluster.Provisioner, error) {
		return mockProvisioner{}, nil
	})
	args := append([]string{
		"init", "-f", filepath.Join(dir, "cube.yaml"),
		"--kubeconfig", filepath.Join(dir, "kubeconfig"),
	}, extraArgs...)
	var out, errBuf bytes.Buffer
	code = execute(t.Context(), root, args, &out, &errBuf)
	return code, out.String(), errBuf.String()
}

func TestInitScaffoldsMissingConfig(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		args     []string
		wantName *regexp.Regexp // must match the scaffolded metadata.name
	}{
		{"generated name", nil, regexp.MustCompile(`^[a-z]+-[a-z]+$`)},
		{"explicit --name", []string{"--name", "my-cube"}, regexp.MustCompile(`^my-cube$`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()

			code, stdout, stderr := execInit(t, dir, tt.args...)
			if code != 0 {
				t.Fatalf("exit = %d, stderr: %s", code, stderr)
			}
			cfg, err := config.LoadFile(filepath.Join(dir, "cube.yaml"))
			if err != nil {
				t.Fatalf("scaffolded config does not load: %v", err)
			}
			if !tt.wantName.MatchString(cfg.Name) {
				t.Errorf("scaffolded metadata.name %q does not match %v", cfg.Name, tt.wantName)
			}
			notice := fmt.Sprintf("scaffolded %s — cube %q", filepath.Join(dir, "cube.yaml"), cfg.Name)
			if !strings.Contains(stdout, notice) {
				t.Errorf("stdout %q missing scaffold notice %q", stdout, notice)
			}
			if _, err := os.Stat(filepath.Join(dir, "kubeconfig")); err != nil {
				t.Errorf("kubeconfig not written after scaffold+provision: %v", err)
			}
		})
	}
}

func TestInitNameMismatchFails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "cube.yaml")
	original := []byte("apiVersion: cube-idp.dev/v1alpha1\nkind: Config\nmetadata:\n  name: dev\nspec:\n  cluster: {}\n")
	if err := os.WriteFile(cfgPath, original, 0o600); err != nil {
		t.Fatal(err)
	}

	code, _, stderr := execInit(t, dir, "--name", "other")
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stderr, "CUBE-CFG-005") {
		t.Errorf("stderr %q should carry CUBE-CFG-005", stderr)
	}
	after, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, original) {
		t.Errorf("config mutated by --name mismatch:\n%s", after)
	}
	if _, err := os.Stat(filepath.Join(dir, "kubeconfig")); err == nil {
		t.Error("provisioning ran despite the name conflict")
	}
}

func TestInitMatchingNameProceeds(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "cube.yaml")
	doc := "apiVersion: cube-idp.dev/v1alpha1\nkind: Config\nmetadata:\n  name: dev\nspec:\n  cluster: {}\n"
	if err := os.WriteFile(cfgPath, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := execInit(t, dir, "--name", "dev")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 — matching --name must keep init idempotent (stderr: %s)", code, stderr)
	}
	if strings.Contains(stdout, "scaffolded") {
		t.Errorf("stdout %q claims a scaffold, but the config already existed", stdout)
	}
	if _, err := os.Stat(filepath.Join(dir, "kubeconfig")); err != nil {
		t.Errorf("kubeconfig not written: %v", err)
	}
}

func TestInitScaffoldRejectsInvalidName(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	code, _, stderr := execInit(t, dir, "--name", "Not_A_DNS_Label")
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stderr, "CUBE-CFG-003") {
		t.Errorf("stderr %q should carry CUBE-CFG-003", stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "cube.yaml")); err == nil {
		t.Error("invalid scaffold name must leave no file behind")
	}
}
