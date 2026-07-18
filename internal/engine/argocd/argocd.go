// Package argocd implements the GitOpsEngine over Argo CD (D2). Delivery
// shape: one Application per pack with an OCI repository source pointing at
// the in-cluster zot registry. ENGINE-SPECIFIC REQUIREMENT (spec §7): this
// engine needs an Argo CD version with OCI repository support; if that path
// proves insufficient the documented fallback is delivery via the gitea
// pack — see the Phase 2 plan, Task 2.
//
// Version pin: argo-cd v3.4.5 (see hack/gen-argocd-manifests.sh) — the
// latest stable 3.x release at pin time (3.5 was still release-candidate
// only), verified via `gh release list --repo argoproj/argo-cd` to carry
// native OCI application-source support
// (https://argo-cd.readthedocs.io/en/stable/user-guide/oci/).
//
// Resolved concern (was an open Task 2 risk; closed by Task 14): oci.PushRendered
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
// via Task 14's e2e engine matrix (`CUBE_IDP_E2E_ENGINE=argocd`). If
// hack/gen-argocd-manifests.sh is re-run against a newer ARGOCD_VERSION,
// that data: block must be reapplied by hand (see the comment above the
// ConfigMap in manifests/install.yaml).
package argocd

import (
	"context"
	_ "embed"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cube-idp/cube-idp/internal/apply"
	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/diag"
	"github.com/cube-idp/cube-idp/internal/engine"
)

const Namespace = "argocd"

//go:embed manifests/install.yaml
var installYAML []byte

//go:embed manifests/repo-secret.yaml
var repoSecretYAML []byte

type ArgoCD struct {
	// tuning is the closed engine.tuning knob set (GT1, U3) applied over
	// the embedded install manifests by InstallManifests. nil = untuned.
	tuning *config.EngineTuning
}

// New returns an untuned ArgoCD engine (tests and callers that only need
// the stock install).
func New() *ArgoCD { return &ArgoCD{} }

// NewTuned returns an ArgoCD engine whose InstallManifests patches the
// embedded manifests with t (GT1). The factory is the production caller.
func NewTuned(t *config.EngineTuning) *ArgoCD { return &ArgoCD{tuning: t} }

// clusterScopedKinds are the non-namespaced kinds argo-cd's own
// manifests/install.yaml ships (plus the Namespace object this package
// prepends); everything else in that file is namespace-scoped.
var clusterScopedKinds = map[string]bool{
	"Namespace":                true,
	"ClusterRole":              true,
	"ClusterRoleBinding":       true,
	"CustomResourceDefinition": true,
}

// defaultNamespace fills in metadata.namespace for namespace-scoped objects
// that omit it. Argo CD's community install.yaml (unlike flux's `flux
// install --export` output) never sets metadata.namespace on its own
// resources: it's designed for `kubectl apply -n argocd -f install.yaml`,
// relying on kubectl's -n flag / current-context namespace to supply it.
// cube-idp's Applier SSA-applies raw unstructured objects with no such
// implicit default-namespace behavior, so without this step every
// namespace-scoped object (ServiceAccount, Deployment, Role, ...) would
// fail to apply ("namespace not specified") — found the hard way running
// the contract suite's install_health_uninstall_on_cluster subtest against
// a real (envtest) API server.
func defaultNamespace(objs []*unstructured.Unstructured) {
	for _, o := range objs {
		if o.GetNamespace() == "" && !clusterScopedKinds[o.GetKind()] {
			o.SetNamespace(Namespace)
		}
	}
}

func (g *ArgoCD) InstallManifests() ([]*unstructured.Unstructured, error) {
	objs, err := apply.ParseMultiDoc(installYAML)
	if err != nil {
		return nil, err
	}
	defaultNamespace(objs)
	secretObjs, err := apply.ParseMultiDoc(repoSecretYAML)
	if err != nil {
		return nil, err
	}
	all := append(objs, secretObjs...)
	// GT1 (U3): apply this engine's tuning last, so the objects Install
	// SSAs and `up` inventories are the tuned ones. An unknown tuning
	// component surfaces as CUBE-3009 here.
	if err := engine.ApplyTuning(all, g.tuning); err != nil {
		return nil, err
	}
	return all, nil
}

func (g *ArgoCD) Install(ctx context.Context, a *apply.Applier, timeout time.Duration) error {
	objs, err := g.InstallManifests()
	if err != nil {
		return diag.Wrap(err, diag.CodeEngineManifestsInv, "embedded argocd manifests are invalid",
			"this is a cube-idp bug — regenerate with hack/gen-argocd-manifests.sh and report it")
	}
	return a.Apply(ctx, objs, true, timeout)
}

func (g *ArgoCD) Uninstall(ctx context.Context, a *apply.Applier, timeout time.Duration) error {
	// Same posture as flux: removal is inventory-driven by `down`; the engine
	// needs nothing beyond being present in the inventory.
	return nil
}

var applicationListGVK = schema.GroupVersionKind{
	Group: "argoproj.io", Version: "v1alpha1", Kind: "ApplicationList",
}

// Health lists this cube's delivered Applications and reports each one's
// sync/health status. Unlike phase-1 flux Health (a documented gap — see the
// Phase 2 plan, Task 2, Task 0 finding 0.9), this treats a missing
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
