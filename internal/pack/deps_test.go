package pack_test

import (
	"testing"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
	"github.com/cube-idp/cube-idp/internal/pack"
)

// inst builds one resolved instance: the pack's own name, an optional explicit
// id, and its dependsOn targets. Nothing is fetched — the graph is pure, so
// its tests fabricate what resolution would have produced.
func inst(name, id string, dependsOn ...string) pack.Instance {
	return pack.Instance{
		Name: name,
		Spec: v1alpha1.PackSpec{ID: id, PackRef: "./" + name, DependsOn: dependsOn},
	}
}

// order resolves the graph and returns the install order as plain strings.
func order(t *testing.T, instances ...pack.Instance) []string {
	t.Helper()
	graph, err := pack.ResolveOrder(instances)
	if err != nil {
		t.Fatalf("ResolveOrder() = error %v, want a graph", err)
	}
	out := make([]string, len(graph.Order))
	for i, id := range graph.Order {
		out[i] = string(id)
	}
	return out
}

func TestResolveOrder(t *testing.T) {
	tests := []struct {
		name      string
		instances []pack.Instance
		want      []string
	}{
		{
			name:      "no packs",
			instances: nil,
			want:      []string{},
		},
		{
			// With no edges the document's own order is the install order.
			name:      "no dependencies preserves declaration order",
			instances: []pack.Instance{inst("c", ""), inst("a", ""), inst("b", "")},
			want:      []string{"c", "a", "b"},
		},
		{
			name:      "an effective id defaults to the pack name",
			instances: []pack.Instance{inst("traefik", "")},
			want:      []string{"traefik"},
		},
		{
			name:      "an explicit id wins over the pack name",
			instances: []pack.Instance{inst("traefik", "gateway-prod")},
			want:      []string{"gateway-prod"},
		},
		{
			name:      "a dependency is installed first",
			instances: []pack.Instance{inst("app", "", "database"), inst("database", "")},
			want:      []string{"database", "app"},
		},
		{
			name: "a chain is fully ordered",
			instances: []pack.Instance{
				inst("c", "", "b"), inst("b", "", "a"), inst("a", ""),
			},
			want: []string{"a", "b", "c"},
		},
		{
			name: "several dependents of one dependency keep declared order",
			instances: []pack.Instance{
				inst("b", "", "base"), inst("c", "", "base"), inst("base", ""),
			},
			want: []string{"base", "b", "c"},
		},
		{
			name:      "dependsOn resolves an explicit id",
			instances: []pack.Instance{inst("app", "", "db-prod"), inst("postgres", "db-prod")},
			want:      []string{"db-prod", "app"},
		},
		{
			// Two copies of one pack, told apart by id.
			name: "two instances of one pack",
			instances: []pack.Instance{
				inst("monitoring", "monitoring-a"),
				inst("monitoring", "monitoring-b", "monitoring-a"),
			},
			want: []string{"monitoring-a", "monitoring-b"},
		},
		{
			name:      "the same target named twice is one edge",
			instances: []pack.Instance{inst("app", "", "db", "db"), inst("db", "")},
			want:      []string{"db", "app"},
		},
		{
			// An id is unambiguous by construction, so a pack name that
			// happens to equal one never shadows it.
			name: "an id match wins over a name match",
			instances: []pack.Instance{
				inst("app", "", "shared"),
				inst("other", "shared"),
				inst("shared", "actually-shared"),
			},
			want: []string{"shared", "app", "actually-shared"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := order(t, tt.instances...)
			if len(got) != len(tt.want) {
				t.Fatalf("ResolveOrder() order = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("ResolveOrder() order = %v, want %v", got, tt.want)
					return
				}
			}
			// The sequence rows above would catch a dropped instance only
			// where one was enumerated; this is the property itself.
			wantEveryInstanceOnce(t, tt.instances, got)
		})
	}
}

// wantEveryInstanceOnce asserts the order is a permutation of the input: an
// install order that silently omits a pack would install less than the setup
// asked for.
func wantEveryInstanceOnce(t *testing.T, instances []pack.Instance, got []string) {
	t.Helper()

	seen := map[string]int{}
	for _, id := range got {
		seen[id]++
	}
	if len(seen) != len(got) {
		t.Errorf("ResolveOrder() order = %v, want each instance exactly once", got)
	}
	if len(got) != len(instances) {
		t.Fatalf("ResolveOrder() ordered %d instances, want all %d", len(got), len(instances))
	}
}
