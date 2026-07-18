package config

import (
	_ "embed"
	"fmt"
	"os"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"gopkg.in/yaml.v3"
	sigyaml "sigs.k8s.io/yaml"

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

	// Migration guard (decision 3, spec 2026-07-18-cluster-forprovider-design
	// §3): providerConfig was replaced in this release. Probed pre-CUE like
	// the spoke checks — the closed schema would otherwise reject the key
	// with a generic CUBE-0002 instead of the migration recipe.
	var legacy struct {
		Spec struct {
			Cluster struct {
				ProviderConfig string `yaml:"providerConfig"`
			} `yaml:"cluster"`
			Spokes []struct {
				Cluster struct {
					ProviderConfig string `yaml:"providerConfig"`
				} `yaml:"cluster"`
			} `yaml:"spokes"`
		} `yaml:"spec"`
	}
	if err := yaml.Unmarshal(raw, &legacy); err == nil {
		found := legacy.Spec.Cluster.ProviderConfig != ""
		for _, s := range legacy.Spec.Spokes {
			found = found || s.Cluster.ProviderConfig != ""
		}
		if found {
			return nil, diag.New(diag.CodeProviderConfigRemoved,
				"cluster.providerConfig has been replaced by providerConfigRef and forProvider",
				"a file path becomes providerConfigRef: <path>; an inline YAML blob becomes structured fields under forProvider:; run `cube-idp config schema` for the shape")
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

// SaveValidated writes cube to file with init's writer shape
// (sigs.k8s.io/yaml + 0o644), validating the candidate through Load — the
// exact schema + cross-field checks `up` applies — via a temp file in the
// same directory before it replaces the original, so a rejected mutation
// leaves the file untouched. Lifted from cmd's pack-install writer (W2.T11)
// so every config-mutating command (pack install, spoke add/remove) shares
// one save path.
func SaveValidated(file string, cube *Cube) error {
	raw, err := sigyaml.Marshal(cube)
	if err != nil {
		return err
	}
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	if _, err := Load(tmp); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, file)
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
		if len(cl.ExtraPorts) > 0 || len(cl.Mounts) > 0 ||
			cl.ProviderConfigRef != "" || len(cl.ForProvider) > 0 || cl.KubernetesVersion != "" {
			return diag.New(diag.CodeClusterFieldsConflict,
				"cluster.extraPorts/mounts/providerConfigRef/forProvider/kubernetesVersion imply node creation and are not valid with provider: existing",
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
	// P7 (the gitea guarantee, decision 13): a delivery: repo pack needs
	// gitea present, and gitea itself can never be repo-delivered — its
	// repo would have to be served by the very pack being delivered. Gitea
	// presence is matched by the same ref-substring convention
	// cmd/init.go's filterSelectedPacks/packCatalogName use (a ref
	// containing the catalog name — OCI refs and --local checkout paths
	// both carry "gitea" as the pack directory/image name).
	hasGitea := false
	for _, p := range c.Spec.Packs {
		if strings.Contains(p.Ref, "gitea") {
			hasGitea = true
			if p.Delivery == "repo" {
				return diag.New(diag.CodeRepoDeliveryConfig,
					"the gitea pack cannot use delivery: repo (its repo would live in the gitea it is itself delivering)",
					"remove delivery: repo from the gitea pack — it always delivers as an OCI artifact")
			}
		}
	}
	for _, p := range c.Spec.Packs {
		if p.Delivery == "repo" && !hasGitea {
			return diag.New(diag.CodeRepoDeliveryConfig,
				fmt.Sprintf("pack %q has delivery: repo but the gitea pack is not in spec.packs", p.Ref),
				"add the gitea pack or use delivery: oci")
		}
	}
	// U2 (decision 3): the opt-in plain-HTTP host port must not collide with
	// the HTTPS gateway port or any typed extraPorts mapping — each host
	// port can be bound once.
	if hp := c.Spec.Gateway.HTTPPort; hp != 0 {
		if hp == c.Spec.Gateway.Port {
			return diag.New(diag.CodeConfigInvalid,
				"gateway.httpPort must differ from gateway.port and extraPorts",
				fmt.Sprintf("spec.gateway.httpPort (%d) equals spec.gateway.port — pick a distinct host port for the plain-HTTP listener (e.g. 8080)", hp))
		}
		for _, ep := range c.Spec.Cluster.ExtraPorts {
			if int(ep.HostPort) == hp {
				return diag.New(diag.CodeConfigInvalid,
					"gateway.httpPort must differ from gateway.port and extraPorts",
					fmt.Sprintf("spec.gateway.httpPort (%d) is already mapped by spec.cluster.extraPorts — pick a distinct host port or drop that extraPorts entry", hp))
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
