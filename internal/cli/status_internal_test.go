package cli

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
	"github.com/cube-idp/cube-idp/internal/cluster"
	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

// absentClusterProvisioner reports the cluster as not existing; the
// embedded mock answers everything else.
type absentClusterProvisioner struct{ mockProvisioner }

func (absentClusterProvisioner) Exists(context.Context, string) (bool, error) { return false, nil }

// serverProvisioner emits a kubeconfig pointing at a caller-controlled
// server URL, so reachability outcomes are deterministic in tests.
type serverProvisioner struct {
	mockProvisioner
	server string
}

func (p serverProvisioner) Kubeconfig(_ context.Context, name string) ([]byte, error) {
	kc := `apiVersion: v1
kind: Config
clusters:
  - name: kind-NAME
    cluster:
      server: SERVER
contexts:
  - name: kind-NAME
    context:
      cluster: kind-NAME
      user: kind-NAME
users:
  - name: kind-NAME
    user: {}
current-context: kind-NAME
`
	kc = strings.ReplaceAll(kc, "NAME", name)
	return []byte(strings.ReplaceAll(kc, "SERVER", p.server)), nil
}

// installContext runs create with p so the cube context in dir points at
// p's server, then returns; status is exercised separately.
func installContext(t *testing.T, dir string, p cluster.Provisioner) {
	t.Helper()
	root := newRootCmd(func(v1alpha1.ClusterProvider) (cluster.Provisioner, error) {
		return p, nil
	}, defaultEngine)
	var out, errBuf bytes.Buffer
	code := execute(t.Context(), root, []string{
		"create", "-f", filepath.Join(dir, "cube.yaml"),
		"--kubeconfig", filepath.Join(dir, "kubeconfig"),
	}, &out, &errBuf)
	if code != 0 {
		t.Fatalf("create exit = %d, stderr: %s", code, errBuf.String())
	}
}

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
	}, defaultEngine)
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	installContext(t, dir, serverProvisioner{server: server.URL})

	code, stdout, stderr := execStatus(t, dir, mockProvisioner{})
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	for _, want := range []string{
		`cluster "dev": exists`,
		`kubeconfig context "cube-idp.dev/dev": installed in ` + filepath.Join(dir, "kubeconfig"),
		"api server: reachable",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout)
		}
	}
}

// TestStatusAPIServerUnreachable: an installed context whose server does
// not answer is a finding — the line reads unreachable and status still
// exits 0.
func TestStatusAPIServerUnreachable(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cube.yaml"), []byte(statusClusterYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.NewServeMux())
	url := server.URL
	server.Close() // the port is now closed: connection refused, deterministically
	installContext(t, dir, serverProvisioner{server: url})

	code, stdout, stderr := execStatus(t, dir, mockProvisioner{})
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "api server: unreachable") {
		t.Errorf("stdout missing %q:\n%s", "api server: unreachable", stdout)
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
		"api server: not checked (context not installed)",
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

// TestAPIServerStateKubeconfigUnreadable covers internal/cli/status.go's
// own read — the fifth CUBE-CLU-005 read-site #203 concerns, added to the
// issue's evidence by the operator. `status` is documented read-only, so a
// failure here must not claim an update failed.
//
// apiServerState is exercised directly rather than through the status verb
// because the site is only reachable in a window that cannot be driven
// deterministically from outside: cluster.Status reads the kubeconfig to
// decide ContextInstalled, and apiServerState reads it a second time. A
// file unreadable for the whole run fails at the first read and never gets
// here; only a file that becomes unreadable between the two does.
func TestAPIServerStateKubeconfigUnreadable(t *testing.T) {
	t.Parallel()
	rep := cluster.StatusReport{
		ContextInstalled: true,
		ContextName:      "cube-idp.dev/dev",
		KubeconfigPath:   filepath.Join(t.TempDir(), "vanished"),
	}

	_, err := apiServerState(t.Context(), rep)

	var coded *cubeerr.Coded
	if !errors.As(err, &coded) {
		t.Fatalf("err = %v, want a *cubeerr.Coded", err)
	}
	if coded.Code != cluster.CodeKubeconfigReadFailed {
		t.Fatalf("Code = %s, want %s", coded.Code, cluster.CodeKubeconfigReadFailed)
	}
	if strings.Contains(coded.Summary, "update failed") {
		t.Errorf("status is read-only; Summary = %q", coded.Summary)
	}
}
