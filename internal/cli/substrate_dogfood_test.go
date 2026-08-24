package cli

import (
	"reflect"
	"testing"

	"github.com/cube-idp/cube-idp/internal/engine/substrate"
	"github.com/cube-idp/cube-idp/internal/pack"
)

// TestSubstrateIsConformingPack is the M10 gate's dogfood test, placed at the
// composition edge because only the edge may import both domains: loading and
// rendering the substrate's embedded directory through internal/pack must
// yield exactly the substrate's own parsed install objects — deep equality of
// the ordered object lists after both parse paths, so the raw-render
// semantics (lexical file order, .yaml|.yml filtering, empty-document
// skipping, empty-render rejection) are all in scope. If the pack contract
// and the substrate ever disagree, this test breaks; neither side may drift
// silently.
func TestSubstrateIsConformingPack(t *testing.T) {
	t.Parallel()
	p, err := pack.Load(t.Context(), substrate.FS(), "embedded substrate")
	if err != nil {
		t.Fatalf("pack.Load(substrate.FS()): %v", err)
	}

	meta := p.Metadata()
	if meta.Name != "flux" || meta.Type != pack.TypeRaw || meta.Category != "engine" {
		t.Errorf("substrate pack metadata = %+v, want name=flux type=raw category=engine", meta)
	}
	if meta.Version != substrate.Version {
		t.Errorf("pack.cue version %q != substrate.Version %q", meta.Version, substrate.Version)
	}
	if meta.Namespace != "" {
		t.Errorf("substrate pack declares namespace %q; it must not — the payload carries its own", meta.Namespace)
	}

	plan, err := p.Render(t.Context(), pack.RenderOptions{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if len(plan.Prerequisites) != 0 {
		t.Errorf("substrate render produced %d prerequisites, want none", len(plan.Prerequisites))
	}

	want, err := substrate.Objects()
	if err != nil {
		t.Fatalf("substrate.Objects: %v", err)
	}
	if len(plan.Objects) != len(want) {
		t.Fatalf("pack render = %d objects, substrate parse = %d — the two parse paths disagree", len(plan.Objects), len(want))
	}
	for i := range want {
		if !reflect.DeepEqual(plan.Objects[i].Object, want[i].Object) {
			t.Fatalf("object %d differs between pack render (%s %s) and substrate parse (%s %s)",
				i, plan.Objects[i].GetKind(), plan.Objects[i].GetName(), want[i].GetKind(), want[i].GetName())
		}
	}
}
