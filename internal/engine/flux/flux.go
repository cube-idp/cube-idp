// Package flux implements the GitOpsEngine over Flux's source-controller and
// kustomize-controller. Delivery shape: one OCIRepository + one Kustomization
// per pack, pointing at the in-cluster zot registry (ADR 0015 — in-cluster
// registry and transport).
//
// Only source-controller and kustomize-controller are installed — helm
// rendering happens client-side (pack.Render), so helm-controller is never
// needed in-cluster.
package flux

import (
	"context"
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

// Flux implements engine.Engine. Engine-as-pack (2026-07-19): flux no longer
// carries an embedded install manifest — `up` fetches and renders the
// cube-engine-flux pack and SSAs the result (the engine is a pure translator +
// operator now; Install/InstallManifests left the interface).
type Flux struct{}

// New returns a Flux engine.
func New() *Flux { return &Flux{} }

// OrdersDeliveries reports that flux orders delivery reconciliation
// natively via Kustomization spec.dependsOn (p6 DEP3) — `up`'s wave gate
// never runs for this engine.
func (f *Flux) OrdersDeliveries() bool { return true }

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
