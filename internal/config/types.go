// Package config loads and validates cube-idp's cube.yaml configuration
// file, applying schema defaults (via CUE) and cross-field checks that CUE
// cannot express cleanly.
package config

// Cube is the root of a cube.yaml document.
type Cube struct {
	APIVersion string   `yaml:"apiVersion" json:"apiVersion"`
	Kind       string   `yaml:"kind" json:"kind"`
	Metadata   Metadata `yaml:"metadata" json:"metadata"`
	Spec       Spec     `yaml:"spec" json:"spec"`
}

// Metadata identifies the Cube profile.
type Metadata struct {
	Name string `yaml:"name" json:"name"`
}

// Spec is the body of a Cube document.
type Spec struct {
	Cluster ClusterSpec `yaml:"cluster" json:"cluster"`
	Engine  EngineSpec  `yaml:"engine" json:"engine"`
	Gateway GatewaySpec `yaml:"gateway" json:"gateway"`
	// Packs is optional (`packs?` in schema.cue); omitempty keeps a nil or
	// empty slice out of marshaled output instead of an explicit `packs:
	// null`, which CUE re-validation would reject (see ClusterSpec).
	Packs []PackRef `yaml:"packs,omitempty" json:"packs,omitempty"`
}

// ClusterSpec configures the local or remote Kubernetes cluster cube-idp
// targets.
type ClusterSpec struct {
	Provider          string        `yaml:"provider" json:"provider"`                             // "kind" | "existing"
	Context           string        `yaml:"context,omitempty" json:"context,omitempty"`           // for existing
	KubernetesVersion string        `yaml:"kubernetesVersion,omitempty" json:"kubernetesVersion,omitempty"`
	// omitempty on the nil-able optional fields (ExtraPorts and Mounts here;
	// Mirrors/Insecure inside RegistrySpec; Spec.Packs and PackRef.Values)
	// matters, not just for tidy output: sigs.k8s.io/yaml.Marshal
	// (cmd/init.go) would otherwise write explicit `extraPorts: null` /
	// `mounts: null` for their nil zero values, and CUE's re-validation on
	// the next Load rejects an explicit null against a `[...]`/`{...}`-typed
	// optional field (mismatched types list/map and null) — every `cube-idp
	// init`-generated cube.yaml would fail to load. Optional strings
	// (Context, KubernetesVersion, ProviderConfig) marshal as "" rather than
	// null, so their omitempty is cosmetic only. Registry carries no
	// omitempty: it is a non-pointer struct, on which the tag is a no-op —
	// the real fix lives on RegistrySpec's own fields.
	ExtraPorts     []PortMapping `yaml:"extraPorts,omitempty" json:"extraPorts,omitempty"`         // D10 layer 1
	Registry       RegistrySpec  `yaml:"registry" json:"registry"`                                 // D10 layer 1
	Mounts         []Mount       `yaml:"mounts,omitempty" json:"mounts,omitempty"`                 // D10 layer 1
	ProviderConfig string        `yaml:"providerConfig,omitempty" json:"providerConfig,omitempty"` // D10 layer 2: file path or inline YAML
}

// PortMapping maps a host port to a kind node port.
type PortMapping struct {
	HostPort int32 `yaml:"hostPort" json:"hostPort"`
	NodePort int32 `yaml:"nodePort" json:"nodePort"`
}

// RegistrySpec configures registry mirrors and insecure registries for the
// cluster provider.
type RegistrySpec struct {
	Mirrors  map[string]string `yaml:"mirrors,omitempty" json:"mirrors,omitempty"`
	Insecure []string          `yaml:"insecure,omitempty" json:"insecure,omitempty"`
}

// Mount describes a host path mounted into cluster nodes.
type Mount struct {
	HostPath string `yaml:"hostPath" json:"hostPath"`
	NodePath string `yaml:"nodePath" json:"nodePath"`
}

// EngineSpec selects the GitOps reconciliation engine.
type EngineSpec struct {
	Type string `yaml:"type" json:"type"` // "flux" | "argocd"
}

// GatewaySpec configures the ingress/gateway pack.
type GatewaySpec struct {
	Pack string `yaml:"pack" json:"pack"`
	Host string `yaml:"host" json:"host"`
	Port int    `yaml:"port" json:"port"`
	// Ref overrides the pack source `up` fetches for the gateway pack. When
	// unset, `up` falls back to "packs/<Pack>" (a repo-local checkout path,
	// only valid when cube-idp runs from a checkout); `cube-idp init
	// --local` fills this with an absolute path so `up` works from any cwd.
	Ref string `yaml:"ref,omitempty" json:"ref,omitempty"`
}

// PackRef resolves the pack source `up`/`diff` fetch for the gateway pack:
// an explicit g.Ref always wins; otherwise it falls back to
// "packs/<Pack>", a path that only resolves when cube-idp runs from a
// checkout of its own repo. `cube-idp init --local <repo>` sets Ref to an
// absolute path so callers work from any working directory.
func (g GatewaySpec) PackRef() string {
	if g.Ref != "" {
		return g.Ref
	}
	return "packs/" + g.Pack
}

// PackRef references an installable pack and its values overrides.
type PackRef struct {
	Ref string `yaml:"ref" json:"ref"`
	// Values holds pack value overrides. Numeric entries are normalized by
	// Load to int/float64 (never int64, CUE's raw decode type). omitempty:
	// see the ClusterSpec comment above — a nil Values map must round-trip
	// as an absent key, not an explicit YAML null, or re-validation against
	// schema.cue's `values?: {...}` fails.
	Values map[string]any `yaml:"values,omitempty" json:"values,omitempty"`
}

// Default returns the D9 default profile that `cube-idp init` writes:
// kind cluster, flux engine, traefik gateway, gitea + argocd packs.
func Default(name string) *Cube {
	return &Cube{
		APIVersion: "cube-idp.dev/v1alpha1",
		Kind:       "Cube",
		Metadata:   Metadata{Name: name},
		Spec: Spec{
			Cluster: ClusterSpec{Provider: "kind", KubernetesVersion: "v1.33.1"},
			Engine:  EngineSpec{Type: "flux"},
			Gateway: GatewaySpec{Pack: "traefik", Host: "cube-idp.localtest.me", Port: 8443},
			Packs: []PackRef{
				{Ref: "oci://ghcr.io/cube-idp/packs/gitea:0.1.0"},
				{Ref: "oci://ghcr.io/cube-idp/packs/argocd:0.1.0"},
			},
		},
	}
}
