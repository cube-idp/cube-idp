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
