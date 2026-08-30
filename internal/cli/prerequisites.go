package cli

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
	"github.com/cube-idp/cube-idp/internal/bootstrap"
	"github.com/cube-idp/cube-idp/internal/ca"
	"github.com/cube-idp/cube-idp/internal/gateway"
)

// prereqInputs are the values the resolved prerequisite entries are built
// from: the list itself (materialized by api's Default(), so it is never
// empty), the cube's resolved base domain, and the effective CA material the
// edge ensured before composing. Resolution performs no cluster I/O — every
// unit is a function of these.
type prereqInputs struct {
	// units is spec.prerequisites in list order. Order IS the install
	// order: bootstrap solves no graph (docs/domains/gateway.md).
	units []v1alpha1.PrerequisiteSpec
	// domain is the cube's base domain, resolved by gatewayDomain.
	domain string
	// ensured is the effective CA material. It is the zero value when the
	// list carries no ca-secrets entry, in which case nothing reads it.
	ensured ca.EnsureResult
}

// unitSpec is the edge's own description of one prerequisite unit, before it
// becomes a bootstrap.Unit. bootstrap.Unit has no exported fields by design,
// so resolution is expressed in this inspectable shape and converted only at
// the end: the resolution table can then assert what each entry produced
// without internal/bootstrap growing accessors that exist only for tests.
type unitSpec struct {
	// name is the list entry's name; it names the unit in bootstrap's
	// wait diagnostics.
	name string
	// objs are the unit's objects, in apply order.
	objs []*unstructured.Unstructured
	// judge is the reconciliation judgment. Non-nil selects the CR flavor.
	judge bootstrap.ReconciledFunc
	// inert marks a unit of status-less objects for which a successful
	// apply IS readiness. It is ignored when judge is set.
	inert bool
}

// unit converts the spec into the opaque bootstrap unit its flavor calls for.
func (s unitSpec) unit() bootstrap.Unit {
	switch {
	case s.judge != nil:
		return bootstrap.NewCRUnit(s.name, s.objs, s.judge)
	case s.inert:
		return bootstrap.NewInertUnit(s.name, s.objs)
	default:
		return bootstrap.NewRawUnit(s.name, s.objs)
	}
}

// prerequisiteUnits resolves spec.prerequisites into the ordered units
// bootstrap installs between the substrate wait and the engine's sync wiring.
func prerequisiteUnits(ctx context.Context, in prereqInputs) ([]bootstrap.Unit, error) {
	specs, err := prerequisiteSpecs(ctx, in)
	if err != nil {
		return nil, err
	}
	units := make([]bootstrap.Unit, 0, len(specs))
	for _, s := range specs {
		units = append(units, s.unit())
	}
	return units, nil
}

// prerequisiteSpecs resolves the list in order, one unit per entry. The
// returned order IS EngineInstall.Prerequisites: order and inter-unit
// dependency are the list author's, and a list that drops or reorders a unit
// owns what follows (docs/domains/gateway.md, replace-whole-list).
func prerequisiteSpecs(ctx context.Context, in prereqInputs) ([]unitSpec, error) {
	specs := make([]unitSpec, 0, len(in.units))
	for _, entry := range in.units {
		s, err := prerequisiteSpec(ctx, entry, in)
		if err != nil {
			return nil, err
		}
		specs = append(specs, s)
	}
	return specs, nil
}

// prerequisiteSpec resolves one entry. Dispatch is BY NAME FIRST, ref second,
// which is the contract's rule and a structural guarantee rather than an
// ordering convention: a built-in name is answered before the reference
// grammar is reachable at all, so cube-owned content can never be pointed
// somewhere else. A cube-shipped pack name takes the embedded copy when its
// ref is empty and the override path when it is not; every other name is an
// override, which document validation already requires a ref for.
func prerequisiteSpec(ctx context.Context, entry v1alpha1.PrerequisiteSpec, in prereqInputs) (unitSpec, error) {
	switch entry.Name {
	case v1alpha1.PrerequisiteGatewayPlatform:
		if err := checkBuiltInHasNoRef(entry); err != nil {
			return unitSpec{}, err
		}
		return unitSpec{name: entry.Name, objs: gateway.PlatformObjects()}, nil
	case v1alpha1.PrerequisiteCASecrets:
		if err := checkBuiltInHasNoRef(entry); err != nil {
			return unitSpec{}, err
		}
		return caSecretsSpec(entry.Name, in.ensured), nil
	case v1alpha1.PrerequisiteGatewayAPICRDs:
		if entry.Ref == "" {
			return crdsSpec(entry.Name)
		}
	case v1alpha1.PrerequisiteTraefikGateway:
		if entry.Ref == "" {
			return embeddedGatewaySpec(entry.Name, in.domain), nil
		}
	}
	return overrideSpec(ctx, entry, in.domain)
}

// checkBuiltInHasNoRef rejects a ref on a built-in unit. There is nothing to
// point one at — the content and the behavior are the cube's — and document
// validation already forbids it, so this input cannot arrive through
// config.LoadFile. It is checked anyway: the resolver's invariant is that
// built-in content never resolves through a reference, and an invariant that
// only holds because of a check somewhere else fails silently when that
// somewhere else moves.
func checkBuiltInHasNoRef(entry v1alpha1.PrerequisiteSpec) error {
	if entry.Ref == "" {
		return nil
	}
	return fmt.Errorf("prerequisite unit %s is built-in and takes no ref; document validation forbids it", entry.Name)
}

// crdsSpec builds the Gateway API CRDs unit from the embedded, provenance-
// checked payload. Raw flavor: every CRD waits Established before the next
// unit applies, which is what the units after it depend on. Nothing is
// stamped — the payload is cluster-scoped.
func crdsSpec(name string) (unitSpec, error) {
	objs, err := gateway.CRDsPackObjects()
	if err != nil {
		return unitSpec{}, err
	}
	return unitSpec{name: name, objs: objs}, nil
}

// caSecretsSpec builds the CA handoff unit: the two Secrets the ca domain
// emitted from the material the edge ensured, placed by the gateway domain's
// exported facts. Inert flavor — Secrets carry no status, so applying them is
// their readiness.
func caSecretsSpec(name string, ensured ca.EnsureResult) unitSpec {
	placement := ca.SecretPlacement{
		Namespace:  gateway.Namespace,
		CASecret:   gateway.CASecretName,
		LeafSecret: gateway.LeafSecretName,
	}
	return unitSpec{name: name, objs: ca.SecretObjects(placement, ensured), inert: true}
}

// embeddedGatewaySpec builds the gateway implementation unit from the
// cube-shipped thin-helm pack: the rendered CR pair, stamped with the gateway
// namespace here because a helm render is deliberately namespace-less, plus
// the cube-authored Gateway applied beside it (docs/domains/gateway.md — it
// carries its own namespace, and the units before this one have already made
// its kind Established and placed the Secret its certificateRefs names).
//
// Stamping in place needs no copy: HelmPairObjects builds fresh maps on every
// call, so the dogfood test's own call is unaffected by what is stamped here.
func embeddedGatewaySpec(name, domain string) unitSpec {
	objs := gateway.HelmPairObjects()
	for _, o := range objs {
		o.SetNamespace(gateway.Namespace)
	}
	objs = append(objs, gateway.GatewayObject(domain))
	return unitSpec{name: name, objs: objs, judge: gatewayUnitJudge}
}

// gatewayUnitJudge judges the gateway implementation unit's objects. The
// cube-authored Gateway is applied beside the CR pair but its readiness is
// deliberately not gated in M11 (docs/domains/gateway.md): no cube-owned
// endpoints exist to serve until M12, and a Programmed-condition predicate is
// a named breakdown option, not gate surface. Everything else — the CR pair,
// and any object the domain does not cover — goes to gateway.Reconciled, so a
// genuine CUBE-GWY-003 still surfaces rather than being swallowed by a blanket
// "unknown means ready".
//
// The Gateway is matched by identity, never by pointer: bootstrap judges the
// object it read back from the cluster, not the one the edge handed in.
func gatewayUnitJudge(obj *unstructured.Unstructured) (bool, string, error) {
	// "Gateway" is the kind the gateway domain emits beside its CR pair;
	// the apiVersion is that domain's exported spelling.
	if obj.GetAPIVersion() == gateway.GatewayAPIVersion && obj.GetKind() == "Gateway" &&
		obj.GetName() == gateway.Name && obj.GetNamespace() == gateway.Namespace {
		return true, "", nil
	}
	return gateway.Reconciled(obj)
}

// hasPrerequisite reports whether the resolved list carries an entry by this
// name. It is what gates the edge's own behavior — the CA read and sync, and
// the CoreDNS splice — on the units that actually install: a list that drops
// a built-in genuinely drops it, and a cube without the gateway fabric is
// constructible by explicit choice.
func hasPrerequisite(units []v1alpha1.PrerequisiteSpec, name string) bool {
	for _, u := range units {
		if u.Name == name {
			return true
		}
	}
	return false
}

// hasGatewayFabric reports whether the list installs both halves the
// in-cluster rewrite needs: the platform unit, which owns the DNS target the
// block points at (the stable Service), and the gateway unit, whose
// reconciliation the contract sequences the splice after — an implementation
// behind that target. A list carrying only one half is the author's
// constructible choice and gets no splice: pointing cluster DNS at a half
// fabric is the silent degrade the safety envelope forbids.
func hasGatewayFabric(units []v1alpha1.PrerequisiteSpec) bool {
	return hasPrerequisite(units, v1alpha1.PrerequisiteGatewayPlatform) &&
		hasPrerequisite(units, v1alpha1.PrerequisiteTraefikGateway)
}
