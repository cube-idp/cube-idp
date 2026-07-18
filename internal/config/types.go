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
	// Spokes is optional (`spokes?` in schema.cue); same omitempty
	// discipline as Packs — a nil slice must round-trip as an absent key,
	// not an explicit YAML null.
	Spokes []SpokeSpec `yaml:"spokes,omitempty" json:"spokes,omitempty"`
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
	// Tuning optionally patches the engine's embedded install manifests
	// before SSA (GT1, U3): a closed knob set — components.<name>.replicas
	// and components.<name>.resources only. These are NOT helm values (the
	// vocabulary stone, GT15): the engine installs from pre-rendered plain
	// manifests, so tuning is an in-memory walk-and-set, never a chart
	// re-render. nil = absent; omitempty keeps the round-trip discipline of
	// PackRef.Values (an absent key, not an explicit YAML null).
	Tuning *EngineTuning `yaml:"tuning,omitempty" json:"tuning,omitempty"`
}

// EngineTuning is the closed engine.tuning knob set (GT1). Component names
// are validated against the engine's actual Deployments when the manifests
// are rendered (engine.ApplyTuning) — an unknown name is a typed CUBE-3009
// listing the valid ones, never a silent ignore.
type EngineTuning struct {
	Components map[string]ComponentTuning `yaml:"components,omitempty" json:"components,omitempty"`
}

// ComponentTuning tunes one engine Deployment: spec.replicas and every
// container's resources. Replicas nil = untouched. Resources replaces each
// container's resources block verbatim (k8s ResourceRequirements shape);
// numeric leaves keep CUE's int64 decode type — deliberately NOT normalized
// to int like PackRef.Values, because the consumer is unstructured SSA
// (DeepCopyJSONValue accepts int64, not int), not helm.
type ComponentTuning struct {
	Replicas  *int           `yaml:"replicas,omitempty" json:"replicas,omitempty"`
	Resources map[string]any `yaml:"resources,omitempty" json:"resources,omitempty"`
}

// GatewayNodePort is the node port every cluster-creating provider (kindp,
// k3dp) must map the host gateway port onto; the traefik starter pack's
// service pins the same value (packs/traefik/chart.yaml
// ports.websecure.nodePort, HTTPS, Phase 2 Task 9). Defined here rather than
// in internal/cluster: cluster's provider factory imports kindp/k3dp, so
// kindp/k3dp importing internal/cluster back for this constant would be an
// import cycle; internal/config has no such cycle and every party already
// imports it.
const GatewayNodePort = 30443

// GatewayHTTPNodePort is GatewayNodePort's plain-HTTP twin: the node port
// BOTH gateway packs already pin in-cluster for their HTTP listener
// (packs/traefik/chart.yaml ports.web.nodePort,
// packs/envoy-gateway/manifests/10-gatewayclass.yaml's data-plane Service).
// The host side is mapped onto it only when the opt-in spec.gateway.httpPort
// is set (U2, decision 3) — absent means no mapping and byte-identical
// cluster config to before; the packs need no change either way.
const GatewayHTTPNodePort = 30080

// GatewaySpec configures the ingress/gateway pack.
type GatewaySpec struct {
	Pack string `yaml:"pack" json:"pack"`
	Host string `yaml:"host" json:"host"`
	Port int    `yaml:"port" json:"port"`
	// HTTPPort optionally maps a host port onto the gateway's plain-HTTP
	// listener (GatewayHTTPNodePort, 30080). Opt-in per decision 3: zero =
	// absent = no mapping. Cluster-shape field — like Port it is baked in
	// at cluster creation (recreate to change). Must differ from Port and
	// from every cluster.extraPorts hostPort (CUBE-0002 at load).
	HTTPPort int `yaml:"httpPort,omitempty" json:"httpPort,omitempty"`
	// Ref is the pack source `up` fetches for the gateway pack. `init`
	// always fills it: the published oci://ghcr.io/cube-idp/packs/<pack>
	// ref by default (P4, F12 closed), or an absolute path into a local
	// cube-idp/packs checkout with `init --local`. When unset (hand-written
	// cube.yaml), `up` falls back to "packs/<Pack>" — a checkout-relative
	// last resort that only resolves when run from a packs checkout root.
	Ref string `yaml:"ref,omitempty" json:"ref,omitempty"`
}

// PackRef resolves the pack source `up`/`diff` fetch for the gateway pack:
// an explicit g.Ref always wins; otherwise it falls back to
// "packs/<Pack>", a path that only resolves when cube-idp runs from the
// root of a packs checkout (cube-idp/packs) — the documented last resort
// for checkout users; anywhere else the fetch fails cleanly.
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
	// GT15 (the values stone): values are HELM values, only, always —
	// consumed exclusively by the pack's chart.yaml render. Setting them on
	// a chartless pack is CUBE-4016 at render time.
	Values map[string]any `yaml:"values,omitempty" json:"values,omitempty"`
	// ExtraManifests is GT15's uniform extras channel, valid for every pack
	// kind: a multi-doc YAML string that RenderWith parses,
	// ${GATEWAY_*}-substitutes, and appends after the pack's own objects
	// (CUBE-4017 when it is not valid YAML). A pack installed with
	// non-empty Values or ExtraManifests is CUSTOMIZED in its D11 record
	// (`kubectl get packs`). omitempty: absent must round-trip as an absent
	// key — schema.cue's `extraManifests?: string & !=""` rejects an
	// explicit empty string, same discipline as Values above.
	ExtraManifests string `yaml:"extraManifests,omitempty" json:"extraManifests,omitempty"`
	// Delivery selects how `up` hands this pack to the engine (P7, decision
	// 4/13): "" or "oci" (the default) pushes the render to zot and
	// registers an OCI source; "repo" pushes the render into a Gitea repo
	// (cube-pack-<name>) and registers a git source instead — the payoff is
	// an editable, in-cluster fork (edit in the Gitea UI, the engine
	// reconciles; cube.yaml stays the source of truth and a re-run `up`
	// re-syncs the repo's manifests/ to the render). Guarded at load: repo
	// delivery requires the gitea pack in spec.packs, and gitea itself can
	// never be repo-delivered (CUBE-7304). omitempty: absent must
	// round-trip as an absent key — schema.cue's `delivery?: "oci"|"repo"`
	// rejects an explicit "" (same discipline as Values/ExtraManifests).
	Delivery string `yaml:"delivery,omitempty" json:"delivery,omitempty"`
}

// SpokeSpec declares a managed spoke cluster (spec §5, Phase 5). cube-idp
// only bootstraps and registers spokes — delivering workloads to them is
// engine content, never packs. Provider is limited to kind|existing in v1
// (GT6); k3d spokes need a shared docker network and are deferred.
type SpokeSpec struct {
	Name    string      `yaml:"name" json:"name"`
	Cluster ClusterSpec `yaml:"cluster" json:"cluster"`
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
			// P4 (F12 closed): the gateway pack resolves from the published
			// packs monorepo — the downloaded binary needs no checkout.
			Gateway: GatewaySpec{Pack: "traefik", Host: "cube-idp.localtest.me", Port: 8443,
				Ref: "oci://ghcr.io/cube-idp/packs/traefik:0.2.0"},
			Packs: []PackRef{
				{Ref: "oci://ghcr.io/cube-idp/packs/gitea:0.2.0"},
				{Ref: "oci://ghcr.io/cube-idp/packs/argocd:0.2.0"},
			},
		},
	}
}
