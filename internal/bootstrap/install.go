// Package bootstrap is the micro-bootstrap applier: it SSA-applies the
// injected engine-substrate install objects plus the injected driver sync
// wiring, executes the phased readiness waits, records an inventory into
// the injected namespace, then hands steady-state ownership to the engine.
// Everything it installs is content the CLI edge composes and hands in —
// it embeds nothing, derives nothing from config, and never imports
// internal/kube or the engine domain (domains never import each other).
// Contract: docs/domains/bootstrap.md.
package bootstrap

import (
	"context"
	"slices"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// InstallEngine bootstraps the gitops engine from injected content, in three
// readiness phases sharing one total ctx budget: install the substrate
// objects and wait the kind-set (phase 1); when the driver emitted sync
// wiring, record the full owned set, apply the wiring — only after the
// substrate's CRDs are established, so its kinds map; the inventory is
// recorded BEFORE the wiring apply, so a half-applied wiring is still
// visible to a future `down` — and wait for it to reconcile with the
// injected judgment (phase 2); then poll the declared engine objects,
// content bootstrap did NOT apply, until they reconcile too (phase 3 — an
// empty list is skipped, the flux case). What the wiring looks like — and
// whether any exists — is the driver's business, decided at the edge;
// version assertion happened there too (CUBE-ENG-005).
func (a *Applier) InstallEngine(ctx context.Context, substrateObjs, wiringObjs []*unstructured.Unstructured, engineWait EngineWait) error {
	if err := a.Install(ctx, substrateObjs); err != nil {
		return err
	}
	if len(wiringObjs) > 0 {
		if err := a.RecordInventory(ctx, slices.Concat(substrateObjs, wiringObjs)); err != nil {
			return err
		}
		if err := a.Apply(ctx, wiringObjs); err != nil {
			return err
		}
		if err := a.WaitReconciled(ctx, wiringObjs, engineWait.Reconciled); err != nil {
			return err
		}
	}
	return a.WaitReconciled(ctx, engineWait.EngineObjects, engineWait.Reconciled)
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
