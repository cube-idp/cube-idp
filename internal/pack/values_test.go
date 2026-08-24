package pack_test

import (
	"testing"
	"testing/fstest"

	"github.com/cube-idp/cube-idp/internal/cubeerr"
	"github.com/cube-idp/cube-idp/internal/pack"
)

// helmPack builds a helm pack around a pack.cue body. A helm pack is thin, so
// the metadata file is the whole payload: it isolates the values layer from
// any payload-reading render backend, which is what makes it the type these
// values tests use.
func helmPack(cue string) fstest.MapFS {
	return fstest.MapFS{"pack.cue": &fstest.MapFile{Data: []byte(cue)}}
}

// chartCUE is the chart block a thin helm pack must carry.
const chartCUE = `chart: {
	kind:    "repo"
	url:     "https://charts.example.com"
	name:    "v"
	version: "1.2.3"
}
`

const valuesCUE = `name:    "v"
version: "1"
type:    "helm"
` + chartCUE + `#Values: {
	replicas: int | *1
	image!:   string
	tls?:     bool
}
`

// Values are validated before the render dispatch, so a #Values violation is
// reported as a values fault rather than as whatever the render backend would
// have made of it. An empty want means the values are accepted and the pack
// renders.
func TestValuesValidatedBeforeRenderDispatch(t *testing.T) {
	tests := []struct {
		name   string
		cue    string
		values map[string]any
		want   cubeerr.Code
	}{
		{
			name:   "undeclared field is rejected by the closed definition",
			cue:    valuesCUE,
			values: map[string]any{"image": "nginx", "nope": true},
			want:   pack.CodeValuesRejected,
		},
		{
			name:   "wrong type is rejected",
			cue:    valuesCUE,
			values: map[string]any{"image": "nginx", "replicas": "three"},
			want:   pack.CodeValuesRejected,
		},
		{
			name:   "missing required value is rejected",
			cue:    valuesCUE,
			values: map[string]any{"replicas": 3},
			want:   pack.CodeValuesRejected,
		},
		{
			name:   "values satisfying #Values reach the render dispatch",
			cue:    valuesCUE,
			values: map[string]any{"image": "nginx"},
		},
		{
			name:   "a pack without #Values passes values through",
			cue:    "name: \"v\"\nversion: \"1\"\ntype: \"helm\"\n" + chartCUE,
			values: map[string]any{"anything": "goes"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := pack.Load(t.Context(), helmPack(tt.cue), "./p")
			if err != nil {
				t.Fatalf("Load() = error %v, want a pack", err)
			}
			_, err = p.Render(t.Context(), pack.RenderOptions{Values: tt.values})
			if tt.want == "" {
				if err != nil {
					t.Fatalf("Render(%v) = error %v, want a plan", tt.values, err)
				}
				return
			}
			wantCode(t, err, tt.want)
		})
	}
}

// A raw pack has no templating step, so values are meaningless to it. The
// declared type makes that detectable before anything is rendered.
func TestValuesOnRawPack(t *testing.T) {
	files := rawPackWith(rawCUE, map[string]*fstest.MapFile{"a.yaml": manifest("ConfigMap", "a", "")})
	p, err := pack.Load(t.Context(), files, "./p")
	if err != nil {
		t.Fatalf("Load() = error %v, want a pack", err)
	}

	_, err = p.Render(t.Context(), pack.RenderOptions{Values: map[string]any{"x": 1}})
	wantCode(t, err, pack.CodeValuesOnRawPack)

	// The same pack renders fine when no values are supplied.
	if _, err := p.Render(t.Context(), pack.RenderOptions{}); err != nil {
		t.Errorf("Render(no values) = error %v, want a plan", err)
	}
}

// A #Values whose required fields are unsatisfiable with no user values is
// reported when the pack is rendered without any, not silently accepted.
func TestValuesDefaultsMustBeSatisfiable(t *testing.T) {
	p, err := pack.Load(t.Context(), helmPack(valuesCUE), "./p")
	if err != nil {
		t.Fatalf("Load() = error %v, want a pack", err)
	}
	_, err = p.Render(t.Context(), pack.RenderOptions{})
	wantCode(t, err, pack.CodeValuesRejected)
}

// Render must not retain or mutate the caller's values map.
func TestRenderDoesNotMutateCallerValues(t *testing.T) {
	p, err := pack.Load(t.Context(), helmPack(valuesCUE), "./p")
	if err != nil {
		t.Fatalf("Load() = error %v, want a pack", err)
	}

	values := map[string]any{"image": "nginx"}
	_, _ = p.Render(t.Context(), pack.RenderOptions{Values: values})

	if len(values) != 1 || values["image"] != "nginx" {
		t.Errorf("Render() mutated the caller's values: got %v, want map[image:nginx]", values)
	}
}
