package bootstrap

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/wait"
)

// defaultReconciledSubject names the declared object set in CUBE-BST-009
// messages — the subject of every WaitReconciled call. Prerequisite units
// name themselves.
const defaultReconciledSubject = "declared resources"

// ReconciledFunc judges one polled object's live state: reconciled, not yet
// (with a human-readable reason that feeds the CUBE-BST-009 timeout
// diagnostics), or an error for an object the judgment does not recognize.
// It is bootstrap's own narrow input type — the CLI/orchestrator edge adapts
// the engine driver's judgment into it, so this domain imports no engine
// package.
type ReconciledFunc func(obj *unstructured.Unstructured) (bool, string, error)

// EngineWait carries the engine-readiness inputs injected at the edge for the
// post-apply wait phases. A nil Reconciled skips both phases (no judgment
// injected — the degenerate wiring until a driver supplies one); an empty
// EngineObjects skips phase 3 (the flux case: the substrate doubles as the
// engine and no separate bundle exists).
type EngineWait struct {
	// Reconciled judges the polled objects — the applied sync wiring
	// (phase 2) and the declared engine objects (phase 3). Prerequisite
	// units carry their own judgment, injected per unit.
	Reconciled ReconciledFunc
	// EngineObjects is the engine's declared install bundle — content
	// bootstrap did NOT apply (it arrives through the tier-1 source),
	// polled by declared identity.
	EngineObjects []*unstructured.Unstructured
}

// pendingObject is one not-yet-reconciled object with the latest reason the
// judgment (or the poll itself) gave for it.
type pendingObject struct {
	obj    *unstructured.Unstructured
	reason string
}

// WaitReconciled polls objs until judge reports every one reconciled, or ctx
// is done — the reconciliation wait shared by phases 2 and 3. Transient
// conditions are pending, never terminal: an object whose kind has no REST
// mapping yet (its CRD may still be arriving through the source — discovery is
// re-consulted on every poll) or an object not yet created keeps waiting until
// the deadline. Permanent polling failures are coded at the failure point
// (CUBE-BST-010, or the judgment's own code) and are never retagged as a
// timeout; only a deadline with objects still pending is CUBE-BST-009.
func (a *Applier) WaitReconciled(ctx context.Context, objs []*unstructured.Unstructured, judge ReconciledFunc) error {
	return a.waitReconciled(ctx, objs, judge, defaultReconciledSubject)
}

// waitReconciled is WaitReconciled with the timeout message's subject supplied
// by the caller, so a prerequisite unit's timeout names the unit instead of the
// declared set.
func (a *Applier) waitReconciled(ctx context.Context, objs []*unstructured.Unstructured, judge ReconciledFunc, subject string) error {
	if judge == nil || len(objs) == 0 {
		return nil
	}
	pending := make([]pendingObject, 0, len(objs))
	for _, obj := range objs {
		pending = append(pending, pendingObject{obj: obj, reason: "not polled yet"})
	}
	err := wait.PollUntilContextCancel(ctx, a.interval, true, func(ctx context.Context) (bool, error) {
		remaining, err := a.checkReconciled(ctx, pending, judge)
		if err != nil {
			return false, err
		}
		pending = remaining
		return len(pending) == 0, nil
	})
	if err != nil {
		return newReconcileWaitError(subject, pending, err)
	}
	return nil
}

// checkReconciled runs one poll pass and returns the objects not yet
// reconciled, each with its current reason. Errors it returns are terminal
// and already coded: the judgment's own coded error passes through untouched,
// and everything else permanent is CUBE-BST-010.
func (a *Applier) checkReconciled(ctx context.Context, objs []pendingObject, judge ReconciledFunc) ([]pendingObject, error) {
	var pending []pendingObject
	for _, p := range objs {
		live, err := a.k.live(ctx, p.obj)
		if err != nil {
			reason, transient := transientPollReason(err)
			if !transient {
				return nil, terminalPollError(p.obj, err)
			}
			pending = append(pending, pendingObject{obj: p.obj, reason: reason})
			continue
		}
		ok, reason, err := judge(live)
		if err != nil {
			return nil, terminalPollError(p.obj, err)
		}
		if !ok {
			pending = append(pending, pendingObject{obj: p.obj, reason: reason})
		}
	}
	return pending, nil
}

// transientPollReason classifies a live-read error: no REST mapping (the kind
// is not served yet) and NotFound (the object is not created yet) are
// transient — the wait keeps polling; anything else is permanent.
func transientPollReason(err error) (string, bool) {
	if meta.IsNoMatchError(err) {
		return "kind not served by the cluster yet", true
	}
	if apierrors.IsNotFound(err) {
		return "object not created yet", true
	}
	return "", false
}
