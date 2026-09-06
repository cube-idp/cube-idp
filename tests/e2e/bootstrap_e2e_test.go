package e2e

import (
	"context"
	"maps"
	"os"
	"slices"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
	"github.com/cube-idp/cube-idp/internal/bootstrap"
	"github.com/cube-idp/cube-idp/internal/cluster"
	"github.com/cube-idp/cube-idp/internal/cluster/kind"
	"github.com/cube-idp/cube-idp/internal/engine/flux"
	"github.com/cube-idp/cube-idp/internal/engine/substrate"
	"github.com/cube-idp/cube-idp/internal/kube"
)

// The sync fixture the round-trip points Flux at: an orphan branch in this
// repository, which by rule must never be moved. It lives on a branch because
// EngineSource.Ref is emitted as a git branch
// (internal/engine/flux/flux.go:83) and no tag or commit can be expressed
// through production config today. The branch carries its own rules in its
// commit message; docs/domains/bootstrap.md is the pointer.
//
// syncFixtureCommit is what makes the pin real. The branch is protected
// against force-push and deletion but NOT against ordinary updates
// (lock_branch is false), so nothing at the remote prevents it moving. Drift
// is caught here, by asserting the fetched commit.
const (
	syncFixtureURL    = "https://github.com/cube-idp/cube-idp.git"
	syncFixtureRef    = "e2e/sync-fixture"
	syncFixturePath   = "./"
	syncFixtureCommit = "1f85328936ab1a602e636415130c0e20b20633c4"
)

var (
	gitRepoGVR = schema.GroupVersionResource{Group: "source.toolkit.fluxcd.io", Version: "v1", Resource: "gitrepositories"}
	kustomGVR  = schema.GroupVersionResource{Group: "kustomize.toolkit.fluxcd.io", Version: "v1", Resource: "kustomizations"}
	configMap  = schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}
	namespaces = schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}
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
			URL:  syncFixtureURL,
			Ref:  syncFixtureRef, Path: syncFixturePath, Interval: "1m",
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

	// Round-trip. Neither assertion is fatal, so once both are reached a single
	// run reports which half of "fetched the right bytes and applied them"
	// broke. Reaching them is not guaranteed: an earlier t.Fatalf — InstallEngine
	// above, most of all — still ends the run before either executes.
	assertSyncDelivered(ctx, t, dyn)
	assertFetchedRevision(ctx, t, dyn)

	if err := p.Delete(ctx, name); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

// assertSyncDelivered proves the sync path actually delivered content, which
// no existing assertion did: the CR checks prove the wiring was applied, and
// the closing GitRepository poll proves only the fetch. The apply was judged
// only inside InstallEngine, so nothing after it distinguished "reconciled"
// from "reconciled and delivered what we expected".
//
// It compares against the same constants the hermetic contract test locks the
// mirror under testdata to, so mirror and delivered object are tied together.
// Flux-injected metadata (kustomize.toolkit.fluxcd.io/name and /namespace) is
// deliberately outside the compared set — the mirror's closed schema rejects
// labels and annotations, so the applied object always carries metadata the
// mirror does not and whole-object equality would be permanently red.
func assertSyncDelivered(ctx context.Context, t *testing.T, dyn dynamic.Interface) {
	t.Helper()
	deliveredCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	var got *unstructured.Unstructured
	err := wait.PollUntilContextCancel(deliveredCtx, 5*time.Second, true, func(ctx context.Context) (bool, error) {
		o, err := dyn.Resource(configMap).Namespace(fixtureNamespaceName).
			Get(ctx, fixtureConfigMapName, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		got = o
		return true, nil
	})
	if err != nil {
		t.Errorf("sync fixture ConfigMap %s/%s never appeared: %v",
			fixtureNamespaceName, fixtureConfigMapName, err)
		return
	}
	if ns := got.GetNamespace(); ns != fixtureNamespaceName {
		t.Errorf("delivered ConfigMap namespace = %q, want %q", ns, fixtureNamespaceName)
	}
	// The Namespace object itself, not just the ConfigMap's placement field.
	// That the fixture carries its own Namespace is the property this whole
	// change turns on: the emitted Kustomization sets no targetNamespace, so
	// nothing else would have created it.
	if _, err := dyn.Resource(namespaces).Get(ctx, fixtureNamespaceName, metav1.GetOptions{}); err != nil {
		t.Errorf("fixture Namespace %s was not delivered: %v", fixtureNamespaceName, err)
	}
	// The key set exactly, not just the one key's value: an extra key is
	// content the source delivered that nothing reviewed.
	data, _, err := unstructured.NestedStringMap(got.Object, "data")
	if err != nil {
		t.Errorf("delivered ConfigMap data is not a string map: %v", err)
		return
	}
	if len(data) != 1 {
		t.Errorf("delivered ConfigMap data has %d keys (%v), want exactly [%s]",
			len(data), slices.Sorted(maps.Keys(data)), fixtureDataKey)
	}
	if got := data[fixtureDataKey]; got != fixtureDataValue {
		t.Errorf("delivered ConfigMap data.%s = %q, want %q", fixtureDataKey, got, fixtureDataValue)
	}
}

// assertFetchedRevision waits for the source to report an artifact and checks
// it is exactly the pinned fixture commit. The branch is protected against
// force-push and deletion but not against ordinary updates, so this is what
// turns fixture drift into a failure instead of a silent content change
// (https://github.com/cube-idp/cube-idp/issues/204).
func assertFetchedRevision(ctx context.Context, t *testing.T, dyn dynamic.Interface) {
	t.Helper()
	readyCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	var got *unstructured.Unstructured
	err := wait.PollUntilContextCancel(readyCtx, 5*time.Second, true, func(ctx context.Context) (bool, error) {
		o, err := dyn.Resource(gitRepoGVR).Namespace("flux-system").Get(ctx, "flux-system", metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		got = o
		return readyCondition(o), nil
	})
	if err != nil {
		t.Errorf("GitRepository did not reconcile Ready: %v", err)
		return
	}
	revision, _, _ := unstructured.NestedString(got.Object, "status", "artifact", "revision")
	if err := checkFetchedRevision(revision, syncFixtureCommit); err != nil {
		t.Errorf("sync source pin: %v", err)
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
