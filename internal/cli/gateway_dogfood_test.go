package cli

import (
	"reflect"
	"testing"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
	"github.com/cube-idp/cube-idp/internal/gateway"
	"github.com/cube-idp/cube-idp/internal/pack"
)

// The M11 gate's dogfood tests for the gateway domain's two embedded
// prerequisite packs, placed at the composition edge because only the edge
// may import both internal/pack and a content domain. No wiring lands here;
// the gateway domain itself never imports internal/pack, and these tests are
// what keep its own emission and the pack contract from drifting apart.

// TestGatewayCRDsPackIsConforming asserts the embedded Gateway API CRDs pack
// is a conforming raw pack: loading and rendering its directory through
// internal/pack must yield exactly the objects the domain parses itself, deep
// equality of the ordered lists, so the raw-render semantics (lexical file
// order, .yaml|.yml filtering, empty-document skipping) are all in scope.
func TestGatewayCRDsPackIsConforming(t *testing.T) {
	t.Parallel()
	fsys, err := gateway.CRDsPackFS()
	if err != nil {
		t.Fatalf("gateway.CRDsPackFS(): %v", err)
	}
	p, err := pack.Load(t.Context(), fsys, "embedded gateway-api-crds")
	if err != nil {
		t.Fatalf("pack.Load(gateway.CRDsPackFS()): %v", err)
	}

	meta := p.Metadata()
	if meta.Name != v1alpha1.PrerequisiteGatewayAPICRDs || meta.Type != pack.TypeRaw || meta.Category != "gateway" {
		t.Errorf("CRDs pack metadata = %+v, want name=%s type=raw category=gateway", meta, v1alpha1.PrerequisiteGatewayAPICRDs)
	}
	if meta.Version != gateway.CRDsVersion {
		t.Errorf("pack.cue version %q != gateway.CRDsVersion %q", meta.Version, gateway.CRDsVersion)
	}
	if meta.Namespace != "" {
		t.Errorf("CRDs pack declares namespace %q; it must not — the payload is cluster-scoped", meta.Namespace)
	}

	plan, err := p.Render(t.Context(), pack.RenderOptions{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if len(plan.Prerequisites) != 0 {
		t.Errorf("CRDs render produced %d prerequisites, want none", len(plan.Prerequisites))
	}

	want, err := gateway.CRDsPackObjects()
	if err != nil {
		t.Fatalf("gateway.CRDsPackObjects: %v", err)
	}
	if len(plan.Objects) != len(want) {
		t.Fatalf("pack render = %d objects, gateway parse = %d — the two parse paths disagree", len(plan.Objects), len(want))
	}
	for i := range want {
		if !reflect.DeepEqual(plan.Objects[i].Object, want[i].Object) {
			t.Fatalf("object %d differs between pack render (%s %s) and gateway parse (%s %s)",
				i, plan.Objects[i].GetKind(), plan.Objects[i].GetName(), want[i].GetKind(), want[i].GetName())
		}
	}
}

// TestGatewayHelmPackIsConforming is the loud guard on a deliberate
// duplication: the thin-helm pack's static values live twice — CUE #Values
// defaults in pack.cue, Go literals in gateway.HelmPairObjects — because
// ARCHITECTURE §8 confines cuelang.org/go to internal/pack, so the domain
// cannot parse its own pack.cue. Rendering the embedded pack must reproduce
// the hand-built pair field for field, which additionally locks every M9
// render rule the pair depends on: source-first order, the effective-id
// releaseName, the namespace-less CRs the edge stamps later.
func TestGatewayHelmPackIsConforming(t *testing.T) {
	t.Parallel()
	fsys, err := gateway.HelmPackFS()
	if err != nil {
		t.Fatalf("gateway.HelmPackFS(): %v", err)
	}
	p, err := pack.Load(t.Context(), fsys, "embedded traefik-gateway")
	if err != nil {
		t.Fatalf("pack.Load(gateway.HelmPackFS()): %v", err)
	}

	meta := p.Metadata()
	if meta.Name != v1alpha1.PrerequisiteTraefikGateway || meta.Type != pack.TypeHelm || meta.Category != "gateway" {
		t.Errorf("helm pack metadata = %+v, want name=%s type=helm category=gateway", meta, v1alpha1.PrerequisiteTraefikGateway)
	}
	if meta.Version != gateway.ChartVersion {
		t.Errorf("pack.cue version %q != gateway.ChartVersion %q", meta.Version, gateway.ChartVersion)
	}
	if meta.Namespace != "gateway-system" {
		t.Errorf("helm pack namespace = %q, want gateway-system", meta.Namespace)
	}
	if meta.Chart == nil || meta.Chart.Digest != gateway.ChartDigest {
		t.Errorf("helm pack chart = %+v, want digest %q", meta.Chart, gateway.ChartDigest)
	}

	plan, err := p.Render(t.Context(), pack.RenderOptions{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	want := gateway.HelmPairObjects()
	if len(want) != 2 {
		t.Fatalf("gateway.HelmPairObjects() = %d objects, want the source + release pair", len(want))
	}
	if len(plan.Objects) != len(want) {
		t.Fatalf("pack render = %d objects, gateway emission = %d", len(plan.Objects), len(want))
	}
	for i := range want {
		if !reflect.DeepEqual(plan.Objects[i].Object, want[i].Object) {
			t.Fatalf("object %d differs\npack render: %#v\ngateway emission: %#v",
				i, plan.Objects[i].Object, want[i].Object)
		}
	}
}
