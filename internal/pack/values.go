package pack

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"math"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"k8s.io/apimachinery/pkg/runtime"

	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
)

// instanceValues assembles the values one setup entry supplies to its pack:
// the valuesRef document as the base, with the inline values merged over it.
//
// Nothing is validated against the pack here. This step decides what the values
// *are*; whether the pack accepts them is resolveValues' answer, so a pack with
// no #Values and a pack with a strict one both receive the same merged map.
func instanceValues(ctx context.Context, spec v1alpha1.PackSpec, resolve resolveDocumentFunc) (map[string]any, error) {
	base, err := baseValues(ctx, spec.ValuesRef, resolve)
	if err != nil {
		return nil, err
	}
	patch, err := inlineValues(spec.Values)
	if err != nil {
		return nil, err
	}
	merged := base
	if patch != nil {
		merged = mergePatch(base, patch)
	}
	values, _ := normalizeNumbers(merged).(map[string]any)
	return values, nil
}

// maxExactInt is the largest magnitude a float64 represents exactly. Past it a
// decoded number is already approximate, so calling it an integer would be a
// claim the input does not support.
const maxExactInt = 1 << 53

// normalizeNumbers rewrites whole-number float64 values as int64, in place.
//
// Both values sources decode through JSON, which has one number type, so
// `replicas: 3` arrives as float64(3). CUE reads that as the float 3.0, which
// does not satisfy `replicas: int` — so without this a correctly written value
// is rejected by the pack's own #Values, and the author is told their input is
// wrong when it was not. A genuine 1.5 stays a float.
func normalizeNumbers(value any) any {
	switch v := value.(type) {
	case map[string]any:
		for name, item := range v {
			v[name] = normalizeNumbers(item)
		}
		return v
	case []any:
		for i, item := range v {
			v[i] = normalizeNumbers(item)
		}
		return v
	case float64:
		if v == math.Trunc(v) && math.Abs(v) <= maxExactInt {
			return int64(v)
		}
		return v
	default:
		return value
	}
}

// baseValues resolves valuesRef in single-document mode. The document must be
// exactly one YAML mapping: a stream of several is ambiguous about which one
// configures the pack, and guessing is the class of silence this domain removes.
func baseValues(ctx context.Context, reference string, resolve resolveDocumentFunc) (map[string]any, error) {
	if reference == "" {
		return nil, nil
	}
	data, err := resolve(ctx, reference)
	if err != nil {
		// The reference resolver's coded error already names the reference;
		// only the field it came from is context the caller lacks.
		return nil, fmt.Errorf("valuesRef: %w", err)
	}
	docs, err := decodeDocuments(data)
	if err != nil || len(docs) != 1 {
		return nil, newValuesDocumentError(reference, len(docs), err)
	}
	return docs[0], nil
}

// inlineValues decodes the setup's inline values, which reach this package as
// the opaque JSON api/ deliberately keeps them as. A nil result means there is
// no patch to apply, which is different from an empty one: an empty mapping is
// a patch that changes nothing.
func inlineValues(raw *runtime.RawExtension) (map[string]any, error) {
	if raw == nil || len(raw.Raw) == 0 {
		return nil, nil
	}
	var doc map[string]any
	if err := json.Unmarshal(raw.Raw, &doc); err != nil {
		return nil, newInlineValuesError(err)
	}
	return doc, nil
}

// mergePatch applies patch over base as an RFC 7386 JSON merge patch: a null
// value deletes its key, a mapping merges recursively, and every other value —
// an array included — replaces what was there.
//
// Arrays replacing rather than merging is the rule that makes the operation
// predictable: there is no key to merge list elements by, so any element-wise
// rule would be a guess about the author's intent.
func mergePatch(base, patch map[string]any) map[string]any {
	merged := maps.Clone(base)
	if merged == nil {
		merged = make(map[string]any, len(patch))
	}
	for name, value := range patch {
		switch v := value.(type) {
		case nil:
			delete(merged, name)
		case map[string]any:
			sub, _ := merged[name].(map[string]any)
			merged[name] = mergePatch(sub, v)
		default:
			merged[name] = v
		}
	}
	return merged
}

// resolveValues validates the supplied values against the pack's #Values
// definition and returns them with that definition's defaults filled in.
//
// #Values is a closed CUE definition, and that closedness is the lockdown a
// pack author gets: an undeclared field is rejected rather than silently
// carried. A pack that declares no #Values passes its values through
// unchanged — a documented sharp edge, since there is then no schema to reject
// a typo against (docs/domains/pack.md).
//
// Values are meaningless to a raw pack, which has no templating step to
// consume them, so supplying any is an error the declared type makes
// detectable before anything is rendered.
func (p *Pack) resolveValues(values map[string]any) (map[string]any, error) {
	if len(values) == 0 {
		return p.defaults()
	}
	if p.meta.Type == TypeRaw {
		return nil, newValuesOnRawPackError()
	}
	if !p.values.Exists() {
		return maps.Clone(values), nil
	}

	// A one-off context is deliberate: cue.Value.Context is deprecated, and
	// values built in different contexts unify correctly.
	unified := p.values.Unify(cuecontext.New().Encode(values))
	if err := unified.Validate(cue.Concrete(true), cue.Final()); err != nil {
		return nil, newValuesRejectedError(cueDetails(err))
	}
	return decodeValues(unified)
}

// defaults returns the pack's own #Values defaults, with no user values
// merged in. A #Values that leaves a required field open is reported the same
// way a bad user value is: the values do not satisfy the definition.
func (p *Pack) defaults() (map[string]any, error) {
	if !p.values.Exists() {
		return nil, nil
	}
	if err := p.values.Validate(cue.Concrete(true), cue.Final()); err != nil {
		return nil, newValuesRejectedError(cueDetails(err))
	}
	return decodeValues(p.values)
}

func decodeValues(v cue.Value) (map[string]any, error) {
	out := map[string]any{}
	if err := v.Decode(&out); err != nil {
		return nil, newValuesRejectedError(cueDetails(err))
	}
	return out, nil
}
