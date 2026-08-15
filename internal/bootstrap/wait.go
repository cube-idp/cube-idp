package bootstrap

import (
	"context"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/wait"
)

// pollInterval is how often WaitReady re-checks the bootstrap kind-set.
const pollInterval = 2 * time.Second

// WaitReady blocks until every object in the bootstrap kind-set reports ready —
// CRD Established, Deployment/StatefulSet available, Job complete, Namespace
// Active — or ctx is done. Objects outside the kind-set are skipped: their
// readiness (engine CRs) is the M9 seam's concern, not bootstrap's.
func (a *Applier) WaitReady(ctx context.Context, objs []*unstructured.Unstructured) error {
	pending := filterKindSet(objs)
	err := wait.PollUntilContextCancel(ctx, a.interval, true, func(ctx context.Context) (bool, error) {
		remaining, err := a.checkReady(ctx, pending)
		if err != nil {
			return false, err
		}
		pending = remaining
		return len(pending) == 0, nil
	})
	if err != nil {
		return newWaitError(pending, err)
	}
	return nil
}

// checkReady runs one readiness pass and returns the objects not yet ready.
func (a *Applier) checkReady(ctx context.Context, objs []*unstructured.Unstructured) ([]*unstructured.Unstructured, error) {
	var pending []*unstructured.Unstructured
	for _, obj := range objs {
		ready, err := a.ready(ctx, obj)
		if err != nil {
			return nil, err
		}
		if !ready {
			pending = append(pending, obj)
		}
	}
	return pending, nil
}

// ready fetches an object's live state and evaluates its kind's readiness
// predicate. A not-yet-created object is simply not ready (keep waiting).
func (a *Applier) ready(ctx context.Context, obj *unstructured.Unstructured) (bool, error) {
	live, err := a.k.get(ctx, obj)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	switch obj.GetKind() {
	case "CustomResourceDefinition":
		return conditionTrue(live, "Established"), nil
	case "Deployment", "StatefulSet":
		return workloadReady(live), nil
	case "Job":
		return conditionTrue(live, "Complete"), nil
	case "Namespace":
		return nestedString(live, "status", "phase") == "Active", nil
	}
	return true, nil
}

// filterKindSet keeps only the objects whose readiness bootstrap waits on.
func filterKindSet(objs []*unstructured.Unstructured) []*unstructured.Unstructured {
	var out []*unstructured.Unstructured
	for _, obj := range objs {
		switch obj.GetKind() {
		case "CustomResourceDefinition", "Deployment", "StatefulSet", "Job", "Namespace":
			out = append(out, obj)
		}
	}
	return out
}

// workloadReady reports whether a Deployment/StatefulSet has rolled out: its
// status is observed for the current generation and ready replicas meet the
// desired count (a 0-replica workload is trivially ready).
func workloadReady(o *unstructured.Unstructured) bool {
	if observed, found, _ := unstructured.NestedInt64(o.Object, "status", "observedGeneration"); found && observed < o.GetGeneration() {
		return false
	}
	desired, found, _ := unstructured.NestedInt64(o.Object, "spec", "replicas")
	if !found {
		desired = 1
	}
	if desired == 0 {
		return true
	}
	ready, _, _ := unstructured.NestedInt64(o.Object, "status", "readyReplicas")
	return ready >= desired
}

// conditionTrue reports whether o has a status condition of the given type with
// status "True".
func conditionTrue(o *unstructured.Unstructured, condType string) bool {
	conds, found, _ := unstructured.NestedSlice(o.Object, "status", "conditions")
	if !found {
		return false
	}
	for _, c := range conds {
		cm, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if t, _, _ := unstructured.NestedString(cm, "type"); t == condType {
			s, _, _ := unstructured.NestedString(cm, "status")
			return s == "True"
		}
	}
	return false
}

func nestedString(o *unstructured.Unstructured, fields ...string) string {
	s, _, _ := unstructured.NestedString(o.Object, fields...)
	return s
}
