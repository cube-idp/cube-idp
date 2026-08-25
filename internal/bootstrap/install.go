// Package bootstrap is the micro-bootstrap applier: it SSA-applies the
// injected engine-substrate install objects plus the source/sync CRs derived
// from spec.engine, executes the phased readiness waits, records an inventory
// into the injected namespace, then hands steady-state ownership to the
// engine. It runs against injected client-go interfaces and never imports
// internal/kube or the engine domain (domains never import each other) — the
// CLI edge injects clients and content alike. Contract:
// docs/domains/bootstrap.md.
package bootstrap

import (
	"context"
	"slices"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
)

// InstallEngine bootstraps the configured gitops engine from injected content,
// in three readiness phases sharing one total ctx budget: install the
// substrate objects and wait the kind-set (phase 1); when a source is
// configured, record the full owned set, apply the source + Kustomization
// CRs — only after the substrate's CRDs are established, so their kinds map;
// the inventory is recorded BEFORE the source apply, so a half-applied source
// is still visible to a future `down` — and wait for them to reconcile with
// the injected judgment (phase 2); then poll the declared engine objects,
// content bootstrap did NOT apply, until they reconcile too (phase 3 — an
// empty list is skipped, the flux case). Version assertion happens at the
// edge (the substrate owns the pin — CUBE-ENG-005); bootstrap applies what it
// is handed.
func (a *Applier) InstallEngine(ctx context.Context, engine *v1alpha1.EngineSpec, substrateObjs []*unstructured.Unstructured, engineWait EngineWait) error {
	if err := a.Install(ctx, substrateObjs); err != nil {
		return err
	}
	if src := configuredSource(engine); src != nil {
		srcObjs, err := sourceObjects(src)
		if err != nil {
			return err
		}
		if err := a.RecordInventory(ctx, slices.Concat(substrateObjs, srcObjs)); err != nil {
			return err
		}
		if err := a.Apply(ctx, srcObjs); err != nil {
			return err
		}
		if err := a.WaitReconciled(ctx, srcObjs, engineWait.Reconciled); err != nil {
			return err
		}
	}
	return a.WaitReconciled(ctx, engineWait.EngineObjects, engineWait.Reconciled)
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
