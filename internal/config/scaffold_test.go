package config_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

// assertCoded asserts err is a *cubeerr.Coded with the given code and a
// non-empty remediation, returning it for further checks.
func assertCoded(t *testing.T, err error, want cubeerr.Code) *cubeerr.Coded {
	t.Helper()
	var coded *cubeerr.Coded
	if !errors.As(err, &coded) {
		t.Fatalf("error %v is not a *cubeerr.Coded", err)
	}
	if coded.Code != want {
		t.Errorf("code = %s, want %s", coded.Code, want)
	}
	if coded.Remediation == "" {
		t.Error("coded error must carry a remediation")
	}
	return coded
}

const scaffoldedYAML = `apiVersion: cube-idp.dev/v1alpha1
kind: Config
metadata:
  name: quirky-otter
spec:
  cluster: {}
`

func TestScaffoldFileValid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cube.yaml")
	if err := config.ScaffoldFile(path, "quirky-otter"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Byte-exact on purpose: the template is a user-visible contract.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != scaffoldedYAML {
		t.Errorf("scaffolded content:\n%s\nwant:\n%s", raw, scaffoldedYAML)
	}

	cfg, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("scaffolded file must load valid: %v", err)
	}
	if cfg.Name != "quirky-otter" {
		t.Errorf("Name = %q, want quirky-otter", cfg.Name)
	}
	if cfg.Spec.Cluster == nil {
		t.Error("spec.cluster must be present (default cluster semantics)")
	}
}

func TestScaffoldFileInvalidName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cube.yaml")
	err := config.ScaffoldFile(path, "NOT-valid")

	_ = assertCoded(t, err, config.CodeInvalidConfig)
	if _, statErr := os.Stat(path); !errors.Is(statErr, fs.ErrNotExist) {
		t.Errorf("no file may be written for an invalid name; stat err = %v", statErr)
	}
}

func TestScaffoldFileNeverClobbers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cube.yaml")
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := config.ScaffoldFile(path, "quirky-otter")
	_ = assertCoded(t, err, config.CodeAlreadyExists)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "original" {
		t.Errorf("existing content was clobbered: %q", raw)
	}
}

func TestScaffoldFileMissingDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope", "cube.yaml")
	err := config.ScaffoldFile(path, "quirky-otter")

	coded := assertCoded(t, err, config.CodeScaffoldFailed)
	if !strings.Contains(coded.Summary, path) {
		t.Errorf("summary %q should name the target path %q", coded.Summary, path)
	}
	if coded.Unwrap() == nil {
		t.Error("coded error must wrap the fs cause")
	}
}

func TestErrNameConflict(t *testing.T) {
	err := config.ErrNameConflict("cube.yaml",
		"dev" /* documentName */, "prod" /* flagName */)

	coded := assertCoded(t, err, config.CodeNameConflict)
	if !strings.Contains(coded.Remediation, "metadata.name") {
		t.Errorf("remediation %q must point at editing metadata.name", coded.Remediation)
	}
}
