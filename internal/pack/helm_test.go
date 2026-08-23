package pack_test

import (
	"strings"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/cube-idp/cube-idp/internal/pack"
)

// renderHelmYAML renders a thin helm pack and returns its objects as one YAML
// stream. Comparing the whole stream rather than field-by-field is deliberate:
// the emitted CRs are the contract, so a golden document catches a field that
// appears as surely as one that changes.
func renderHelmYAML(t *testing.T, cue string, opts pack.RenderOptions) string {
	t.Helper()
	p, err := pack.Load(t.Context(), helmPack(cue), "./p")
	if err != nil {
		t.Fatalf("Load() = error %v, want a pack", err)
	}
	plan, err := p.Render(t.Context(), opts)
	if err != nil {
		t.Fatalf("Render() = error %v, want a plan", err)
	}
	if plan.Prerequisites != nil {
		t.Errorf("Render().Prerequisites = %v, want nil — a helm pack declares none", plan.Prerequisites)
	}
	docs := make([]string, 0, len(plan.Objects))
	for _, obj := range plan.Objects {
		out, err := yaml.Marshal(obj.Object)
		if err != nil {
			t.Fatalf("Marshal(%v) = error %v, want YAML", obj.Object, err)
		}
		docs = append(docs, string(out))
	}
	return strings.Join(docs, "---\n")
}

// A repo-addressed helm pack renders its HelmRepository and the HelmRelease
// that pulls the chart through it — source first, and nothing else.
func TestRenderHelmRepo(t *testing.T) {
	const cue = `name:      "podinfo"
version:   "1"
namespace: "web"
chart: {
	kind:    "repo"
	url:     "https://stefanprodan.github.io/podinfo"
	name:    "podinfo"
	version: "6.5.4"
}
type: "helm"
#Values: {
	replicaCount: int | *2
	ingress: {
		enabled: bool | *true
		hosts: [...string] | *["example.com"]
	}
}
`
	const want = `apiVersion: source.toolkit.fluxcd.io/v1
kind: HelmRepository
metadata:
  name: podinfo
spec:
  interval: 10m
  url: https://stefanprodan.github.io/podinfo
---
apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: podinfo
spec:
  chart:
    spec:
      chart: podinfo
      sourceRef:
        kind: HelmRepository
        name: podinfo
      version: 6.5.4
  install:
    createNamespace: true
  interval: 10m
  releaseName: podinfo
  targetNamespace: web
  values:
    ingress:
      enabled: true
      hosts:
      - example.com
    replicaCount: 2
`

	if got := renderHelmYAML(t, cue, pack.RenderOptions{}); got != want {
		t.Errorf("Render(repo chart) =\n%s\nwant:\n%s", got, want)
	}
}

// An oci-addressed helm pack renders an OCIRepository instead, with the chart
// selected by layer and pinned by digest, and the release referencing it
// through chartRef rather than an inline chart spec.
func TestRenderHelmOCIWithDigest(t *testing.T) {
	const cue = `name:    "podinfo"
version: "1"
type:    "helm"
chart: {
	kind:    "oci"
	url:     "oci://ghcr.io/stefanprodan/charts/podinfo"
	version: "6.5.4"
	digest:  "sha256:0000000000000000000000000000000000000000000000000000000000000abc"
}
`
	const want = `apiVersion: source.toolkit.fluxcd.io/v1
kind: OCIRepository
metadata:
  name: podinfo
spec:
  interval: 10m
  layerSelector:
    mediaType: application/vnd.cncf.helm.chart.content.v1.tar+gzip
    operation: copy
  ref:
    digest: sha256:0000000000000000000000000000000000000000000000000000000000000abc
    tag: 6.5.4
  url: oci://ghcr.io/stefanprodan/charts/podinfo
---
apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: podinfo
spec:
  chartRef:
    kind: OCIRepository
    name: podinfo
  interval: 10m
  releaseName: podinfo
`

	if got := renderHelmYAML(t, cue, pack.RenderOptions{}); got != want {
		t.Errorf("Render(oci chart) =\n%s\nwant:\n%s", got, want)
	}
}

// An oci chart with no digest is a mutable reference, and renders as one: the
// tag is emitted and no digest field appears at all.
func TestRenderHelmOCIWithoutDigest(t *testing.T) {
	const cue = `name:    "app"
version: "1"
type:    "helm"
chart: {
	kind:    "oci"
	url:     "oci://ghcr.io/org/charts/app"
	version: "1.2.3-rc.1"
}
`
	got := renderHelmYAML(t, cue, pack.RenderOptions{})
	if !strings.Contains(got, "tag: 1.2.3-rc.1") {
		t.Errorf("Render(oci, no digest) =\n%s\nwant a ref.tag of 1.2.3-rc.1", got)
	}
	if strings.Contains(got, "digest:") {
		t.Errorf("Render(oci, no digest) =\n%s\nwant no digest field", got)
	}
}

// Without pack.namespace there is no target namespace, and therefore nothing
// for the release to create: both fields are absent rather than empty.
func TestRenderHelmWithoutNamespace(t *testing.T) {
	const cue = `name:    "app"
version: "1"
type:    "helm"
` + chartCUE

	got := renderHelmYAML(t, cue, pack.RenderOptions{})
	for _, field := range []string{"targetNamespace", "createNamespace", "install"} {
		if strings.Contains(got, field) {
			t.Errorf("Render(no pack namespace) =\n%s\nwant no %s field", got, field)
		}
	}
}

// The rendered objects are named after the effective instance id, so two
// copies of one pack are individually addressable. An empty id is artifact
// mode, where the pack's own name is the effective id.
func TestRenderHelmInstanceID(t *testing.T) {
	const cue = `name:    "app"
version: "1"
type:    "helm"
` + chartCUE

	tests := []struct {
		name string
		id   string
		want string
	}{
		{name: "artifact mode falls back to the pack name", id: "", want: "app"},
		{name: "an explicit instance id names every object", id: "app-staging", want: "app-staging"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := pack.Load(t.Context(), helmPack(cue), "./p")
			if err != nil {
				t.Fatalf("Load() = error %v, want a pack", err)
			}
			plan, err := p.Render(t.Context(), pack.RenderOptions{InstanceID: tt.id})
			if err != nil {
				t.Fatalf("Render(id=%q) = error %v, want a plan", tt.id, err)
			}
			if len(plan.Objects) != 2 {
				t.Fatalf("Render(id=%q) rendered %d objects, want 2", tt.id, len(plan.Objects))
			}
			for _, obj := range plan.Objects {
				if got := obj.GetName(); got != tt.want {
					t.Errorf("Render(id=%q) %s name = %q, want %q", tt.id, obj.GetKind(), got, tt.want)
				}
				if got := obj.GetNamespace(); got != "" {
					t.Errorf("Render(id=%q) %s namespace = %q, want none", tt.id, obj.GetKind(), got)
				}
			}
			// The release must point at the source object by that same name,
			// or the pair is delivered together and still does not resolve.
			if ref := plan.Objects[1].Object["spec"].(map[string]any)["chart"]; ref != nil {
				spec, _ := ref.(map[string]any)["spec"].(map[string]any)
				source, _ := spec["sourceRef"].(map[string]any)
				if source["name"] != tt.want {
					t.Errorf("Render(id=%q) sourceRef.name = %v, want %q", tt.id, source["name"], tt.want)
				}
			}
		})
	}
}

// Values reach spec.values nested exactly as written: the flat-string rule and
// the ${VAR} grammar are kustomize concerns and must not touch this path.
func TestRenderHelmValuesNestedVerbatim(t *testing.T) {
	const cue = `name:    "a"
version: "1"
type:    "helm"
` + chartCUE

	got := renderHelmYAML(t, cue, pack.RenderOptions{Values: map[string]any{
		"top": map[string]any{
			"nested": map[string]any{"list": []any{1, 2}, "flag": false},
		},
		"literal": "${NOT_SUBSTITUTED}",
	}})

	for _, want := range []string{"nested:", "flag: false", "- 1", "literal: ${NOT_SUBSTITUTED}"} {
		if !strings.Contains(got, want) {
			t.Errorf("Render(nested values) =\n%s\nwant it to contain %q", got, want)
		}
	}
}

// A #Values definition the pack cannot resolve to concrete data is a values
// fault, not a render fault: the same code a rejected user value raises.
func TestRenderHelmNonConcreteValues(t *testing.T) {
	const cue = `name:    "a"
version: "1"
type:    "helm"
` + chartCUE + `#Values: {
	image!: string
}
`
	p, err := pack.Load(t.Context(), helmPack(cue), "./p")
	if err != nil {
		t.Fatalf("Load() = error %v, want a pack", err)
	}
	_, err = p.Render(t.Context(), pack.RenderOptions{})
	wantCode(t, err, pack.CodeValuesRejected)
}
