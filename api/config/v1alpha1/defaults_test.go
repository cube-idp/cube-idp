package v1alpha1_test

import (
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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

func TestDefaultCA(t *testing.T) {
	tests := []struct {
		name string
		in   v1alpha1.ConfigSpec
		want v1alpha1.CAProvider // "" means CA must stay nil
	}{
		{name: "absent ca stays nil", in: v1alpha1.ConfigSpec{}, want: ""},
		{name: "empty provider defaults to cube",
			in:   v1alpha1.ConfigSpec{CA: &v1alpha1.CASpec{}},
			want: v1alpha1.CAProviderCube},
		{name: "set provider untouched (idempotent)",
			in:   v1alpha1.ConfigSpec{CA: &v1alpha1.CASpec{Provider: v1alpha1.CAProviderCube}},
			want: v1alpha1.CAProviderCube},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := v1alpha1.Config{Spec: tt.in}
			c.Default()
			if tt.want == "" {
				if c.Spec.CA != nil {
					t.Fatalf("CA = %+v, want nil", c.Spec.CA)
				}
				return
			}
			if got := c.Spec.CA.Provider; got != tt.want {
				t.Fatalf("Provider = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDefaultGateway(t *testing.T) {
	tests := []struct {
		name       string
		cubeName   string
		in         *v1alpha1.GatewaySpec // nil means Gateway must stay nil
		wantDomain string
	}{
		{name: "absent gateway stays nil", cubeName: "dev", in: nil},
		{name: "empty domain derives from the cube name",
			cubeName: "dev", in: &v1alpha1.GatewaySpec{}, wantDomain: "dev.cube.test"},
		{name: "explicit domain untouched (idempotent)",
			cubeName: "dev", in: &v1alpha1.GatewaySpec{Domain: "lab.example.com"},
			wantDomain: "lab.example.com"},
		{name: "unusable cube name derives nothing",
			cubeName: "Dev", in: &v1alpha1.GatewaySpec{}, wantDomain: ""},
		{name: "empty cube name derives nothing",
			cubeName: "", in: &v1alpha1.GatewaySpec{}, wantDomain: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := v1alpha1.Config{
				ObjectMeta: metav1.ObjectMeta{Name: tt.cubeName},
				Spec:       v1alpha1.ConfigSpec{Gateway: tt.in},
			}
			c.Default()
			if tt.in == nil {
				if c.Spec.Gateway != nil {
					t.Fatalf("Gateway = %+v, want nil", c.Spec.Gateway)
				}
				return
			}
			if got := c.Spec.Gateway.Domain; got != tt.wantDomain {
				t.Fatalf("Domain = %q, want %q", got, tt.wantDomain)
			}
		})
	}
}

// TestDefaultPrerequisites pins the replace-whole-list rule: absent or empty
// materializes the compiled defaults, and any present list is kept verbatim —
// entries and order both, since order is the whole contract here.
func TestDefaultPrerequisites(t *testing.T) {
	tests := []struct {
		name string
		in   []v1alpha1.PrerequisiteSpec
		want []v1alpha1.PrerequisiteSpec
	}{
		{name: "absent list materializes the compiled defaults", in: nil,
			want: []v1alpha1.PrerequisiteSpec{
				{Name: v1alpha1.PrerequisiteGatewayPlatform},
				{Name: v1alpha1.PrerequisiteGatewayAPICRDs},
				{Name: v1alpha1.PrerequisiteCASecrets},
				{Name: v1alpha1.PrerequisiteTraefikGateway},
			}},
		{name: "explicitly empty list materializes the same",
			in: []v1alpha1.PrerequisiteSpec{},
			want: []v1alpha1.PrerequisiteSpec{
				{Name: v1alpha1.PrerequisiteGatewayPlatform},
				{Name: v1alpha1.PrerequisiteGatewayAPICRDs},
				{Name: v1alpha1.PrerequisiteCASecrets},
				{Name: v1alpha1.PrerequisiteTraefikGateway},
			}},
		{name: "a present list replaces the defaults entirely",
			in:   []v1alpha1.PrerequisiteSpec{{Name: "only", Ref: "./p"}},
			want: []v1alpha1.PrerequisiteSpec{{Name: "only", Ref: "./p"}}},
		{name: "a single built-in replaces all four",
			in:   []v1alpha1.PrerequisiteSpec{{Name: v1alpha1.PrerequisiteCASecrets}},
			want: []v1alpha1.PrerequisiteSpec{{Name: v1alpha1.PrerequisiteCASecrets}}},
		{name: "order is preserved verbatim",
			in: []v1alpha1.PrerequisiteSpec{
				{Name: v1alpha1.PrerequisiteTraefikGateway},
				{Name: v1alpha1.PrerequisiteGatewayPlatform},
			},
			want: []v1alpha1.PrerequisiteSpec{
				{Name: v1alpha1.PrerequisiteTraefikGateway},
				{Name: v1alpha1.PrerequisiteGatewayPlatform},
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := v1alpha1.Config{Spec: v1alpha1.ConfigSpec{Prerequisites: tt.in}}
			c.Default()
			if !reflect.DeepEqual(c.Spec.Prerequisites, tt.want) {
				t.Fatalf("Prerequisites = %+v, want %+v", c.Spec.Prerequisites, tt.want)
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
