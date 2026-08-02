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
