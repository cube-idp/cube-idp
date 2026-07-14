package flux

import (
	"context"
	"errors"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"github.com/rafpe/cube-idp/internal/apply"
	"github.com/rafpe/cube-idp/internal/diag"
	"github.com/rafpe/cube-idp/internal/engine"
	"github.com/rafpe/cube-idp/internal/pack"
)

// installFluxCRDs installs the flux CRDs from the embedded install manifests
// into the envtest API server and creates the flux-system namespace, so
// delivered OCIRepository/GitRepository/Kustomization objects can be applied.
func installFluxCRDs(t *testing.T) {
	t.Helper()
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
	if _, err := envtest.InstallCRDs(testREST, envtest.CRDInstallOptions{CRDs: crds}); err != nil {
		t.Fatal(err)
	}
	a, err := apply.New(testREST, "pokecube")
	if err != nil {
		t.Fatal(err)
	}
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: fluxNS}}
	if err := a.Client().Create(context.Background(), ns); err != nil && !apierrors.IsAlreadyExists(err) {
		t.Fatal(err)
	}
}

// cleanupDelivered deletes objs once the test ends. The envtest API server is
// shared across the whole package (one TestMain), and TestUninstall...'s final
// assertion lists flux-system unfiltered, so every delivered object a test
// applies must be removed or it leaks into other tests.
func cleanupDelivered(t *testing.T, c client.Client, objs []*unstructured.Unstructured) {
	t.Helper()
	t.Cleanup(func() {
		for _, o := range objs {
			if err := c.Delete(context.Background(), o); err != nil && !apierrors.IsNotFound(err) {
				t.Errorf("cleanup delete %s/%s: %v", o.GetKind(), o.GetName(), err)
			}
		}
	})
}

func TestPokePatchesOCIRepositoryAnnotation(t *testing.T) {
	if testREST == nil {
		t.Skip("KUBEBUILDER_ASSETS not set; envtest unavailable")
	}
	ctx := context.Background()
	installFluxCRDs(t)

	a, err := apply.New(testREST, "pokecube")
	if err != nil {
		t.Fatal(err)
	}
	f := New()

	// Deliver an OCI-shaped pack "demo" and apply it (wait=false; no controllers).
	delivered, err := f.Deliver(ctx, &pack.Rendered{Name: "demo", Version: "0.1.0"},
		engine.ArtifactRef{Repo: "packs/demo", Tag: "0.1.0"})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Apply(ctx, delivered, false, time.Minute); err != nil {
		t.Fatal(err)
	}
	cleanupDelivered(t, a.Client(), delivered)

	before := time.Now().Add(-time.Second)
	if err := f.Poke(ctx, a, "demo"); err != nil {
		t.Fatalf("Poke: %v", err)
	}

	repo := &unstructured.Unstructured{}
	repo.SetAPIVersion("source.toolkit.fluxcd.io/v1")
	repo.SetKind("OCIRepository")
	if err := a.Client().Get(ctx, client.ObjectKey{Namespace: fluxNS, Name: "cube-idp-demo"}, repo); err != nil {
		t.Fatal(err)
	}
	got := repo.GetAnnotations()["reconcile.fluxcd.io/requestedAt"]
	if got == "" {
		t.Fatal("Poke must set the reconcile.fluxcd.io/requestedAt annotation flux reconcile writes")
	}
	ts, err := time.Parse(time.RFC3339Nano, got)
	if err != nil {
		t.Fatalf("requestedAt %q is not RFC3339Nano: %v", got, err)
	}
	if ts.Before(before) {
		t.Fatalf("requestedAt %v predates the Poke call (%v)", ts, before)
	}
}

func TestPokeFallsBackToGitRepository(t *testing.T) {
	if testREST == nil {
		t.Skip("KUBEBUILDER_ASSETS not set; envtest unavailable")
	}
	ctx := context.Background()
	installFluxCRDs(t)

	a, err := apply.New(testREST, "pokecube")
	if err != nil {
		t.Fatal(err)
	}
	f := New()

	// git delivery: no OCIRepository, only a GitRepository named cube-idp-gitpack.
	delivered, err := f.DeliverGit(ctx, "gitpack",
		engine.GitSource{URL: "http://gitea-http.gitea.svc.cluster.local:3000/gitea_admin/gitpack.git", Branch: "main", Path: "./"})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Apply(ctx, delivered, false, time.Minute); err != nil {
		t.Fatal(err)
	}
	cleanupDelivered(t, a.Client(), delivered)

	if err := f.Poke(ctx, a, "gitpack"); err != nil {
		t.Fatalf("Poke of a git-delivered pack must find the GitRepository: %v", err)
	}
	repo := &unstructured.Unstructured{}
	repo.SetAPIVersion("source.toolkit.fluxcd.io/v1")
	repo.SetKind("GitRepository")
	if err := a.Client().Get(ctx, client.ObjectKey{Namespace: fluxNS, Name: "cube-idp-gitpack"}, repo); err != nil {
		t.Fatal(err)
	}
	if repo.GetAnnotations()["reconcile.fluxcd.io/requestedAt"] == "" {
		t.Fatal("Poke must set requestedAt on the GitRepository")
	}
}

func TestPokeUndeliveredPackIsCUBE3007(t *testing.T) {
	if testREST == nil {
		t.Skip("KUBEBUILDER_ASSETS not set; envtest unavailable")
	}
	ctx := context.Background()
	installFluxCRDs(t)

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
