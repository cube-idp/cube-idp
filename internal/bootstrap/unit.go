package bootstrap

import (
	"context"
	"slices"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Unit is one prerequisite unit of the ordered list bootstrap installs between
// the substrate kind-set wait and the engine's sync wiring. Its objects and its
// readiness flavor are values composed at the CLI/orchestrator edge: this
// domain knows nothing of packs, gateways or certificate authorities — it
// applies what it is handed, in the order it is handed, and waits the way the
// unit declares. Build one with NewRawUnit, NewCRUnit or NewInertUnit; the zero
// Unit is an empty raw unit.
type Unit struct {
	name       string
	objects    []*unstructured.Unstructured
	kind       unitKind
	reconciled ReconciledFunc
}

// unitKind selects the post-apply wait a Unit gets. The raw flavor is the
// zero value, so a Unit built by anything other than the constructors still
// waits the kind-set rather than silently skipping readiness.
type unitKind string

const (
	unitRaw   unitKind = ""
	unitCR    unitKind = "cr"
	unitInert unitKind = "inert"
)

// NewRawUnit builds a prerequisite unit whose readiness is the bootstrap
// kind-set wait: CRDs established, workloads rolled out, Jobs complete,
// Namespaces Active. Objects outside the kind-set are ignored by design —
// bootstrap has no readiness opinion about them, and a unit made only of such
// objects is ready the moment it applies.
func NewRawUnit(name string, objs []*unstructured.Unstructured) Unit {
	return Unit{name: name, objects: objs, kind: unitRaw}
}

// NewCRUnit builds a prerequisite unit of custom resources whose readiness is a
// reconciliation wait under the injected predicate — the same judgment seam the
// engine waits use, supplied by whoever composed the unit. reconciled is
// required, and the misuse is caught rather than tolerated: a unit built with a
// nil predicate fails the run pre-flight as CUBE-BST-010, before anything is
// recorded or applied, instead of silently passing a wait that never ran.
func NewCRUnit(name string, objs []*unstructured.Unstructured, reconciled ReconciledFunc) Unit {
	return Unit{name: name, objects: objs, kind: unitCR, reconciled: reconciled}
}

// NewInertUnit builds a prerequisite unit of status-less objects — Secrets,
// ConfigMaps and their kin — for which a successful apply IS readiness. No
// post-apply wait runs, deliberately: there is no status to poll and nothing
// to observe, so a later reader must not "fix" the absent wait by adding one.
func NewInertUnit(name string, objs []*unstructured.Unstructured) Unit {
	return Unit{name: name, objects: objs, kind: unitInert}
}

// installPrerequisites applies the ordered prerequisite units between the
// substrate kind-set wait and the engine's sync wiring, one unit at a time:
// the inventory is re-recorded with the cumulative owned set BEFORE the unit's
// objects are applied — so a half-applied unit is still visible to a future
// `down` — then the unit's declared wait runs before the next unit starts.
// The list is checked by checkUnits before the run starts, not here, so a
// composition defect costs nothing at all. It returns the cumulative applied
// set, built on a fresh backing array so the caller's slice is never appended
// to in place.
func (a *Applier) installPrerequisites(ctx context.Context, applied []*unstructured.Unstructured, units []Unit) ([]*unstructured.Unstructured, error) {
	for _, u := range units {
		applied = slices.Concat(applied, u.objects)
		if err := a.RecordInventory(ctx, applied); err != nil {
			return applied, err
		}
		if err := a.Apply(ctx, u.objects); err != nil {
			return applied, err
		}
		if err := a.waitUnit(ctx, u); err != nil {
			return applied, err
		}
	}
	return applied, nil
}

// checkUnits pre-flights the list for defects only the composer can fix. A CR
// unit without a judgment is the one that would otherwise pass silently: its
// wait would skip on the nil predicate and the unit would count as ready on
// apply. InstallEngine calls it before installing anything — ahead of the
// substrate, not just ahead of the units — so a pure composition defect leaves
// the cluster untouched rather than half-installed.
func checkUnits(units []Unit) error {
	for _, u := range units {
		if u.kind == unitCR && u.reconciled == nil {
			return newUnitJudgeError(u.name)
		}
	}
	return nil
}

// waitUnit runs the post-apply wait the unit declared — never one inferred
// from the objects' kinds: a CR unit reconciles under its injected judgment, an
// inert unit is already ready, and everything else waits the kind-set. Timeouts
// name the unit, so one code still points at one step.
func (a *Applier) waitUnit(ctx context.Context, u Unit) error {
	subject := "prerequisite unit " + u.name
	switch u.kind {
	case unitCR:
		return a.waitReconciled(ctx, u.objects, u.reconciled, subject)
	case unitInert:
		return nil
	default:
		return a.waitReady(ctx, u.objects, subject)
	}
}
