package pack_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/cube-idp/cube-idp/internal/cubeerr"
	"github.com/cube-idp/cube-idp/internal/pack"
)

func TestResolveOrderErrors(t *testing.T) {
	tests := []struct {
		name      string
		instances []pack.Instance
		want      cubeerr.Code
	}{
		{
			name:      "a repeated pack name with no ids",
			instances: []pack.Instance{inst("monitoring", ""), inst("monitoring", "")},
			want:      pack.CodeInstanceIDRequired,
		},
		{
			name:      "a repeated pack name where only one has an id",
			instances: []pack.Instance{inst("monitoring", "monitoring-a"), inst("monitoring", "")},
			want:      pack.CodeInstanceIDRequired,
		},
		{
			name:      "two explicit ids collide",
			instances: []pack.Instance{inst("a", "same"), inst("b", "same")},
			want:      pack.CodeDuplicateInstanceID,
		},
		{
			// An explicit id colliding with another pack's defaulted name.
			name:      "an explicit id collides with a defaulted one",
			instances: []pack.Instance{inst("traefik", ""), inst("other", "traefik")},
			want:      pack.CodeDuplicateInstanceID,
		},
		{
			name:      "dependsOn names nothing in the setup",
			instances: []pack.Instance{inst("app", "", "nowhere")},
			want:      pack.CodeUnknownDependency,
		},
		{
			name: "dependsOn names a pack two instances install",
			instances: []pack.Instance{
				inst("app", "", "monitoring"),
				inst("monitoring", "monitoring-a"),
				inst("monitoring", "monitoring-b"),
			},
			want: pack.CodeAmbiguousDependency,
		},
		{
			name:      "an instance depends on itself by name",
			instances: []pack.Instance{inst("app", "", "app")},
			want:      pack.CodeDependencyCycle,
		},
		{
			name:      "an instance depends on itself by id",
			instances: []pack.Instance{inst("app", "app-prod", "app-prod")},
			want:      pack.CodeDependencyCycle,
		},
		{
			name:      "two instances depend on each other",
			instances: []pack.Instance{inst("a", "", "b"), inst("b", "", "a")},
			want:      pack.CodeDependencyCycle,
		},
		{
			name: "a longer cycle",
			instances: []pack.Instance{
				inst("a", "", "c"), inst("b", "", "a"), inst("c", "", "b"),
			},
			want: pack.CodeDependencyCycle,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := pack.ResolveOrder(tt.instances)
			wantCode(t, err, tt.want)
		})
	}
}

// An ambiguous dependency is only useful if it names the candidates the
// operator has to choose between.
func TestAmbiguousDependencyNamesCandidates(t *testing.T) {
	_, err := pack.ResolveOrder([]pack.Instance{
		inst("app", "", "monitoring"),
		inst("monitoring", "monitoring-a"),
		inst("monitoring", "monitoring-b"),
	})
	wantCode(t, err, pack.CodeAmbiguousDependency)

	var coded *cubeerr.Coded
	if !errors.As(err, &coded) {
		t.Fatal("error is not coded")
	}
	for _, want := range []string{"monitoring-a", "monitoring-b"} {
		if !strings.Contains(coded.Summary, want) {
			t.Errorf("summary %q should name the candidate %s", coded.Summary, want)
		}
	}
}

// A cycle error must name its members, or there is nothing to go and fix.
func TestCycleNamesItsMembers(t *testing.T) {
	_, err := pack.ResolveOrder([]pack.Instance{inst("a", "", "b"), inst("b", "", "a")})
	wantCode(t, err, pack.CodeDependencyCycle)

	var coded *cubeerr.Coded
	if !errors.As(err, &coded) {
		t.Fatal("error is not coded")
	}
	for _, want := range []string{"a", "b"} {
		if !strings.Contains(coded.Summary, want) {
			t.Errorf("summary %q should name the cycle member %s", coded.Summary, want)
		}
	}
}

// The graph records exactly the declared edges — nothing is inferred.
func TestNoImplicitEdges(t *testing.T) {
	graph, err := pack.ResolveOrder([]pack.Instance{inst("a", ""), inst("b", ""), inst("c", "", "a")})
	if err != nil {
		t.Fatalf("ResolveOrder() = error %v, want a graph", err)
	}

	if got := graph.Dependencies["c"]; len(got) != 1 || got[0] != "a" {
		t.Errorf("Dependencies[c] = %v, want [a]", got)
	}
	for _, id := range []pack.InstanceID{"a", "b"} {
		if got := graph.Dependencies[id]; len(got) != 0 {
			t.Errorf("Dependencies[%s] = %v, want none — no edge was declared", id, got)
		}
	}
}

// The same setup must yield the same order every run, so the result cannot
// depend on Go's map iteration.
func TestResolveOrderIsDeterministic(t *testing.T) {
	instances := []pack.Instance{
		inst("gateway", "", "certs"),
		inst("monitoring", "", "certs"),
		inst("certs", ""),
		inst("apps", "", "gateway", "monitoring"),
	}

	first := order(t, instances...)
	for i := range 20 {
		got := order(t, instances...)
		for j := range got {
			if got[j] != first[j] {
				t.Fatalf("run %d order = %v, want %v", i, got, first)
			}
		}
	}
	if first[0] != "certs" || first[len(first)-1] != "apps" {
		t.Errorf("order = %v, want certs first and apps last", first)
	}
}
