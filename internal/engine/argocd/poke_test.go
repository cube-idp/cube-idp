package argocd

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"github.com/rafpe/cube-idp/internal/apply"
	"github.com/rafpe/cube-idp/internal/diag"
	"github.com/rafpe/cube-idp/internal/engine"
	"github.com/rafpe/cube-idp/internal/pack"
)

// testREST is non-nil only when KUBEBUILDER_ASSETS is set (make test-engines);
// envtest-backed tests skip without it while the pure unit tests still run
// under a plain `go test ./...`.
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

// installArgoCDCRDs installs the Application CRD from the embedded install
// manifests and creates the argocd namespace, so delivered Applications can
// be applied and poked.
func installArgoCDCRDs(t *testing.T) {
	t.Helper()
	objs, err := New().InstallManifests()
	if err != nil {
		t.Fatal(err)
	}
	var crds []*apiextensionsv1.CustomResourceDefinition
	for _, o := range objs {
		if o.GetKind() != "CustomResourceDefinition" {
			continue
		}
		crd := &apiextensionsv1.CustomResourceDefinition{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(o.Object, crd); err != nil {
			t.Fatal(err)
		}
		crds = append(crds, crd)
	}
	if _, err := envtest.InstallCRDs(testREST, envtest.CRDInstallOptions{CRDs: crds}); err != nil {
		t.Fatal(err)
	}
	a, err := apply.New(testREST, "pokecube")
	if err != nil {
		t.Fatal(err)
	}
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: Namespace}}
	if err := a.Client().Create(context.Background(), ns); err != nil && !apierrors.IsAlreadyExists(err) {
		t.Fatal(err)
	}
}

func TestPokePatchesApplicationRefresh(t *testing.T) {
	if testREST == nil {
		t.Skip("KUBEBUILDER_ASSETS not set; envtest unavailable")
	}
	ctx := context.Background()
	installArgoCDCRDs(t)

	a, err := apply.New(testREST, "pokecube")
	if err != nil {
		t.Fatal(err)
	}
	g := New()
	delivered, err := g.Deliver(ctx, &pack.Rendered{Name: "demo", Version: "0.1.0"},
		engine.ArtifactRef{Repo: "packs/demo", Tag: "0.1.0"})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Apply(ctx, delivered, false, time.Minute); err != nil {
		t.Fatal(err)
	}

	if err := g.Poke(ctx, a, "demo"); err != nil {
		t.Fatalf("Poke: %v", err)
	}
	app := &unstructured.Unstructured{}
	app.SetAPIVersion("argoproj.io/v1alpha1")
	app.SetKind("Application")
	if err := a.Client().Get(ctx, client.ObjectKey{Namespace: Namespace, Name: "cube-idp-demo"}, app); err != nil {
		t.Fatal(err)
	}
	if got := app.GetAnnotations()["argocd.argoproj.io/refresh"]; got != "normal" {
		t.Fatalf("refresh annotation = %q, want \"normal\"", got)
	}
}

func TestPokeUndeliveredPackIsCUBE3007(t *testing.T) {
	if testREST == nil {
		t.Skip("KUBEBUILDER_ASSETS not set; envtest unavailable")
	}
	ctx := context.Background()
	installArgoCDCRDs(t)

	a, err := apply.New(testREST, "pokecube")
	if err != nil {
		t.Fatal(err)
	}
	err = New().Poke(ctx, a, "never-delivered")
	if err == nil {
		t.Fatal("Poke of an undelivered pack must error")
	}
	var de *diag.Error
	if !errors.As(err, &de) || de.Code != diag.CodePokeTargetMissing {
		t.Fatalf("want CUBE-3007, got %v", err)
	}
}
