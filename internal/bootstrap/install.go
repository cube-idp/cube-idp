// Package bootstrap is the micro-bootstrap applier: it SSA-applies the
// injected engine-substrate install objects, then the ordered prerequisite
// units the edge composed, then the injected driver sync wiring, executing
// each step's declared readiness wait and re-recording an inventory into the
// injected namespace as the owned set grows, before handing steady-state
// ownership to the engine. Everything it installs is content the CLI edge
// composes and hands in — it embeds nothing, derives nothing from config, and
// never imports internal/kube or the engine domain (domains never import each
// other). Contract: docs/domains/bootstrap.md.
package bootstrap

import (
	"context"
	"slices"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// EngineInstall is the content the edge composes for one bootstrap run: the
// engine substrate, the ordered prerequisite units that must be in place
// before the engine starts syncing, the driver's sync wiring, and the
// reconciliation inputs for the post-apply waits. Every field is injected —
// bootstrap decides the sequence, never the content.
type EngineInstall struct {
	// Substrate is the engine's install bundle, applied and waited first.
	Substrate []*unstructured.Unstructured
	// Prerequisites are installed in list order between the substrate wait
	// and the wiring; order is the edge's declaration, not a graph
	// bootstrap solves.
	Prerequisites []Unit
	// Wiring is the driver's sync wiring, applied last so it lands only
	// after everything it may reference exists.
	Wiring []*unstructured.Unstructured
	// Wait carries the reconciliation judgment and the declared engine
	// objects for the final phase.
	Wait EngineWait
}

// InstallEngine bootstraps the gitops engine from injected content, in
// readiness phases sharing one total ctx budget: install the substrate objects
// and wait the kind-set (phase 1); install each prerequisite unit in order,
// re-recording the cumulative inventory BEFORE its apply and then running the
// wait the unit declared — kind-set, reconciliation, or none for inert content
// (no new phase concept: units reuse the existing waits); when the driver
// emitted sync wiring, record the full owned set, apply the wiring — only
// after the substrate's CRDs are established, so its kinds map — and wait for
// it to reconcile with the injected judgment
// (phase 2); then poll the declared engine objects, content bootstrap did NOT
// apply, until they reconcile too (phase 3 — an empty list is skipped, the flux
// case). Every inventory record precedes the apply it covers, so a half-applied
// step is still visible to a future `down`. What the units and the wiring look
// like — and whether any exist — is decided at the edge; version assertion
// happened there too (CUBE-ENG-005). Composition defects in the units are
// caught before the first apply, so a defective run installs nothing.
func (a *Applier) InstallEngine(ctx context.Context, in EngineInstall) error {
	if err := checkUnits(in.Prerequisites); err != nil {
		return err
	}
	if err := a.Install(ctx, in.Substrate); err != nil {
		return err
	}
	applied, err := a.installPrerequisites(ctx, in.Substrate, in.Prerequisites)
	if err != nil {
		return err
	}
	if len(in.Wiring) > 0 {
		applied = slices.Concat(applied, in.Wiring)
		if err := a.RecordInventory(ctx, applied); err != nil {
			return err
		}
		if err := a.Apply(ctx, in.Wiring); err != nil {
			return err
		}
		if err := a.WaitReconciled(ctx, in.Wiring, in.Wait.Reconciled); err != nil {
			return err
		}
	}
	return a.WaitReconciled(ctx, in.Wait.EngineObjects, in.Wait.Reconciled)
}

// Install runs phase 1 of the bootstrap in the one order that makes it
// recoverable: apply the objects, record the inventory (so a partial install
// is already visible to a future `down`), then wait for the bootstrap kind-set
// to become ready. InstallEngine sequences it ahead of the prerequisite units,
// the sync wiring and the reconciliation waits. Cancel or bound readiness
// through ctx.
func (a *Applier) Install(ctx context.Context, objs []*unstructured.Unstructured) error {
	if err := a.Apply(ctx, objs); err != nil {
		return err
	}
	if err := a.RecordInventory(ctx, objs); err != nil {
		return err
	}
	return a.WaitReady(ctx, objs)
}
