// Package argocd implements the GitOpsEngine over Argo CD (see ADR 0018 —
// GitOps engine interface seam). Delivery
// shape: one Application per pack with an OCI repository source pointing at
// the in-cluster zot registry. ENGINE-SPECIFIC REQUIREMENT: this
// engine needs an Argo CD version with OCI repository support; if that path
// proves insufficient the documented fallback is delivery via the gitea
// pack.
//
// Version pin: argo-cd v3.4.5 (see hack/gen-argocd-manifests.sh) — the
// latest stable 3.x release at pin time (3.5 was still release-candidate
// only), verified via `gh release list --repo argoproj/argo-cd` to carry
// native OCI application-source support
// (https://argo-cd.readthedocs.io/en/stable/user-guide/oci/).
//
// Resolved concern (an open risk while this engine was being built, closed
// once the argocd e2e engine matrix ran green): oci.PushRendered
// (shared by every engine) pushes packs using fluxcd/pkg/oci's default
// layer media type (application/vnd.cncf.flux.content.v1.tar+gzip), which
// is NOT one of argo-cd's default accepted OCI layer media types
// (application/vnd.oci.image.layer.v1.tar[+gzip],
// application/vnd.cncf.helm.chart.content.v1.tar+gzip — see
// cmd/argocd-repo-server/commands/argocd_repo_server.go's --oci-layer-media-types
// default), so argocd-repo-server would otherwise reject every cube-idp
// pack pull with a media-type error. The fix lives in the vendored manifest
// itself, not in Go: manifests/install.yaml's argocd-cmd-params-cm
// ConfigMap (the SAME object the base install already ships, edited in
// place — not a second partial-object apply of the same
// name/namespace/kind, so there's no SSA field-manager/pruning risk) adds a
// reposerver.oci.layer.media.types data key appending
// application/vnd.cncf.flux.content.v1.tar+gzip to the allow-list;
// install.yaml already wires ARGOCD_REPO_SERVER_OCI_LAYER_MEDIA_TYPES from
// that key (upstream's own env-from-configmap plumbing), so repo-server
// picks it up with no further changes. Verified against a real kind cluster
// via the e2e engine matrix (`CUBE_IDP_E2E_ENGINE=argocd`). If
// hack/gen-argocd-manifests.sh is re-run against a newer ARGOCD_VERSION,
// that data: block must be reapplied by hand (see the comment above the
// ConfigMap in manifests/install.yaml).
package argocd

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cube-idp/cube-idp/internal/apply"
	"github.com/cube-idp/cube-idp/internal/diag"
	"github.com/cube-idp/cube-idp/internal/engine"
)

const Namespace = "argocd"

// ArgoCD implements engine.Engine. Engine-as-pack (2026-07-19): argocd no
// longer carries an embedded install manifest or the defaultNamespace
// transform — `up` fetches and renders the cube-engine-argocd pack (whose
// chart stamps explicit namespaces) and SSAs the result. The engine is a pure
// translator + operator now; Install/InstallManifests left the interface.
type ArgoCD struct{}

// New returns an ArgoCD engine.
func New() *ArgoCD { return &ArgoCD{} }

// OrdersDeliveries reports that argocd does NOT order delivery
// reconciliation natively — there is no cross-Application dependsOn (p6
// DEP3). `up`'s wave gate (waitDepsHealthy) is the enforcement side; Deliver
// still stamps the cube-idp.dev/depends-on annotation for humans/tooling.
func (g *ArgoCD) OrdersDeliveries() bool { return false }

func (g *ArgoCD) Uninstall(ctx context.Context, a *apply.Applier, timeout time.Duration) error {
	// Same posture as flux: removal is inventory-driven by `down`; the engine
	// needs nothing beyond being present in the inventory.
	return nil
}

var applicationListGVK = schema.GroupVersionKind{
	Group: "argoproj.io", Version: "v1alpha1", Kind: "ApplicationList",
}

// Health lists this cube's delivered Applications and reports each one's
// sync/health status. Unlike the original flux Health (which had no
// missing-CRD handling — since hardened), this treats a missing
// Application CRD (fresh cluster, engine not yet installed or install still
// converging) as "nothing delivered yet", not an error: meta.IsNoMatchError
// on the List call, mirroring flux's listDelivered on the Uninstall path.
func (g *ArgoCD) Health(ctx context.Context, a *apply.Applier) ([]engine.ComponentHealth, error) {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(applicationListGVK)
	err := a.Client().List(ctx, list,
		client.InNamespace(Namespace), client.MatchingLabels{apply.CubeLabel: a.Cube()})
	if meta.IsNoMatchError(err) {
		return nil, nil
	}
	if err != nil {
		return nil, diag.Wrap(err, diag.CodeEngineHealthTimeout, "cannot list argocd Applications",
			"check kubeconfig and cluster connectivity")
	}
	var out []engine.ComponentHealth
	for _, item := range list.Items {
		health, _, _ := unstructured.NestedString(item.Object, "status", "health", "status")
		sync, _, _ := unstructured.NestedString(item.Object, "status", "sync", "status")
		msg, _, _ := unstructured.NestedString(item.Object, "status", "operationState", "message")
		out = append(out, engine.ComponentHealth{
			Name:    item.GetName(),
			Ready:   health == "Healthy" && sync == "Synced",
			Message: sync + "/" + health + " " + msg,
		})
	}
	return out, nil
}
