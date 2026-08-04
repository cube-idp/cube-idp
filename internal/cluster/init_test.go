package cluster

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

type mockProvisioner struct {
	EnsureFunc     func(ctx context.Context, s Spec) error
	DeleteFunc     func(ctx context.Context, name string) error
	KubeconfigFunc func(ctx context.Context, name string) ([]byte, error)
}

func (m *mockProvisioner) Ensure(ctx context.Context, s Spec) error {
	if m.EnsureFunc != nil {
		return m.EnsureFunc(ctx, s)
	}
	return nil
}
func (m *mockProvisioner) Exists(context.Context, string) (bool, error) { return false, nil }
func (m *mockProvisioner) Delete(ctx context.Context, name string) error {
	if m.DeleteFunc != nil {
		return m.DeleteFunc(ctx, name)
	}
	return nil
}
func (m *mockProvisioner) Kubeconfig(ctx context.Context, name string) ([]byte, error) {
	if m.KubeconfigFunc != nil {
		return m.KubeconfigFunc(ctx, name)
	}
	return []byte(strings.ReplaceAll(kindStyleKubeconfig, "kind-dev", "kind-"+name)), nil
}

// TestInitNoHomeFails: with neither KUBECONFIG nor a home directory the
// default kubeconfig location is undeterminable — a coded error, never a
// silent CWD-relative write.
func TestInitNoHomeFails(t *testing.T) {
	t.Setenv("KUBECONFIG", "")
	t.Setenv("HOME", "")
	err := Init(t.Context(), &mockProvisioner{}, InitOptions{Spec: Spec{Name: "dev"}})

	var coded *cubeerr.Coded
	if !errors.As(err, &coded) || coded.Code != CodeKubeconfigFailed {
		t.Fatalf("err = %v, want code %s", err, CodeKubeconfigFailed)
	}
}

func TestInit(t *testing.T) {
	initCases := []struct {
		name     string
		opts     InitOptions
		mock     *mockProvisioner
		wantCode cubeerr.Code // "" = success
		wantIn   []string     // substrings expected in the target file
		explicit bool         // true → assert default location untouched
	}{
		{
			name:   "merges into KUBECONFIG default with derived context",
			opts:   InitOptions{Spec: Spec{Name: "dev"}},
			mock:   &mockProvisioner{},
			wantIn: []string{"cube-idp.dev/dev", "current-context: cube-idp.dev/dev"},
		},
		{
			name: "explicit path writes file, no merge into default",
			opts: InitOptions{Spec: Spec{Name: "dev"}}, // KubeconfigPath set in test body
			mock: &mockProvisioner{}, explicit: true,
			wantIn: []string{"cube-idp.dev/dev"},
		},
		{
			name:   "context name override and namespace stamped",
			opts:   InitOptions{Spec: Spec{Name: "dev"}, ContextName: "my-ctx", Namespace: "platform"},
			mock:   &mockProvisioner{},
			wantIn: []string{"name: my-ctx", "namespace: platform"},
		},
		{
			name: "ensure failure surfaces driver error untouched",
			opts: InitOptions{Spec: Spec{Name: "dev"}},
			mock: &mockProvisioner{EnsureFunc: func(context.Context, Spec) error {
				return NewProvisionFailedError("create", "dev", errors.New("boom"))
			}},
			wantCode: CodeProvisionFailed,
		},
		{
			name: "kubeconfig failure wraps as CLU-005",
			opts: InitOptions{Spec: Spec{Name: "dev"}},
			mock: &mockProvisioner{KubeconfigFunc: func(context.Context, string) ([]byte, error) {
				return nil, fmt.Errorf("boom")
			}},
			wantCode: CodeKubeconfigFailed,
		},
	}

	for _, tt := range initCases {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			defaultPath := filepath.Join(dir, "default-kubeconfig")
			t.Setenv("KUBECONFIG", defaultPath)
			target := defaultPath
			if tt.explicit {
				target = filepath.Join(dir, "explicit-kubeconfig")
				tt.opts.KubeconfigPath = target
			}

			err := Init(t.Context(), tt.mock, tt.opts)

			if tt.wantCode != "" {
				var coded *cubeerr.Coded
				if !errors.As(err, &coded) || coded.Code != tt.wantCode {
					t.Fatalf("err = %v, want code %s", err, tt.wantCode)
				}
				return
			}
			if err != nil {
				t.Fatalf("Init: %v", err)
			}
			got, err := os.ReadFile(target)
			if err != nil {
				t.Fatalf("read target: %v", err)
			}
			for _, s := range tt.wantIn {
				if !strings.Contains(string(got), s) {
					t.Errorf("target missing %q:\n%s", s, got)
				}
			}
			if tt.explicit {
				if _, err := os.Stat(defaultPath); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("default kubeconfig was touched despite explicit --kubeconfig path")
				}
			}
		})
	}
}
