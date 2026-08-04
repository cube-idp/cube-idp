package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
	"github.com/cube-idp/cube-idp/internal/cluster"
	"github.com/cube-idp/cube-idp/internal/config"
)

// execInit executes init. The injected factory still returns the shared
// mock — init must never need a provisioner, and a mock factory keeps
// that true even if a regression wires one back in.
func execInit(t *testing.T, dir string, extraArgs ...string) (code int, stdout, stderr string) {
	t.Helper()
	root := newRootCmd(func(v1alpha1.ClusterProvider) (cluster.Provisioner, error) {
		return mockProvisioner{}, nil
	})
	args := append([]string{"init", "-f", filepath.Join(dir, "cube.yaml")}, extraArgs...)
	var out, errBuf bytes.Buffer
	code = execute(t.Context(), root, args, &out, &errBuf)
	return code, out.String(), errBuf.String()
}

// assertNoKubeconfig fails if anything appeared at dir/kubeconfig —
// init is config-only; provisioning and kubeconfig writes are create's.
func assertNoKubeconfig(t *testing.T, dir string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(dir, "kubeconfig")); !errors.Is(err, fs.ErrNotExist) {
		t.Error("init produced a kubeconfig — provisioning is create's job")
	}
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
			if hint := `run "cube-idp create" to provision the cluster`; !strings.Contains(stdout, hint) {
				t.Errorf("stdout %q missing next-step hint %q", stdout, hint)
			}
			assertNoKubeconfig(t, dir)
		})
	}
}

// TestInitExistingConfigReports: a second init is an idempotent
// load-and-report — exit 0, the exists line, no scaffold notice, no hint.
func TestInitExistingConfigReports(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "cube.yaml")
	doc := "apiVersion: cube-idp.dev/v1alpha1\nkind: Config\nmetadata:\n  name: dev\nspec:\n  cluster: {}\n"
	if err := os.WriteFile(cfgPath, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := execInit(t, dir)
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	if want := fmt.Sprintf("config %s exists — cube %q", cfgPath, "dev"); !strings.Contains(stdout, want) {
		t.Errorf("stdout %q missing exists report %q", stdout, want)
	}
	for _, unwanted := range []string{"scaffolded", "cube-idp create"} {
		if strings.Contains(stdout, unwanted) {
			t.Errorf("stdout %q must not contain %q on the exists path", stdout, unwanted)
		}
	}
	assertNoKubeconfig(t, dir)
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
	if want := fmt.Sprintf("config %s exists — cube %q", cfgPath, "dev"); !strings.Contains(stdout, want) {
		t.Errorf("stdout %q missing exists report %q", stdout, want)
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
