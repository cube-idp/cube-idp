package spoke

import (
	"context"
	"os"
	"testing"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"github.com/cube-idp/cube-idp/internal/kube"
)

// startEnv boots envtest exactly the way internal/up's envtest harness does
// (crd_wait_envtest_test.go: no build tag, per-test skip + own env.Start).
// Without KUBEBUILDER_ASSETS the test skips; run it with the Makefile's
// setup-envtest incantation. The returned Conn carries only REST — exactly
// what Bootstrap consumes.
func startEnv(t *testing.T) (*kube.Conn, func()) {
	t.Helper()
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("KUBEBUILDER_ASSETS not set; run via the Makefile's setup-envtest incantation")
	}
	env := &envtest.Environment{}
	cfg, err := env.Start()
	if err != nil {
		t.Fatal(err)
	}
	return &kube.Conn{REST: cfg}, func() { _ = env.Stop() }
}

func TestBootstrapIdempotentAndTokenIssued(t *testing.T) {
	conn, stop := startEnv(t)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cred1, err := Bootstrap(ctx, conn, "flux", 30*time.Second)
	if err != nil {
		t.Fatalf("first bootstrap: %v", err)
	}
	if cred1.Token == "" || len(cred1.CAData) == 0 {
		t.Fatalf("empty credential: %+v", cred1)
	}
	// Second run must succeed cleanly (SSA idempotency) and re-issue.
	cred2, err := Bootstrap(ctx, conn, "flux", 30*time.Second)
	if err != nil {
		t.Fatalf("second bootstrap: %v", err)
	}
	if cred2.Token == "" {
		t.Fatal("re-issued token empty")
	}
}
