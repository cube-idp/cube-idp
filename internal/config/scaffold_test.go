package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

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

	var coded *cubeerr.Coded
	if !errors.As(err, &coded) {
		t.Fatalf("error %v is not a *cubeerr.Coded", err)
	}
	if coded.Code != config.CodeInvalidConfig {
		t.Errorf("code = %s, want %s", coded.Code, config.CodeInvalidConfig)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Errorf("no file may be written for an invalid name; stat err = %v", statErr)
	}
}

func TestScaffoldFileNeverClobbers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cube.yaml")
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := config.ScaffoldFile(path, "quirky-otter"); err == nil {
		t.Fatal("scaffolding over an existing file must error")
	}
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
	if err == nil {
		t.Fatal("scaffolding into a missing directory must error")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error %v should name the target path %q", err, path)
	}
}

func TestErrNameConflict(t *testing.T) {
	err := config.ErrNameConflict("cube.yaml", "dev", "prod")

	var coded *cubeerr.Coded
	if !errors.As(err, &coded) {
		t.Fatalf("error %v is not a *cubeerr.Coded", err)
	}
	if coded.Code != config.CodeNameConflict {
		t.Errorf("code = %s, want %s", coded.Code, config.CodeNameConflict)
	}
	if !strings.Contains(coded.Remediation, "metadata.name") {
		t.Errorf("remediation %q must point at editing metadata.name", coded.Remediation)
	}
}
