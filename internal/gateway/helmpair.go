package gateway

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// The Flux API versions and kinds the thin-helm prerequisite pack renders
// to, and that this domain's predicates therefore judge. They are the pack
// contract's constants, not this domain's, so they are written out here
// once rather than derived — the same call internal/pack/helm.go makes.
// See docs/domains/pack.md, "Render output".
const (
	helmReleaseAPIVersion = "helm.toolkit.fluxcd.io/v2"
	sourceAPIVersion      = "source.toolkit.fluxcd.io/v1"

	kindHelmRelease   = "HelmRelease"
	kindOCIRepository = "OCIRepository"
)

// helmInterval is the reconcile interval every rendered CR carries. Fixed
// by the pack contract (both CRDs require the field and the pack surface
// has no interval knob), not by this domain — spelled out for the same
// reason the API versions are.
const helmInterval = "10m"

// helmChartLayerMediaType selects the chart layer of an OCI artifact. The
// pack contract emits it unconditionally for an OCI chart.
const helmChartLayerMediaType = "application/vnd.cncf.helm.chart.content.v1.tar+gzip"

// HelmPairObjects returns the traefik gateway pack's rendered output,
// hand-built: the OCIRepository followed by the HelmRelease, deliberately
// namespace-less exactly as pack.Render leaves them (the edge stamps
// metadata.namespace after rendering). It cannot fail — the pair is
// literals. An edge dogfood test locks it to what the pack contract
// renders from the embedded pack.cue, which is the single guard on that
// deliberate duplication.
func HelmPairObjects() []*unstructured.Unstructured {
	return []*unstructured.Unstructured{chartSource(), helmRelease()}
}

// chartSource builds the Flux source CR the HelmRelease pulls the pinned
// chart from.
func chartSource() *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": sourceAPIVersion,
		"kind":       kindOCIRepository,
		"metadata":   map[string]any{"name": ImplementationID},
		"spec": map[string]any{
			"url":      "oci://ghcr.io/traefik/helm/traefik",
			"interval": helmInterval,
			"layerSelector": map[string]any{
				"mediaType": helmChartLayerMediaType,
				"operation": "copy",
			},
			"ref": map[string]any{
				"tag":    ChartVersion,
				"digest": ChartDigest,
			},
		},
	}}
}

// helmRelease builds the HelmRelease that delegates the chart to the
// substrate's helm-controller, with the pack's static values inline.
func helmRelease() *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": helmReleaseAPIVersion,
		"kind":       kindHelmRelease,
		"metadata":   map[string]any{"name": ImplementationID},
		"spec": map[string]any{
			"interval":        helmInterval,
			"releaseName":     ImplementationID,
			"targetNamespace": Namespace,
			"install":         map[string]any{"createNamespace": true},
			"chartRef":        map[string]any{"kind": kindOCIRepository, "name": ImplementationID},
			"values":          chartValues(),
		},
	}}
}

// chartValues mirrors packs/traefik-gateway/pack.cue's #Values defaults,
// which is the duplication the edge dogfood test exists to guard.
//
// The numbers are float64 because pack.Render round-trips spec.values
// through JSON, and deep equality against that output is the test's whole
// point; an untyped 80 would be an int and fail with a diff that looks
// identical. Only the values map is round-tripped — every other field
// above stays a plain literal.
func chartValues() map[string]any {
	return map[string]any{
		"providers":        map[string]any{"kubernetesGateway": map[string]any{"enabled": true}},
		"gateway":          map[string]any{"enabled": false},
		"nodeSelector":     map[string]any{"ingress-ready": "true"},
		"fullnameOverride": ImplementationID,
		"ports": map[string]any{
			"web":       map[string]any{"hostPort": float64(80)},
			"websecure": map[string]any{"hostPort": float64(443)},
		},
	}
}
