// Package-file for the valuesRef half of GT15: fetching a remote base
// values document and merging inline values over it (spec 2026-07-19 §5.1).
//
// IMPLEMENTATION NOTE (T5 finding): the plan's sketch had this file import
// internal/refval, but internal/refval imports internal/pack (for FetchFile),
// so that is an import cycle — `imports .../internal/pack from refval.go:
// import cycle not allowed`. The observable contract is unchanged: the shared
// machinery the design's G5 actually names (ref grammar, cache, auth, guards)
// is FetchFile, which lives in THIS package, and RFC 7386 is the same
// jsonpatch.MergePatch primitive refval.Merge wraps. Only refval's
// NormalizeIntegral is mirrored here (as the unexported normalizeIntegral),
// behaviour-for-behaviour.
package pack

import (
	"context"
	"encoding/json"
	"fmt"
	"math"

	jsonpatch "github.com/evanphx/json-patch/v5"
	sigyaml "sigs.k8s.io/yaml"

	"github.com/cube-idp/cube-idp/internal/config"
	"github.com/cube-idp/cube-idp/internal/diag"
)

// EffectiveValues resolves valuesRef (pack ref grammar, one YAML mapping)
// and RFC 7386 merge-patches inline over it: null deletes, arrays replace —
// the same ladder direction as forProvider over providerConfigRef. The
// result is int-normalized for the Helm consumer (normalizeIntegral, the
// JSON-round-trip cousin of config.Load's normalizePackValues). No ref
// → inline passes through untouched with no pin.
func EffectiveValues(ctx context.Context, valuesRef string, inline map[string]any, cacheDir string) (map[string]any, string, error) {
	if valuesRef == "" {
		return inline, "", nil
	}
	base, pin, err := resolveValuesDoc(ctx, valuesRef, cacheDir)
	if err != nil {
		return nil, "", diag.Wrap(err, diag.CodePackValuesRefFetch,
			fmt.Sprintf("cannot fetch valuesRef %q", valuesRef),
			"the ref must resolve to one readable YAML mapping document (helm values shape)")
	}
	merged, err := mergeValuesPatch(base, inline)
	if err != nil {
		return nil, "", diag.Wrap(err, diag.CodePackValuesRefFetch,
			fmt.Sprintf("cannot merge inline values over valuesRef %q", valuesRef),
			"check that both documents are plain YAML mappings")
	}
	return normalizeIntegral(merged).(map[string]any), pin, nil
}

// RenderResolved is the shared render entry for up.Run and diff's
// desiredState: the GT15 chartless guard extended to valuesRef (checked
// BEFORE any fetch — no network on a doomed render), EffectiveValues, then
// RenderWith. Returns the rendered objects plus the values pin for
// cube.lock's valuesPin column.
func RenderResolved(ctx context.Context, pk *Pack, pref config.PackRef, gw config.GatewaySpec, cacheDir string) (*Rendered, string, error) {
	if pref.ValuesRef != "" && !pk.HasChart() {
		return nil, "", diag.New(diag.CodePackValuesChartless,
			fmt.Sprintf("pack %s has no chart.yaml — valuesRef/values are helm values only (GT15)", pk.Name),
			"use extraManifests to add raw resources, or remove valuesRef")
	}
	values, pin, err := EffectiveValues(ctx, pref.ValuesRef, pref.Values, cacheDir)
	if err != nil {
		return nil, "", err
	}
	r, err := pk.RenderWith(values, pref.ExtraManifests, gw)
	if err != nil {
		return nil, "", err
	}
	return r, pin, nil
}

// resolveValuesDoc is refval.Resolve's body with the in-package FetchFile
// call (see the package-file note above): fetch one YAML document, decode it
// JSON-typed, reject anything that is not a mapping. Errors carry the pack
// layer's own diag codes; EffectiveValues wraps them with CUBE-4021.
func resolveValuesDoc(ctx context.Context, ref, cacheDir string) (map[string]any, string, error) {
	raw, pin, err := FetchFile(ctx, ref, cacheDir)
	if err != nil {
		return nil, "", err
	}
	j, err := sigyaml.YAMLToJSON(raw)
	if err != nil {
		return nil, "", fmt.Errorf("ref %q is not valid YAML: %w", ref, err)
	}
	var m map[string]any
	if err := json.Unmarshal(j, &m); err != nil {
		return nil, "", fmt.Errorf("ref %q is not a YAML mapping document: %w", ref, err)
	}
	if m == nil { // empty file decodes to JSON null
		m = map[string]any{}
	}
	return m, pin, nil
}

// mergeValuesPatch applies patch onto base per RFC 7386 — the same
// jsonpatch.MergePatch primitive refval.Merge and compose.Merge ride, so the
// values ladder and the forProvider ladder agree by construction. Inputs stay
// untouched; the result is never a nil map.
func mergeValuesPatch(base, patch map[string]any) (map[string]any, error) {
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

// normalizeIntegral mirrors refval.NormalizeIntegral (same bounds, same
// in-place recursion): float64 leaves holding integral values become int.
// JSON round-trips type every number float64; Helm values want plain ints,
// the same reason config.Load runs normalizePackValues over inline values.
func normalizeIntegral(v any) any {
	switch t := v.(type) {
	case float64:
		if t == math.Trunc(t) && t >= math.MinInt32 && t <= math.MaxInt32 {
			return int(t)
		}
		return t
	case map[string]any:
		for k, vv := range t {
			t[k] = normalizeIntegral(vv)
		}
		return t
	case []any:
		for i, vv := range t {
			t[i] = normalizeIntegral(vv)
		}
		return t
	default:
		return v
	}
}
