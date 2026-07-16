package pack

import (
	"reflect"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/cube-idp/cube-idp/internal/config"
)

// TestChartRefDecode pins ChartRef's struct tags: sigs.k8s.io/yaml converts
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
	var got ChartRef
	if err := yaml.Unmarshal(doc, &got); err != nil {
		t.Fatal(err)
	}
	want := ChartRef{
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
		t.Fatalf("ChartRef decode mismatch:\n got: %+v\nwant: %+v", got, want)
	}
}

// TestRenderChartRefIncludesCRDs pins the crds/ recovery: Helm's dry-run
// render (rel.Manifest) omits objects a chart ships under crds/, so
// renderChartRef must re-inject them from chrt.CRDObjects(). The local
// testdata/crds-chart fixture ships two CRDs under crds/ plus one templated
// ConfigMap; without the fix only the ConfigMap would render. Network-free:
// the chart is a local directory LocateChart resolves without a pull.
func TestRenderChartRefIncludesCRDs(t *testing.T) {
	ref := ChartRef{
		Chart:       "testdata/crds-chart",
		ReleaseName: "crds",
		Namespace:   "crds",
	}
	objs, err := renderChartRef(ref, nil, config.GatewaySpec{})
	if err != nil {
		t.Fatal(err)
	}

	var haveCM bool
	crds := map[string]bool{}
	for _, o := range objs {
		switch o.GetKind() {
		case "ConfigMap":
			if o.GetName() == "crds-chart-cm" {
				haveCM = true
			}
		case "CustomResourceDefinition":
			crds[o.GetName()] = true
		}
	}
	if !haveCM {
		t.Fatalf("expected the templated ConfigMap crds-chart-cm to render, got %d objects", len(objs))
	}
	for _, want := range []string{"widgets.example.cube-idp.io", "gadgets.example.cube-idp.io"} {
		if !crds[want] {
			t.Fatalf("crds/ CRD %q missing from rendered objects (Helm dropped it and the fix did not re-inject it); got CRDs %v", want, crds)
		}
	}

	// CRDs must sort ahead of the namespace and the templated ConfigMap so a
	// consumer reading the stream directly sees definitions before instances.
	if objs[0].GetKind() != "CustomResourceDefinition" {
		t.Fatalf("expected a CustomResourceDefinition first, got %s/%s", objs[0].GetKind(), objs[0].GetName())
	}
}

// TestRenderChartRefIncludesHooks pins the hook recovery: Helm returns hook
// manifests (templates carrying helm.sh/hook annotations) on Release.Hooks,
// NOT in Release.Manifest, so — exactly like crds/ — the in-process render
// silently dropped them. envoy's gateway-helm ships its certgen Job (which
// creates the TLS secret the controller mounts) as a pre-install hook, so
// without this the controller pod never starts. The fixture ships a
// pre-install Job, a post-install ConfigMap and a test-hook Pod:
//   - install hooks must render, with the helm.sh/hook* annotations stripped
//     (so the GitOps engine treats them as plain resources) and other
//     annotations retained;
//   - pre-install hooks sort before the regular manifests, post-install
//     after;
//   - test hooks (and other non-install events) must NOT render.
func TestRenderChartRefIncludesHooks(t *testing.T) {
	ref := ChartRef{
		Chart:       "testdata/crds-chart",
		ReleaseName: "crds",
		Namespace:   "crds",
	}
	objs, err := renderChartRef(ref, nil, config.GatewaySpec{})
	if err != nil {
		t.Fatal(err)
	}

	idx := map[string]int{}
	for i, o := range objs {
		idx[o.GetKind()+"/"+o.GetName()] = i
	}

	jobAt, ok := idx["Job/crds-chart-certgen"]
	if !ok {
		t.Fatalf("pre-install hook Job crds-chart-certgen missing from rendered objects (Release.Hooks were dropped); got %v", idx)
	}
	postAt, ok := idx["ConfigMap/crds-chart-post"]
	if !ok {
		t.Fatalf("post-install hook ConfigMap crds-chart-post missing from rendered objects; got %v", idx)
	}
	if _, leaked := idx["Pod/crds-chart-test"]; leaked {
		t.Fatalf("test hook Pod crds-chart-test must not render, got %v", idx)
	}

	cmAt, ok := idx["ConfigMap/crds-chart-cm"]
	if !ok {
		t.Fatalf("templated ConfigMap crds-chart-cm missing; got %v", idx)
	}
	if !(jobAt < cmAt && cmAt < postAt) {
		t.Fatalf("hook ordering wrong: want pre-install Job (%d) < manifest ConfigMap (%d) < post-install ConfigMap (%d)", jobAt, cmAt, postAt)
	}

	ann := objs[jobAt].GetAnnotations()
	for _, k := range []string{"helm.sh/hook", "helm.sh/hook-delete-policy", "helm.sh/hook-weight"} {
		if _, still := ann[k]; still {
			t.Errorf("hook Job still carries %q annotation — the GitOps engine would mistreat it: %v", k, ann)
		}
	}
	if ann["example.cube-idp.io/keep"] != "yes" {
		t.Errorf("non-hook annotation was stripped too: %v", ann)
	}
	if postAnn := objs[postAt].GetAnnotations(); postAnn != nil {
		if _, still := postAnn["helm.sh/hook"]; still {
			t.Errorf("post-install hook still carries helm.sh/hook: %v", postAnn)
		}
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
