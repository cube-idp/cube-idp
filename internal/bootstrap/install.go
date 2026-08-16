package bootstrap

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
)

// InstallEngine bootstraps the configured gitops engine: install and wait for
// Flux, then — when a source is configured — apply its Flux source +
// Kustomization CRs and re-record the inventory with them. The source CRs are
// applied only after the Flux CRDs are established (Install's readiness wait),
// so their kinds map. Bootstrap does NOT wait on engine-CR reconciliation —
// that is the M9 seam's job; it applies and records, then hands over.
func (a *Applier) InstallEngine(ctx context.Context, engine *v1alpha1.EngineSpec) error {
	fluxObjs, err := FluxObjects()
	if err != nil {
		return err
	}
	if err := a.Install(ctx, fluxObjs); err != nil {
		return err
	}
	src := configuredSource(engine)
	if src == nil {
		return nil
	}
	srcObjs, err := sourceObjects(src)
	if err != nil {
		return err
	}
	if err := a.Apply(ctx, srcObjs); err != nil {
		return err
	}
	return a.RecordInventory(ctx, append(fluxObjs, srcObjs...))
}

// configuredSource returns the engine source to wire, or nil when none is set.
func configuredSource(engine *v1alpha1.EngineSpec) *v1alpha1.EngineSource {
	if engine == nil || engine.Source == nil || engine.Source.URL == "" {
		return nil
	}
	return engine.Source
}

// Install performs the whole micro-bootstrap in the one order that makes it
// recoverable: apply the objects, record the inventory (so a partial install
// is already visible to a future `down`), then wait for the bootstrap kind-set
// to become ready. Cancel or bound readiness through ctx.
func (a *Applier) Install(ctx context.Context, objs []*unstructured.Unstructured) error {
	if err := a.Apply(ctx, objs); err != nil {
		return err
	}
	if err := a.RecordInventory(ctx, objs); err != nil {
		return err
	}
	return a.WaitReady(ctx, objs)
}
