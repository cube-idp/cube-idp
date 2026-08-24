package pack_test

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/cube-idp/cube-idp/internal/pack"
)

// collapseSpaces squeezes runs of spaces to one, so an assertion about a
// derived constraint does not also assert the formatter's column alignment.
func collapseSpaces(s string) string {
	return regexp.MustCompile(` +`).ReplaceAllString(s, " ")
}

// The derived #Values locks the top-level surface without forbidding anything
// nested: every field optional, every nested struct left open, defaults taken
// from what the chart's own values.yaml happened to hold.
func TestNewFromChartDerivesLossyValues(t *testing.T) {
	const values = `replicaCount: 2
enabled: true
name: web
ratio: 1.5
tolerations: []
empty: {}
nothing: null
image:
  repository: nginx
  tag: "1.0"
"dashed-key": 1
`
	chart := writeChart(t, podinfoChart, values)
	dir, src := scaffoldFromChart(t, pack.NewOptions{}, chart)

	// cue/format aligns each run of fields into its own column group, so the
	// assertions compare on collapsed whitespace: the constraint is the
	// contract, the padding is the formatter's business.
	flat := collapseSpaces(src)
	for _, want := range []string{
		"replicaCount?: int | *2",
		"enabled?: bool | *true",
		`name?: string | *"web"`,
		"ratio?: number | *1.5",
		"tolerations?: [...] | *[]",
		"nothing?: _",
		`"dashed-key"?: int | *1`,
		`repository?: string | *"nginx"`,
	} {
		if !strings.Contains(flat, want) {
			t.Errorf("derived #Values missing %q; got:\n%s", want, src)
		}
	}
	// Nested structs stay open, so a knob the chart's defaults omit is still
	// accepted; the definition itself stays closed.
	if strings.Count(src, "...") < 2 {
		t.Errorf("derived #Values does not leave nested structs open:\n%s", src)
	}
	if !strings.Contains(src, "lossy") {
		t.Errorf("derived #Values does not say it is lossy:\n%s", src)
	}

	// The lockdown still works: a top-level field the chart never declared is
	// rejected, while an undeclared *nested* one is accepted.
	p, err := pack.Load(t.Context(), os.DirFS(dir), dir)
	if err != nil {
		t.Fatalf("Load() = error %v, want a pack", err)
	}
	_, err = p.Render(t.Context(), pack.RenderOptions{Values: map[string]any{"undeclared": 1}})
	wantCode(t, err, pack.CodeValuesRejected)

	if _, err := p.Render(t.Context(), pack.RenderOptions{Values: map[string]any{
		"image": map[string]any{"repository": "nginx", "pullPolicy": "Always"},
	}}); err != nil {
		t.Errorf("Render(nested knob the chart omitted) = error %v, want a plan", err)
	}
}

// A chart with no values.yaml scaffolds a pack with an empty values surface
// rather than failing: a chart may expose nothing.
func TestNewFromChartWithoutValues(t *testing.T) {
	chart := writeChart(t, podinfoChart, "")
	dir, src := scaffoldFromChart(t, pack.NewOptions{}, chart)

	if !strings.Contains(src, "#Values: {") {
		t.Errorf("scaffolded pack.cue declares no #Values:\n%s", src)
	}
	p, err := pack.Load(t.Context(), os.DirFS(dir), dir)
	if err != nil {
		t.Fatalf("Load() = error %v, want a pack", err)
	}
	if _, err := p.Render(t.Context(), pack.RenderOptions{}); err != nil {
		t.Errorf("Render() = error %v, want a plan", err)
	}
}

// The same chart always scaffolds the same bytes: map iteration order must not
// reach the file.
func TestNewFromChartIsDeterministic(t *testing.T) {
	const values = "z: 1\na: 2\nm:\n  q: 1\n  b: 2\n"
	chart := writeChart(t, podinfoChart, values)

	_, first := scaffoldFromChart(t, pack.NewOptions{}, chart)
	for i := range 5 {
		if _, got := scaffoldFromChart(t, pack.NewOptions{}, chart); got != first {
			t.Fatalf("scaffold run %d differs:\ngot:\n%s\nwant:\n%s", i, got, first)
		}
	}
}
