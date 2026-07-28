package config_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

const validYAML = `apiVersion: cube-idp.dev/v1alpha1
kind: Config
metadata:
  name: dev
spec: {}
`

func TestLoad(t *testing.T) {
	tests := []struct {
		name     string
		yaml     string
		wantCode cubeerr.Code // "" = expect success
	}{
		{"valid minimal", validYAML, ""},
		{"server-side metadata accepted and ignored",
			"apiVersion: cube-idp.dev/v1alpha1\nkind: Config\nmetadata:\n  name: dev\n  uid: abc\n", ""},
		{"unknown top-level field",
			validYAML + "bogus: 1\n", config.CodeUnknownField},
		{"unknown spec field",
			"apiVersion: cube-idp.dev/v1alpha1\nkind: Config\nmetadata:\n  name: dev\nspec:\n  bogus: 1\n", config.CodeUnknownField},
		{"wrong apiVersion",
			"apiVersion: nope.dev/v1\nkind: Config\nmetadata:\n  name: dev\n", config.CodeUnsupportedAPIVersion},
		{"wrong kind",
			"apiVersion: cube-idp.dev/v1alpha1\nkind: Cluster\nmetadata:\n  name: dev\n", config.CodeUnsupportedAPIVersion},
		{"empty file", "", config.CodeUnsupportedAPIVersion},
		{"missing name",
			"apiVersion: cube-idp.dev/v1alpha1\nkind: Config\nmetadata: {}\n", config.CodeInvalidConfig},
		{"invalid name",
			"apiVersion: cube-idp.dev/v1alpha1\nkind: Config\nmetadata:\n  name: BAD\n", config.CodeInvalidConfig},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fsys := fstest.MapFS{"cube.yaml": {Data: []byte(tt.yaml)}}
			cfg, err := config.Load(fsys, "cube.yaml")

			if tt.wantCode == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if cfg.Name != "dev" {
					t.Errorf("Name = %q, want dev", cfg.Name)
				}
				return
			}

			var coded *cubeerr.Coded
			if !errors.As(err, &coded) {
				t.Fatalf("error %v is not a *cubeerr.Coded", err)
			}
			if coded.Code != tt.wantCode {
				t.Errorf("code = %s, want %s", coded.Code, tt.wantCode)
			}
			if coded.Remediation == "" {
				t.Error("coded error must carry a remediation")
			}
		})
	}
}

func TestLoadUnreadableFile(t *testing.T) {
	tests := []struct {
		name string
		fsys fstest.MapFS
	}{
		{"missing file", fstest.MapFS{}},
		{"path is a directory", fstest.MapFS{"cube.yaml": {Mode: fs.ModeDir}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := config.Load(tt.fsys, "cube.yaml")

			var coded *cubeerr.Coded
			if !errors.As(err, &coded) {
				t.Fatalf("error %v is not a *cubeerr.Coded", err)
			}
			if coded.Code != config.CodeUnreadableConfig {
				t.Errorf("code = %s, want %s", coded.Code, config.CodeUnreadableConfig)
			}
			if coded.Remediation == "" {
				t.Error("coded error must carry a remediation")
			}
			if coded.Unwrap() == nil {
				t.Error("coded error must wrap the fs cause")
			}
		})
	}
}

func TestLoadFile(t *testing.T) {
	t.Run("valid file loads", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "cube.yaml")
		if err := os.WriteFile(path, []byte(validYAML), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg, err := config.LoadFile(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Name != "dev" {
			t.Errorf("Name = %q, want dev", cfg.Name)
		}
	})

	t.Run("missing file carries user-supplied path", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "sub", "nope.yaml")
		_, err := config.LoadFile(path)

		var coded *cubeerr.Coded
		if !errors.As(err, &coded) {
			t.Fatalf("error %v is not a *cubeerr.Coded", err)
		}
		if coded.Code != config.CodeUnreadableConfig {
			t.Errorf("code = %s, want %s", coded.Code, config.CodeUnreadableConfig)
		}
		// Path context is the behavior under test here, not error identity,
		// so a containment check on the wrapped cause is deliberate.
		if cause := coded.Unwrap(); cause == nil || !strings.Contains(cause.Error(), path) {
			t.Errorf("cause %v should name the full user-supplied path %q", cause, path)
		}
	})
}
