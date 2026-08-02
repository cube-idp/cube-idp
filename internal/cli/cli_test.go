package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cube-idp/cube-idp/internal/cli"
)

const validYAML = `apiVersion: cube-idp.dev/v1alpha1
kind: Config
metadata:
  name: dev
spec: {}
`

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cube.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func run(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errBuf bytes.Buffer
	code = cli.Execute(t.Context(), args, &out, &errBuf)
	return code, out.String(), errBuf.String()
}

func TestConfigValidateValid(t *testing.T) {
	path := writeTemp(t, validYAML)
	code, stdout, _ := run(t, "config", "validate", "-f", path)
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.Contains(stdout, "valid") {
		t.Errorf("stdout %q should confirm validity", stdout)
	}
}

func TestConfigValidateInvalid(t *testing.T) {
	path := writeTemp(t, strings.Replace(validYAML, "name: dev", "name: \"\"", 1))
	code, _, stderr := run(t, "config", "validate", "-f", path)
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(stderr, "CUBE-CFG-003") {
		t.Errorf("stderr %q should carry CUBE-CFG-003", stderr)
	}
	if !strings.Contains(stderr, "metadata.name") {
		t.Errorf("stderr %q should name the offending field", stderr)
	}
}

func TestConfigShowRoundTrip(t *testing.T) {
	path := writeTemp(t, validYAML)
	code, stdout, _ := run(t, "config", "show", "-f", path)
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	for _, want := range []string{"apiVersion: cube-idp.dev/v1alpha1", "kind: Config", "name: dev"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("show output missing %q; got:\n%s", want, stdout)
		}
	}
}

func TestMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope.yaml")
	code, _, stderr := run(t, "config", "validate", "-f", path)
	if code != 2 {
		t.Fatalf("exit = %d, want 2 for missing config file", code)
	}
	if !strings.Contains(stderr, "CUBE-CFG-004") {
		t.Errorf("stderr %q should carry CUBE-CFG-004", stderr)
	}
	if !strings.Contains(stderr, path) {
		t.Errorf("stderr %q should name the full user-supplied path %q", stderr, path)
	}
}

// TestConfigValidateForProvider drives the real kind driver's pure
// SpecValidator capability — no container runtime is touched, so these
// stay in the hermetic gate. Both rows share one code path: load, then
// provider-side validation at the CLI edge.
func TestConfigValidateForProvider(t *testing.T) {
	tests := []struct {
		name        string
		forProvider string
		wantCode    int
		wantStderr  string // empty = expect success output on stdout
	}{
		{
			name:        "valid kind payload",
			forProvider: "      nodes:\n        - role: control-plane\n",
			wantCode:    0,
		},
		{
			name:        "invalid kind payload",
			forProvider: "      notAKindField: true\n",
			wantCode:    1,
			wantStderr:  "CUBE-CLU-003",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := "apiVersion: cube-idp.dev/v1alpha1\nkind: Config\nmetadata:\n  name: dev\nspec:\n  cluster:\n    provider: kind\n    forProvider:\n" + tt.forProvider
			path := writeTemp(t, doc)
			code, stdout, stderr := run(t, "config", "validate", "-f", path)
			if code != tt.wantCode {
				t.Fatalf("exit = %d, want %d (stderr: %s)", code, tt.wantCode, stderr)
			}
			if tt.wantStderr == "" {
				if !strings.Contains(stdout, "valid") {
					t.Errorf("stdout %q should confirm validity", stdout)
				}
				return
			}
			if !strings.Contains(stderr, tt.wantStderr) {
				t.Errorf("stderr %q should carry %s", stderr, tt.wantStderr)
			}
		})
	}
}

func TestInitRequiresCluster(t *testing.T) {
	path := writeTemp(t, validYAML)
	code, _, stderr := run(t, "init", "-f", path)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr, "CUBE-CLU-001") {
		t.Errorf("stderr %q should carry CUBE-CLU-001", stderr)
	}
}

// TestConfigShowGolden is the one sanctioned byte-exact check of CLI stdout.
func TestConfigShowGolden(t *testing.T) {
	path := writeTemp(t, validYAML)
	code, stdout, stderr := run(t, "config", "show", "-f", path)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "show.golden"))
	if err != nil {
		t.Fatal(err)
	}
	if stdout != string(want) {
		t.Errorf("config show output differs from testdata/show.golden:\ngot:\n%s\nwant:\n%s", stdout, want)
	}
}
