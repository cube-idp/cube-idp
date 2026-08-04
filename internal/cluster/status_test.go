package cluster

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

func TestStatus(t *testing.T) {
	existsMock := &mockProvisioner{ExistsFunc: func(context.Context, string) (bool, error) {
		return true, nil
	}}
	statusCases := []struct {
		name          string
		install       bool // run Init first so the context is installed
		initOpts      InitOptions
		opts          StatusOptions
		mock          *mockProvisioner
		explicit      bool         // route init and status through an explicit file
		seedBadTarget bool         // write an unparseable kubeconfig at the target
		wantCode      cubeerr.Code // "" = success
		want          StatusReport // ContextName/KubeconfigPath checked when non-empty
	}{
		{
			name:    "cluster exists and context installed",
			install: true, initOpts: InitOptions{Spec: Spec{Name: "dev"}},
			opts: StatusOptions{Name: "dev"},
			mock: existsMock,
			want: StatusReport{ClusterExists: true, ContextInstalled: true, ContextName: "cube-idp.dev/dev"},
		},
		{
			name: "cluster exists but context not installed",
			opts: StatusOptions{Name: "dev"},
			mock: existsMock,
			want: StatusReport{ClusterExists: true, ContextName: "cube-idp.dev/dev"},
		},
		{
			name: "cluster absent and nothing installed",
			opts: StatusOptions{Name: "dev"},
			mock: &mockProvisioner{},
			want: StatusReport{ContextName: "cube-idp.dev/dev"},
		},
		{
			name:    "explicit path and context name override",
			install: true, initOpts: InitOptions{Spec: Spec{Name: "dev"}, ContextName: "my-ctx"},
			opts:     StatusOptions{Name: "dev", ContextName: "my-ctx"},
			mock:     existsMock,
			explicit: true,
			want:     StatusReport{ClusterExists: true, ContextInstalled: true, ContextName: "my-ctx"},
		},
		{
			name: "driver Exists failure surfaces untouched",
			opts: StatusOptions{Name: "dev"},
			mock: &mockProvisioner{ExistsFunc: func(_ context.Context, name string) (bool, error) {
				return false, NewProvisionFailedError("list", name, errors.New("boom"))
			}},
			wantCode: CodeProvisionFailed,
		},
		{
			name:          "unparseable kubeconfig wraps as CLU-005",
			opts:          StatusOptions{Name: "dev"},
			mock:          &mockProvisioner{},
			seedBadTarget: true,
			wantCode:      CodeKubeconfigFailed,
		},
	}

	for _, tt := range statusCases {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			defaultPath := filepath.Join(dir, "default-kubeconfig")
			t.Setenv("KUBECONFIG", defaultPath)
			target := defaultPath
			if tt.explicit {
				target = filepath.Join(dir, "explicit-kubeconfig")
				tt.initOpts.KubeconfigPath = target
				tt.opts.KubeconfigPath = target
			}
			if tt.seedBadTarget {
				if err := os.WriteFile(target, []byte(":\tnot yaml"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if tt.install {
				if err := Init(t.Context(), tt.mock, tt.initOpts); err != nil {
					t.Fatalf("Init: %v", err)
				}
			}

			got, err := Status(t.Context(), tt.mock, tt.opts)

			if tt.wantCode != "" {
				var coded *cubeerr.Coded
				if !errors.As(err, &coded) || coded.Code != tt.wantCode {
					t.Fatalf("err = %v, want code %s", err, tt.wantCode)
				}
				return
			}
			if err != nil {
				t.Fatalf("Status: %v", err)
			}
			if got.ClusterExists != tt.want.ClusterExists {
				t.Errorf("ClusterExists = %v, want %v", got.ClusterExists, tt.want.ClusterExists)
			}
			if got.ContextInstalled != tt.want.ContextInstalled {
				t.Errorf("ContextInstalled = %v, want %v", got.ContextInstalled, tt.want.ContextInstalled)
			}
			if got.ContextName != tt.want.ContextName {
				t.Errorf("ContextName = %q, want %q", got.ContextName, tt.want.ContextName)
			}
			if got.KubeconfigPath != target {
				t.Errorf("KubeconfigPath = %q, want %q", got.KubeconfigPath, target)
			}
		})
	}
}

// TestStatusNoHomeFails mirrors TestInitNoHomeFails: an undeterminable
// default kubeconfig location is a coded error, not a silent report.
func TestStatusNoHomeFails(t *testing.T) {
	t.Setenv("KUBECONFIG", "")
	t.Setenv("HOME", "")

	_, err := Status(t.Context(), &mockProvisioner{}, StatusOptions{Name: "dev"})
	var coded *cubeerr.Coded
	if !errors.As(err, &coded) || coded.Code != CodeKubeconfigFailed {
		t.Fatalf("err = %v, want code %s", err, CodeKubeconfigFailed)
	}
}
