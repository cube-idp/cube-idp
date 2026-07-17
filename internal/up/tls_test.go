package up

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/trust"
	"github.com/cube-idp/cube-idp/internal/ui"
)

func TestGatewayTLSSecretShape(t *testing.T) {
	ca, err := trust.EnsureCA(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	gw := config.GatewaySpec{Pack: "traefik", Host: "cube-idp.localtest.me", Port: 8443}
	objs, err := gatewayTLSObjects(ca, gw, 365*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 2 { // Namespace + Secret
		t.Fatalf("want ns+secret, got %d objects", len(objs))
	}
	sec := objs[1]
	if sec.GetKind() != "Secret" || sec.GetName() != "cube-idp-gateway-tls" || sec.GetNamespace() != "traefik" {
		t.Fatalf("secret identity: %s/%s/%s", sec.GetKind(), sec.GetNamespace(), sec.GetName())
	}
	typ, _, _ := unstructured.NestedString(sec.Object, "type")
	if typ != "kubernetes.io/tls" {
		t.Fatalf("type: %s", typ)
	}
	crt, _, _ := unstructured.NestedString(sec.Object, "stringData", "tls.crt")
	if crt == "" {
		t.Fatal("tls.crt empty")
	}
	if !ca.LeafStillValid([]byte(crt), []string{gw.Host, "*." + gw.Host}, 24*time.Hour) {
		t.Fatal("issued cert must cover the host and its wildcard")
	}
}

// TestRunOrdersCABeforeCluster asserts the D12 ordering requirement ("cert
// material is generated before cluster creation") without refactoring
// Run's step sequence into a seam: Run has no test hook for its step order
// (checkpoint 0.13's structure is a single linear function that writes
// "▸ [stage] ..." lines to out as it goes), so a full refactor into an
// ordered []stepFn slice would be the invasive option the brief allows
// falling back to only if no seam exists.
//
// Instead this exploits the sequential, synchronous nature of Run itself:
// pointing spec.cluster at provider "existing" with a kubeconfig context
// that cannot exist makes cluster.Provider.Ensure fail fast (a local
// map-lookup miss in internal/cluster/existing.go — no network I/O, no
// real cluster contacted, so this is fast and hermetic). Run prints the
// "ca" step immediately before calling cluster.New/Ensure and returns the
// error before ever printing the "cluster" step. So observing "▸ [ca]" in
// the captured output AND the total absence of "▸ [cluster]" proves the CA
// step executed strictly before the (failed) attempt to ensure the
// cluster — exactly the ordering D12 requires — using only Run's existing,
// unmodified public entry point.
func TestRunOrdersCABeforeCluster(t *testing.T) {
	// Isolate trust.Dir() (os.UserConfigDir(), which reads $HOME on this
	// platform) and mkcert adoption from the real developer machine: Run's
	// first step really does write CA files to disk.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CAROOT", t.TempDir())

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "cube.yaml")
	cfg := `apiVersion: cube-idp.dev/v1alpha1
kind: Cube
metadata:
  name: ordertest
spec:
  cluster:
    provider: existing
    context: cube-idp-ordering-test-context-does-not-exist
  engine:
    type: flux
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	// Task 14b: Run takes a *ui.Console now; RunPipeline (with a
	// bytes.Buffer, which always projects plain) is the seam that replaces
	// the old `Run(ctx, cfgPath, &out)` call. The assertions below are
	// byte-for-byte the pre-14b ones — only the call plumbing changed.
	var out bytes.Buffer
	err := ui.RunPipeline(context.Background(), "up", &out,
		func(ctx context.Context, con *ui.Console) error {
			return Run(ctx, Options{ConfigPath: cfgPath, Con: con})
		})
	if err == nil {
		t.Fatal("want an error from the unreachable cluster context, got nil")
	}
	got := out.String()
	if !strings.Contains(got, "▸ [ca]") {
		t.Fatalf("ca step must run before cluster.Ensure is attempted; output:\n%s", got)
	}
	// R1 (TUI spec §5): the cluster stage now prints a "▸ [cluster] msg...\n"
	// start line before Ensure runs, so its presence no longer disproves the
	// ordering — what must stay absent is a cluster COMPLETION line (one
	// without the "..." start suffix).
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, "▸ [cluster]") && !strings.HasSuffix(line, "...") {
			t.Fatalf("cluster step must not have completed (Ensure should have failed first); output:\n%s", got)
		}
	}
}
