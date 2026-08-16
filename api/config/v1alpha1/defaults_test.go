package v1alpha1_test

import (
	"testing"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
)

func TestDefaultCluster(t *testing.T) {
	tests := []struct {
		name string
		in   v1alpha1.ConfigSpec
		want v1alpha1.ClusterProvider // "" means Cluster must stay nil
	}{
		{name: "absent cluster stays nil", in: v1alpha1.ConfigSpec{}, want: ""},
		{name: "empty provider defaults to kind",
			in:   v1alpha1.ConfigSpec{Cluster: &v1alpha1.ClusterSpec{}},
			want: v1alpha1.ClusterProviderKind},
		{name: "set provider untouched (idempotent)",
			in:   v1alpha1.ConfigSpec{Cluster: &v1alpha1.ClusterSpec{Provider: v1alpha1.ClusterProviderKind}},
			want: v1alpha1.ClusterProviderKind},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := v1alpha1.Config{Spec: tt.in}
			c.Default()
			if tt.want == "" {
				if c.Spec.Cluster != nil {
					t.Fatalf("Cluster = %+v, want nil", c.Spec.Cluster)
				}
				return
			}
			if got := c.Spec.Cluster.Provider; got != tt.want {
				t.Fatalf("Provider = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDefaultEngine(t *testing.T) {
	tests := []struct {
		name string
		in   v1alpha1.ConfigSpec
		want v1alpha1.EngineProvider // "" means Engine must stay nil
	}{
		{name: "absent engine stays nil", in: v1alpha1.ConfigSpec{}, want: ""},
		{name: "empty provider defaults to flux",
			in:   v1alpha1.ConfigSpec{Engine: &v1alpha1.EngineSpec{}},
			want: v1alpha1.EngineProviderFlux},
		{name: "set provider untouched (idempotent)",
			in:   v1alpha1.ConfigSpec{Engine: &v1alpha1.EngineSpec{Provider: v1alpha1.EngineProviderFlux}},
			want: v1alpha1.EngineProviderFlux},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := v1alpha1.Config{Spec: tt.in}
			c.Default()
			if tt.want == "" {
				if c.Spec.Engine != nil {
					t.Fatalf("Engine = %+v, want nil", c.Spec.Engine)
				}
				return
			}
			if got := c.Spec.Engine.Provider; got != tt.want {
				t.Fatalf("Provider = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDefaultEngineSource(t *testing.T) {
	tests := []struct {
		name                            string
		in                              v1alpha1.EngineSource
		wantKind                        v1alpha1.EngineSourceKind
		wantRef, wantPath, wantInterval string
	}{
		{name: "git defaults", in: v1alpha1.EngineSource{URL: "https://x"},
			wantKind: v1alpha1.EngineSourceGit, wantRef: "main", wantPath: "./", wantInterval: "10m"},
		{name: "oci ref defaults to latest",
			in:       v1alpha1.EngineSource{Kind: v1alpha1.EngineSourceOCI, URL: "oci://x"},
			wantKind: v1alpha1.EngineSourceOCI, wantRef: "latest", wantPath: "./", wantInterval: "10m"},
		{name: "set fields untouched (idempotent)",
			in: v1alpha1.EngineSource{Kind: v1alpha1.EngineSourceGit, URL: "https://x",
				Ref: "release", Path: "clusters/prod", Interval: "1m"},
			wantKind: v1alpha1.EngineSourceGit, wantRef: "release", wantPath: "clusters/prod", wantInterval: "1m"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := tt.in
			c := v1alpha1.Config{Spec: v1alpha1.ConfigSpec{Engine: &v1alpha1.EngineSpec{Source: &src}}}
			c.Default()
			got := c.Spec.Engine.Source
			if got.Kind != tt.wantKind || got.Ref != tt.wantRef || got.Path != tt.wantPath || got.Interval != tt.wantInterval {
				t.Fatalf("defaulted = %+v, want kind=%s ref=%s path=%s interval=%s",
					got, tt.wantKind, tt.wantRef, tt.wantPath, tt.wantInterval)
			}
		})
	}
}
