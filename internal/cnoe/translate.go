package cnoe

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/cube-idp/cube-idp/internal/diag"
	"github.com/cube-idp/cube-idp/internal/pack"
)

// translateApplication converts a single Argo Application document into an
// App. fileDir is the directory the document was loaded from — cnoe://
// paths resolve relative to it (idpbuilder semantics).
func translateApplication(doc *unstructured.Unstructured, fileDir string) (*App, error) {
	name := doc.GetName()
	repoURL, _, _ := unstructured.NestedString(doc.Object, "spec", "source", "repoURL")
	rev, _, _ := unstructured.NestedString(doc.Object, "spec", "source", "targetRevision")
	srcPath, _, _ := unstructured.NestedString(doc.Object, "spec", "source", "path")
	chart, _, _ := unstructured.NestedString(doc.Object, "spec", "source", "chart")
	destNS, _, _ := unstructured.NestedString(doc.Object, "spec", "destination", "namespace")
	app := &App{Name: name, Namespace: destNS}

	switch {
	case strings.HasPrefix(repoURL, "cnoe://"):
		rel := strings.TrimPrefix(repoURL, "cnoe://")
		dir := filepath.Join(fileDir, filepath.FromSlash(rel))
		if srcPath != "" && srcPath != "." {
			dir = filepath.Join(dir, filepath.FromSlash(srcPath))
		}
		abs, err := filepath.Abs(dir)
		if err != nil {
			return nil, diag.Wrap(err, diag.CodePackCnoeUnres, "cannot resolve cnoe path for "+name, "check the repoURL")
		}
		if _, err := os.Stat(abs); err != nil {
			return nil, diag.New(diag.CodePackCnoeUnres,
				fmt.Sprintf("application %q points at cnoe://%s but %s does not exist", name, rel, abs),
				"cnoe:// paths are relative to the Application YAML's directory — fix the path")
		}
		app.CnoeDir = abs

	case chart != "":
		app.Helm = &pack.ChartRef{Chart: chart, Repo: repoURL, Version: rev, ReleaseName: name, Namespace: destNS}

	case strings.HasPrefix(repoURL, "https://") || strings.HasPrefix(repoURL, "http://"):
		if rev == "" || rev == "HEAD" || strings.Contains(rev, "*") {
			return nil, diag.New(diag.CodePackCnoeInvalid,
				fmt.Sprintf("application %q tracks git revision %q, which cube-idp cannot pin", name, rev),
				"set spec.source.targetRevision to a tag or full commit SHA, then re-import")
		}
		host := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(repoURL, "https://"), "http://"), ".git")
		app.GitRef = host + "//" + srcPath + "@" + rev

	default:
		return nil, diag.New(diag.CodePackCnoeInvalid,
			fmt.Sprintf("application %q has an unsupported repoURL %q", name, repoURL),
			"supported: cnoe://<relative-dir>, https git repos with pinned revisions, and helm chart sources")
	}
	return app, nil
}

// expandApplicationSet supports the list generator (the only one idpbuilder
// setups commonly use); everything else is rejected loudly.
func expandApplicationSet(doc *unstructured.Unstructured, fileDir string) ([]App, error) {
	name := doc.GetName()
	gens, _, _ := unstructured.NestedSlice(doc.Object, "spec", "generators")
	tmpl, ok, _ := unstructured.NestedMap(doc.Object, "spec", "template")
	if !ok || len(gens) == 0 {
		return nil, diag.New(diag.CodePackCnoeInvalid, fmt.Sprintf("applicationset %q has no generators or template", name),
			"add a list generator and a template, or split it into plain Applications")
	}
	var apps []App
	for _, g := range gens {
		gm, _ := g.(map[string]any)
		listGen, ok := gm["list"].(map[string]any)
		if !ok {
			return nil, diag.New(diag.CodePackCnoeInvalid,
				fmt.Sprintf("applicationset %q uses the %q generator, which cube-idp does not support (only list generators)",
					name, generatorName(gm)),
				"expand the ApplicationSet into plain Applications and re-import")
		}
		elements, _ := listGen["elements"].([]any)
		for _, el := range elements {
			vars, _ := el.(map[string]any)
			rendered := substitute(tmpl, vars)
			appDoc := &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "argoproj.io/v1alpha1", "kind": "Application",
				"metadata": rendered["metadata"], "spec": rendered["spec"],
			}}
			app, err := translateApplication(appDoc, fileDir)
			if err != nil {
				return nil, err
			}
			apps = append(apps, *app)
		}
	}
	return apps, nil
}

// generatorName names the generator a rejected ApplicationSet entry uses
// (e.g. "clusters", "git"): the first non-"list" key in the generator map,
// sorted for determinism. Falls back to "unknown" for an empty or opaque
// entry (or a "list" key holding a non-map value).
func generatorName(gm map[string]any) string {
	keys := make([]string, 0, len(gm))
	for k := range gm {
		if k != "list" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	if len(keys) > 0 {
		return keys[0]
	}
	return "unknown"
}

// substitute deep-copies v, replacing {{key}} placeholders in every string.
func substitute(v map[string]any, vars map[string]any) map[string]any {
	out := make(map[string]any, len(v))
	for k, val := range v {
		out[k] = substituteAny(val, vars)
	}
	return out
}

func substituteAny(v any, vars map[string]any) any {
	switch t := v.(type) {
	case string:
		s := t
		for k, val := range vars {
			s = strings.ReplaceAll(s, "{{"+k+"}}", fmt.Sprint(val))
		}
		return s
	case map[string]any:
		return substitute(t, vars)
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = substituteAny(e, vars)
		}
		return out
	default:
		return v
	}
}
