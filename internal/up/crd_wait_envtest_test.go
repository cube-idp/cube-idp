package up

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
	"time"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"github.com/cube-idp/cube-idp/internal/apply"
	"github.com/cube-idp/cube-idp/internal/diag"
	"github.com/cube-idp/cube-idp/internal/ui"
)

// httpRouteCRDObject is a minimal-but-valid CustomResourceDefinition for the
// Gateway API HTTPRoute kind waitCRDEstablished polls for. It carries only
// what the API server needs to register and Establish the kind — the wait
// keys on the Established condition, not on the schema's shape.
func httpRouteCRDObject() *apiextensionsv1.CustomResourceDefinition {
	return &apiextensionsv1.CustomResourceDefinition{
		// gateway.networking.k8s.io is a protected group: the API server
		// rejects a CRD for it without this approval annotation.
		ObjectMeta: metav1.ObjectMeta{
			Name:        httpRouteCRD,
			Annotations: map[string]string{"api-approved.kubernetes.io": "https://github.com/kubernetes-sigs/gateway-api/pull/1"},
		},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "gateway.networking.k8s.io",
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural:   "httproutes",
				Singular: "httproute",
				Kind:     "HTTPRoute",
				ListKind: "HTTPRouteList",
			},
			Scope: apiextensionsv1.NamespaceScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{
				Name:    "v1",
				Served:  true,
				Storage: true,
				Schema: &apiextensionsv1.CustomResourceValidation{
					OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
						Type:                   "object",
						XPreserveUnknownFields: boolPtr(true),
					},
				},
			}},
		},
	}
}

func boolPtr(b bool) *bool { return &b }

// runWithConsole runs fn with a real *ui.Console wired to a throwaway buffer
// (never a TTY -> plain renderer), the same harness the plain-output tests use,
// so waitCRDEstablished's Progress calls exercise the real Console path.
func runWithConsole(t *testing.T, fn func(context.Context, *ui.Console) error) error {
	t.Helper()
	var out bytes.Buffer
	return ui.RunPipeline(context.Background(), "up", &out, fn)
}

// TestWaitCRDEstablished exercises the Gateway-API-CRD wait seam against a real
// API server (envtest): it times out with CUBE-5005 when the CRD is absent (the
// envoy-gateway race the fix targets), and returns cleanly once the CRD is
// installed and the API server has Established it (the steady state both the
// traefik and envoy paths reach).
//
// Mirrors tls_envtest_test.go: this package also has unit tests that run
// without envtest, so the harness is per-test (skip + own env.Start) rather
// than a TestMain gate. Run via `make test-apply`; without KUBEBUILDER_ASSETS
// it skips.
func TestWaitCRDEstablished(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("KUBEBUILDER_ASSETS not set; run via `make test-apply`")
	}

	env := &envtest.Environment{}
	cfg, err := env.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = env.Stop() })

	a, err := apply.New(cfg, "crdtest")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// 1. Absent CRD: a short hard deadline must fire CUBE-5005, not hang.
	err = runWithConsole(t, func(_ context.Context, con *ui.Console) error {
		return waitCRDEstablished(ctx, a, con, httpRouteCRD, 1*time.Second)
	})
	if err == nil {
		t.Fatal("expected a timeout error while the HTTPRoute CRD is absent")
	}
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != diag.CodeRegistryRouteCRDTimeout {
		t.Fatalf("expected CUBE-5005 (CodeRegistryRouteCRDTimeout), got %v", err)
	}

	// 2. Install the CRD; the API server's establishing controller flips its
	//    Established condition true, and the wait returns nil within deadline.
	if err := a.Client().Create(ctx, httpRouteCRDObject()); err != nil {
		t.Fatalf("installing the HTTPRoute CRD: %v", err)
	}
	t.Cleanup(func() {
		_ = a.Client().Delete(context.Background(), &apiextensionsv1.CustomResourceDefinition{
			ObjectMeta: metav1.ObjectMeta{Name: httpRouteCRD},
		})
	})
	err = runWithConsole(t, func(_ context.Context, con *ui.Console) error {
		return waitCRDEstablished(ctx, a, con, httpRouteCRD, 30*time.Second)
	})
	if err != nil {
		t.Fatalf("wait must succeed once the CRD is Established: %v", err)
	}

	// Sanity: the CRD really is Established (guards against a false green from
	// a wait that returned before the condition was set).
	var crd apiextensionsv1.CustomResourceDefinition
	if err := a.Client().Get(ctx, client.ObjectKey{Name: httpRouteCRD}, &crd); err != nil {
		t.Fatal(err)
	}
	if !crdEstablished(&crd) {
		t.Fatal("CRD should report Established=true after a successful wait")
	}
}
