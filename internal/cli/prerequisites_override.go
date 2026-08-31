package cli

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
	"github.com/cube-idp/cube-idp/internal/gateway"
	"github.com/cube-idp/cube-idp/internal/pack"
	"github.com/cube-idp/cube-idp/internal/ref"
)

// overrideSpec resolves a list entry that points at a pack of the author's
// own: the reference grammar resolves the tree, the pack contract loads and
// renders it, and the render's flavor decides the unit's wait. Every failure
// here belongs to the layer that raised it — CUBE-REF-* for resolution,
// CUBE-PKG-* for load and render — and passes through untouched.
//
// It renders with no values and no instance id: a prerequisite pack has no
// spec.packs instance, so it renders at its #Values defaults and its own name
// is the effective id.
func overrideSpec(ctx context.Context, entry v1alpha1.PrerequisiteSpec, domain string) (unitSpec, error) {
	tree, err := ref.ResolveTree(ctx, entry.Ref)
	if err != nil {
		return unitSpec{}, err
	}
	p, err := pack.Load(ctx, tree.FS(), entry.Ref)
	if err != nil {
		return unitSpec{}, err
	}
	plan, err := p.Render(ctx, pack.RenderOptions{})
	if err != nil {
		return unitSpec{}, err
	}
	meta := p.Metadata()
	if err := checkNoPackPrerequisites(entry.Name, meta.Name, plan); err != nil {
		return unitSpec{}, err
	}
	spec, err := overrideFlavor(entry.Name, meta, plan.Objects)
	if err != nil {
		return unitSpec{}, err
	}
	return attachGatewayObject(spec, entry.Name, domain), nil
}

// overrideFlavor decides the override unit's wait from what the pack renders.
// A helm pack renders exactly the CR pair the gateway predicate judges, so it
// becomes a CR unit; a raw or kustomize pack renders ordinary objects and
// waits the kind-set.
//
// Only a helm render comes back namespace-less, so only it is stamped —
// planRaw and planKustomize already applied the pack's namespace. A helm pack
// that declares no namespace is rejected rather than stamped with a guess: its
// CRs would otherwise be sent to "default", installing a prerequisite
// somewhere nobody declared and nobody looks.
func overrideFlavor(name string, meta pack.Metadata, objs []*unstructured.Unstructured) (unitSpec, error) {
	if meta.Type != pack.TypeHelm {
		return unitSpec{name: name, objs: objs}, nil
	}
	if meta.Namespace == "" {
		return unitSpec{}, fmt.Errorf(
			"prerequisite unit %s: helm pack %s declares no namespace, so its rendered resources have no target namespace; declare one in its pack.cue",
			name, meta.Name)
	}
	for _, o := range objs {
		o.SetNamespace(meta.Namespace)
	}
	return unitSpec{name: name, objs: objs, judge: gateway.Reconciled}, nil
}

// attachGatewayObject appends the cube-authored Gateway to the unit named
// traefik-gateway, whatever content that entry resolved to: the object is
// name-selected domain behavior, so an entry pointing at someone else's pack
// still gets it, and a list that renames the unit gets no listener — which the
// list author owns (docs/domains/gateway.md).
//
// A CR unit's judge is re-composed here for the reason the embedded path
// composes one: gateway.Reconciled raises CUBE-GWY-003 for anything outside
// the CR pair, and bootstrap returns that terminally, so judging the appended
// Gateway with it would kill the run on the first poll. In a raw unit the
// Gateway simply falls outside the kind-set and is ignored, which matches M11
// not gating its readiness either way.
func attachGatewayObject(spec unitSpec, name, domain string) unitSpec {
	if name != v1alpha1.PrerequisiteTraefikGateway {
		return spec
	}
	spec.objs = append(spec.objs, gateway.GatewayObject(domain))
	if spec.judge != nil {
		spec.judge = gatewayUnitJudge
	}
	return spec
}

// checkNoPackPrerequisites rejects a pack that declares lifecycle:pre external
// manifests of its own. That mechanism and this list share a word, never a
// mechanism (docs/domains/gateway.md): nothing here delivers those manifests,
// and silently dropping content an author declared is the wrong default.
//
// No render path fills RenderPlan.Prerequisites today — only RenderInstance
// does, and a prerequisite pack has no instance — so this is the guard that
// keeps the severance true if that ever changes, not a live branch.
func checkNoPackPrerequisites(unitName, packName string, plan pack.RenderPlan) error {
	if len(plan.Prerequisites) == 0 {
		return nil
	}
	return fmt.Errorf(
		"prerequisite unit %s: pack %s declares %d lifecycle:pre manifest(s), which the bootstrap prerequisite list does not deliver; give them their own list entry",
		unitName, packName, len(plan.Prerequisites))
}
