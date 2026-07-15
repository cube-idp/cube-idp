package flux

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/rafpe/cube-idp/internal/apply"
	"github.com/rafpe/cube-idp/internal/diag"
)

// pokeAnnotation is the key `flux reconcile source ...` writes to force an
// out-of-band reconcile; source-controller reconciles whenever its value
// changes.
const pokeAnnotation = "reconcile.fluxcd.io/requestedAt"

// pokeSourceKinds are the flux source kinds a pack may be delivered as, in
// priority order: OCI-delivered packs (Deliver) first, git-delivered packs
// (DeliverGit) second. Poke refreshes whichever exists.
var pokeSourceKinds = []string{"OCIRepository", "GitRepository"}

// Poke forces flux to reconcile pack's delivery now instead of on its poll
// interval, by stamping the reconcile.fluxcd.io/requestedAt annotation on the
// pack's source object (the same thing `flux reconcile` does). It tries the
// OCIRepository named cube-idp-<pack> first, then the GitRepository of the
// same name; a pack with neither is CUBE-3007. A source that exists but can't
// be read or updated (a transient engine IO failure) is CUBE-3008. Idempotent
// and cheap: a single get + annotation update, no apply and no wait.
func (f *Flux) Poke(ctx context.Context, a *apply.Applier, packName string) error {
	c := a.Client()
	name := deliveryName(packName)
	for _, kind := range pokeSourceKinds {
		src := &unstructured.Unstructured{}
		src.SetAPIVersion("source.toolkit.fluxcd.io/v1")
		src.SetKind(kind)
		err := c.Get(ctx, client.ObjectKey{Namespace: fluxNS, Name: name}, src)
		if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
			continue // this kind's object (or its CRD) is absent — try the next shape
		}
		if err != nil {
			return diag.Wrap(err, diag.CodePokeIOFail,
				fmt.Sprintf("cannot read %s %s/%s to poke", kind, fluxNS, name),
				"check kubeconfig and cluster connectivity; re-run the command")
		}
		anns := src.GetAnnotations()
		if anns == nil {
			anns = map[string]string{}
		}
		anns[pokeAnnotation] = time.Now().Format(time.RFC3339Nano)
		src.SetAnnotations(anns)
		if err := c.Update(ctx, src); err != nil {
			return diag.Wrap(err, diag.CodePokeIOFail,
				fmt.Sprintf("cannot poke %s %s/%s", kind, fluxNS, name),
				"check RBAC on namespace flux-system; re-run the command")
		}
		return nil
	}
	return diag.New(diag.CodePokeTargetMissing,
		fmt.Sprintf("pack %q has no delivery source to poke", packName),
		"run `cube-idp sync <dir>` or `cube-idp up` first — Poke only refreshes an existing delivery")
}
