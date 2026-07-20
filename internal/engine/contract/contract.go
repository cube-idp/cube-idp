// Package contract is the shared GitOpsEngine conformance suite.
// Every engine implementation registers itself via a small contract_test.go
// and must pass identical assertions — the mechanism that keeps the
// GitOpsEngine interface seam honest (an abstraction with one implementation
// is a lie). See docs/adr/0018-gitops-engine-interface-seam.md.
package contract

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/yaml"

	"github.com/cube-idp/cube-idp/internal/apply"
	"github.com/cube-idp/cube-idp/internal/diag"
	"github.com/cube-idp/cube-idp/internal/engine"
	"github.com/cube-idp/cube-idp/internal/pack"
	"github.com/cube-idp/cube-idp/internal/registry"
)

type Impl struct {
	Name string
	New  func() engine.Engine
	// CRDs returns the engine's own CustomResourceDefinitions (testdata —
	// formerly extracted from the embedded install manifests) so envtest
	// can apply the engine's delivered objects.
	CRDs func() ([]byte, error)
}

func Run(t *testing.T, impl Impl) {
	ctx := context.Background()
	demo := &pack.Rendered{Name: "demo", Version: "0.1.0"}
	demoRef := engine.ArtifactRef{Repo: "packs/demo", Tag: "0.1.0"}
	demoGit := engine.GitSource{
		URL:    "http://gitea-http.gitea.svc.cluster.local:3000/gitea_admin/demo.git",
		Branch: "main",
		Path:   "./",
	}

	t.Run("deliver_returns_addressable_objects", func(t *testing.T) {
		objs, err := impl.New().Deliver(ctx, demo, demoRef)
		if err != nil {
			t.Fatal(err)
		}
		if len(objs) == 0 {
			t.Fatal("Deliver returned no objects")
		}
		for _, o := range objs {
			if o.GetKind() == "" || o.GetName() == "" || o.GetNamespace() == "" {
				t.Fatalf("delivery object missing kind/name/namespace: %v", o.Object)
			}
		}
	})

	t.Run("deliver_references_the_artifact", func(t *testing.T) {
		objs, err := impl.New().Deliver(ctx, demo, demoRef)
		if err != nil {
			t.Fatal(err)
		}
		blob := marshalAll(t, objs)
		wantURL := fmt.Sprintf("oci://%s/%s", registry.InClusterURL, demoRef.Repo)
		if !strings.Contains(blob, wantURL) {
			t.Fatalf("delivery objects never reference %q:\n%s", wantURL, blob)
		}
		if !strings.Contains(blob, demoRef.Tag) {
			t.Fatalf("delivery objects never reference tag %q:\n%s", demoRef.Tag, blob)
		}
	})

	t.Run("deliver_is_deterministic", func(t *testing.T) {
		a, err := impl.New().Deliver(ctx, demo, demoRef)
		if err != nil {
			t.Fatal(err)
		}
		b, err := impl.New().Deliver(ctx, demo, demoRef)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(a, b) {
			t.Fatal("two Deliver calls with identical input produced different objects")
		}
	})

	t.Run("deliver_names_are_distinct_per_pack", func(t *testing.T) {
		aObjs, _ := impl.New().Deliver(ctx, demo, demoRef)
		other := &pack.Rendered{Name: "other", Version: "0.1.0"}
		bObjs, _ := impl.New().Deliver(ctx, other, engine.ArtifactRef{Repo: "packs/other", Tag: "0.1.0"})
		names := map[string]bool{}
		for _, o := range aObjs {
			names[o.GetKind()+"/"+o.GetName()] = true
		}
		for _, o := range bObjs {
			if names[o.GetKind()+"/"+o.GetName()] {
				t.Fatalf("packs demo and other collide on %s/%s — down/prune cannot tell them apart", o.GetKind(), o.GetName())
			}
		}
	})

	t.Run("deliver_git_returns_addressable_objects", func(t *testing.T) {
		objs, err := impl.New().DeliverGit(ctx, "demo", demoGit, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(objs) == 0 {
			t.Fatal("DeliverGit returned no objects")
		}
		for _, o := range objs {
			if o.GetKind() == "" || o.GetName() == "" || o.GetNamespace() == "" {
				t.Fatalf("git delivery object missing kind/name/namespace: %v", o.Object)
			}
		}
		blob := marshalAll(t, objs)
		if !strings.Contains(blob, demoGit.URL) {
			t.Fatalf("git delivery objects never reference the clone URL %q:\n%s", demoGit.URL, blob)
		}
		if !strings.Contains(blob, demoGit.Branch) {
			t.Fatalf("git delivery objects never reference branch %q:\n%s", demoGit.Branch, blob)
		}
	})

	// p6 DEP3: every engine must answer OrdersDeliveries consciously and
	// translate a resolved DependsOn into SOME engine-native ordering
	// intent — this is deliberately format-agnostic (flux: spec.dependsOn;
	// argocd: the cube-idp.dev/depends-on annotation) so a future engine
	// can pick its own shape, but it cannot silently drop the dependency.
	t.Run("deliver_translates_depends_on", func(t *testing.T) {
		depRendered := &pack.Rendered{Name: "demo", Version: "0.1.0", DependsOn: []string{"x"}}
		objs, err := impl.New().Deliver(ctx, depRendered, demoRef)
		if err != nil {
			t.Fatal(err)
		}
		blob := marshalAll(t, objs)
		if impl.New().OrdersDeliveries() {
			if !strings.Contains(blob, "dependsOn") {
				t.Fatalf("%s orders deliveries natively but Deliver with DependsOn produced no dependsOn reference:\n%s", impl.Name, blob)
			}
		} else {
			if !strings.Contains(blob, "cube-idp.dev/depends-on") {
				t.Fatalf("%s does not order deliveries natively but Deliver with DependsOn produced no cube-idp.dev/depends-on annotation:\n%s", impl.Name, blob)
			}
		}
	})

	t.Run("poke_is_idempotent", func(t *testing.T) {
		cfg := startEnvtest(t)
		a, err := apply.New(cfg, "contract-"+impl.Name)
		if err != nil {
			t.Fatal(err)
		}
		installEngineCRDs(t, cfg, impl)

		delivered, err := impl.New().Deliver(ctx, demo, demoRef)
		if err != nil {
			t.Fatal(err)
		}
		ensureNamespaces(t, a, delivered)
		if err := a.Apply(ctx, delivered, false, time.Minute); err != nil {
			t.Fatalf("delivered objects must SSA-apply cleanly: %v", err)
		}

		// Idempotent + cheap: two Pokes in a row both succeed (annotation patch).
		if err := impl.New().Poke(ctx, a, "demo"); err != nil {
			t.Fatalf("first Poke: %v", err)
		}
		if err := impl.New().Poke(ctx, a, "demo"); err != nil {
			t.Fatalf("second Poke must be idempotent: %v", err)
		}

		// A pack with no delivery source to poke is CUBE-3007.
		err = impl.New().Poke(ctx, a, "never-delivered")
		if err == nil {
			t.Fatal("Poke of an undelivered pack must error")
		}
		var de *diag.Error
		if !errors.As(err, &de) || de.Code != diag.CodePokeTargetMissing {
			t.Fatalf("Poke of an undelivered pack must be CUBE-3007, got %v", err)
		}
	})

	// Engine-as-pack (2026-07-19): the engine's own install left the engine
	// seam — `up` SSAs the rendered cube-engine-<type> pack, not
	// eng.InstallManifests() (deleted). The install-manifests-parse and
	// install-on-cluster subtests retired with it; the health/uninstall
	// guarantee they carried survives here, leaner: Health and Uninstall must
	// tolerate a fresh, engine-less cluster (no install step precedes them).
	t.Run("health_tolerates_fresh_cluster", func(t *testing.T) {
		cfg := startEnvtest(t)
		a, err := apply.New(cfg, "contract-"+impl.Name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := impl.New().Health(ctx, a); err != nil {
			t.Fatalf("Health must not error before the engine is installed: %v", err)
		}
		if err := impl.New().Uninstall(ctx, a, time.Minute); err != nil {
			t.Fatalf("Uninstall must not error on an empty cluster: %v", err)
		}
	})
}

func marshalAll(t *testing.T, objs []*unstructured.Unstructured) string {
	t.Helper()
	var b strings.Builder
	for _, o := range objs {
		y, err := yaml.Marshal(o.Object)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(y)
		b.WriteString("---\n")
	}
	return b.String()
}

// installEngineCRDs installs an engine's own CustomResourceDefinitions (from
// the impl's CRDs testdata fixture — formerly extracted from the embedded
// install manifests) into the envtest API server, so its delivered objects
// (flux OCIRepository/GitRepository/Kustomization, argocd Application) can be
// applied and poked. Engine-agnostic: each engine ships exactly the CRDs its
// delivery shapes need.
func installEngineCRDs(t *testing.T, cfg *rest.Config, impl Impl) {
	t.Helper()
	raw, err := impl.CRDs()
	if err != nil {
		t.Fatal(err)
	}
	objs, err := apply.ParseMultiDoc(raw)
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
		t.Fatal("engine install manifests carry no CRDs — cannot apply delivered objects")
	}
	if _, err := envtest.InstallCRDs(cfg, envtest.CRDInstallOptions{CRDs: crds}); err != nil {
		t.Fatal(err)
	}
}

// ensureNamespaces creates every namespace referenced by objs (idempotently),
// since envtest starts empty and delivered objects live in the engine's
// namespace (flux-system / argocd) which no controller has created yet.
func ensureNamespaces(t *testing.T, a *apply.Applier, objs []*unstructured.Unstructured) {
	t.Helper()
	seen := map[string]bool{}
	for _, o := range objs {
		ns := o.GetNamespace()
		if ns == "" || seen[ns] {
			continue
		}
		seen[ns] = true
		nsObj := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}
		if err := a.Client().Create(context.Background(), nsObj); err != nil && !apierrors.IsAlreadyExists(err) {
			t.Fatal(err)
		}
	}
}

func startEnvtest(t *testing.T) *rest.Config {
	t.Helper()
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("KUBEBUILDER_ASSETS not set — run via `make test-engines`")
	}
	env := &envtest.Environment{}
	cfg, err := env.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = env.Stop() })
	return cfg
}
