package engine

import (
	"fmt"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/diag"
)

// ApplyTuning implements GT1: the closed engine.tuning knob set (replicas,
// resources) patched over the embedded install manifests in memory, before
// SSA. Plain manifests are the only engine install path (no helm) — this
// is a walk-and-set, not a re-render.
func ApplyTuning(objs []*unstructured.Unstructured, v *config.EngineTuning) error {
	if v == nil || len(v.Components) == 0 {
		return nil
	}
	deployments := map[string]*unstructured.Unstructured{}
	for _, o := range objs {
		if o.GetKind() == "Deployment" {
			deployments[o.GetName()] = o
		}
	}
	for name, tune := range v.Components {
		d, ok := deployments[name]
		if !ok {
			valid := make([]string, 0, len(deployments))
			for n := range deployments {
				valid = append(valid, n)
			}
			sort.Strings(valid)
			// The valid-name list lives in the summary, not the remediation:
			// diag.Error.Error() renders Code+Summary(+Cause) only, and the
			// contract is that err.Error() names the valid components.
			return diag.New(diag.CodeEngineTuningUnknown,
				fmt.Sprintf("engine.tuning.components.%s: no such engine component (valid: %s)", name, strings.Join(valid, ", ")),
				"fix spec.engine.tuning.components to use one of this engine's Deployment names")
		}
		if tune.Replicas != nil {
			if err := unstructured.SetNestedField(d.Object, int64(*tune.Replicas), "spec", "replicas"); err != nil {
				return diag.Wrap(err, diag.CodeEngineTuningUnknown, "cannot set replicas", "report this as a bug")
			}
		}
		if len(tune.Resources) > 0 {
			cs, found, err := unstructured.NestedSlice(d.Object, "spec", "template", "spec", "containers")
			if err != nil || !found || len(cs) == 0 {
				return diag.New(diag.CodeEngineTuningUnknown,
					fmt.Sprintf("engine.tuning.components.%s: deployment has no containers to patch", name),
					"report this as a bug — the embedded manifest changed shape")
			}
			for i := range cs {
				c := cs[i].(map[string]any)
				c["resources"] = deepCopyJSON(tune.Resources)
				cs[i] = c
			}
			if err := unstructured.SetNestedSlice(d.Object, cs, "spec", "template", "spec", "containers"); err != nil {
				return diag.Wrap(err, diag.CodeEngineTuningUnknown, "cannot set resources", "report this as a bug")
			}
		}
	}
	return nil
}

// deepCopyJSON keeps the caller's map unshared and its leaves
// DeepCopyJSONValue-safe: SetNestedSlice deep-copies via
// runtime.DeepCopyJSONValue, which accepts int64 but panics on Go int —
// config.Load leaves tuning numbers as CUE's int64, but hand-constructed
// tunings (tests, future callers) may carry int, so ints are widened here.
func deepCopyJSON(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = deepCopyJSONValue(v)
	}
	return out
}

func deepCopyJSONValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return deepCopyJSON(t)
	case []any:
		out := make([]any, len(t))
		for i, vv := range t {
			out[i] = deepCopyJSONValue(vv)
		}
		return out
	case int:
		return int64(t)
	default:
		return v
	}
}
