package v1alpha1_test

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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

func TestDefaultIsIdempotent(t *testing.T) {
	c := validConfig()
	c.Default()
	before := c.DeepCopy()
	c.Default()
	if c.Name != before.Name {
		t.Error("Default() must be idempotent")
	}
}
