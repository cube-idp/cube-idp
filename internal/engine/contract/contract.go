// Package contract is the shared GitOpsEngine conformance suite (spec §5).
// Every engine implementation registers itself via a small contract_test.go
// and must pass identical assertions — the mechanism that keeps D2 honest.
package contract

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/yaml"

	"github.com/rafpe/cube-idp/internal/apply"
	"github.com/rafpe/cube-idp/internal/engine"
	"github.com/rafpe/cube-idp/internal/pack"
	"github.com/rafpe/cube-idp/internal/registry"
)

type Impl struct {
	Name string
	New  func() engine.Engine // Engine carries InstallManifests() (interface method since phase 1 Task 10)
}

func Run(t *testing.T, impl Impl) {
	ctx := context.Background()
	demo := &pack.Rendered{Name: "demo", Version: "0.1.0"}
	demoRef := engine.ArtifactRef{Repo: "packs/demo", Tag: "0.1.0"}

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

	t.Run("install_manifests_parse", func(t *testing.T) {
		objs, err := impl.New().InstallManifests()
		if err != nil {
			t.Fatal(err)
		}
		if len(objs) < 10 {
			t.Fatalf("install manifests look empty (%d objects) — regenerate them", len(objs))
		}
		hasNS := false
		for _, o := range objs {
			if o.GetKind() == "Namespace" {
				hasNS = true
			}
		}
		if !hasNS {
			t.Fatal("install manifests must carry their own Namespace (offline, self-contained install)")
		}
	})

	t.Run("install_health_uninstall_on_cluster", func(t *testing.T) {
		cfg := startEnvtest(t)
		a, err := apply.New(cfg, "contract-"+impl.Name)
		if err != nil {
			t.Fatal(err)
		}
		objs, err := impl.New().InstallManifests()
		if err != nil {
			t.Fatal(err)
		}
		// wait=false: envtest runs no controllers, Deployments never go Ready.
		// Readiness is asserted end-to-end in the CI engine matrix (Task 14).
		if err := a.Apply(ctx, objs, false, time.Minute); err != nil {
			t.Fatalf("install manifests must SSA-apply cleanly: %v", err)
		}
		if _, err := impl.New().Health(ctx, a); err != nil {
			t.Fatalf("Health must not error on a fresh, empty install: %v", err)
		}
		if err := impl.New().Uninstall(ctx, a, time.Minute); err != nil {
			t.Fatalf("Uninstall must not error: %v", err)
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
