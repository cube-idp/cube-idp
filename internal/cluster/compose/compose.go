// Package compose implements the provider-agnostic half of the cluster
// customization ladder (spec 2026-07-18-cluster-forprovider-design.md §4-§5):
// layer 1 (providerConfigRef, fetched) + layer 2 (forProvider, RFC 7386
// merge-patched on top), returned as a plain JSON-typed map. Compose knows
// nothing about kind or k3d and never validates provider fields — that is
// the provider's strict decode (layer 3-4 live in kindp/k3dp).
package compose

import (
	"context"
	"fmt"

	"github.com/cube-idp/cube-idp/internal/diag"
	"github.com/cube-idp/cube-idp/internal/refval"
)

// Resolve fetches ref (pack ref grammar, one YAML file — refval.Resolve)
// and decodes it to a JSON-typed map plus its pin. An empty ref resolves to
// an empty, non-nil map so Merge and the provider decode need no special
// case. Every failure wraps as CUBE-1005 with the pack-layer cause preserved.
func Resolve(ctx context.Context, ref, cacheDir string) (map[string]any, string, error) {
	m, pin, err := refval.Resolve(ctx, ref, cacheDir)
	if err != nil {
		return nil, "", diag.Wrap(err, diag.CodeProviderConfigRefFetch,
			fmt.Sprintf("cannot fetch providerConfigRef %q", ref),
			"the ref must resolve to one readable YAML mapping document; inspect with `cube-idp config render-cluster`")
	}
	return m, pin, nil
}

// Merge applies patch onto base per RFC 7386 (decision 4). One algorithm
// for every inline-over-fetched ladder — the implementation lives in refval.
func Merge(base, patch map[string]any) (map[string]any, error) {
	return refval.Merge(base, patch)
}

// Compose is Resolve + Merge: the full generic half of the ladder, plus
// the pin `up` records in cube.lock's cluster section (spec 2026-07-19 §6).
func Compose(ctx context.Context, ref string, forProvider map[string]any, cacheDir string) (map[string]any, string, error) {
	base, pin, err := Resolve(ctx, ref, cacheDir)
	if err != nil {
		return nil, "", err
	}
	if len(forProvider) == 0 {
		return base, pin, nil
	}
	m, err := Merge(base, forProvider)
	return m, pin, err
}
