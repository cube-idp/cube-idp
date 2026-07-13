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
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/rafpe/cube-idp/internal/apply"
	"github.com/rafpe/cube-idp/internal/diag"
)

//go:embed manifests/install.yaml
var installYAML []byte

// Flux implements engine.Engine.
type Flux struct{}

// New returns a Flux engine.
func New() *Flux { return &Flux{} }

// InstallManifests parses the embedded, pre-rendered Flux install manifest
// (source-controller + kustomize-controller only; see
// hack/gen-flux-manifests.sh for how it's regenerated).
func InstallManifests() ([]*unstructured.Unstructured, error) {
	return apply.ParseMultiDoc(installYAML)
}

func (f *Flux) Install(ctx context.Context, a *apply.Applier, timeout time.Duration) error {
	objs, err := InstallManifests()
	if err != nil {
		return diag.Wrap(err, "CUBE-3003", "embedded flux manifests are invalid",
			"this is a cube-idp bug — regenerate with hack/gen-flux-manifests.sh and report it")
	}
	return a.Apply(ctx, objs, true, timeout)
}

func (f *Flux) Uninstall(ctx context.Context, a *apply.Applier, timeout time.Duration) error {
	// Engine removal is inventory-driven like everything else; `down`
	// deletes the whole inventory, so nothing engine-specific is needed
	// in Phase 1 beyond being present in the inventory.
	return nil
}
