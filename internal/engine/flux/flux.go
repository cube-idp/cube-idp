// Package flux implements the GitOpsEngine over Flux's source-controller and
// kustomize-controller. Delivery shape: one OCIRepository + one Kustomization
// per pack, pointing at the in-cluster zot registry (spec §4.1, §4.3).
//
// Only source-controller and kustomize-controller are installed — helm
// rendering happens client-side (pack.Render), so helm-controller is never
// needed in-cluster.
package flux

import (
	"context"
	_ "embed"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cube-idp/cube-idp/internal/apply"
	"github.com/cube-idp/cube-idp/internal/diag"
)

//go:embed manifests/install.yaml
var installYAML []byte

// Flux implements engine.Engine.
type Flux struct{}

// New returns a Flux engine.
func New() *Flux { return &Flux{} }

// OrdersDeliveries reports that flux orders delivery reconciliation
// natively via Kustomization spec.dependsOn (p6 DEP3) — `up`'s wave gate
// never runs for this engine.
func (f *Flux) OrdersDeliveries() bool { return true }

// InstallManifests parses the embedded, pre-rendered Flux install manifest
// (source-controller + kustomize-controller only; see
// hack/gen-flux-manifests.sh for how it's regenerated).
func InstallManifests() ([]*unstructured.Unstructured, error) {
	return apply.ParseMultiDoc(installYAML)
}

func (f *Flux) Install(ctx context.Context, a *apply.Applier, timeout time.Duration) error {
	objs, err := f.InstallManifests()
	if err != nil {
		return diag.Wrap(err, diag.CodeEngineManifestsInv, "embedded flux manifests are invalid",
			"this is a cube-idp bug — regenerate with hack/gen-flux-manifests.sh and report it")
	}
	return a.Apply(ctx, objs, true, timeout)
}

// InstallManifests implements engine.Engine, delegating to the package-level
// InstallManifests func (kept for tests and for callers that only need the
// manifests, not a Flux value).
func (f *Flux) InstallManifests() ([]*unstructured.Unstructured, error) {
	return InstallManifests()
}

// deliveredListGVKs are the engine-native kinds Deliver creates per pack;
// Uninstall must remove exactly these, and wait for their prune finalizers.
var deliveredListGVKs = []schema.GroupVersionKind{
	{Group: "kustomize.toolkit.fluxcd.io", Version: "v1", Kind: "KustomizationList"},
	{Group: "source.toolkit.fluxcd.io", Version: "v1", Kind: "OCIRepositoryList"},
}

// Uninstall is phase one of teardown: it deletes this cube's delivered
// Kustomizations and OCIRepositories, then polls until both lists are empty
// — which means kustomize-controller has finished running the
// Kustomizations' prune finalizers, i.e. every workload flux delivered is
// gone. Only after that is it safe for `down` to delete the flux
// controllers themselves (via the inventory-driven DeleteAll); deleting the
// controllers first would orphan delivered workloads and leave the
// finalized objects stuck Terminating forever.
func (f *Flux) Uninstall(ctx context.Context, a *apply.Applier, timeout time.Duration) error {
	c := a.Client()
	if remaining, err := deleteDelivered(ctx, c, a.Cube()); err != nil {
		return err
	} else if remaining == 0 {
		return nil
	}

	deadline := time.Now().Add(timeout)
	for {
		remaining, err := countDelivered(ctx, c, a.Cube())
		if err != nil {
			return err
		}
		if remaining == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return diag.New(diag.CodeEngineUninstallFail,
				fmt.Sprintf("engine did not finish pruning delivered workloads within %s (%d object(s) still terminating)", timeout, remaining),
				"check kustomize-controller logs in flux-system; re-run `cube-idp down`")
		}
		select {
		case <-ctx.Done():
			return diag.Wrap(ctx.Err(), diag.CodeEngineUninstallFail, "engine did not finish pruning delivered workloads",
				"check kustomize-controller logs in flux-system; re-run `cube-idp down`")
		case <-time.After(2 * time.Second):
		}
	}
}

// deleteDelivered lists this cube's delivered flux objects in flux-system
// and issues deletes for each, returning how many existed. Kinds whose CRD
// is absent (engine never installed, or already torn down) count as zero.
func deleteDelivered(ctx context.Context, c client.Client, cube string) (int, error) {
	total := 0
	for _, gvk := range deliveredListGVKs {
		items, err := listDelivered(ctx, c, gvk, cube)
		if err != nil {
			return 0, err
		}
		for i := range items {
			if err := c.Delete(ctx, &items[i]); err != nil && !apierrors.IsNotFound(err) {
				return 0, diag.Wrap(err, diag.CodeEngineUninstallFail,
					fmt.Sprintf("cannot delete %s %s/%s", items[i].GetKind(), items[i].GetNamespace(), items[i].GetName()),
					"check RBAC on namespace flux-system; re-run `cube-idp down`")
			}
		}
		total += len(items)
	}
	return total, nil
}

// countDelivered reports how many of this cube's delivered flux objects
// still exist (i.e. still hold prune finalizers).
func countDelivered(ctx context.Context, c client.Client, cube string) (int, error) {
	total := 0
	for _, gvk := range deliveredListGVKs {
		items, err := listDelivered(ctx, c, gvk, cube)
		if err != nil {
			return 0, err
		}
		total += len(items)
	}
	return total, nil
}

func listDelivered(ctx context.Context, c client.Client, gvk schema.GroupVersionKind, cube string) ([]unstructured.Unstructured, error) {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(gvk)
	err := c.List(ctx, list,
		client.InNamespace(fluxNS),
		client.MatchingLabels{apply.CubeLabel: cube},
	)
	if meta.IsNoMatchError(err) {
		return nil, nil // CRD absent: nothing was ever delivered via this kind
	}
	if err != nil {
		return nil, diag.Wrap(err, diag.CodeEngineUninstallFail, fmt.Sprintf("cannot list %s in %s", gvk.Kind, fluxNS),
			"check kubeconfig and cluster connectivity; re-run `cube-idp down`")
	}
	return list.Items, nil
}
