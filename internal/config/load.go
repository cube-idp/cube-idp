package config

import (
	_ "embed"
	"fmt"
	"os"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"gopkg.in/yaml.v3"

	"github.com/cube-idp/cube-idp/internal/diag"
)

//go:embed schema.cue
var schemaCUE string

// Schema returns the embedded CUE schema source cube.yaml is validated
// against, surfaced by `cube-idp config schema` (the command every CUBE-0002
// remediation points at).
func Schema() string { return schemaCUE }

func cuePath(s string) cue.Path { return cue.ParsePath(s) }

func Load(path string) (*Cube, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, diag.Wrap(err, diag.CodeConfigRead, fmt.Sprintf("cannot read %s", path),
			"run `cube-idp init` to generate a starter cube.yaml")
	}
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, diag.Wrap(err, diag.CodeConfigInvalid, fmt.Sprintf("%s is not valid YAML", path),
			"fix the YAML syntax; run `cube-idp config schema` for the expected shape")
	}

	// Spoke cross-checks run BEFORE CUE validation: schema.cue's narrow
	// spoke provider enum (*"kind" | "existing" — deliberately not widened,
	// GT6) would otherwise reject a k3d spoke first with a generic
	// CUBE-0002 "empty disjunction"; the contract is the typed CUBE-8001
	// with the spoke-specific fix line. The probe decode is best-effort — a
	// document too malformed to probe skips this pass and gets CUE's
	// canonical CUBE-0002 below.
	var probe struct {
		Spec struct {
			Spokes []SpokeSpec `yaml:"spokes"`
		} `yaml:"spec"`
	}
	if err := yaml.Unmarshal(raw, &probe); err == nil {
		if err := validateSpokes(probe.Spec.Spokes); err != nil {
			return nil, err
		}
	}

	ctx := cuecontext.New()
	schema := ctx.CompileString(schemaCUE).LookupPath(cuePath("#Cube"))
	val := schema.Unify(ctx.Encode(doc))
	if err := val.Validate(); err != nil {
		return nil, diag.Wrap(err, diag.CodeConfigInvalid, fmt.Sprintf("%s failed validation", path),
			"run `cube-idp config schema` to see allowed fields and values")
	}

	var c Cube
	if err := val.Decode(&c); err != nil { // decodes with CUE defaults applied
		return nil, diag.Wrap(err, diag.CodeConfigInvalid, fmt.Sprintf("%s failed validation", path),
			"run `cube-idp config schema` to see allowed fields and values")
	}
	normalizePackValues(&c)
	if err := crossValidate(&c); err != nil {
		return nil, err
	}
	// kubernetesVersion has no CUE default (an explicit version with
	// provider "existing" must be rejected above, D10/spec §4.1), so the
	// documented default for cluster-creating providers is applied here
	// instead. k3d shares kind's default (both are local dev clusters).
	if (c.Spec.Cluster.Provider == "kind" || c.Spec.Cluster.Provider == "k3d") && c.Spec.Cluster.KubernetesVersion == "" {
		c.Spec.Cluster.KubernetesVersion = "v1.33.1"
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
		if len(cl.ExtraPorts) > 0 || len(cl.Mounts) > 0 || cl.ProviderConfig != "" || cl.KubernetesVersion != "" {
			return diag.New(diag.CodeClusterFieldsConflict,
				"cluster.extraPorts/mounts/providerConfig/kubernetesVersion imply node creation and are not valid with provider: existing",
				"remove those fields, or switch to provider: kind or k3d")
		}
	}
	if c.Spec.Engine.Type == "argocd" {
		for _, p := range c.Spec.Packs {
			if strings.Contains(p.Ref, "packs/argocd") {
				return diag.New(diag.CodeArgoPackRedun,
					"the argocd pack is redundant when engine.type is argocd (the engine installs Argo CD, UI included)",
					"remove the argocd pack from spec.packs")
			}
		}
	}
	return nil
}

// validateSpokes enforces the spoke rules (GT6): providers kind|existing
// only (k3d spokes are deferred), existing requires a context, names are
// unique. Typed CUBE-8001 with a spoke-specific fix line, not a generic
// CUBE-0002 schema failure. Called from Load's pre-CUE probe pass — see
// the comment there for why this cannot live in crossValidate.
func validateSpokes(spokes []SpokeSpec) error {
	seen := map[string]bool{}
	for _, s := range spokes {
		if seen[s.Name] {
			return diag.New(diag.CodeSpokeProviderUnsupported,
				fmt.Sprintf("duplicate spoke name %q", s.Name),
				"spoke names must be unique within a cube")
		}
		seen[s.Name] = true
		switch s.Cluster.Provider {
		case "", "kind": // absent provider takes the CUE default "kind"
		case "existing":
			if s.Cluster.Context == "" {
				return diag.New(diag.CodeSpokeProviderUnsupported,
					fmt.Sprintf("spoke %q: provider \"existing\" requires cluster.context", s.Name),
					"set spec.spokes[].cluster.context to the spoke's kubeconfig context")
			}
		default:
			return diag.New(diag.CodeSpokeProviderUnsupported,
				fmt.Sprintf("spoke %q: provider %q is not supported for spokes", s.Name, s.Cluster.Provider),
				"spokes support provider: kind or existing in this release (k3d spokes are deferred)")
		}
	}
	return nil
}
