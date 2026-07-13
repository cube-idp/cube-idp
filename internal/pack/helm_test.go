package pack

import (
	"reflect"
	"testing"

	"sigs.k8s.io/yaml"
)

// TestChartRefDecode pins chartRef's struct tags: sigs.k8s.io/yaml converts
// YAML to JSON and unmarshals with encoding/json, so the tags must be json
// tags. Every key in our chart.yaml schema also happens to match its field
// name case-insensitively (encoding/json's fallback), so no single key can
// distinguish tagged from untagged decoding — instead this asserts full
// struct equality so any future tag or schema drift fails loudly.
func TestChartRefDecode(t *testing.T) {
	doc := []byte(`chart: traefik
repo: https://traefik.github.io/charts
version: "34.1.0"
releaseName: traefik-rel
namespace: traefik-ns
values:
  replicas: 2
  service:
    type: ClusterIP
`)
	var got chartRef
	if err := yaml.Unmarshal(doc, &got); err != nil {
		t.Fatal(err)
	}
	want := chartRef{
		Chart:       "traefik",
		Repo:        "https://traefik.github.io/charts",
		Version:     "34.1.0",
		ReleaseName: "traefik-rel",
		Namespace:   "traefik-ns",
		Values: map[string]any{
			"replicas": float64(2), // JSON numbers decode as float64
			"service":  map[string]any{"type": "ClusterIP"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("chartRef decode mismatch:\n got: %+v\nwant: %+v", got, want)
	}
}

func TestMergeValuesOverrideWins(t *testing.T) {
	base := map[string]any{
		"replicas": 1,
		"image":    map[string]any{"tag": "v1", "pullPolicy": "IfNotPresent"},
	}
	override := map[string]any{
		"replicas": 3,
		"image":    map[string]any{"tag": "v2"},
	}
	got := mergeValues(base, override)
	want := map[string]any{
		"replicas": 3,
		"image":    map[string]any{"tag": "v2", "pullPolicy": "IfNotPresent"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mergeValues:\n got: %+v\nwant: %+v", got, want)
	}
	// inputs must not be mutated
	if base["replicas"] != 1 || base["image"].(map[string]any)["tag"] != "v1" {
		t.Fatalf("mergeValues mutated base: %+v", base)
	}
}
