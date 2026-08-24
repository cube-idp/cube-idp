package flux

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/cube-idp/cube-idp/internal/engine"
)

// Reconciled judges one declared object's status: reconciled, not yet
// (with a reason that feeds the CUBE-BST-009 timeout diagnostics), or
// the coded error for an object outside the driver's coverage. The
// no-stale-success principle in Flux's own freshness vocabulary: the
// Ready condition must be true AND status.observedGeneration must equal
// metadata.generation — a fresh-looking Ready left over from before a
// spec change does not count. Pure: it reads the handed-in status and
// never fetches.
func (f *Flux) Reconciled(obj *unstructured.Unstructured) (bool, string, error) {
	if !recognized(obj) {
		return false, "", engine.NewUnrecognizedObjectError(obj.GetAPIVersion(), obj.GetKind())
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

// recognized reports whether obj is one of the driver's declared kinds —
// the sync wiring CRs; flux declares no engine bundle.
func recognized(obj *unstructured.Unstructured) bool {
	switch obj.GetAPIVersion() {
	case sourceAPIVersion:
		return obj.GetKind() == "GitRepository" || obj.GetKind() == "OCIRepository"
	case kustomizeAPIVersion:
		return obj.GetKind() == "Kustomization"
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
// "Ready=<status>: <reason>: <message>", dropping the parts the
// controller left empty.
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
