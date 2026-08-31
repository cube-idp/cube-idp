package cli

import (
	"strings"
	"testing"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
)

// TestPrerequisiteSpecsBuiltInWithRef: a built-in unit's content and behavior
// are the cube's, so there is nothing to point a ref at. Document validation
// forbids it, which is why this cannot arrive through a loaded config — the
// resolver rejects it anyway, so its invariant does not depend on a check
// living somewhere else.
func TestPrerequisiteSpecsBuiltInWithRef(t *testing.T) {
	t.Parallel()
	for _, name := range []string{v1alpha1.PrerequisiteGatewayPlatform, v1alpha1.PrerequisiteCASecrets} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := prerequisiteSpecs(t.Context(), prereqInputs{
				units:  []v1alpha1.PrerequisiteSpec{{Name: name, Ref: "./somewhere-else"}},
				domain: testDomain,
			})
			if err == nil {
				t.Fatal("a built-in unit accepted a ref")
			}
			if !strings.Contains(err.Error(), name) {
				t.Errorf("error %q does not name the unit", err)
			}
		})
	}
}

// TestHasGatewayFabric pins what the CoreDNS splice is gated on: both halves,
// never one.
func TestHasGatewayFabric(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		units []string
		want  bool
	}{
		{name: "the compiled defaults carry both", want: true, units: []string{
			v1alpha1.PrerequisiteGatewayPlatform, v1alpha1.PrerequisiteTraefikGateway}},
		{name: "the platform unit alone", units: []string{v1alpha1.PrerequisiteGatewayPlatform}},
		{name: "the gateway unit alone", units: []string{v1alpha1.PrerequisiteTraefikGateway}},
		{name: "neither", units: []string{v1alpha1.PrerequisiteCASecrets}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			units := make([]v1alpha1.PrerequisiteSpec, 0, len(tc.units))
			for _, name := range tc.units {
				units = append(units, v1alpha1.PrerequisiteSpec{Name: name})
			}
			if got := hasGatewayFabric(units); got != tc.want {
				t.Errorf("hasGatewayFabric = %v, want %v", got, tc.want)
			}
		})
	}
}
