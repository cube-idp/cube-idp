package config

import (
	_ "embed"
	"fmt"
	"os"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"gopkg.in/yaml.v3"

	"github.com/rafpe/cube-idp/internal/diag"
)

//go:embed schema.cue
var schemaCUE string

func cuePath(s string) cue.Path { return cue.ParsePath(s) }

func Load(path string) (*Cube, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, diag.Wrap(err, "CUBE-0001", fmt.Sprintf("cannot read %s", path),
			"run `cube-idp init` to generate a starter cube.yaml")
	}
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, diag.Wrap(err, "CUBE-0002", fmt.Sprintf("%s is not valid YAML", path),
			"fix the YAML syntax; run `cube-idp config schema` for the expected shape")
	}

	ctx := cuecontext.New()
	schema := ctx.CompileString(schemaCUE).LookupPath(cuePath("#Cube"))
	val := schema.Unify(ctx.Encode(doc))
	if err := val.Validate(); err != nil {
		return nil, diag.Wrap(err, "CUBE-0002", fmt.Sprintf("%s failed validation", path),
			"run `cube-idp config schema` to see allowed fields and values")
	}

	var c Cube
	if err := val.Decode(&c); err != nil { // decodes with CUE defaults applied
		return nil, diag.Wrap(err, "CUBE-0002", fmt.Sprintf("%s failed validation", path),
			"run `cube-idp config schema` to see allowed fields and values")
	}
	normalizePackValues(&c)
	if err := crossValidate(&c); err != nil {
		return nil, err
	}
	return &c, nil
}

// normalizePackValues rewrites int64 (CUE's default Go type for decoded
// integers) to plain int within each pack's Values map, so callers can
// compare against ordinary int literals instead of having to know CUE's
// internal number representation.
func normalizePackValues(c *Cube) {
	for i := range c.Spec.Packs {
		c.Spec.Packs[i].Values = normalizeAny(c.Spec.Packs[i].Values).(map[string]any)
	}
}

func normalizeAny(v any) any {
	switch t := v.(type) {
	case int64:
		return int(t)
	case map[string]any:
		for k, vv := range t {
			t[k] = normalizeAny(vv)
		}
		return t
	case []any:
		for i, vv := range t {
			t[i] = normalizeAny(vv)
		}
		return t
	default:
		return v
	}
}

// crossValidate enforces rules CUE can't express cleanly across fields.
func crossValidate(c *Cube) error {
	cl := c.Spec.Cluster
	if cl.Provider == "existing" {
		if len(cl.ExtraPorts) > 0 || len(cl.Mounts) > 0 || cl.ProviderConfig != "" {
			return diag.New("CUBE-1003",
				"cluster.extraPorts/mounts/providerConfig imply node creation and are not valid with provider: existing",
				"remove those fields, or switch to provider: kind")
		}
	}
	return nil
}
