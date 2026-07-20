package pack

// renderHelm is the ONLY file in internal/pack that imports the Helm SDK
// (plan-header risk rule): a chart.yaml next to pack.cue is a reference to
// a chart, rendered client-side with `helm template` semantics — no
// cluster access, no install, no helm-controller in the loop (engines
// receive rendered manifests). It reads chart.yaml, pulls the
// pinned chart, template-renders it with the pack's chart-level default
// values merged UNDER the caller's (already CUE-validated) values, and
// returns unstructured objects.
//
// Helm SDK version note (2026-07-14): ported from helm.sh/helm/v3
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
	"sort"

	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/chart/common"
	"helm.sh/helm/v4/pkg/chart/loader"
	chartv2 "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/cli"
	"helm.sh/helm/v4/pkg/registry"
	release "helm.sh/helm/v4/pkg/release/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"github.com/cube-idp/cube-idp/internal/apply"
	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/diag"
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
// field-name fallback. Exported for the cnoe-compat loader, which
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
// file importing the Helm SDK. cnoe-compat sources are out of D15's scope
// (they carry no gw context), so this performs no gateway substitution — the
// zero config.GatewaySpec{} is a no-op (see substitute in expose.go).
func RenderChart(ref ChartRef, values map[string]any) ([]*unstructured.Unstructured, error) {
	return renderChartRef(ref, values, config.GatewaySpec{})
}

// renderHelm reads chart.yaml in dir, pulls the pinned chart, and
// template-renders it with values (chartRef.Values as the base, overridden
// by the caller's values). Failures are reported as CUBE-4005.
func renderHelm(dir string, values map[string]any, gw config.GatewaySpec) ([]*unstructured.Unstructured, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "chart.yaml"))
	if err != nil {
		return nil, diag.Wrap(err, diag.CodePackChartErr, "cannot read chart.yaml", "check file permissions")
	}
	var ref ChartRef
	if err := yaml.Unmarshal(raw, &ref); err != nil {
		return nil, diag.Wrap(err, diag.CodePackChartErr, "chart.yaml is not valid YAML", "fix the chart.yaml syntax")
	}
	return renderChartRef(ref, values, gw)
}

// renderChartRef pulls the chart referenced by ref and template-renders it
// with values (ref.Values as the base, overridden by the caller's values).
// Failures are reported as CUBE-4005.
func renderChartRef(ref ChartRef, values map[string]any, gw config.GatewaySpec) ([]*unstructured.Unstructured, error) {
	if ref.Chart == "" {
		return nil, diag.New(diag.CodePackChartErr, "chart.yaml is missing 'chart'", "add: chart: \"<chart-name>\"")
	}
	if ref.ReleaseName == "" {
		ref.ReleaseName = ref.Chart
	}

	settings := helmSettings()
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

	// D15: substitute AFTER merging pack defaults (ref.Values) with the
	// caller's values, so ${GATEWAY_HOST}/${GATEWAY_FQDN} tokens are
	// resolved whichever side they came from before the chart template
	// engine sees them.
	merged := substituteValues(mergeValues(ref.Values, values), gw).(map[string]any)
	relAny, err := install.Run(chrt, merged)
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

	// Helm returns hook manifests (templates carrying helm.sh/hook
	// annotations) on rel.Hooks, NOT in rel.Manifest — `helm install` runs
	// them out-of-band around the manifest apply, so like crds/ they were
	// silently dropped from the rendered artifact. Our render is a static
	// artifact for a GitOps engine, so install-time hooks (pre-install /
	// post-install) must become plain resources in the stream: envoy's
	// gateway-helm ships its certgen Job (which creates the TLS secret the
	// controller mounts) as a pre-install hook, and without it the
	// controller pod never starts. The helm.sh/hook* annotations are
	// stripped so the engine applies them as ordinary objects; a hook Job
	// applied as a plain resource runs once, and an unchanged SSA re-apply
	// of the (immutable) Job is a no-op. Hooks whose events would not fire
	// on a fresh install (test, delete/rollback-only, upgrade-only) are
	// skipped. Pre-install hooks go before the manifest objects, post-install
	// after, mirroring helm's own execution order.
	preHooks, postHooks, err := hookObjects(rel.Hooks, ref.Chart)
	if err != nil {
		return nil, err
	}
	objs = append(preHooks, objs...)
	objs = append(objs, postHooks...)

	if ref.Namespace != "" && !hasNamespaceObject(objs, ref.Namespace) {
		nsObj := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Namespace",
			"metadata":   map[string]any{"name": ref.Namespace},
		}}
		objs = append([]*unstructured.Unstructured{nsObj}, objs...)
	}

	// Helm's dry-run render (rel.Manifest) deliberately OMITS the objects a
	// chart ships in its crds/ directory — `helm install` applies those
	// out-of-band, before the templated manifest, and `helm template`
	// likewise drops them unless --include-crds is passed. The Install SDK
	// action exposes no such flag, so charts delivering the Gateway API CRDs
	// (envoy gateway-helm) or any other crds/-shipped CRDs would silently
	// lose them from the rendered artifact. Recover them from the loaded
	// chart via CRDObjects() (which also walks subcharts) and prepend them,
	// CRDs first — ahead of the injected Namespace and any custom resources
	// the same chart templates — so the definitions that establish their
	// kinds always land before their instances. (The applier stages by kind,
	// CRDs ahead of everything, so this ordering is belt-and-suspenders, not
	// load-bearing, but it keeps the rendered stream sane for callers that
	// consume it directly.)
	crdObjs, err := chartCRDObjects(chrt, ref.Chart)
	if err != nil {
		return nil, err
	}
	if len(crdObjs) > 0 {
		objs = append(dedupeCRDs(crdObjs, objs), objs...)
	}
	return objs, nil
}

// helmSettings pins helm's repo cache/config under the cube-idp cache root
// (applies to ALL chart packs): hermetic renders, no
// interference with the operator's own helm state. Best-effort — on a
// cache-dir failure helm's defaults still work.
func helmSettings() *cli.EnvSettings {
	settings := cli.New()
	if dir, err := DefaultCacheDir(); err == nil {
		settings.RepositoryCache = filepath.Join(dir, "helm", "repository")
		settings.RepositoryConfig = filepath.Join(dir, "helm", "repositories.yaml")
	}
	return settings
}

func hasNamespaceObject(objs []*unstructured.Unstructured, name string) bool {
	for _, o := range objs {
		if o.GetKind() == "Namespace" && o.GetName() == name {
			return true
		}
	}
	return false
}

// chartCRDObjects parses the CRD manifests a chart ships in its crds/
// directory into unstructured objects. loader.Load returns chart.Charter
// (an alias for any); Helm's crds/ machinery — CRDObjects(), which also
// recurses into subcharts — lives on the concrete apiVersion v1/v2 chart
// type (*chartv2.Chart). apiVersion v3 charts are still experimental and
// their loader type lives in an internal Helm package we cannot import, so
// a non-v2 chart simply yields no CRDs here (its templated CRDs, if any,
// still render via the manifest). A crds/ file may itself be a multi-doc
// YAML stream, so each is parsed with ParseMultiDoc.
func chartCRDObjects(chrt any, chartName string) ([]*unstructured.Unstructured, error) {
	c, ok := chrt.(*chartv2.Chart)
	if !ok {
		return nil, nil
	}
	var out []*unstructured.Unstructured
	for _, crd := range c.CRDObjects() {
		if crd.File == nil || len(crd.File.Data) == 0 {
			continue
		}
		objs, err := apply.ParseMultiDoc(crd.File.Data)
		if err != nil {
			return nil, diag.Wrap(err, diag.CodePackChartErr,
				fmt.Sprintf("helm chart %q ships an invalid CRD manifest %s", chartName, crd.Name),
				"this is likely a bug in the chart; inspect the chart's crds/ directory")
		}
		out = append(out, objs...)
	}
	return out, nil
}

// hookObjects converts a release's install-time hooks into plain objects:
// hooks firing on pre-install land in pre, on post-install in post (a hook
// declaring both counts as pre — it must exist before the manifests need
// it). Hooks that would not fire on a fresh install (test, delete- or
// rollback- or upgrade-only) are skipped entirely. Hooks are processed in
// helm's execution order (ascending helm.sh/hook-weight, stable within a
// weight) and each object has its helm.sh/hook* annotations stripped so the
// GitOps engine treats it as an ordinary resource.
func hookObjects(hooks []*release.Hook, chartName string) (pre, post []*unstructured.Unstructured, err error) {
	sorted := make([]*release.Hook, len(hooks))
	copy(sorted, hooks)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Weight < sorted[j].Weight })
	for _, h := range sorted {
		var isPre, isPost bool
		for _, e := range h.Events {
			switch e {
			case release.HookPreInstall:
				isPre = true
			case release.HookPostInstall:
				isPost = true
			}
		}
		if !isPre && !isPost {
			continue
		}
		objs, err := apply.ParseMultiDoc([]byte(h.Manifest))
		if err != nil {
			return nil, nil, diag.Wrap(err, diag.CodePackChartErr,
				fmt.Sprintf("helm chart %q rendered an invalid hook manifest %s", chartName, h.Path),
				"this is likely a bug in the chart; try `helm template --no-hooks` manually")
		}
		for _, o := range objs {
			stripHookAnnotations(o)
		}
		if isPre {
			pre = append(pre, objs...)
		} else {
			post = append(post, objs...)
		}
	}
	return pre, post, nil
}

// stripHookAnnotations removes the helm.sh/hook* annotations (hook event,
// weight, delete policy, output-log policy) from o, leaving all other
// annotations intact. Without this a GitOps engine (or anyone reading the
// artifact) would see a resource that claims to be a helm hook while being
// delivered as a plain object.
func stripHookAnnotations(o *unstructured.Unstructured) {
	ann := o.GetAnnotations()
	if ann == nil {
		return
	}
	for _, k := range []string{
		release.HookAnnotation,          // helm.sh/hook
		release.HookWeightAnnotation,    // helm.sh/hook-weight
		release.HookDeleteAnnotation,    // helm.sh/hook-delete-policy
		release.HookOutputLogAnnotation, // helm.sh/hook-output-log-policy
	} {
		delete(ann, k)
	}
	if len(ann) == 0 {
		o.SetAnnotations(nil)
		return
	}
	o.SetAnnotations(ann)
}

// dedupeCRDs returns the subset of crdObjs whose GroupVersionKind+namespace+
// name is not already present in existing. A chart that both templates a CRD
// (so it lands in the rendered manifest) and ships the same CRD under crds/
// is pathological, but SSA on two identical objects is not idempotent enough
// to rely on — the second apply of the same GVK/name is a conflict — so we
// drop the crds/ copy defensively and let the templated one win.
func dedupeCRDs(crdObjs, existing []*unstructured.Unstructured) []*unstructured.Unstructured {
	if len(existing) == 0 {
		return crdObjs
	}
	seen := make(map[string]struct{}, len(existing))
	for _, o := range existing {
		seen[objKey(o)] = struct{}{}
	}
	out := crdObjs[:0:0]
	for _, o := range crdObjs {
		if _, dup := seen[objKey(o)]; dup {
			continue
		}
		out = append(out, o)
	}
	return out
}

// objKey identifies an object by apiVersion, kind, namespace and name — the
// tuple that makes a server-side-apply target unique.
func objKey(o *unstructured.Unstructured) string {
	return o.GetAPIVersion() + "|" + o.GetKind() + "|" + o.GetNamespace() + "|" + o.GetName()
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

// substituteValues returns a copy of v with the D15 gateway substitution
// (substitute, in expose.go) applied to every string leaf, recursing
// through nested maps and slices — the shapes CUE/YAML/JSON decoding
// produces for chart values. Non-string, non-container leaves (numbers,
// bools, nil) pass through unchanged. A zero gw is a no-op (substitute
// already short-circuits on it), so this is safe to call unconditionally.
func substituteValues(v any, gw config.GatewaySpec) any {
	if gw.Host == "" {
		return v
	}
	switch t := v.(type) {
	case string:
		return substitute(t, gw)
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, vv := range t {
			out[k] = substituteValues(vv, gw)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, vv := range t {
			out[i] = substituteValues(vv, gw)
		}
		return out
	default:
		return v
	}
}
