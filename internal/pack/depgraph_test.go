package pack

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/diag"
)

// mk builds one index-aligned fixture entry: a *Pack{Name, DependsOn}, a
// config.PackRef{Ref: "packs/" + name}, and a *Rendered{Name, Objects: one
// unstructured object per gvk}. delivery, when non-empty, sets the ref's
// Delivery field (used by implicit edge (b) cases).
func mk(name string, deps []string, gvks ...schema.GroupVersionKind) (*Pack, config.PackRef, *Rendered) {
	p := &Pack{Name: name, DependsOn: deps}
	ref := config.PackRef{Ref: "packs/" + name}
	objs := make([]*unstructured.Unstructured, 0, len(gvks))
	for _, gvk := range gvks {
		o := &unstructured.Unstructured{}
		o.SetGroupVersionKind(gvk)
		objs = append(objs, o)
	}
	r := &Rendered{Name: name, Objects: objs}
	return p, ref, r
}

// split fans a slice of (*Pack, config.PackRef, *Rendered) triples into the
// three index-aligned slices ResolveOrder wants.
func split(entries ...struct {
	p *Pack
	r config.PackRef
	d *Rendered
}) ([]*Pack, []config.PackRef, []*Rendered) {
	packs := make([]*Pack, len(entries))
	refs := make([]config.PackRef, len(entries))
	rendered := make([]*Rendered, len(entries))
	for i, e := range entries {
		packs[i] = e.p
		refs[i] = e.r
		rendered[i] = e.d
	}
	return packs, refs, rendered
}

func entry(p *Pack, r config.PackRef, d *Rendered) struct {
	p *Pack
	r config.PackRef
	d *Rendered
} {
	return struct {
		p *Pack
		r config.PackRef
		d *Rendered
	}{p, r, d}
}

func codeOf(t *testing.T, err error) diag.Code {
	t.Helper()
	var de *diag.Error
	if !errors.As(err, &de) {
		t.Fatalf("want a *diag.Error, got %v (%T)", err, err)
	}
	return de.Code
}

// gatewayHTTPRoute is the implicit-edge-(a) trigger GVK from the brief.
var gatewayHTTPRoute = schema.GroupVersionKind{Group: gatewayAPIGroup, Version: "v1", Kind: "HTTPRoute"}

func TestResolveOrderNoDepsDeclaredOrder(t *testing.T) {
	gwP, gwR, gwD := mk("traefik", nil)
	aP, aR, aD := mk("a", nil)
	bP, bR, bD := mk("b", nil)
	packs, refs, rendered := split(entry(gwP, gwR, gwD), entry(aP, aR, aD), entry(bP, bR, bD))

	order, deps, err := ResolveOrder(packs, refs, rendered, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(order, []int{0, 1, 2}) {
		t.Fatalf("want declared order [0 1 2], got %v", order)
	}
	if len(deps) != 0 {
		t.Fatalf("want empty deps map, got %v", deps)
	}
}

func TestResolveOrderDiamond(t *testing.T) {
	// declared: gw, a, b, c, d — d->b, d->c, b->a, c->a
	gwP, gwR, gwD := mk("gw", nil)
	aP, aR, aD := mk("a", nil)
	bP, bR, bD := mk("b", []string{"a"})
	cP, cR, cD := mk("c", []string{"a"})
	dP, dR, dD := mk("d", []string{"b", "c"})
	packs, refs, rendered := split(
		entry(gwP, gwR, gwD), entry(aP, aR, aD), entry(bP, bR, bD), entry(cP, cR, cD), entry(dP, dR, dD),
	)

	order, deps, err := ResolveOrder(packs, refs, rendered, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// index-of helper
	pos := func(idx int) int {
		for i, v := range order {
			if v == idx {
				return i
			}
		}
		t.Fatalf("index %d missing from order %v", idx, order)
		return -1
	}
	if pos(1) >= pos(2) { // a before b
		t.Fatalf("a must precede b: order=%v", order)
	}
	if pos(1) >= pos(3) { // a before c
		t.Fatalf("a must precede c: order=%v", order)
	}
	if pos(2) >= pos(4) { // b before d
		t.Fatalf("b must precede d: order=%v", order)
	}
	if pos(3) >= pos(4) { // c before d
		t.Fatalf("c must precede d: order=%v", order)
	}
	// DD6 tie-break: declared order wins among ready packs. Ready order:
	// gw(0) first, then a(1) ready, then b(2) before c(3) (declared index).
	if !reflect.DeepEqual(order, []int{0, 1, 2, 3, 4}) {
		t.Fatalf("want tie-broken declared order [0 1 2 3 4], got %v", order)
	}
	if !reflect.DeepEqual(deps["b"], []string{"a"}) {
		t.Fatalf("b deps: got %v", deps["b"])
	}
	if !reflect.DeepEqual(deps["d"], []string{"b", "c"}) {
		t.Fatalf("d deps: got %v", deps["d"])
	}
}

func TestResolveOrderUnknownName(t *testing.T) {
	gwP, gwR, gwD := mk("gw", nil)
	aP, aR, aD := mk("a", []string{"nope"})
	packs, refs, rendered := split(entry(gwP, gwR, gwD), entry(aP, aR, aD))

	_, _, err := ResolveOrder(packs, refs, rendered, nil)
	if err == nil {
		t.Fatal("want an error for an unknown dependsOn name")
	}
	if got := codeOf(t, err); got != diag.CodePackDepUnknown {
		t.Fatalf("want CUBE-4018, got %v", got)
	}
	if !strings.Contains(err.Error(), "gw") || !strings.Contains(err.Error(), "a") {
		t.Fatalf("message must contain the installed list: %v", err)
	}
}

func TestResolveOrderSelfDep(t *testing.T) {
	gwP, gwR, gwD := mk("gw", nil)
	aP, aR, aD := mk("a", []string{"a"})
	packs, refs, rendered := split(entry(gwP, gwR, gwD), entry(aP, aR, aD))

	_, _, err := ResolveOrder(packs, refs, rendered, nil)
	if err == nil {
		t.Fatal("want an error for a self-dependency")
	}
	if got := codeOf(t, err); got != diag.CodePackDepCycle {
		t.Fatalf("want CUBE-4019, got %v", got)
	}
	if !strings.Contains(err.Error(), "a → a") {
		t.Fatalf("message must show path a -> a, got: %v", err)
	}
}

func TestResolveOrderTwoCycle(t *testing.T) {
	gwP, gwR, gwD := mk("gw", nil)
	aP, aR, aD := mk("a", []string{"b"})
	bP, bR, bD := mk("b", []string{"a"})
	packs, refs, rendered := split(entry(gwP, gwR, gwD), entry(aP, aR, aD), entry(bP, bR, bD))

	_, _, err := ResolveOrder(packs, refs, rendered, nil)
	if err == nil {
		t.Fatal("want an error for a 2-cycle")
	}
	if got := codeOf(t, err); got != diag.CodePackDepCycle {
		t.Fatalf("want CUBE-4019, got %v", got)
	}
	msg := err.Error()
	if !strings.Contains(msg, "a") || !strings.Contains(msg, "b") {
		t.Fatalf("message must contain both cycle members: %v", err)
	}
	// path repeats its head: "a → b → a" or "b → a → b"
	if !strings.Contains(msg, "a → b → a") && !strings.Contains(msg, "b → a → b") {
		t.Fatalf("message must show the repeated head: %v", err)
	}
}

func TestResolveOrderThreeCycle(t *testing.T) {
	gwP, gwR, gwD := mk("gw", nil)
	aP, aR, aD := mk("a", []string{"b"})
	bP, bR, bD := mk("b", []string{"c"})
	cP, cR, cD := mk("c", []string{"a"})
	packs, refs, rendered := split(entry(gwP, gwR, gwD), entry(aP, aR, aD), entry(bP, bR, bD), entry(cP, cR, cD))

	_, _, err := ResolveOrder(packs, refs, rendered, nil)
	if err == nil {
		t.Fatal("want an error for a 3-cycle")
	}
	if got := codeOf(t, err); got != diag.CodePackDepCycle {
		t.Fatalf("want CUBE-4019, got %v", got)
	}
	msg := err.Error()
	for _, name := range []string{"a", "b", "c"} {
		if !strings.Contains(msg, name) {
			t.Fatalf("message must contain cycle member %q: %v", name, err)
		}
	}
	// repeated head: path contains each member once plus the repeated head
	if strings.Count(msg, "a") < 2 && strings.Count(msg, "b") < 2 && strings.Count(msg, "c") < 2 {
		t.Fatalf("message must show a repeated head: %v", err)
	}
}

func TestResolveOrderGatewayPackCueDependsOnIsCUBE4020(t *testing.T) {
	gwP, gwR, gwD := mk("gw", []string{"a"})
	aP, aR, aD := mk("a", nil)
	packs, refs, rendered := split(entry(gwP, gwR, gwD), entry(aP, aR, aD))

	_, _, err := ResolveOrder(packs, refs, rendered, nil)
	if err == nil {
		t.Fatal("want an error for a gateway pack.cue dependsOn")
	}
	if got := codeOf(t, err); got != diag.CodePackDepGateway {
		t.Fatalf("want CUBE-4020, got %v", got)
	}
}

func TestResolveOrderGatewayRefDependsOnIsCUBE4020(t *testing.T) {
	gwP, gwR, gwD := mk("gw", nil)
	gwR.DependsOn = []string{"a"}
	aP, aR, aD := mk("a", nil)
	packs, refs, rendered := split(entry(gwP, gwR, gwD), entry(aP, aR, aD))

	_, _, err := ResolveOrder(packs, refs, rendered, nil)
	if err == nil {
		t.Fatal("want an error for a gateway cube.yaml dependsOn")
	}
	if got := codeOf(t, err); got != diag.CodePackDepGateway {
		t.Fatalf("want CUBE-4020, got %v", got)
	}
}

func TestResolveOrderImplicitGatewayEdge(t *testing.T) {
	// gw's own Gateway-API objects add no self-edge; a pack rendering an
	// HTTPRoute sorts after the gateway even declared first (relative to
	// no explicit deps), and the deps map shows the gateway name.
	gwP, gwR, gwD := mk("gw", nil, gatewayHTTPRoute)
	aP, aR, aD := mk("a", nil, gatewayHTTPRoute)
	packs, refs, rendered := split(entry(gwP, gwR, gwD), entry(aP, aR, aD))

	order, deps, err := ResolveOrder(packs, refs, rendered, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(order, []int{0, 1}) {
		t.Fatalf("want gw before a: order=%v", order)
	}
	if !reflect.DeepEqual(deps["a"], []string{"gw"}) {
		t.Fatalf("a's deps must show gw: got %v", deps["a"])
	}
	if _, ok := deps["gw"]; ok {
		t.Fatalf("gw must have no self-edge from its own Gateway-API objects: deps=%v", deps)
	}
}

func TestResolveOrderImplicitRepoEdge(t *testing.T) {
	gwP, gwR, gwD := mk("gw", nil)
	giteaP, giteaR, giteaD := mk("gitea", nil)
	myP, myR, myD := mk("mypack", nil)
	myR.Delivery = "repo"
	packs, refs, rendered := split(entry(gwP, gwR, gwD), entry(giteaP, giteaR, giteaD), entry(myP, myR, myD))

	order, deps, err := ResolveOrder(packs, refs, rendered, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(order, []int{0, 1, 2}) {
		t.Fatalf("want gitea before mypack: order=%v", order)
	}
	if !reflect.DeepEqual(deps["mypack"], []string{"gitea"}) {
		t.Fatalf("mypack deps must show gitea: got %v", deps["mypack"])
	}
}

func TestResolveOrderRepoDeliveryNoGiteaIsCUBE4018(t *testing.T) {
	gwP, gwR, gwD := mk("gw", nil)
	myP, myR, myD := mk("mypack", nil)
	myR.Delivery = "repo"
	packs, refs, rendered := split(entry(gwP, gwR, gwD), entry(myP, myR, myD))

	_, _, err := ResolveOrder(packs, refs, rendered, nil)
	if err == nil {
		t.Fatal("want an error for repo delivery with no gitea pack")
	}
	if got := codeOf(t, err); got != diag.CodePackDepUnknown {
		t.Fatalf("want CUBE-4018, got %v", got)
	}
}

func TestResolveOrderRepoDeliveryGuaranteeAgainstArgocd(t *testing.T) {
	// declared [gw, argocd, gitea, mypack(repo)] — gitea before mypack
	// (the guarantee half); argocd may precede or follow gitea.
	gwP, gwR, gwD := mk("gw", nil)
	argoP, argoR, argoD := mk("argocd", nil)
	giteaP, giteaR, giteaD := mk("gitea", nil)
	myP, myR, myD := mk("mypack", nil)
	myR.Delivery = "repo"
	packs, refs, rendered := split(
		entry(gwP, gwR, gwD), entry(argoP, argoR, argoD), entry(giteaP, giteaR, giteaD), entry(myP, myR, myD),
	)

	order, _, err := ResolveOrder(packs, refs, rendered, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	pos := func(idx int) int {
		for i, v := range order {
			if v == idx {
				return i
			}
		}
		t.Fatalf("index %d missing from order %v", idx, order)
		return -1
	}
	if pos(2) >= pos(3) { // gitea(2) before mypack(3)
		t.Fatalf("gitea must precede mypack: order=%v", order)
	}
	// no edges at all involving argocd -> declared order preserved (DD6):
	// with no dependency between argocd and gitea, Kahn picks lowest ready
	// index each round, so argocd(1) precedes gitea(2).
	if !reflect.DeepEqual(order, []int{0, 1, 2, 3}) {
		t.Fatalf("want declared order preserved [0 1 2 3], got %v", order)
	}
}

func TestResolveOrderDuplicateNamesIsCUBE4018(t *testing.T) {
	gwP, gwR, gwD := mk("gw", nil)
	a1P, a1R, a1D := mk("dup", nil)
	a2P, a2R, a2D := mk("dup", nil)
	packs, refs, rendered := split(entry(gwP, gwR, gwD), entry(a1P, a1R, a1D), entry(a2P, a2R, a2D))

	_, _, err := ResolveOrder(packs, refs, rendered, nil)
	if err == nil {
		t.Fatal("want an error for duplicate pack names")
	}
	if got := codeOf(t, err); got != diag.CodePackDepUnknown {
		t.Fatalf("want CUBE-4018, got %v", got)
	}
	if !strings.Contains(err.Error(), "dup") {
		t.Fatalf("collision message must name the pack: %v", err)
	}
}

// crdRendered builds a *Rendered carrying one CustomResourceDefinition whose
// spec.group is group — the shape a CRD-bearing prerequisite (e.g. the Gateway
// API CRDs) renders, from which ProvidedGroups reads the satisfied group.
func crdRendered(name, group string) *Rendered {
	crd := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apiextensions.k8s.io/v1",
		"kind":       "CustomResourceDefinition",
		"metadata":   map[string]any{"name": "httproutes." + group},
		"spec":       map[string]any{"group": group},
	}}
	return &Rendered{Name: name, Objects: []*unstructured.Unstructured{crd}}
}

// TestProvidedGroupsReadsCRDGroup pins the capability-inference extractor
// (ADR-0045): ProvidedGroups returns exactly the groups a render establishes
// by shipping their CRDs, read from spec.group. A render with no CRD provides
// nothing (nil), so the graph is unaffected when no prerequisite carries CRDs.
func TestProvidedGroupsReadsCRDGroup(t *testing.T) {
	got := ProvidedGroups([]*Rendered{crdRendered("gateway-api-crds", gatewayAPIGroup)})
	if !got[gatewayAPIGroup] || len(got) != 1 {
		t.Fatalf("want {%s:true}, got %v", gatewayAPIGroup, got)
	}
	// A prerequisite rendering only non-CRD objects (or none) provides nothing.
	if g := ProvidedGroups([]*Rendered{{Name: "kyverno", Objects: nil}}); g != nil {
		t.Fatalf("no CRD => nil, got %v", g)
	}
	if g := ProvidedGroups(nil); g != nil {
		t.Fatalf("no renders => nil, got %v", g)
	}
}

// TestResolveOrderPrerequisiteSatisfiesGatewayGroup is the T3 capability-
// inference contract: when a prerequisite provides the Gateway API group, a
// pack rendering an HTTPRoute acquires NO implicit edge (a) to the gateway
// pack — the CRDs are already Established by the pre-engine prerequisite, so
// the phantom dependency is gone. Contrast TestResolveOrderImplicitGatewayEdge,
// where with no such prerequisite the edge (and the ordering) still hold.
func TestResolveOrderPrerequisiteSatisfiesGatewayGroup(t *testing.T) {
	gwP, gwR, gwD := mk("gw", nil)
	aP, aR, aD := mk("a", nil, gatewayHTTPRoute) // renders an HTTPRoute
	packs, refs, rendered := split(entry(gwP, gwR, gwD), entry(aP, aR, aD))

	// The Gateway API CRDs arrive as a prerequisite, satisfying the group.
	provided := ProvidedGroups([]*Rendered{crdRendered("gateway-api-crds", gatewayAPIGroup)})
	_, deps, err := ResolveOrder(packs, refs, rendered, provided)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// No phantom edge: "a" depends on nothing now.
	if _, ok := deps["a"]; ok {
		t.Fatalf("a must have NO gateway edge when a prerequisite provides the CRDs: deps=%v", deps)
	}

	// Sanity: WITHOUT the prerequisite, the edge is still there (regression
	// fence for the pre-ADR-0045 behavior).
	_, deps2, err := ResolveOrder(packs, refs, rendered, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(deps2["a"], []string{"gw"}) {
		t.Fatalf("without a prerequisite the gateway edge must remain: got %v", deps2["a"])
	}
}
