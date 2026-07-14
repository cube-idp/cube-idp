package pack

// renderHelm is the ONLY file in internal/pack that imports the Helm SDK
// (plan-header risk rule): a chart.yaml next to pack.cue is a reference to
// a chart, rendered client-side with `helm template` semantics — no
// cluster access, no install, no helm-controller in the loop (spec §4:
// engines receive rendered manifests). It reads chart.yaml, pulls the
// pinned chart, template-renders it with the pack's chart-level default
// values merged UNDER the caller's (already CUE-validated) values, and
// returns unstructured objects.
//
// Helm SDK version note (Task 13.5, 2026-07-14): ported from helm.sh/helm/v3
// to helm.sh/helm/v4 (v4.2.3). v4's action.Install replaces the v3
// DryRun/ClientOnly bools with a single DryRunStrategy enum; the client-only,
// no-cluster-access rendering path this file needs is action.DryRunClient
// (verified against the v4 source: with that strategy, Install.Run mocks
// Capabilities/KubeClient/Releases internally and bails out before any
// cluster interaction — the same semantics the old DryRun=true,
// ClientOnly=true combination provided). The v3-vs-v4 API-shape concern noted
// in the original plan did not block the port.

import (
	"fmt"
	"os"
	"path/filepath"

	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/chart/common"
	"helm.sh/helm/v4/pkg/chart/loader"
	"helm.sh/helm/v4/pkg/cli"
	"helm.sh/helm/v4/pkg/registry"
	release "helm.sh/helm/v4/pkg/release/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"github.com/rafpe/cube-idp/internal/apply"
	"github.com/rafpe/cube-idp/internal/diag"
)

// defaultRenderKubeVersion mirrors internal/config's default Kubernetes
// version for kind clusters (v1.33.1); it stands in for helm's own default
// Capabilities.KubeVersion (v1.20.0), which is too old for many current
// charts to render against.
const defaultRenderKubeVersion = "v1.33.1"

// ChartRef is the chart.yaml shape documented in pack.go's package doc.
// Field tags are json (not yaml) because sigs.k8s.io/yaml converts the YAML
// to JSON and then unmarshals with encoding/json, which honors json tags
// only — yaml tags would work solely via encoding/json's case-insensitive
// field-name fallback. Exported for the cnoe-compat loader (Task 13), which
// builds a ChartRef straight from an Argo Application's helm source — this
// file remains the only one importing the Helm SDK.
type ChartRef struct {
	Chart       string         `json:"chart"`
	Repo        string         `json:"repo"`
	Version     string         `json:"version"`
	ReleaseName string         `json:"releaseName"`
	Namespace   string         `json:"namespace"`
	Values      map[string]any `json:"values"`
}

// RenderChart renders a chart reference exactly as a pack's chart.yaml would
// be rendered. Exported for the cnoe-compat loader; helm.go remains the only
// file importing the Helm SDK.
func RenderChart(ref ChartRef, values map[string]any) ([]*unstructured.Unstructured, error) {
	return renderChartRef(ref, values)
}

// renderHelm reads chart.yaml in dir, pulls the pinned chart, and
// template-renders it with values (chartRef.Values as the base, overridden
// by the caller's values). Failures are reported as CUBE-4005.
func renderHelm(dir string, values map[string]any) ([]*unstructured.Unstructured, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "chart.yaml"))
	if err != nil {
		return nil, diag.Wrap(err, diag.CodePackChartErr, "cannot read chart.yaml", "check file permissions")
	}
	var ref ChartRef
	if err := yaml.Unmarshal(raw, &ref); err != nil {
		return nil, diag.Wrap(err, diag.CodePackChartErr, "chart.yaml is not valid YAML", "fix the chart.yaml syntax")
	}
	return renderChartRef(ref, values)
}

// renderChartRef pulls the chart referenced by ref and template-renders it
// with values (ref.Values as the base, overridden by the caller's values).
// Failures are reported as CUBE-4005.
func renderChartRef(ref ChartRef, values map[string]any) ([]*unstructured.Unstructured, error) {
	if ref.Chart == "" {
		return nil, diag.New(diag.CodePackChartErr, "chart.yaml is missing 'chart'", "add: chart: \"<chart-name>\"")
	}
	if ref.ReleaseName == "" {
		ref.ReleaseName = ref.Chart
	}

	settings := cli.New()
	cfg := new(action.Configuration) // zero config: client-only, no cluster access

	install := action.NewInstall(cfg)
	install.DryRunStrategy, install.Replace = action.DryRunClient, true
	install.ReleaseName = ref.ReleaseName
	install.Namespace = ref.Namespace
	install.ChartPathOptions.Version = ref.Version
	install.CreateNamespace = false // we manage the Namespace object ourselves, below
	// helm's built-in default Capabilities.KubeVersion (v1.20.0) is old
	// enough that many current charts refuse to render against it; use the
	// same default Kubernetes version cube-idp provisions kind clusters
	// with (internal/config's default), so charts see a realistic target.
	if kv, err := common.ParseKubeVersion(defaultRenderKubeVersion); err == nil {
		install.KubeVersion = kv
	}

	if registry.IsOCI(ref.Chart) {
		rc, err := registry.NewClient()
		if err != nil {
			return nil, diag.Wrap(err, diag.CodePackChartErr, "cannot create helm registry client", "check network access to the OCI chart registry")
		}
		install.SetRegistryClient(rc)
	} else {
		install.ChartPathOptions.RepoURL = ref.Repo
	}

	chartPath, err := install.ChartPathOptions.LocateChart(ref.Chart, settings)
	if err != nil {
		return nil, diag.Wrap(err, diag.CodePackChartErr, fmt.Sprintf("cannot locate chart %q", ref.Chart),
			"check chart repo/version in chart.yaml; try `helm template` manually")
	}
	chrt, err := loader.Load(chartPath)
	if err != nil {
		return nil, diag.Wrap(err, diag.CodePackChartErr, fmt.Sprintf("cannot load chart %q", ref.Chart),
			"check chart repo/version in chart.yaml; try `helm template` manually")
	}

	relAny, err := install.Run(chrt, mergeValues(ref.Values, values))
	if err != nil {
		return nil, diag.Wrap(err, diag.CodePackChartErr, fmt.Sprintf("helm render failed for pack chart %q", ref.Chart),
			"check chart repo/version in chart.yaml; try `helm template` manually")
	}
	rel, ok := relAny.(*release.Release)
	if !ok {
		return nil, diag.New(diag.CodePackChartErr, fmt.Sprintf("helm render for pack chart %q returned an unexpected release type", ref.Chart),
			"this is likely a helm SDK version mismatch; report a cube-idp bug")
	}

	objs, err := apply.ParseMultiDoc([]byte(rel.Manifest))
	if err != nil {
		return nil, diag.Wrap(err, diag.CodePackChartErr, fmt.Sprintf("helm chart %q rendered invalid YAML", ref.Chart),
			"this is likely a bug in the chart; try `helm template` manually")
	}

	if ref.Namespace != "" && !hasNamespaceObject(objs, ref.Namespace) {
		nsObj := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Namespace",
			"metadata":   map[string]any{"name": ref.Namespace},
		}}
		objs = append([]*unstructured.Unstructured{nsObj}, objs...)
	}
	return objs, nil
}

func hasNamespaceObject(objs []*unstructured.Unstructured, name string) bool {
	for _, o := range objs {
		if o.GetKind() == "Namespace" && o.GetName() == name {
			return true
		}
	}
	return false
}

// mergeValues deep-merges override on top of base (chart.yaml's default
// values), with override winning on conflicts. Neither input is mutated.
func mergeValues(base, override map[string]any) map[string]any {
	if len(base) == 0 {
		return override
	}
	out := make(map[string]any, len(base)+len(override))
	for k, v := range base {
		out[k] = v
	}
	for k, ov := range override {
		if bv, ok := out[k]; ok {
			if bMap, ok1 := bv.(map[string]any); ok1 {
				if oMap, ok2 := ov.(map[string]any); ok2 {
					out[k] = mergeValues(bMap, oMap)
					continue
				}
			}
		}
		out[k] = ov
	}
	return out
}
