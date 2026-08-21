package pack

import (
	"maps"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
)

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
