// Package compose implements the provider-agnostic half of the cluster
// customization ladder (spec 2026-07-18-cluster-forprovider-design.md §4-§5):
// layer 1 (providerConfigRef, fetched) + layer 2 (forProvider, RFC 7386
// merge-patched on top), returned as a plain JSON-typed map. Compose knows
// nothing about kind or k3d and never validates provider fields — that is
// the provider's strict decode (layer 3-4 live in kindp/k3dp).
package compose

import (
	"context"
	"encoding/json"
	"fmt"

	jsonpatch "github.com/evanphx/json-patch/v5"
	sigyaml "sigs.k8s.io/yaml"

	"github.com/cube-idp/cube-idp/internal/diag"
	"github.com/cube-idp/cube-idp/internal/pack"
)

// Resolve fetches ref (pack ref grammar, one YAML file — pack.FetchFile)
// and decodes it to a JSON-typed map. An empty ref resolves to an empty,
// non-nil map so Merge and the provider decode need no special case. Every
// failure wraps as CUBE-1005 with the pack-layer cause preserved.
func Resolve(ctx context.Context, ref, cacheDir string) (map[string]any, error) {
	if ref == "" {
		return map[string]any{}, nil
	}
	raw, err := pack.FetchFile(ctx, ref, cacheDir)
	if err != nil {
		return nil, diag.Wrap(err, diag.CodeProviderConfigRefFetch,
			fmt.Sprintf("cannot fetch providerConfigRef %q", ref),
			"the ref must resolve to one readable YAML file; inspect with `cube-idp config render-cluster`")
	}
	j, err := sigyaml.YAMLToJSON(raw)
	if err != nil {
		return nil, diag.Wrap(err, diag.CodeProviderConfigRefFetch,
			fmt.Sprintf("providerConfigRef %q is not valid YAML", ref), "fix the referenced file")
	}
	var m map[string]any
	if err := json.Unmarshal(j, &m); err != nil {
		return nil, diag.Wrap(err, diag.CodeProviderConfigRefFetch,
			fmt.Sprintf("providerConfigRef %q is not a YAML mapping document", ref),
			"the file must contain a single provider config object (e.g. a kind Cluster)")
	}
	if m == nil { // empty file decodes to JSON null
		m = map[string]any{}
	}
	return m, nil
}

// Merge applies patch onto base per RFC 7386 (decision 4): maps deep-merge,
// lists replace wholesale, null deletes. Inputs stay untouched.
func Merge(base, patch map[string]any) (map[string]any, error) {
	bj, err := json.Marshal(base)
	if err != nil {
		return nil, err
	}
	pj, err := json.Marshal(patch)
	if err != nil {
		return nil, err
	}
	mj, err := jsonpatch.MergePatch(bj, pj)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(mj, &m); err != nil {
		return nil, err
	}
	if m == nil {
		m = map[string]any{}
	}
	return m, nil
}

// Compose is Resolve + Merge: the full generic half of the ladder.
func Compose(ctx context.Context, ref string, forProvider map[string]any, cacheDir string) (map[string]any, error) {
	base, err := Resolve(ctx, ref, cacheDir)
	if err != nil {
		return nil, err
	}
	if len(forProvider) == 0 {
		return base, nil
	}
	return Merge(base, forProvider)
}
