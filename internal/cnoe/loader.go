// Package cnoe ingests CNOE/idpbuilder Argo Application and ApplicationSet
// YAMLs and translates them into cube-idp deliveries: cnoe:// paths become
// local renders pushed to the in-cluster OCI registry, remote sources become
// cube pack refs — engine-neutral, so both flux and argocd cubes can absorb
// an existing idpbuilder setup (spec §4.4, launch-critical).
package cnoe

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/rafpe/cube-idp/internal/apply"
	"github.com/rafpe/cube-idp/internal/diag"
	"github.com/rafpe/cube-idp/internal/lock"
	"github.com/rafpe/cube-idp/internal/pack"
)

// App is one translated Argo Application (or one expanded ApplicationSet
// element): exactly one of CnoeDir, GitRef, or Helm is set, matching the
// source translateApplication chose.
type App struct {
	Name      string
	Namespace string // Argo destination.namespace ("" = leave objects untouched)
	CnoeDir   string // resolved absolute dir when repoURL is cnoe://
	GitRef    string // translated cube git pack ref for remote git sources
	Helm      *pack.ChartRef
}

// Load scans every *.yaml/*.yml directly in dir for Argo Application and
// ApplicationSet documents (apiVersion argoproj.io/v1alpha1) and translates
// them into Apps; any other document is ignored.
func Load(dir string) ([]App, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, diag.Wrap(err, diag.CodePackCnoeInvalid, fmt.Sprintf("cannot read %s", dir), "pass a directory of Argo Application YAMLs")
	}
	var apps []App
	for _, e := range entries {
		if e.IsDir() || (filepath.Ext(e.Name()) != ".yaml" && filepath.Ext(e.Name()) != ".yml") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, diag.Wrap(err, diag.CodePackCnoeInvalid, "cannot read "+path, "check file permissions")
		}
		docs, err := apply.ParseMultiDoc(raw)
		if err != nil {
			return nil, diag.Wrap(err, diag.CodePackCnoeInvalid, path+" is not valid YAML", "fix the document")
		}
		for _, doc := range docs {
			if doc.GetAPIVersion() != "argoproj.io/v1alpha1" {
				continue
			}
			switch doc.GetKind() {
			case "Application":
				app, err := translateApplication(doc, filepath.Dir(path))
				if err != nil {
					return nil, err
				}
				apps = append(apps, *app)
			case "ApplicationSet":
				expanded, err := expandApplicationSet(doc, filepath.Dir(path))
				if err != nil {
					return nil, err
				}
				apps = append(apps, expanded...)
			}
		}
	}
	return apps, nil
}

// Render produces the deliverable for one app; the tag is a 12-char content
// hash so re-imports of changed sources roll forward automatically.
func (a *App) Render(ctx context.Context, cacheDir string) (*pack.Rendered, error) {
	var objs []*unstructured.Unstructured
	var err error
	switch {
	case a.CnoeDir != "":
		objs, err = renderLocalDir(a.CnoeDir)
	case a.GitRef != "":
		// FetchTree, not Fetch: real idpbuilder Applications point at plain
		// manifest trees that were never authored as cube packs, so there is
		// no pack.cue to load — render the fetched tree exactly like a
		// cnoe:// directory (kustomization.yaml if present, else a walk).
		var dir string
		if dir, err = pack.FetchTree(ctx, a.GitRef, cacheDir); err == nil {
			objs, err = renderLocalDir(dir)
		}
	case a.Helm != nil:
		objs, err = pack.RenderChart(*a.Helm, a.Helm.Values)
	}
	if err != nil {
		return nil, err
	}
	objs = applyDestinationNamespace(objs, a.Namespace)
	h, err := lock.RenderedHash(objs)
	if err != nil {
		return nil, err
	}
	return &pack.Rendered{Name: "cnoe-" + a.Name, Version: strings.TrimPrefix(h, "sha256:")[:12], Objects: objs}, nil
}

// renderLocalDir renders a cnoe:// directory: kustomization.yaml if present,
// otherwise every YAML document in the directory (recursively).
func renderLocalDir(dir string) ([]*unstructured.Unstructured, error) {
	if _, err := os.Stat(filepath.Join(dir, "kustomization.yaml")); err == nil {
		return pack.RenderDir(dir)
	}
	var objs []*unstructured.Unstructured
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || (filepath.Ext(path) != ".yaml" && filepath.Ext(path) != ".yml") {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return diag.Wrap(err, diag.CodePackCnoeInvalid, "cannot read "+path, "check file permissions")
		}
		parsed, err := apply.ParseMultiDoc(raw)
		if err != nil {
			return diag.Wrap(err, diag.CodePackCnoeInvalid, path+" is not valid YAML", "fix the manifest")
		}
		objs = append(objs, parsed...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(objs) == 0 {
		return nil, diag.New(diag.CodePackCnoeUnres, "cnoe path "+dir+" contains no manifests",
			"point the Application's cnoe:// repoURL at a directory of Kubernetes YAML")
	}
	return objs, nil
}

// clusterScoped mirrors Argo's own CreateNamespace behavior: cluster-scoped
// kinds are left untouched even when destination.namespace is set.
var clusterScoped = map[string]bool{
	"Namespace": true, "ClusterRole": true, "ClusterRoleBinding": true,
	"CustomResourceDefinition": true, "StorageClass": true, "PriorityClass": true,
	"IngressClass": true, "GatewayClass": true, "PersistentVolume": true,
	"ValidatingWebhookConfiguration": true, "MutatingWebhookConfiguration": true,
}

// applyDestinationNamespace defaults every namespaced object with no
// namespace of its own to ns, and prepends a Namespace object (Argo's
// CreateNamespace behavior) when ns is set — unless the render already
// carries one: pack.RenderChart prepends a Namespace for chart sources
// (helm.go's hasNamespaceObject path), and manifest trees may declare their
// own, so prepending unconditionally would bake a byte-identical duplicate
// into the artifact (and its content hash).
func applyDestinationNamespace(objs []*unstructured.Unstructured, ns string) []*unstructured.Unstructured {
	if ns == "" {
		return objs
	}
	for _, o := range objs {
		if o.GetNamespace() == "" && !clusterScoped[o.GetKind()] {
			o.SetNamespace(ns)
		}
	}
	for _, o := range objs {
		if o.GetKind() == "Namespace" && o.GetName() == ns {
			return objs // already present — don't duplicate it
		}
	}
	nsObj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "Namespace", "metadata": map[string]any{"name": ns}}}
	return append([]*unstructured.Unstructured{nsObj}, objs...)
}
