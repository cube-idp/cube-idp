package gateway

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Reconciled judges one of this domain's declared CRs: reconciled, not yet
// (with a reason that feeds the bootstrap timeout diagnostics), or the
// coded error for an object outside the domain's coverage. The
// no-stale-success doctrine in this domain's vocabulary — the Ready
// condition must be true AND status.observedGeneration must equal
// metadata.generation, so a Ready left over from before a spec change does
// not count. Pure: it reads the handed-in status and never fetches.
//
// It is a package-level function, not a method: there is no driver seam
// here, and this shape is structurally assignable to bootstrap's own
// ReconciledFunc, which is how the judgment crosses the edge as neutral
// vocabulary without either domain importing the other.
func Reconciled(obj *unstructured.Unstructured) (bool, string, error) {
	if !recognized(obj) {
		return false, "", newUnrecognizedObjectError(obj.GetAPIVersion(), obj.GetKind())
	}
	status, condReason, found := readyCondition(obj)
	switch {
	case !found:
		return false, "no Ready condition reported yet (the controller may not have observed the object)", nil
	case status != "True":
		return false, condReason, nil
	}
	observed, found, _ := unstructured.NestedInt64(obj.Object, "status", "observedGeneration")
	gen := obj.GetGeneration()
	switch {
	case !found:
		return false, "no observedGeneration reported yet (cannot verify the Ready status is current)", nil
	case observed != gen:
		return false, fmt.Sprintf(
			"stale status: observedGeneration %d does not match generation %d (the Ready condition does not describe the latest spec)",
			observed, gen), nil
	}
	return true, "", nil
}

// recognized reports whether obj is one of this domain's declared kinds —
// the thin-helm prerequisite unit's rendered CR pair, and nothing else.
func recognized(obj *unstructured.Unstructured) bool {
	switch obj.GetAPIVersion() {
	case sourceAPIVersion:
		return obj.GetKind() == kindOCIRepository
	case helmReleaseAPIVersion:
		return obj.GetKind() == kindHelmRelease
	}
	return false
}

// readyCondition returns the Ready condition's status plus a
// human-actionable rendering of its reason and message; found is false
// when no Ready condition exists yet.
func readyCondition(obj *unstructured.Unstructured) (status, reason string, found bool) {
	conds, _, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	for _, c := range conds {
		cm, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if t, _, _ := unstructured.NestedString(cm, "type"); t != "Ready" {
			continue
		}
		s, _, _ := unstructured.NestedString(cm, "status")
		return s, describeCondition(s, cm), true
	}
	return "", "", false
}

// describeCondition renders a not-ready Ready condition as
// "Ready=<status>: <reason>: <message>", dropping the parts the controller
// left empty.
func describeCondition(status string, cond map[string]any) string {
	out := "Ready=" + status
	if r, _, _ := unstructured.NestedString(cond, "reason"); r != "" {
		out += ": " + r
	}
	if m, _, _ := unstructured.NestedString(cond, "message"); m != "" {
		out += ": " + m
	}
	return out
}
