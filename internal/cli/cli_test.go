package cli_test

import (
	"bytes"
	"context"
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
	code = cli.Execute(context.Background(), args, &out, &errBuf)
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
	code, _, stderr := run(t, "config", "validate", "-f", filepath.Join(t.TempDir(), "nope.yaml"))
	if code == 0 {
		t.Fatal("exit = 0, want non-zero for missing file")
	}
	if stderr == "" {
		t.Error("expected an error message on stderr")
	}
}
