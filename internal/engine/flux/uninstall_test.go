package flux

import (
	"context"
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
	"github.com/rafpe/cube-idp/internal/engine"
	"github.com/rafpe/cube-idp/internal/pack"
)

// testREST is non-nil only when KUBEBUILDER_ASSETS is set (make test-apply);
// envtest-backed tests skip without it while the pure unit tests in
// flux_test.go still run under a plain `go test ./...`.
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

// TestUninstallDeletesDeliveredSources proves Uninstall's list/delete/poll
// wiring against a real API server: it deletes every cube-labeled
// Kustomization and OCIRepository in flux-system and returns only once both
// lists are empty.
//
// Honesty note: envtest runs no kustomize-controller, so no prune finalizers
// are ever added and deletion completes immediately. On a real cluster the
// poll is what waits for the controller to finish pruning delivered
// workloads; here it proves the list/delete/poll mechanics, not the
// finalizer wait itself (that is covered by the e2e suite's down).
func TestUninstallDeletesDeliveredSources(t *testing.T) {
	if testREST == nil {
		t.Skip("KUBEBUILDER_ASSETS not set; envtest unavailable")
	}
	ctx := context.Background()

	// Install the flux CRDs from the same embedded manifests `up` applies.
	objs, err := InstallManifests()
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
	if len(crds) == 0 {
		t.Fatal("no CRDs found in flux install manifests")
	}
	if _, err := envtest.InstallCRDs(testREST, envtest.CRDInstallOptions{CRDs: crds}); err != nil {
		t.Fatal(err)
	}

	a, err := apply.New(testREST, "testcube")
	if err != nil {
		t.Fatal(err)
	}
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: fluxNS}}
	if err := a.Client().Create(ctx, ns); err != nil && !apierrors.IsAlreadyExists(err) {
		t.Fatal(err)
	}

	// Deliver produces exactly the labeled OCIRepository + Kustomization
	// shapes Uninstall must find and remove.
	f := New()
	delivered, err := f.Deliver(ctx, &pack.Rendered{Name: "gitea", Version: "0.1.0"},
		engine.ArtifactRef{Repo: "packs/gitea", Tag: "0.1.0"})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Apply(ctx, delivered, false, time.Minute); err != nil {
		t.Fatal(err)
	}

	if err := f.Uninstall(ctx, a, 30*time.Second); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}

	for _, gvk := range []struct{ group, version, kind string }{
		{"kustomize.toolkit.fluxcd.io", "v1", "KustomizationList"},
		{"source.toolkit.fluxcd.io", "v1", "OCIRepositoryList"},
	} {
		list := &unstructured.UnstructuredList{}
		list.SetAPIVersion(gvk.group + "/" + gvk.version)
		list.SetKind(gvk.kind)
		if err := a.Client().List(ctx, list, client.InNamespace(fluxNS)); err != nil {
			t.Fatal(err)
		}
		if len(list.Items) != 0 {
			t.Fatalf("%s: %d object(s) survived Uninstall", gvk.kind, len(list.Items))
		}
	}
}
