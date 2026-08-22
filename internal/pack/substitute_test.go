package pack_test

import (
	"errors"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/cube-idp/cube-idp/internal/cubeerr"
	"github.com/cube-idp/cube-idp/internal/pack"
)

// substPack is a kustomize pack whose single ConfigMap carries the given data
// body, so substitution can be observed end to end.
func substPack(cue, data string) fstest.MapFS {
	return kPack(cue, "resources:\n- cm.yaml\n", map[string]string{
		"cm.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: c\ndata:\n" + data,
	})
}

// renderData renders the pack and returns its ConfigMap's data map.
func renderData(t *testing.T, fsys fstest.MapFS, values map[string]any) map[string]any {
	t.Helper()
	p, err := pack.Load(t.Context(), fsys, "./p")
	if err != nil {
		t.Fatalf("Load() = error %v, want a pack", err)
	}
	plan, err := p.Render(t.Context(), pack.RenderOptions{Values: values})
	if err != nil {
		t.Fatalf("Render() = error %v, want a plan", err)
	}
	if len(plan.Objects) != 1 {
		t.Fatalf("Render() produced %d objects, want 1", len(plan.Objects))
	}
	data, _ := plan.Objects[0].Object["data"].(map[string]any)
	return data
}

const openValuesCUE = "name: \"web\"\nversion: \"1\"\ntype: \"kustomize\"\n"

func TestSubstitutionThroughRender(t *testing.T) {
	tests := []struct {
		name   string
		data   string
		values map[string]any
		want   map[string]string
	}{
		{
			name:   "value is substituted into the built output",
			data:   "  image: ${IMAGE}\n",
			values: map[string]any{"IMAGE": "nginx:1.27"},
			want:   map[string]string{"image": "nginx:1.27"},
		},
		{
			name:   "substitution reaches every scalar",
			data:   "  a: ${V}\n  b: prefix-${V}\n",
			values: map[string]any{"V": "x"},
			want:   map[string]string{"a": "x", "b": "prefix-x"},
		},
		{
			name:   "escaped reference stays literal",
			data:   "  literal: $${IMAGE}\n",
			values: map[string]any{"IMAGE": "nginx:1.27"},
			want:   map[string]string{"literal": "${IMAGE}"},
		},
		{
			name:   "a bare dollar is left alone",
			data:   "  price: costs $5\n",
			values: map[string]any{},
			want:   map[string]string{"price": "costs $5"},
		},
		{
			// A value nothing references is not an error: a value may be
			// consumed by one overlay and not another.
			name:   "unused values are not an error",
			data:   "  fixed: constant\n",
			values: map[string]any{"UNUSED": "x"},
			want:   map[string]string{"fixed": "constant"},
		},
		{
			name:   "several references in one document",
			data:   "  a: ${A}\n  b: ${B}\n",
			values: map[string]any{"A": "one", "B": "two"},
			want:   map[string]string{"a": "one", "b": "two"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderData(t, substPack(openValuesCUE, tt.data), tt.values)
			for key, want := range tt.want {
				if got[key] != want {
					t.Errorf("rendered data[%q] = %#v, want %q (full: %v)", key, got[key], want, got)
				}
			}
		})
	}
}

// Expanding a whole scalar does not retype it. The fixture leaves ${COUNT}
// unquoted, so only the substitution decides the result's type — an author
// needing a real integer keeps it out of substitution.
func TestSubstitutionResultIsAlwaysAString(t *testing.T) {
	got := renderData(t, substPack(openValuesCUE, "  replicas: ${COUNT}\n"), map[string]any{"COUNT": "3"})

	value, ok := got["replicas"].(string)
	if !ok {
		t.Fatalf("rendered data[replicas] = %#v (%T), want a string", got["replicas"], got["replicas"])
	}
	if value != "3" {
		t.Errorf("rendered data[replicas] = %q, want \"3\"", value)
	}
}

// Substitution must never touch keys — only scalar values — so the output
// cannot become invalid YAML.
func TestSubstitutionLeavesKeysAlone(t *testing.T) {
	fsys := kPack(openValuesCUE, "resources:\n- cm.yaml\n", map[string]string{
		"cm.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: c\ndata:\n  ${KEY}: value\n",
	})
	got := renderData(t, fsys, map[string]any{"KEY": "substituted"})

	if _, ok := got["${KEY}"]; !ok {
		t.Errorf("rendered data = %v, want the literal key ${KEY} preserved", got)
	}
	if _, ok := got["substituted"]; ok {
		t.Errorf("rendered data = %v, want no substitution in keys", got)
	}
}

// A missing variable is an error naming every unresolved name, because
// substituting empty is how a deployment gets an empty image tag.
func TestSubstitutionMissingVariable(t *testing.T) {
	p, err := pack.Load(t.Context(), substPack(openValuesCUE, "  a: ${ALPHA}\n  b: ${BETA}\n"), "./p")
	if err != nil {
		t.Fatalf("Load() = error %v, want a pack", err)
	}
	_, err = p.Render(t.Context(), pack.RenderOptions{Values: map[string]any{}})
	wantCode(t, err, pack.CodeSubstitutionMissing)

	var coded *cubeerr.Coded
	if !errors.As(err, &coded) {
		t.Fatal("error is not coded")
	}
	for _, name := range []string{"ALPHA", "BETA"} {
		if !strings.Contains(coded.Summary, name) {
			t.Errorf("summary %q should name the unresolved variable %s", coded.Summary, name)
		}
	}
}

// Kustomize substitution is textual, so a value that is not a string has no
// meaningful projection onto it.
func TestValuesMustBeFlatStrings(t *testing.T) {
	tests := []struct {
		name   string
		values map[string]any
		want   cubeerr.Code
	}{
		{"nested map", map[string]any{"a": map[string]any{"b": "c"}}, pack.CodeValuesNotFlat},
		{"list", map[string]any{"a": []any{"b"}}, pack.CodeValuesNotFlat},
		{"null", map[string]any{"a": nil}, pack.CodeValuesNotFlat},
		{"integer", map[string]any{"a": 1}, pack.CodeValuesNotFlat},
		{"float", map[string]any{"a": 1.5}, pack.CodeValuesNotFlat},
		{"boolean", map[string]any{"a": true}, pack.CodeValuesNotFlat},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := pack.Load(t.Context(), substPack(openValuesCUE, "  fixed: constant\n"), "./p")
			if err != nil {
				t.Fatalf("Load() = error %v, want a pack", err)
			}
			_, err = p.Render(t.Context(), pack.RenderOptions{Values: tt.values})
			wantCode(t, err, tt.want)
		})
	}
}

// Strings pass, so the flat-string rule rejects only what it must.
func TestFlatStringValuesAccepted(t *testing.T) {
	got := renderData(t, substPack(openValuesCUE, "  a: ${A}\n"), map[string]any{"A": "ok", "B": "also ok"})
	if got["a"] != "ok" {
		t.Errorf("rendered data[a] = %#v, want \"ok\"", got["a"])
	}
}

// #Values defaults feed substitution, which is the schema-with-lockdown that
// plain post-build substitution does not offer.
func TestSubstitutionUsesValuesDefaults(t *testing.T) {
	cue := "name: \"web\"\nversion: \"1\"\ntype: \"kustomize\"\n#Values: {\n\tIMAGE: string | *\"nginx:1.27\"\n}\n"
	got := renderData(t, substPack(cue, "  image: ${IMAGE}\n"), nil)

	if got["image"] != "nginx:1.27" {
		t.Errorf("rendered data[image] = %#v, want the #Values default", got["image"])
	}
}

// A #Values that types a value as a non-string is caught by the flat rule,
// so the lockdown and the substitution contract cannot disagree.
func TestValuesDefaultsMustAlsoBeStrings(t *testing.T) {
	cue := "name: \"web\"\nversion: \"1\"\ntype: \"kustomize\"\n#Values: {\n\treplicas: int | *3\n}\n"
	p, err := pack.Load(t.Context(), substPack(cue, "  fixed: constant\n"), "./p")
	if err != nil {
		t.Fatalf("Load() = error %v, want a pack", err)
	}
	_, err = p.Render(t.Context(), pack.RenderOptions{})
	wantCode(t, err, pack.CodeValuesNotFlat)
}
