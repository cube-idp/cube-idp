package e2e

import (
	"context"
	"os"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
	"github.com/cube-idp/cube-idp/internal/bootstrap"
	"github.com/cube-idp/cube-idp/internal/cluster"
	"github.com/cube-idp/cube-idp/internal/cluster/kind"
	"github.com/cube-idp/cube-idp/internal/engine/flux"
	"github.com/cube-idp/cube-idp/internal/engine/substrate"
	"github.com/cube-idp/cube-idp/internal/kube"
)

var (
	gitRepoGVR = schema.GroupVersionResource{Group: "source.toolkit.fluxcd.io", Version: "v1", Resource: "gitrepositories"}
	kustomGVR  = schema.GroupVersionResource{Group: "kustomize.toolkit.fluxcd.io", Version: "v1", Resource: "kustomizations"}
	configMap  = schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}
)

// TestBootstrapFluxRoundTrip provisions a kind cluster, injects its clients
// into the bootstrap domain exactly like the CLI edge, and runs InstallEngine
// against a public Git source: Flux installs and becomes ready, the source +
// Kustomization CRs are applied (exercising the mapper reset-retry for the
// just-installed CRDs), the inventory is recorded, and the GitRepository
// reconciles Ready — the real round-trip. Teardown via the seam.
func TestBootstrapFluxRoundTrip(t *testing.T) {
	if os.Getenv("CUBE_E2E") != "1" {
		t.Skip("e2e is opt-in: run via `make test-e2e` (sets CUBE_E2E=1)")
	}
	if !runtimeAvailable() {
		t.Skip("no container runtime reachable (docker/podman) — skipping e2e")
	}
	const name = "cube-bootstrap-e2e"
	const domain = name + "." + v1alpha1.DefaultBaseDomain
	ctx := t.Context()

	p, err := kind.New()
	if err != nil {
		t.Fatalf("kind.New: %v", err)
	}
	t.Cleanup(func() { deleteCluster(t, p, name) })
	if err := p.Ensure(ctx, cluster.Spec{Name: name}); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	raw, err := p.Kubeconfig(ctx, name)
	if err != nil {
		t.Fatalf("Kubeconfig: %v", err)
	}
	client, err := kube.New(raw, "")
	if err != nil {
		t.Fatalf("kube.New: %v", err)
	}

	applier := bootstrap.NewApplier(client.Dynamic(), client.RESTMapper(), substrate.Namespace)
	engine := &v1alpha1.EngineSpec{
		Provider: v1alpha1.EngineProviderFlux,
		Source: &v1alpha1.EngineSource{
			Kind: v1alpha1.EngineSourceGit,
			URL:  "https://github.com/stefanprodan/podinfo",
			Ref:  "master", Path: "./kustomize", Interval: "1m",
		},
	}
	substrateObjs, err := substrate.Objects()
	if err != nil {
		t.Fatalf("substrate.Objects: %v", err)
	}
	drv := flux.New()
	wiringObjs, err := drv.SourceObjects(ctx, *engine)
	if err != nil {
		t.Fatalf("SourceObjects: %v", err)
	}
	// The gateway prerequisites make the run network-dependent inside the
	// cluster — the helm-controller pulls the pinned chart — so the budget is
	// above the CLI's own 10m default rather than the engine-only 6m.
	installCtx, cancel := context.WithTimeout(ctx, 12*time.Minute)
	defer cancel()
	// Phase 2 runs with the driver's real judgment: InstallEngine returns only
	// once the wiring reconciles Ready and fresh against the live cluster. The
	// prerequisite units install between the substrate and the wiring, each
	// waiting the way it declares.
	units := gatewayPrerequisites(t, name, domain)
	install := bootstrap.EngineInstall{
		Substrate:     substrateObjs,
		Prerequisites: units,
		Wiring:        wiringObjs,
		Wait:          bootstrap.EngineWait{Reconciled: drv.Reconciled},
	}
	if err := applier.InstallEngine(installCtx, install); err != nil {
		t.Fatalf("InstallEngine: %v", err)
	}

	dyn := client.Dynamic()
	if _, err := dyn.Resource(configMap).Namespace(substrate.Namespace).
		Get(ctx, bootstrap.InventoryName, metav1.GetOptions{}); err != nil {
		t.Errorf("bootstrap inventory ConfigMap not found: %v", err)
	}
	for _, cr := range []struct {
		gvr  schema.GroupVersionResource
		what string
	}{{gitRepoGVR, "GitRepository"}, {kustomGVR, "Kustomization"}} {
		if _, err := dyn.Resource(cr.gvr).Namespace("flux-system").Get(ctx, "flux-system", metav1.GetOptions{}); err != nil {
			t.Errorf("%s not applied: %v", cr.what, err)
		}
	}

	assertGatewayFabric(ctx, t, dyn, domain)
	assertInventoryCovers(ctx, t, dyn)
	// The splice runs where the edge runs it: after InstallEngine returned,
	// which is after the gateway unit reconciled.
	spliceCoreDNSHere(ctx, t, dyn, name, domain)

	// Round-trip: source-controller fetches the public repo → GitRepository Ready.
	readyCtx, rcancel := context.WithTimeout(ctx, 3*time.Minute)
	defer rcancel()
	err = wait.PollUntilContextCancel(readyCtx, 5*time.Second, true, func(ctx context.Context) (bool, error) {
		o, err := dyn.Resource(gitRepoGVR).Namespace("flux-system").Get(ctx, "flux-system", metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return readyCondition(o), nil
	})
	if err != nil {
		t.Fatalf("GitRepository did not reconcile Ready: %v", err)
	}

	if err := p.Delete(ctx, name); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

// readyCondition reports whether o has a status condition Ready=True.
func readyCondition(o *unstructured.Unstructured) bool {
	conds, _, _ := unstructured.NestedSlice(o.Object, "status", "conditions")
	for _, c := range conds {
		cm, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if t, _, _ := unstructured.NestedString(cm, "type"); t == "Ready" {
			s, _, _ := unstructured.NestedString(cm, "status")
			return s == "True"
		}
	}
	return false
}
