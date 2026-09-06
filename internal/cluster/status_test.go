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
		explicit      bool // route init and status through an explicit file
		seedBadTarget bool // write an unparseable kubeconfig at the target
		// seedUnreadableFile writes a kubeconfig the process cannot open,
		// so the read itself fails on a file that does exist.
		seedUnreadableFile bool
		wantCode           cubeerr.Code // "" = success
		wantRemediation    string       // substring, checked when non-empty
		want               StatusReport // ContextName/KubeconfigPath checked when non-empty
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
			// Status is a read-only operation (docs/domains/cluster.md's
			// Operations section), so an unparseable file is a read
			// failure, not the "kubeconfig update failed" CUBE-CLU-005
			// names. This row asserted CUBE-CLU-005 before #203.
			name:          "unparseable kubeconfig wraps as CLU-006",
			opts:          StatusOptions{Name: "dev"},
			mock:          &mockProvisioner{},
			seedBadTarget: true,
			wantCode:      CodeKubeconfigReadFailed,
			// A malformed file is not a missing prerequisite: the
			// guidance must point at the file, not at `create`.
			wantRemediation: "a valid kubeconfig",
		},
		{
			// The only way contextInstalled reaches its read error at
			// all: fs.ErrNotExist is already a clean "not installed".
			name:               "unreadable kubeconfig wraps as CLU-006",
			opts:               StatusOptions{Name: "dev"},
			mock:               &mockProvisioner{},
			seedUnreadableFile: true,
			wantCode:           CodeKubeconfigReadFailed,
			// Not the prerequisite branch: the file is there, so telling
			// the operator to run `create` would be wrong guidance.
			wantRemediation: "a valid kubeconfig",
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
			if tt.seedUnreadableFile {
				if os.Geteuid() == 0 {
					t.Skip("root reads mode-0000 files; the unreadable case is unreachable")
				}
				if err := os.WriteFile(target, []byte("apiVersion: v1\n"), 0o000); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(target, 0o600) })
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
				if tt.wantRemediation != "" && !strings.Contains(coded.Remediation, tt.wantRemediation) {
					t.Errorf("remediation = %q, want it to name %q", coded.Remediation, tt.wantRemediation)
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
// default kubeconfig location is a coded error, not a silent report. The
// pair deliberately differs on the code since #203 — Init is a write, so
// it keeps CUBE-CLU-005; Status changes nothing, so an unsatisfiable read
// is CUBE-CLU-006. The intent asserted here is unchanged; only the code
// that expresses it moved.
func TestStatusNoHomeFails(t *testing.T) {
	t.Setenv("KUBECONFIG", "")
	t.Setenv("HOME", "")

	_, err := Status(t.Context(), &mockProvisioner{}, StatusOptions{Name: "dev"})
	var coded *cubeerr.Coded
	if !errors.As(err, &coded) || coded.Code != CodeKubeconfigReadFailed {
		t.Fatalf("err = %v, want code %s", err, CodeKubeconfigReadFailed)
	}
	// Neither read remediation applies when there is no file to name.
	if !strings.Contains(coded.Remediation, "KUBECONFIG") {
		t.Errorf("remediation = %q, want it to name KUBECONFIG", coded.Remediation)
	}
}
