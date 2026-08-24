package pack

import (
	"encoding/json"
	"fmt"

	"golang.org/x/mod/semver"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

// The Flux API versions and kinds a helm pack renders to. They are the
// engine's contract, not this domain's, so they are written out here once
// rather than derived: bootstrap installs a pinned Flux, and these are the
// kinds that install serves.
const (
	helmReleaseAPIVersion = "helm.toolkit.fluxcd.io/v2"
	sourceAPIVersion      = "source.toolkit.fluxcd.io/v1"

	kindHelmRelease    = "HelmRelease"
	kindHelmRepository = "HelmRepository"
	kindOCIRepository  = "OCIRepository"
)

// helmInterval is the reconcile interval every rendered CR carries.
//
// It is fixed rather than configurable because the CRDs require the field —
// HelmRelease.spec.interval and OCIRepository.spec.interval are both mandatory
// — and the pack surface has no interval knob to fill it from. 10m is
// spec.engine.source's documented default, so one number means one thing
// across the product. An interval field is its own design gate.
const helmInterval = "10m"

// helmChartLayerMediaType selects the chart layer of an OCI artifact. Flux
// needs it to tell a chart layer from anything else pushed alongside it.
const helmChartLayerMediaType = "application/vnd.cncf.helm.chart.content.v1.tar+gzip"

// ChartKind selects how a helm pack's chart is addressed. It is an explicit
// discriminator, never sniffed from the URL, for the same reason a pack's type
// is: the two forms map onto different Flux source kinds, and guessing which
// one an author meant is the class of silence this domain removes.
type ChartKind string

// The chart addressing forms a helm pack may declare.
const (
	// ChartKindRepo addresses a chart by name and version inside a classic
	// Helm repository index.
	ChartKindRepo ChartKind = "repo"
	// ChartKindOCI addresses a chart stored as an OCI artifact.
	ChartKindOCI ChartKind = "oci"
)

// Chart is the coordinates a helm pack delegates to. The pack carries these
// and no chart content: helm-controller pulls and templates the chart in
// cluster, so nothing here is fetched at render time.
//
// Reproducibility differs by kind, and the difference is load-bearing rather
// than incidental: an OCI Digest pins content bit-for-bit, while a repository
// index is its owner's to rewrite, so ChartKindRepo plus an exact Version is a
// *mutable reference*. See docs/domains/pack.md, "Reproducibility".
type Chart struct {
	// Kind selects the addressing form: "repo" or "oci".
	Kind ChartKind `json:"kind"`
	// URL is the repository index URL for kind repo, the chart's oci://
	// location for kind oci.
	URL string `json:"url"`
	// Name is the chart's name within the repository. It is set for kind
	// repo only; for kind oci the name is the last element of URL.
	Name string `json:"name,omitempty"`
	// Version is an exact SemVer — never a range, and never leading-v.
	Version string `json:"version"`
	// Digest optionally pins an OCI artifact bit-for-bit. It is the only
	// field here that pins content rather than naming it.
	Digest string `json:"digest,omitempty"`
}

// checkChartVersion reports whether v is an exact SemVer this domain accepts.
//
// The canonical round-trip is the whole rule: a value is exact exactly when
// canonicalizing it changes nothing. That rejects ranges (">=1.0.0", "6.5.*"),
// partial versions ("6.5", "6"), a leading "v", and SemVer build metadata
// ("1.2.3+meta", which Canonical strips) — while accepting "6.5.4" and
// prereleases like "1.2.3-rc.1". The CUE #ExactSemVer regex is a shape check
// that fails the obvious cases early; this is the authority.
func checkChartVersion(v string) error {
	if prefixed := "v" + v; semver.Canonical(prefixed) != prefixed {
		return newChartVersionError(v)
	}
	return nil
}

// The helm-specific coded errors live here rather than in errors.go, beside
// the only code that raises them. The CUBE-PKG-* catalog itself stays in
// errors.go, which is what ARCHITECTURE §5 pins down; these add no codes.

// newChartContentError reports the inverse payload mismatch: a helm pack is
// thin, so chart content at its root is the violation. It shares
// CodePayloadMismatch because it is the same fault — payload and declared type
// disagree — read from the other direction.
func newChartContentError(found string) error {
	return cubeerr.Wrap(CodePayloadMismatch,
		fmt.Sprintf("pack declares type %q but bundles chart content (%s)", TypeHelm, found),
		fmt.Sprintf("a helm pack carries coordinates, not a chart: delete %s and address the published chart through the chart block in %s", found, MetadataFile), nil)
}

// newChartVersionError reports a chart version that is not an exact SemVer. It
// is a CodeMetadataSchema fault: the version is part of the chart block the
// pack schema declares, so the CUE shape check and this parser answer one
// question and report it one way.
func newChartVersionError(version string) error {
	return cubeerr.Wrap(CodeMetadataSchema,
		fmt.Sprintf("chart version %q is not an exact version", version),
		"give one exact SemVer (6.5.4 or 1.2.3-rc.1) — not a range, a partial version, a leading v, or build metadata", nil)
}

// newSchemaDriftError reports decoded metadata that #Pack should have made
// impossible: an unknown type, or a helm pack with no chart. It is unreachable
// through Load, and exists so drift between the schema and the code that
// consumes it is a coded error rather than a nil dereference or a wrong render.
func newSchemaDriftError(detail string) error {
	return cubeerr.Wrap(CodeMetadataSchema,
		fmt.Sprintf("pack metadata does not match the pack schema: %s", detail),
		fmt.Sprintf("declare type raw, helm, or kustomize in %s — and for helm, a chart block with kind, url, and an exact version", MetadataFile), nil)
}

// instanceID resolves the identity the rendered objects are named after: the
// setup's effective id, or the pack's own name when a pack is rendered as the
// artifact it is, with no setup around it.
func (p *Pack) instanceID(id string) string {
	if id != "" {
		return id
	}
	return p.meta.Name
}

// planHelm renders a helm pack: the chart's Flux source CR followed by the
// HelmRelease that delegates to it.
//
// The order is fixed so output is deterministic, and it is source-first
// because that is the order the objects mean something in. Neither CR gets the
// namespace transform: a helm pack's workload objects do not exist yet, so
// there is nothing to transform, and the pack's namespace is carried by the
// HelmRelease's targetNamespace instead.
func (p *Pack) planHelm(id string, values map[string]any) (RenderPlan, error) {
	chart := p.meta.Chart
	if chart == nil {
		return RenderPlan{}, newSchemaDriftError("type is helm but no chart is declared")
	}
	release, err := p.helmRelease(id, chart, values)
	if err != nil {
		return RenderPlan{}, err
	}
	return RenderPlan{Objects: []*unstructured.Unstructured{chartSource(id, chart), release}}, nil
}

// chartSource builds the Flux source CR the HelmRelease pulls its chart from:
// a HelmRepository for a repository index, an OCIRepository for an artifact.
//
// Neither carries a metadata.namespace. Which namespace a delivery unit lands
// in is the delivery contract's decision, and a rendered stream that hard-codes
// one cannot be delivered anywhere else.
func chartSource(id string, chart *Chart) *unstructured.Unstructured {
	spec := map[string]any{
		"url":      chart.URL,
		"interval": helmInterval,
	}
	kind := kindHelmRepository
	if chart.Kind == ChartKindOCI {
		kind = kindOCIRepository
		spec["layerSelector"] = map[string]any{
			"mediaType": helmChartLayerMediaType,
			"operation": "copy",
		}
		spec["ref"] = ociRef(chart)
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": sourceAPIVersion,
		"kind":       kind,
		"metadata":   map[string]any{"name": id},
		"spec":       spec,
	}}
}

// ociRef pins the artifact the OCIRepository resolves: the version as a tag,
// plus the digest when the pack declares one. Both are emitted together
// deliberately — the tag stays readable while the digest is what actually
// pins, and Flux verifies the digest it resolves.
func ociRef(chart *Chart) map[string]any {
	ref := map[string]any{"tag": chart.Version}
	if chart.Digest != "" {
		ref["digest"] = chart.Digest
	}
	return ref
}

// helmRelease builds the HelmRelease that delegates the chart to
// helm-controller, with the pack's validated values inline.
func (p *Pack) helmRelease(id string, chart *Chart, values map[string]any) (*unstructured.Unstructured, error) {
	spec := map[string]any{
		"interval":    helmInterval,
		"releaseName": id,
	}
	// A pack namespace becomes the release's target namespace, and the
	// release creates it: a thin pack has no payload to bundle a Namespace
	// into, so refusing would leave the field silently requiring an
	// out-of-band create.
	if ns := p.meta.Namespace; ns != "" {
		spec["targetNamespace"] = ns
		spec["install"] = map[string]any{"createNamespace": true}
	}
	if chart.Kind == ChartKindOCI {
		spec["chartRef"] = map[string]any{"kind": kindOCIRepository, "name": id}
	} else {
		spec["chart"] = map[string]any{"spec": map[string]any{
			"chart":     chart.Name,
			"version":   chart.Version,
			"sourceRef": map[string]any{"kind": kindHelmRepository, "name": id},
		}}
	}
	if len(values) > 0 {
		content, err := jsonValues(values)
		if err != nil {
			return nil, err
		}
		spec["values"] = content
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": helmReleaseAPIVersion,
		"kind":       kindHelmRelease,
		"metadata":   map[string]any{"name": id},
		"spec":       spec,
	}}, nil
}

// jsonValues converts the validated values into the types an unstructured
// object may hold, nested exactly as written.
//
// #Values decodes to Go types that are not all JSON-native, and an
// unstructured object carrying one of those panics deep inside apimachinery
// on the first deep copy. Round-tripping through JSON here converts what can
// be converted and turns what cannot into the same coded error a value CUE
// rejects would raise — the pack's values surface is one answer, not two.
func jsonValues(values map[string]any) (map[string]any, error) {
	raw, err := json.Marshal(values)
	if err != nil {
		return nil, newValuesRejectedError(err)
	}
	var content map[string]any
	if err := json.Unmarshal(raw, &content); err != nil {
		return nil, newValuesRejectedError(err)
	}
	return content, nil
}
