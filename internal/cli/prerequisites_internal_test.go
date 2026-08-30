package cli

import (
	"fmt"
	"reflect"
	"slices"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
	"github.com/cube-idp/cube-idp/internal/bootstrap"
	"github.com/cube-idp/cube-idp/internal/ca"
	"github.com/cube-idp/cube-idp/internal/gateway"
)

const testDomain = "dev.cube.test"

// testEnsured is stand-in CA material: resolution never parses it, it only
// places it into Secrets, so opaque bytes are enough and keep the table fast.
func testEnsured() ca.EnsureResult {
	return ca.EnsureResult{
		CA:   ca.Material{CertPEM: []byte("ca-cert"), KeyPEM: []byte("ca-key")},
		Leaf: ca.Material{CertPEM: []byte("leaf-cert"), KeyPEM: []byte("leaf-key")},
	}
}

// defaultPrerequisiteList is api's compiled default list, spelled from the
// same exported constants Default() materializes it from.
func defaultPrerequisiteList() []v1alpha1.PrerequisiteSpec {
	return []v1alpha1.PrerequisiteSpec{
		{Name: v1alpha1.PrerequisiteGatewayPlatform},
		{Name: v1alpha1.PrerequisiteGatewayAPICRDs},
		{Name: v1alpha1.PrerequisiteCASecrets},
		{Name: v1alpha1.PrerequisiteTraefikGateway},
	}
}

// flavorOf names the wait a spec selected. Function values are not
// comparable, so the CR flavor is read off the judge's presence and its
// behavior is asserted separately.
func flavorOf(s unitSpec) string {
	switch {
	case s.judge != nil:
		return "cr"
	case s.inert:
		return "inert"
	default:
		return "raw"
	}
}

// descriptors renders objects as "Kind namespace/name", which is what the
// resolution assertions are about: identity and placement, not content.
func descriptors(objs []*unstructured.Unstructured) []string {
	out := make([]string, 0, len(objs))
	for _, o := range objs {
		out = append(out, fmt.Sprintf("%s %s/%s", o.GetKind(), o.GetNamespace(), o.GetName()))
	}
	return out
}

// resolveSpecs resolves a list with the shared stand-in inputs.
func resolveSpecs(t *testing.T, units []v1alpha1.PrerequisiteSpec) []unitSpec {
	t.Helper()
	specs, err := prerequisiteSpecs(t.Context(), prereqInputs{
		units: units, domain: testDomain, ensured: testEnsured(),
	})
	if err != nil {
		t.Fatalf("prerequisiteSpecs: %v", err)
	}
	return specs
}

// TestPrerequisiteSpecs is the resolution table: what each entry produces,
// in the order the list declares it. The list is the author's — dropping a
// unit genuinely drops it, and reordering reorders the install — so the rows
// are lists, not entries.
func TestPrerequisiteSpecs(t *testing.T) {
	t.Parallel()
	platform := []string{"Namespace /gateway-system", "Service gateway-system/gateway"}
	secrets := []string{"Secret gateway-system/cube-idp-ca", "Secret gateway-system/gateway-tls"}
	// The CR pair is namespace-less as the domain emits it; these are the
	// namespaces the edge stamped. The Gateway carries its own.
	traefik := []string{
		"OCIRepository gateway-system/traefik-gateway",
		"HelmRelease gateway-system/traefik-gateway",
		"Gateway gateway-system/gateway",
	}
	cases := []struct {
		name    string
		units   []v1alpha1.PrerequisiteSpec
		names   []string
		flavors []string
		// objs maps a unit name to the descriptors it must carry. A unit
		// absent from the map is not checked object by object (the CRDs
		// payload has its own test).
		objs map[string][]string
	}{
		{
			name:    "compiled defaults",
			units:   defaultPrerequisiteList(),
			names:   []string{"gateway-platform", "gateway-api-crds", "ca-secrets", "traefik-gateway"},
			flavors: []string{"raw", "raw", "inert", "cr"},
			objs: map[string][]string{
				"gateway-platform": platform, "ca-secrets": secrets, "traefik-gateway": traefik,
			},
		},
		{
			name: "a list without ca-secrets installs no Secrets",
			units: []v1alpha1.PrerequisiteSpec{
				{Name: v1alpha1.PrerequisiteGatewayPlatform},
				{Name: v1alpha1.PrerequisiteGatewayAPICRDs},
				{Name: v1alpha1.PrerequisiteTraefikGateway},
			},
			names:   []string{"gateway-platform", "gateway-api-crds", "traefik-gateway"},
			flavors: []string{"raw", "raw", "cr"},
			objs:    map[string][]string{"traefik-gateway": traefik},
		},
		{
			name: "a list without gateway-platform still installs the rest",
			units: []v1alpha1.PrerequisiteSpec{
				{Name: v1alpha1.PrerequisiteGatewayAPICRDs},
				{Name: v1alpha1.PrerequisiteCASecrets},
				{Name: v1alpha1.PrerequisiteTraefikGateway},
			},
			names:   []string{"gateway-api-crds", "ca-secrets", "traefik-gateway"},
			flavors: []string{"raw", "inert", "cr"},
			objs:    map[string][]string{"ca-secrets": secrets},
		},
		{
			name: "list order is install order",
			units: []v1alpha1.PrerequisiteSpec{
				{Name: v1alpha1.PrerequisiteCASecrets},
				{Name: v1alpha1.PrerequisiteGatewayPlatform},
			},
			names:   []string{"ca-secrets", "gateway-platform"},
			flavors: []string{"inert", "raw"},
			objs:    map[string][]string{"ca-secrets": secrets, "gateway-platform": platform},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			specs := resolveSpecs(t, tc.units)
			if len(specs) != len(tc.names) {
				t.Fatalf("resolved %d units, want %d", len(specs), len(tc.names))
			}
			for i, s := range specs {
				if s.name != tc.names[i] {
					t.Errorf("unit %d name = %q, want %q", i, s.name, tc.names[i])
				}
				if got := flavorOf(s); got != tc.flavors[i] {
					t.Errorf("unit %s flavor = %s, want %s", s.name, got, tc.flavors[i])
				}
				want, checked := tc.objs[s.name]
				if got := descriptors(s.objs); checked && !slices.Equal(got, want) {
					t.Errorf("unit %s objects = %v, want %v", s.name, got, want)
				}
			}
			assertSecretsOnlyWithCAUnit(t, tc.units, specs)
		})
	}
}

// assertSecretsOnlyWithCAUnit pins the guard that matters most when a list
// drops ca-secrets: no Secret may appear anywhere, because the zero
// EnsureResult would otherwise be emitted as two empty kubernetes.io/tls
// Secrets the API server rejects.
func assertSecretsOnlyWithCAUnit(t *testing.T, units []v1alpha1.PrerequisiteSpec, specs []unitSpec) {
	t.Helper()
	secrets := 0
	for _, s := range specs {
		for _, o := range s.objs {
			if o.GetKind() == "Secret" {
				secrets++
			}
		}
	}
	want := 0
	if hasPrerequisite(units, v1alpha1.PrerequisiteCASecrets) {
		want = 2
	}
	if secrets != want {
		t.Errorf("resolved %d Secret objects, want %d", secrets, want)
	}
}

// TestPrerequisiteSpecsCRDsUnit checks the one unit the table leaves
// unenumerated: the embedded payload passes through exactly as the domain
// parsed it — nothing added, nothing stamped, order preserved — and it
// carries the Gateway CRD the units after it depend on being Established.
func TestPrerequisiteSpecsCRDsUnit(t *testing.T) {
	t.Parallel()
	want, err := gateway.CRDsPackObjects()
	if err != nil {
		t.Fatalf("gateway.CRDsPackObjects: %v", err)
	}
	specs := resolveSpecs(t, []v1alpha1.PrerequisiteSpec{{Name: v1alpha1.PrerequisiteGatewayAPICRDs}})

	if got := descriptors(specs[0].objs); !slices.Equal(got, descriptors(want)) {
		t.Fatalf("the unit's objects diverge from the domain's payload: %d vs %d objects", len(got), len(want))
	}
	if !slices.Contains(descriptors(specs[0].objs),
		"CustomResourceDefinition /gateways.gateway.networking.k8s.io") {
		t.Error("the payload carries no Gateway CRD; the units after it declare that kind")
	}
}

// TestPrerequisiteSpecsLeavesDomainEmissionUnstamped guards the stamping
// shortcut: the edge stamps the CR pair in place, which is safe only because
// the domain builds fresh objects on every call. If that ever stopped being
// true, the dogfood test's namespace-less equality would break instead of
// this — so it is asserted here, where the stamping happens.
func TestPrerequisiteSpecsLeavesDomainEmissionUnstamped(t *testing.T) {
	t.Parallel()
	resolveSpecs(t, defaultPrerequisiteList())
	for _, o := range gateway.HelmPairObjects() {
		if ns := o.GetNamespace(); ns != "" {
			t.Errorf("%s came back namespaced (%q) after resolution stamped a pair", o.GetKind(), ns)
		}
	}
}

// TestPrerequisiteUnitsConverts covers the step the spec table cannot see:
// bootstrap.Unit has no exported fields, so the conversion is asserted by
// value against the constructors themselves. The CR case can only be
// asserted negatively — two units holding function fields never compare
// equal — but "not the raw unit and not the inert one" is what would break
// if the judge were dropped, which is CUBE-BST-010 at bootstrap's pre-flight.
func TestPrerequisiteUnitsConverts(t *testing.T) {
	t.Parallel()
	name := "unit"
	objs := gateway.PlatformObjects()
	raw := unitSpec{name: name, objs: objs}
	inert := unitSpec{name: name, objs: objs, inert: true}
	cr := unitSpec{name: name, objs: objs, judge: gatewayUnitJudge}

	if !reflect.DeepEqual(raw.unit(), bootstrap.NewRawUnit(name, objs)) {
		t.Error("a plain spec did not convert to a raw unit")
	}
	if !reflect.DeepEqual(inert.unit(), bootstrap.NewInertUnit(name, objs)) {
		t.Error("an inert spec did not convert to an inert unit")
	}
	if reflect.DeepEqual(cr.unit(), bootstrap.NewRawUnit(name, objs)) ||
		reflect.DeepEqual(cr.unit(), bootstrap.NewInertUnit(name, objs)) {
		t.Error("a judged spec converted to a unit that runs no reconciliation wait")
	}

	units, err := prerequisiteUnits(t.Context(), prereqInputs{
		units: defaultPrerequisiteList(), domain: testDomain, ensured: testEnsured(),
	})
	if err != nil {
		t.Fatalf("prerequisiteUnits: %v", err)
	}
	if len(units) != 4 {
		t.Errorf("converted %d units, want one per list entry", len(units))
	}
}
