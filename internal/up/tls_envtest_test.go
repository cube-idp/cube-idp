package up

import (
	"context"
	"os"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"github.com/rafpe/cube-idp/internal/apply"
	"github.com/rafpe/cube-idp/internal/config"
	"github.com/rafpe/cube-idp/internal/trust"
)

// TestEnsureGatewayTLSReuse exercises ensureGatewayTLS's idempotency contract
// against a real API server (envtest): the first call creates the secret, a
// repeat call with the same CA leaves it untouched (no churn for `up`/`diff`
// re-runs), and a secret whose cert was NOT issued by the current CA is
// re-issued.
//
// Unlike internal/apply, this package has unit tests that must run without
// envtest, so the harness is per-test (skip + own env.Start) rather than an
// os.Exit(0) TestMain gate. Run it via `make test-apply` (which exports
// KUBEBUILDER_ASSETS); without the assets it skips.
func TestEnsureGatewayTLSReuse(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("KUBEBUILDER_ASSETS not set; run via `make test-apply`")
	}
	// ensureGatewayTLS resolves its CA through trust.Dir() (os.UserConfigDir,
	// i.e. $HOME on this platform) and trust.EnsureCA may adopt a mkcert root
	// from $CAROOT — isolate both from the developer's real machine.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CAROOT", t.TempDir())

	env := &envtest.Environment{}
	cfg, err := env.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = env.Stop() })

	a, err := apply.New(cfg, "tlstest")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	gw := config.GatewaySpec{Pack: "traefik", Host: "cube-idp.localtest.me", Port: 8443}
	key := client.ObjectKey{Namespace: gw.Pack, Name: "cube-idp-gateway-tls"}

	// 1. First call creates the namespace + secret.
	if err := ensureGatewayTLS(ctx, a, gw); err != nil {
		t.Fatal(err)
	}
	var sec corev1.Secret
	if err := a.Client().Get(ctx, key, &sec); err != nil {
		t.Fatalf("secret must exist after the first call: %v", err)
	}
	rvCreated := sec.ResourceVersion

	// 2. Second call with the same CA must hit the reuse fast path and not
	// rewrite the secret: resourceVersion unchanged.
	if err := ensureGatewayTLS(ctx, a, gw); err != nil {
		t.Fatal(err)
	}
	if err := a.Client().Get(ctx, key, &sec); err != nil {
		t.Fatal(err)
	}
	if sec.ResourceVersion != rvCreated {
		t.Fatalf("idempotent re-run rewrote the secret: resourceVersion %s -> %s", rvCreated, sec.ResourceVersion)
	}

	// 3. Replace the live cert with one signed by a DIFFERENT CA: the next
	// call must detect it (LeafStillValid fails against the current CA) and
	// re-issue, changing the resourceVersion.
	otherCA, err := trust.EnsureCA(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	foreignCrt, foreignKey, err := otherCA.IssueServerCert([]string{gw.Host, "*." + gw.Host}, 365*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	sec.Data["tls.crt"] = foreignCrt
	sec.Data["tls.key"] = foreignKey
	if err := a.Client().Update(ctx, &sec); err != nil {
		t.Fatal(err)
	}
	if err := a.Client().Get(ctx, key, &sec); err != nil {
		t.Fatal(err)
	}
	rvForeign := sec.ResourceVersion

	if err := ensureGatewayTLS(ctx, a, gw); err != nil {
		t.Fatal(err)
	}
	if err := a.Client().Get(ctx, key, &sec); err != nil {
		t.Fatal(err)
	}
	if sec.ResourceVersion == rvForeign {
		t.Fatal("secret signed by a foreign CA must be re-issued, but resourceVersion did not change")
	}
	// And the re-issued cert must verify against the CA ensureGatewayTLS
	// actually uses (the one under trust.Dir()), covering both hosts.
	dir, err := trust.Dir()
	if err != nil {
		t.Fatal(err)
	}
	ca, err := trust.EnsureCA(dir) // idempotent load of the run's CA
	if err != nil {
		t.Fatal(err)
	}
	if !ca.LeafStillValid(sec.Data["tls.crt"], []string{gw.Host, "*." + gw.Host}, 24*time.Hour) {
		t.Fatal("re-issued cert must be signed by the current CA and cover the host + wildcard")
	}
}
