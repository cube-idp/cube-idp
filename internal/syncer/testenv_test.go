package syncer

import (
	"os"
	"testing"

	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// testREST is a package-scoped envtest API server, the same pattern used by
// internal/apply, internal/engine/flux, and internal/engine/argocd — nil
// (with every envtest-gated test skipping) when KUBEBUILDER_ASSETS isn't
// set, so `go test ./...` without `make test-apply` still passes.
var testREST *rest.Config

func TestMain(m *testing.M) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		os.Exit(m.Run())
	}
	env := &envtest.Environment{}
	cfg, err := env.Start()
	if err != nil {
		panic(err)
	}
	testREST = cfg
	code := m.Run()
	_ = env.Stop()
	os.Exit(code)
}
