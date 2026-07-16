package apply

import (
	"context"

	"github.com/fluxcd/pkg/ssa"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/cube-idp/cube-idp/internal/diag"
)

// Change is one object's server-side-dry-run outcome.
type Change struct {
	Ref    string // "<group>/<Kind>/<ns>/<name>" (group is "" for core/v1)
	Action string // "created" | "configured" | "unchanged" | "skipped" | "deleted" | "unknown"
}

// Diff server-side-dry-runs every object and reports what a real Apply
// would do. Objects are labeled exactly as Apply labels them, so the cube
// label never shows up as perpetual drift.
func (a *Applier) Diff(ctx context.Context, objs []*unstructured.Unstructured) ([]Change, error) {
	a.label(objs)
	out := make([]Change, 0, len(objs))
	for _, o := range objs {
		entry, _, _, err := a.rm.Diff(ctx, o, ssa.DefaultDiffOptions())
		if err != nil {
			return nil, diag.Wrap(err, diag.CodeApplyDiffFailed,
				"server-side diff failed for "+o.GetKind()+"/"+o.GetName(),
				"check cluster connectivity and RBAC (dry-run apply permission required)")
		}
		gk := entry.ObjMetadata.GroupKind
		ref := gk.Group + "/" + gk.Kind + "/" + entry.ObjMetadata.Namespace + "/" + entry.ObjMetadata.Name
		out = append(out, Change{Ref: ref, Action: string(entry.Action)})
	}
	return out, nil
}
