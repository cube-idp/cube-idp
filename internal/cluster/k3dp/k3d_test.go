package k3dp

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	v1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/k3d-io/k3d/v5/pkg/runtimes"
	k3dtypes "github.com/k3d-io/k3d/v5/pkg/types"

	"github.com/cube-idp/cube-idp/internal/diag"
)

// TestEnsureExposedAPIPort guards the k3d kubeconfig-port bug (found by the
// TestK3dUpDown): our library-direct create path skips the k3d CLI's
// random-free-port assignment, so ExposeAPI.HostPort stays "", the server
// node's k3d.server.api.port label is baked empty, and KubeconfigGet emits
// https://0.0.0.0 with NO port (dialing 443 → connection refused). The fix
// assigns a free host port when the user left it unset, mirroring
// cmd/cluster/clusterCreate.go's "Set to random port if port is empty string".
func TestEnsureExposedAPIPort(t *testing.T) {
	t.Run("assigns a free port when unset", func(t *testing.T) {
		var cfg v1alpha5.SimpleConfig
		if err := ensureExposedAPIPort(&cfg); err != nil {
			t.Fatalf("ensureExposedAPIPort: %v", err)
		}
		if cfg.ExposeAPI.HostPort == "" {
			t.Fatal("HostPort still empty: kubeconfig would carry https://0.0.0.0 with no port")
		}
		p, err := strconv.Atoi(cfg.ExposeAPI.HostPort)
		if err != nil || p <= 0 || p > 65535 {
			t.Fatalf("HostPort %q is not a valid TCP port", cfg.ExposeAPI.HostPort)
		}
	})

	t.Run("preserves a user-provided port", func(t *testing.T) {
		cfg := v1alpha5.SimpleConfig{}
		cfg.ExposeAPI.HostPort = "6550"
		if err := ensureExposedAPIPort(&cfg); err != nil {
			t.Fatalf("ensureExposedAPIPort: %v", err)
		}
		if cfg.ExposeAPI.HostPort != "6550" {
			t.Fatalf("user-provided HostPort overwritten: got %q, want 6550", cfg.ExposeAPI.HostPort)
		}
	})
}

// TestImportWithRetry_SucceedsOnSecondAttempt exercises the retry seam: a
// load that fails once (the observed ~1-in-3 transient containerd import
// failure) then succeeds must not surface an error to the caller.
func TestImportWithRetry_SucceedsOnSecondAttempt(t *testing.T) {
	calls := 0
	load := func(ctx context.Context, runtime runtimes.Runtime, tarPaths []string, cluster *k3dtypes.Cluster, opts k3dtypes.ImageImportOpts) error {
		calls++
		if calls == 1 {
			return errors.New("failed to import images in node 'k3d-x-server-0': exit status 1: Logs from failed access process:\ncontainerd digest mismatch")
		}
		return nil
	}
	err := importWithRetry(context.Background(), nil, []string{"a.tar"}, &k3dtypes.Cluster{Name: "x"}, k3dtypes.ImageImportOpts{}, load, 0)
	if err != nil {
		t.Fatalf("importWithRetry: got error %v, want nil after retry succeeds", err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want exactly 2 (one retry)", calls)
	}
}

// TestImportWithRetry_SucceedsFirstTry ensures the happy path does not pay
// for a retry it doesn't need.
func TestImportWithRetry_SucceedsFirstTry(t *testing.T) {
	calls := 0
	load := func(ctx context.Context, runtime runtimes.Runtime, tarPaths []string, cluster *k3dtypes.Cluster, opts k3dtypes.ImageImportOpts) error {
		calls++
		return nil
	}
	err := importWithRetry(context.Background(), nil, []string{"a.tar"}, &k3dtypes.Cluster{Name: "x"}, k3dtypes.ImageImportOpts{}, load, time.Hour)
	if err != nil {
		t.Fatalf("importWithRetry: got error %v, want nil", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want exactly 1 (no retry needed)", calls)
	}
}

// TestImportWithRetry_FailsAfterOneRetry confirms the retry is bounded to
// exactly one attempt and the underlying error (already carrying k3d's own
// captured node-exec output, per docker.execInNode) passes through unchanged.
func TestImportWithRetry_FailsAfterOneRetry(t *testing.T) {
	calls := 0
	underlying := "failed to import images in node 'k3d-x-server-0': exit status 1: Logs from failed access process:\ncontainerd: content digest mismatch"
	load := func(ctx context.Context, runtime runtimes.Runtime, tarPaths []string, cluster *k3dtypes.Cluster, opts k3dtypes.ImageImportOpts) error {
		calls++
		return errors.New(underlying)
	}
	err := importWithRetry(context.Background(), nil, []string{"a.tar"}, &k3dtypes.Cluster{Name: "x"}, k3dtypes.ImageImportOpts{}, load, 0)
	if err == nil {
		t.Fatal("importWithRetry: got nil error, want error after both attempts fail")
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want exactly 2 (initial + one retry, no more)", calls)
	}
	if !strings.Contains(err.Error(), "content digest mismatch") {
		t.Fatalf("error %q lost the underlying node-exec output", err.Error())
	}
}

// TestImportImagesWithDiag_PermanentFailureSurfacesCUBE7006 asserts that a
// permanently-failing k3d image import (both the initial attempt and its
// retry exhausted) surfaces the dedicated consume-side code CUBE-7006
// (CodeBundleImageLoadFail), not the vendor/produce-side CUBE-7002
// (CodeVendorPullFail) it used to be overloaded onto.
func TestImportImagesWithDiag_PermanentFailureSurfacesCUBE7006(t *testing.T) {
	load := func(ctx context.Context, runtime runtimes.Runtime, tarPaths []string, cluster *k3dtypes.Cluster, opts k3dtypes.ImageImportOpts) error {
		return errors.New("permanent containerd import failure")
	}
	cluster := &k3dtypes.Cluster{Name: "x"}

	err := importImagesWithDiag(context.Background(), []string{"a.tar"}, []string{"example.com/app:v1"}, cluster, load, 0)
	if err == nil {
		t.Fatal("importImagesWithDiag: got nil error, want error")
	}
	var de *diag.Error
	if !errors.As(err, &de) {
		t.Fatalf("importImagesWithDiag: error %v is not a *diag.Error", err)
	}
	if de.Code != diag.CodeBundleImageLoadFail {
		t.Fatalf("importImagesWithDiag: code = %s, want %s (CUBE-7006)", de.Code, diag.CodeBundleImageLoadFail)
	}
}
