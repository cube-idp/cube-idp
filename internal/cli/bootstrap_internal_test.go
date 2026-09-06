package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
	"github.com/cube-idp/cube-idp/internal/cluster"
	"github.com/cube-idp/cube-idp/internal/cubeerr"
	"github.com/cube-idp/cube-idp/internal/engine"
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
	}, defaultEngine)
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
// is absent fails at the edge before any apply — exit 1. Nothing is being
// updated here, so since #203 the code is the read-side CUBE-CLU-006 (this
// assertion read CUBE-CLU-005 before), and the absent file earns the
// prerequisite remediation rather than advice about file permissions.
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
	for _, want := range []string{"CUBE-CLU-006", "cube-idp create"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr missing %q:\n%s", want, stderr)
		}
	}
	if strings.Contains(stderr, "kubeconfig update failed") {
		t.Fatalf("bootstrap read must not claim an update failed:\n%s", stderr)
	}
}

// TestBootstrapContextAbsentKeepsKUB002 is the guard for #203's fix, and it
// is GREEN from the first run by design: when the kubeconfig file IS
// present and only the cube context is missing, the good path must survive
// untouched — the edge read succeeds, kube.New raises CUBE-KUB-002, and the
// operator is told to run `cube-idp create`. Issue #203 names that error as
// the behaviour to preserve, so gating the edge on rep.ContextInstalled
// (which would replace it with a cluster code) is what this test forbids.
func TestBootstrapContextAbsentKeepsKUB002(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cube.yaml"), []byte(bootstrapConfigYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	// A valid kubeconfig carrying only a foreign context: readable, so the
	// edge read succeeds, but without cube-idp.dev/dev.
	foreign := `apiVersion: v1
kind: Config
clusters:
  - name: other
    cluster:
      server: https://127.0.0.1:6443
contexts:
  - name: other
    context:
      cluster: other
      user: other
users:
  - name: other
    user:
      token: fake
current-context: other
`
	if err := os.WriteFile(filepath.Join(dir, "kubeconfig"), []byte(foreign), 0o600); err != nil {
		t.Fatal(err)
	}

	code, _, stderr := execBootstrap(t, dir, mockProvisioner{})
	if code != 1 {
		t.Fatalf("exit = %d, want 1; stderr: %s", code, stderr)
	}
	// The code AND its guidance: #203 praises this error for telling the
	// operator to run `create`, so losing the wording would defeat the
	// guard as surely as losing the code.
	for _, want := range []string{"CUBE-KUB-002", "cube-idp create"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr missing %q:\n%s", want, stderr)
		}
	}
	if strings.Contains(stderr, "CUBE-CLU-00") {
		t.Fatalf("a present file with an absent context must stay a kube error:\n%s", stderr)
	}
}

// TestDefaultEngine pins the production engine factory: flux maps to the
// flux driver, anything else is the defensive CUBE-ENG-001 (config
// validation is the primary gate).
func TestDefaultEngine(t *testing.T) {
	t.Parallel()
	drv, err := defaultEngine(v1alpha1.EngineProviderFlux)
	if err != nil || drv == nil {
		t.Fatalf("defaultEngine(flux) = (%v, %v), want a driver", drv, err)
	}
	_, err = defaultEngine("argo")
	var coded *cubeerr.Coded
	if !errors.As(err, &coded) {
		t.Fatalf("defaultEngine(argo) = %v, want *cubeerr.Coded", err)
	}
	if coded.Code != engine.CodeUnsupportedProvider {
		t.Fatalf("code = %s, want %s", coded.Code, engine.CodeUnsupportedProvider)
	}
}

// TestGatewayDomain pins the edge's domain derivation: an explicit
// spec.gateway.domain wins, and its absence — the common config, since
// Default() fills only a sub-struct the user wrote — derives the same name
// api would have derived.
func TestGatewayDomain(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		spec *v1alpha1.GatewaySpec
		want string
	}{
		{name: "absent spec.gateway derives from the cube name", want: "dev.cube.test"},
		{name: "a present but empty spec.gateway derives too",
			spec: &v1alpha1.GatewaySpec{}, want: "dev.cube.test"},
		{name: "an explicit domain wins",
			spec: &v1alpha1.GatewaySpec{Domain: "apps.example.test"}, want: "apps.example.test"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := &v1alpha1.Config{Spec: v1alpha1.ConfigSpec{Gateway: tc.spec}}
			cfg.Name = "dev"
			if got := gatewayDomain(cfg); got != tc.want {
				t.Errorf("gatewayDomain = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestGatewayDomainMatchesAPIDefault is the guard against a forked spelling:
// what the edge derives for an absent spec.gateway must be exactly what api's
// Default() derives for a written one. Two derivations, one answer.
func TestGatewayDomainMatchesAPIDefault(t *testing.T) {
	t.Parallel()
	defaulted := &v1alpha1.Config{Spec: v1alpha1.ConfigSpec{Gateway: &v1alpha1.GatewaySpec{}}}
	defaulted.Name = "dev"
	defaulted.Default()
	absent := &v1alpha1.Config{}
	absent.Name = "dev"

	if got := gatewayDomain(absent); got != defaulted.Spec.Gateway.Domain {
		t.Errorf("edge derived %q, api's Default() derived %q — the spellings have forked",
			got, defaulted.Spec.Gateway.Domain)
	}
}
