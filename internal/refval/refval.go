// Package refval resolves a "one YAML document" ref — the pack ref grammar
// (local path, oci://, bare git@rev, git::/s3::/http(s) getter forms) — to
// a JSON-typed map plus its reproducibility pin. It is the shared resolver
// behind cluster.providerConfigRef (compose), packs[].valuesRef, and
// remote -f (spec 2026-07-19 §4). Errors pass through
// the pack layer's diag codes untouched; each consumer wraps with its own
// domain code (CUBE-1005 / CUBE-4021 / CUBE-0015).
package refval

import (
	"context"
	"encoding/json"
	"fmt"
	"math"

	jsonpatch "github.com/evanphx/json-patch/v5"
	sigyaml "sigs.k8s.io/yaml"

	"github.com/cube-idp/cube-idp/internal/pack"
)

// Resolve fetches ref and decodes it to a JSON-typed map. An empty ref
// resolves to an empty, non-nil map and no pin so callers need no special
// case. A document that is valid YAML but not a mapping is an error —
// every consumer (values, tuning, provider config, cube.yaml) is
// object-shaped.
func Resolve(ctx context.Context, ref, cacheDir string) (map[string]any, string, error) {
	if ref == "" {
		return map[string]any{}, "", nil
	}
	raw, pin, err := pack.FetchFile(ctx, ref, cacheDir)
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

// Merge applies patch onto base per RFC 7386: maps deep-merge, lists
// replace wholesale, null deletes. Inputs stay untouched. (Lifted verbatim
// from compose.Merge, which now delegates here — one merge algorithm for
// every inline-over-fetched ladder.)
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

// NormalizeIntegral rewrites float64 leaves that hold integral values back
// to int, recursively. JSON round-trips (Resolve, Merge) type every number
// float64; Helm values want plain ints (the same reason config.Load
// normalizes CUE's int64 — see normalizePackValues). NOT for engine tuning:
// unstructured SSA forbids plain int, so tuning keeps JSON typing.
func NormalizeIntegral(v any) any {
	switch t := v.(type) {
	case float64:
		if t == math.Trunc(t) && t >= math.MinInt32 && t <= math.MaxInt32 {
			return int(t)
		}
		return t
	case map[string]any:
		for k, vv := range t {
			t[k] = NormalizeIntegral(vv)
		}
		return t
	case []any:
		for i, vv := range t {
			t[i] = NormalizeIntegral(vv)
		}
		return t
	default:
		return v
	}
}
