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
	Provider          string `yaml:"provider" json:"provider"`                   // "kind" | "existing"
	Context           string `yaml:"context,omitempty" json:"context,omitempty"` // for existing
	KubernetesVersion string `yaml:"kubernetesVersion,omitempty" json:"kubernetesVersion,omitempty"`
	// omitempty on the nil-able optional fields (ExtraPorts and Mounts here;
	// Mirrors/Insecure inside RegistrySpec; Spec.Packs and PackRef.Values)
	// matters, not just for tidy output: sigs.k8s.io/yaml.Marshal
	// (cmd/init.go) would otherwise write explicit `extraPorts: null` /
	// `mounts: null` for their nil zero values, and CUE's re-validation on
	// the next Load rejects an explicit null against a `[...]`/`{...}`-typed
	// optional field (mismatched types list/map and null) — every `cube-idp
	// init`-generated cube.yaml would fail to load. Optional strings
	// (Context, KubernetesVersion) marshal as "" rather than
	// null, so their omitempty is cosmetic only. Registry carries no
	// omitempty: it is a non-pointer struct, on which the tag is a no-op —
	// the real fix lives on RegistrySpec's own fields.
	// ExtraPorts, Registry and Mounts are the typed sugar layer of the
	// provider-config merge (ADR-0011): the common operator needs get
	// first-class cube.yaml fields, everything else goes through the
	// provider-native escape hatch below.
	ExtraPorts []PortMapping `yaml:"extraPorts,omitempty" json:"extraPorts,omitempty"`
	Registry   RegistrySpec  `yaml:"registry" json:"registry"`
	Mounts     []Mount       `yaml:"mounts,omitempty" json:"mounts,omitempty"`
	// ProviderConfigRef is the base provider-native document layer (spec
	// 2026-07-18-cluster-forprovider-design.md §3): a base provider-native
	// config fetched by pack ref grammar (local path, oci://, git, s3,
	// http) resolving to exactly one YAML file. ForProvider merge-patches
	// over it (RFC 7386); typed sugar and core injections apply after.
	ProviderConfigRef string `yaml:"providerConfigRef,omitempty" json:"providerConfigRef,omitempty"`
	// ForProvider carries provider-native fields inline (kind
	// v1alpha4.Cluster / k3d SimpleConfig shape). Open at load (CUE
	// `{...}`); the provider strict-decodes it at render time so unknown
	// fields fail config-time with the field name, never
	// kubeadm-time. omitempty: absent must round-trip as an absent key —
	// same discipline as Packs/Values (see the ClusterSpec comment above).
	ForProvider map[string]any `yaml:"forProvider,omitempty" json:"forProvider,omitempty"`
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

// EngineSpec selects the GitOps reconciliation engine and its install pack
// (engine-as-pack spec 2026-07-19).
type EngineSpec struct {
	Type string `yaml:"type" json:"type"` // "flux" | "argocd"
	// Ref optionally overrides the engine pack source (any pack ref form:
	// local dir, oci://, git). Unset = the published default for Type
	// (defaultEngineRefs). The fetched pack's declared name must be
	// PackName() — CUBE-0013 at fetch time (pack.VerifyEnginePackRef).
	Ref string `yaml:"ref,omitempty" json:"ref,omitempty"`
	// Values holds the engine pack's chart values — the OPEN,
	// operator-in-control replacement for the retired engine.tuning
	// (ADR-0019):
	// consumed exclusively by the pack's chart.yaml render, merged over its
	// baked defaults. Same normalization + omitempty discipline as
	// PackRef.Values.
	Values map[string]any `yaml:"values,omitempty" json:"values,omitempty"`
	// SelfManage opts the engine into managing its own install from zot
	// (ADR-0020): after the health gate, `up` pushes the rendered engine pack
	// as the cube-engine artifact and attaches an engine-native self-source
	// with pruning disabled — the engine reconciles itself from then on, so
	// drift between `up`s is corrected. First install and unhealthy-at-start
	// recovery still SSA directly. Sourced from zot only,
	// never Gitea; works offline.
	SelfManage bool `yaml:"selfManage,omitempty" json:"selfManage,omitempty"`
}

// defaultEngineRefs pins the published engine pack per engine type — what
// `up`/`diff` fetch when spec.engine.ref is unset.
var defaultEngineRefs = map[string]string{
	"flux":   "oci://ghcr.io/cube-idp/packs/cube-engine-flux:0.1.0",
	"argocd": "oci://ghcr.io/cube-idp/packs/cube-engine-argocd:0.1.0",
}

// PackName returns the pack name engine.type requires: cube-engine-<type>.
func (e EngineSpec) PackName() string { return "cube-engine-" + e.Type }

// PackRef resolves the engine pack source: an explicit e.Ref always wins;
// otherwise the published default for e.Type. (Unknown Type returns "" —
// unreachable past the factory's CUBE-3001.)
func (e EngineSpec) PackRef() string {
	if e.Ref != "" {
		return e.Ref
	}
	return defaultEngineRefs[e.Type]
}

// GatewayNodePort is the node port every cluster-creating provider (kindp,
// k3dp) must map the host gateway port onto; the traefik starter pack's
// service pins the same value (packs/traefik/chart.yaml
// ports.websecure.nodePort, HTTPS). Defined here rather than
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
// is set — absent means no mapping and byte-identical
// cluster config to before; the packs need no change either way.
const GatewayHTTPNodePort = 30080

// GatewaySpec configures the ingress/gateway pack.
type GatewaySpec struct {
	Pack string `yaml:"pack" json:"pack"`
	Host string `yaml:"host" json:"host"`
	Port int    `yaml:"port" json:"port"`
	// HTTPPort optionally maps a host port onto the gateway's plain-HTTP
	// listener (GatewayHTTPNodePort, 30080). Opt-in: zero =
	// absent = no mapping. Cluster-shape field — like Port it is baked in
	// at cluster creation (recreate to change). Must differ from Port and
	// from every cluster.extraPorts hostPort (CUBE-0002 at load).
	HTTPPort int `yaml:"httpPort,omitempty" json:"httpPort,omitempty"`
	// Ref is the pack source `up` fetches for the gateway pack. `init`
	// always fills it: the published oci://ghcr.io/cube-idp/packs/<pack>
	// ref by default, or an absolute path into a local
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
	// Values are HELM values, only, always (ADR-0004) —
	// consumed exclusively by the pack's chart.yaml render. Setting them on
	// a chartless pack is CUBE-4016 at render time.
	Values map[string]any `yaml:"values,omitempty" json:"values,omitempty"`
	// ExtraManifests is the uniform extras channel, valid for every pack
	// kind: a multi-doc YAML string that RenderWith parses,
	// ${GATEWAY_*}-substitutes, and appends after the pack's own objects
	// (CUBE-4017 when it is not valid YAML). A pack installed with
	// non-empty Values or ExtraManifests is CUSTOMIZED in its Pack record
	// (`kubectl get packs`). omitempty: absent must round-trip as an absent
	// key — schema.cue's `extraManifests?: string & !=""` rejects an
	// explicit empty string, same discipline as Values above.
	ExtraManifests string `yaml:"extraManifests,omitempty" json:"extraManifests,omitempty"`
	// Delivery selects how `up` hands this pack to the engine (ADR-0006):
	// "" or "oci" (the default) pushes the render to zot and
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
	// DependsOn lists pack NAMES (pack.cue name — never refs; DD1) that
	// must be delivered, and per engine semantics healthy, before this
	// pack (spec 2026-07-19 §3.1). Unioned with the pack's own pack.cue
	// dependsOn at graph time (pack.ResolveOrder). omitempty: absent must
	// round-trip as an absent key, not an explicit YAML null — same
	// discipline as Values above.
	DependsOn []string `yaml:"dependsOn,omitempty" json:"dependsOn,omitempty"`
}

// SpokeSpec declares a managed spoke cluster (ADR-0013). cube-idp
// only bootstraps and registers spokes — delivering workloads to them is
// engine content, never packs. Provider is limited to kind|existing in v1;
// k3d spokes need a shared docker network and are deferred.
type SpokeSpec struct {
	Name    string      `yaml:"name" json:"name"`
	Cluster ClusterSpec `yaml:"cluster" json:"cluster"`
}

// Default returns the default profile that `cube-idp init` writes:
// kind cluster, flux engine, traefik gateway, gitea + argocd packs.
func Default(name string) *Cube {
	return &Cube{
		APIVersion: "cube-idp.dev/v1alpha1",
		Kind:       "Cube",
		Metadata:   Metadata{Name: name},
		Spec: Spec{
			Cluster: ClusterSpec{Provider: "kind", KubernetesVersion: "v1.33.1"},
			Engine:  EngineSpec{Type: "flux"},
			// The gateway pack resolves from the published
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
