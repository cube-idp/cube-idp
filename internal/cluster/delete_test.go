package cluster

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

// otherKubeconfig is the foreign-entries fixture a Delete must preserve.
const otherKubeconfig = `apiVersion: v1
kind: Config
clusters:
  - name: other
    cluster:
      server: https://example.com
contexts:
  - name: other
    context:
      cluster: other
      user: other
users:
  - name: other
    user:
      token: abc
current-context: other
`

// TestDeleteReversesInit round-trips Init → Delete: whatever init
// installed is gone, everything else survives, and the file is
// rewritten in place — never unlinked.
func TestDeleteReversesInit(t *testing.T) {
	deleteCases := []struct {
		name       string
		initOpts   InitOptions
		deleteOpts DeleteOptions
		explicit   bool // route both operations through an explicit file
		seedOther  bool
		wantGone   []string
		wantKept   []string
	}{
		{
			name:       "default kubeconfig: cube context removed, foreign entries kept",
			initOpts:   InitOptions{Spec: Spec{Name: "dev"}},
			deleteOpts: DeleteOptions{Name: "dev"},
			seedOther:  true,
			wantGone:   []string{"cube-idp.dev/dev", "current-context"},
			wantKept:   []string{"name: other", "token: abc"},
		},
		{
			name:       "explicit path: emptied but never unlinked",
			initOpts:   InitOptions{Spec: Spec{Name: "dev"}},
			deleteOpts: DeleteOptions{Name: "dev"},
			explicit:   true,
			wantGone:   []string{"cube-idp.dev/dev"},
		},
		{
			name:       "context name override cleans the overridden name",
			initOpts:   InitOptions{Spec: Spec{Name: "dev"}, ContextName: "my-ctx"},
			deleteOpts: DeleteOptions{Name: "dev", ContextName: "my-ctx"},
			explicit:   true,
			wantGone:   []string{"my-ctx"},
		},
	}

	for _, tt := range deleteCases {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			defaultPath := filepath.Join(dir, "default-kubeconfig")
			t.Setenv("KUBECONFIG", defaultPath)
			target := defaultPath
			if tt.explicit {
				target = filepath.Join(dir, "explicit-kubeconfig")
				tt.initOpts.KubeconfigPath = target
				tt.deleteOpts.KubeconfigPath = target
			}
			if tt.seedOther {
				if err := os.WriteFile(target, []byte(otherKubeconfig), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := Init(t.Context(), &mockProvisioner{}, tt.initOpts); err != nil {
				t.Fatalf("Init: %v", err)
			}

			changed, err := Delete(t.Context(), &mockProvisioner{}, tt.deleteOpts)
			if err != nil {
				t.Fatalf("Delete: %v", err)
			}
			if !changed {
				t.Fatal("changed = false, want true after Init installed the context")
			}
			got, err := os.ReadFile(target)
			if err != nil {
				t.Fatalf("target must still exist — Delete never unlinks: %v", err)
			}
			for _, s := range tt.wantGone {
				if strings.Contains(string(got), s) {
					t.Errorf("target still contains %q:\n%s", s, got)
				}
			}
			for _, s := range tt.wantKept {
				if !strings.Contains(string(got), s) {
					t.Errorf("target missing %q:\n%s", s, got)
				}
			}
			if tt.explicit {
				if _, err := os.Stat(defaultPath); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("default kubeconfig was touched despite explicit path")
				}
			}
		})
	}
}

// TestDeleteMissingKubeconfig: no kubeconfig file means nothing is
// installed — a clean no-op that must not create the file.
func TestDeleteMissingKubeconfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kubeconfig")
	t.Setenv("KUBECONFIG", path)

	changed, err := Delete(t.Context(), &mockProvisioner{}, DeleteOptions{Name: "dev"})
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if changed {
		t.Fatal("changed = true, want false for a missing kubeconfig")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("Delete created the kubeconfig file")
	}
}

// TestDeleteAbsentContext: a kubeconfig without the cube context keeps
// its exact bytes — no rewrite of a file we would not change.
func TestDeleteAbsentContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kubeconfig")
	t.Setenv("KUBECONFIG", path)
	if err := os.WriteFile(path, []byte(otherKubeconfig), 0o600); err != nil {
		t.Fatal(err)
	}

	changed, err := Delete(t.Context(), &mockProvisioner{}, DeleteOptions{Name: "dev"})
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if changed {
		t.Fatal("changed = true, want false when the context is absent")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != otherKubeconfig {
		t.Fatalf("file rewritten despite no cube entries:\n%s", got)
	}
}

// TestDeleteDriverFailure: a failing seam Delete surfaces untouched and
// the kubeconfig cleanup must not run — the context still points at
// whatever is left of the cluster.
func TestDeleteDriverFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kubeconfig")
	t.Setenv("KUBECONFIG", path)
	if err := Init(t.Context(), &mockProvisioner{}, InitOptions{Spec: Spec{Name: "dev"}}); err != nil {
		t.Fatalf("Init: %v", err)
	}

	mock := &mockProvisioner{DeleteFunc: func(_ context.Context, name string) error {
		return NewProvisionFailedError("delete", name, errors.New("boom"))
	}}
	_, err := Delete(t.Context(), mock, DeleteOptions{Name: "dev"})
	var coded *cubeerr.Coded
	if !errors.As(err, &coded) || coded.Code != CodeProvisionFailed {
		t.Fatalf("err = %v, want code %s", err, CodeProvisionFailed)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "cube-idp.dev/dev") {
		t.Fatalf("kubeconfig cleaned despite driver failure:\n%s", got)
	}
}

// TestDeleteNoHomeFails mirrors TestInitNoHomeFails: an undeterminable
// default kubeconfig location is a coded error.
func TestDeleteNoHomeFails(t *testing.T) {
	t.Setenv("KUBECONFIG", "")
	t.Setenv("HOME", "")

	_, err := Delete(t.Context(), &mockProvisioner{}, DeleteOptions{Name: "dev"})
	var coded *cubeerr.Coded
	if !errors.As(err, &coded) || coded.Code != CodeKubeconfigFailed {
		t.Fatalf("err = %v, want code %s", err, CodeKubeconfigFailed)
	}
}
