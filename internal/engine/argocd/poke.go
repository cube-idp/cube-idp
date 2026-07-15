package argocd

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/rafpe/cube-idp/internal/apply"
	"github.com/rafpe/cube-idp/internal/diag"
)

// pokeAnnotation forces argocd-application-controller to refresh (re-sync
// against git/OCI) the Application now; "normal" is a hard refresh, the same
// value `argocd app get --refresh` sets.
const pokeAnnotation = "argocd.argoproj.io/refresh"

// Poke forces Argo CD to refresh pack's Application now instead of on its poll
// interval, by stamping the argocd.argoproj.io/refresh=normal annotation on
// the Application named cube-idp-<pack>. One shape covers both delivery kinds
// (OCI and git both land as an Application). A pack with no Application is
// CUBE-3007. An Application that exists but can't be read or updated (a
// transient engine IO failure) is CUBE-3008. Idempotent and cheap: a single
// get + annotation update, no apply and no wait.
func (g *ArgoCD) Poke(ctx context.Context, a *apply.Applier, packName string) error {
	c := a.Client()
	name := deliveryName(packName)
	app := &unstructured.Unstructured{}
	app.SetAPIVersion("argoproj.io/v1alpha1")
	app.SetKind("Application")
	err := c.Get(ctx, client.ObjectKey{Namespace: Namespace, Name: name}, app)
	if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
		return diag.New(diag.CodePokeTargetMissing,
			fmt.Sprintf("pack %q has no delivery source to poke", packName),
			"run `cube-idp sync <dir>` or `cube-idp up` first — Poke only refreshes an existing delivery")
	}
	if err != nil {
		return diag.Wrap(err, diag.CodePokeIOFail,
			fmt.Sprintf("cannot read Application %s/%s to poke", Namespace, name),
			"check kubeconfig and cluster connectivity; re-run the command")
	}
	anns := app.GetAnnotations()
	if anns == nil {
		anns = map[string]string{}
	}
	anns[pokeAnnotation] = "normal"
	app.SetAnnotations(anns)
	if err := c.Update(ctx, app); err != nil {
		return diag.Wrap(err, diag.CodePokeIOFail,
			fmt.Sprintf("cannot poke Application %s/%s", Namespace, name),
			"check RBAC on namespace argocd; re-run the command")
	}
	return nil
}
