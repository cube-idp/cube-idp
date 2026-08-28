package v1alpha1_test

import (
	"reflect"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
)

func validConfig() *v1alpha1.Config {
	return &v1alpha1.Config{
		TypeMeta:   metav1.TypeMeta{APIVersion: "cube-idp.dev/v1alpha1", Kind: "Config"},
		ObjectMeta: metav1.ObjectMeta{Name: "dev"},
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*v1alpha1.Config)
		wantField string // "" = expect valid
	}{
		{"valid minimal", func(c *v1alpha1.Config) {}, ""},
		{"missing name", func(c *v1alpha1.Config) { c.Name = "" }, "metadata.name"},
		{"uppercase name", func(c *v1alpha1.Config) { c.Name = "Dev" }, "metadata.name"},
		{"leading dash", func(c *v1alpha1.Config) { c.Name = "-dev" }, "metadata.name"},
		{"too long", func(c *v1alpha1.Config) { c.Name = strings.Repeat("a", 32) }, "metadata.name"},
		{"valid cluster with kind provider", func(c *v1alpha1.Config) {
			c.Spec.Cluster = &v1alpha1.ClusterSpec{Provider: v1alpha1.ClusterProviderKind}
		}, ""},
		{"empty provider is defaulted before validate", func(c *v1alpha1.Config) {
			c.Spec.Cluster = &v1alpha1.ClusterSpec{}
		}, ""},
		{"unknown cluster provider", func(c *v1alpha1.Config) {
			c.Spec.Cluster = &v1alpha1.ClusterSpec{Provider: "k3d"}
		}, "spec.cluster.provider"},
		{"valid engine with flux provider", func(c *v1alpha1.Config) {
			c.Spec.Engine = &v1alpha1.EngineSpec{Provider: v1alpha1.EngineProviderFlux}
		}, ""},
		{"empty engine provider is defaulted before validate", func(c *v1alpha1.Config) {
			c.Spec.Engine = &v1alpha1.EngineSpec{}
		}, ""},
		{"unknown engine provider", func(c *v1alpha1.Config) {
			c.Spec.Engine = &v1alpha1.EngineSpec{Provider: "argo"}
		}, "spec.engine.provider"},
		{"valid git source", func(c *v1alpha1.Config) {
			c.Spec.Engine = &v1alpha1.EngineSpec{Source: &v1alpha1.EngineSource{
				Kind: v1alpha1.EngineSourceGit, URL: "https://github.com/org/fleet"}}
		}, ""},
		{"valid oci source", func(c *v1alpha1.Config) {
			c.Spec.Engine = &v1alpha1.EngineSpec{Source: &v1alpha1.EngineSource{
				Kind: v1alpha1.EngineSourceOCI, URL: "oci://ghcr.io/org/fleet"}}
		}, ""},
		{"unknown source kind", func(c *v1alpha1.Config) {
			c.Spec.Engine = &v1alpha1.EngineSpec{Source: &v1alpha1.EngineSource{
				Kind: "svn", URL: "https://x"}}
		}, "spec.engine.source.kind"},
		{"source missing url", func(c *v1alpha1.Config) {
			c.Spec.Engine = &v1alpha1.EngineSpec{Source: &v1alpha1.EngineSource{Kind: v1alpha1.EngineSourceGit}}
		}, "spec.engine.source.url"},
		{"oci kind with non-oci url", func(c *v1alpha1.Config) {
			c.Spec.Engine = &v1alpha1.EngineSpec{Source: &v1alpha1.EngineSource{
				Kind: v1alpha1.EngineSourceOCI, URL: "https://github.com/org/fleet"}}
		}, "spec.engine.source.url"},
		{"git kind with oci url", func(c *v1alpha1.Config) {
			c.Spec.Engine = &v1alpha1.EngineSpec{Source: &v1alpha1.EngineSource{
				Kind: v1alpha1.EngineSourceGit, URL: "oci://ghcr.io/org/fleet"}}
		}, "spec.engine.source.url"},
		{"source bad interval", func(c *v1alpha1.Config) {
			c.Spec.Engine = &v1alpha1.EngineSpec{Source: &v1alpha1.EngineSource{
				Kind: v1alpha1.EngineSourceGit, URL: "https://x", Interval: "soon"}}
		}, "spec.engine.source.interval"},
		{"valid gateway with explicit domain", func(c *v1alpha1.Config) {
			c.Spec.Gateway = &v1alpha1.GatewaySpec{Domain: "lab.example.com"}
		}, ""},
		{"empty gateway domain is derived before validate", func(c *v1alpha1.Config) {
			c.Spec.Gateway = &v1alpha1.GatewaySpec{}
		}, ""},
		{"gateway domain not a DNS name", func(c *v1alpha1.Config) {
			c.Spec.Gateway = &v1alpha1.GatewaySpec{Domain: "Not A Domain"}
		}, "spec.gateway.domain"},
		{"gateway domain with an invalid label", func(c *v1alpha1.Config) {
			c.Spec.Gateway = &v1alpha1.GatewaySpec{Domain: "a..b"}
		}, "spec.gateway.domain"},
		{"valid ca with cube provider", func(c *v1alpha1.Config) {
			c.Spec.CA = &v1alpha1.CASpec{Provider: v1alpha1.CAProviderCube}
		}, ""},
		{"empty ca provider is defaulted before validate", func(c *v1alpha1.Config) {
			c.Spec.CA = &v1alpha1.CASpec{}
		}, ""},
		{"unknown ca provider", func(c *v1alpha1.Config) {
			c.Spec.CA = &v1alpha1.CASpec{Provider: "cert-manager"}
		}, "spec.ca.provider"},
		{"materialized default prerequisites validate", func(c *v1alpha1.Config) {
			c.Spec.Prerequisites = nil
		}, ""},
		{"well-known pack name with empty ref", func(c *v1alpha1.Config) {
			c.Spec.Prerequisites = []v1alpha1.PrerequisiteSpec{{Name: v1alpha1.PrerequisiteGatewayAPICRDs}}
		}, ""},
		{"well-known pack name with a ref overrides its content", func(c *v1alpha1.Config) {
			c.Spec.Prerequisites = []v1alpha1.PrerequisiteSpec{
				{Name: v1alpha1.PrerequisiteTraefikGateway, Ref: "./packs/mine"}}
		}, ""},
		{"built-in with empty ref", func(c *v1alpha1.Config) {
			c.Spec.Prerequisites = []v1alpha1.PrerequisiteSpec{{Name: v1alpha1.PrerequisiteGatewayPlatform}}
		}, ""},
		{"unknown name with a ref", func(c *v1alpha1.Config) {
			c.Spec.Prerequisites = []v1alpha1.PrerequisiteSpec{{Name: "my-unit", Ref: "./packs/x"}}
		}, ""},
		{"a list omitting the built-ins", func(c *v1alpha1.Config) {
			c.Spec.Prerequisites = []v1alpha1.PrerequisiteSpec{{Name: v1alpha1.PrerequisiteGatewayAPICRDs}}
		}, ""},
		{"a reordered list", func(c *v1alpha1.Config) {
			c.Spec.Prerequisites = []v1alpha1.PrerequisiteSpec{
				{Name: v1alpha1.PrerequisiteTraefikGateway},
				{Name: v1alpha1.PrerequisiteGatewayPlatform}}
		}, ""},
		{"prerequisite missing a name", func(c *v1alpha1.Config) {
			c.Spec.Prerequisites = []v1alpha1.PrerequisiteSpec{{Ref: "./p"}}
		}, "spec.prerequisites[0].name"},
		{"prerequisite name is whitespace", func(c *v1alpha1.Config) {
			c.Spec.Prerequisites = []v1alpha1.PrerequisiteSpec{{Name: "  ", Ref: "./p"}}
		}, "spec.prerequisites[0].name"},
		{"duplicate prerequisite names", func(c *v1alpha1.Config) {
			c.Spec.Prerequisites = []v1alpha1.PrerequisiteSpec{
				{Name: v1alpha1.PrerequisiteGatewayPlatform},
				{Name: v1alpha1.PrerequisiteGatewayPlatform}}
		}, "spec.prerequisites[1].name"},
		{"ref on the gateway-platform built-in", func(c *v1alpha1.Config) {
			c.Spec.Prerequisites = []v1alpha1.PrerequisiteSpec{
				{Name: v1alpha1.PrerequisiteGatewayPlatform, Ref: "./p"}}
		}, "spec.prerequisites[0].ref"},
		{"ref on the ca-secrets built-in", func(c *v1alpha1.Config) {
			c.Spec.Prerequisites = []v1alpha1.PrerequisiteSpec{
				{Name: v1alpha1.PrerequisiteCASecrets, Ref: "./p"}}
		}, "spec.prerequisites[0].ref"},
		{"unknown name without a ref", func(c *v1alpha1.Config) {
			c.Spec.Prerequisites = []v1alpha1.PrerequisiteSpec{{Name: "my-unit"}}
		}, "spec.prerequisites[0].ref"},
		{"ref with whitespace", func(c *v1alpha1.Config) {
			c.Spec.Prerequisites = []v1alpha1.PrerequisiteSpec{{Name: "my-unit", Ref: "./a b"}}
		}, "spec.prerequisites[0].ref"},
		{"ref with a control character", func(c *v1alpha1.Config) {
			c.Spec.Prerequisites = []v1alpha1.PrerequisiteSpec{{Name: "my-unit", Ref: "./a\tb"}}
		}, "spec.prerequisites[0].ref"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := validConfig()
			tt.mutate(c)
			c.Default()
			errs := c.Validate()

			if tt.wantField == "" {
				if len(errs) != 0 {
					t.Fatalf("expected valid, got: %v", errs.ToAggregate())
				}
				return
			}
			if len(errs) == 0 {
				t.Fatal("expected validation errors, got none")
			}
			if !strings.Contains(errs.ToAggregate().Error(), tt.wantField) {
				t.Errorf("errors %v do not mention field %s", errs.ToAggregate(), tt.wantField)
			}
		})
	}
}

// TestPrerequisiteBuiltInRefIsForbidden pins the error kind, not just the
// field: the contract's word for a ref on a built-in unit is "forbidden", and
// field.Forbidden is what renders it that way.
func TestPrerequisiteBuiltInRefIsForbidden(t *testing.T) {
	c := validConfig()
	c.Spec.Prerequisites = []v1alpha1.PrerequisiteSpec{
		{Name: v1alpha1.PrerequisiteGatewayPlatform, Ref: "./p"}}
	c.Default()

	errs := c.Validate()
	if len(errs) != 1 {
		t.Fatalf("errs = %v, want exactly one", errs.ToAggregate())
	}
	if errs[0].Type != field.ErrorTypeForbidden {
		t.Errorf("error type = %q, want %q", errs[0].Type, field.ErrorTypeForbidden)
	}
}

// TestUnusableCubeNameReportsOnlyTheIdentity pins the point of the defaulting
// guard: an unusable metadata.name leaves spec.gateway.domain underived, so
// the identity is reported once rather than joined by a second error about a
// domain the user never wrote.
func TestUnusableCubeNameReportsOnlyTheIdentity(t *testing.T) {
	c := validConfig()
	c.Name = "Dev"
	c.Spec.Gateway = &v1alpha1.GatewaySpec{}
	c.Default()

	errs := c.Validate()
	if len(errs) != 1 {
		t.Fatalf("errs = %v, want exactly one (metadata.name)", errs.ToAggregate())
	}
	if got := errs[0].Field; got != "metadata.name" {
		t.Errorf("field = %q, want metadata.name", got)
	}
}

// TestDefaultIsIdempotent compares the whole document, not just the cube
// identity: spec.prerequisites is materialized rather than left nil, so a
// second Default() must neither clobber a user's list nor double the
// compiled one.
func TestDefaultIsIdempotent(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*v1alpha1.Config)
	}{
		{"from a user-supplied prerequisite list", func(c *v1alpha1.Config) {
			c.Spec.Prerequisites = []v1alpha1.PrerequisiteSpec{{Name: "only", Ref: "./p"}}
		}},
		{"from an absent prerequisite list", func(c *v1alpha1.Config) {}},
		{"from a present gateway and ca", func(c *v1alpha1.Config) {
			c.Spec.Gateway = &v1alpha1.GatewaySpec{}
			c.Spec.CA = &v1alpha1.CASpec{}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := validConfig()
			tt.mutate(c)
			c.Default()
			before := c.DeepCopy()
			c.Default()
			if !reflect.DeepEqual(c, before) {
				t.Errorf("Default() must be idempotent:\nsecond call = %+v\nfirst call  = %+v", c.Spec, before.Spec)
			}
		})
	}
}
