package v1alpha1

// EngineProvider identifies a gitops engine backend.
type EngineProvider string

// EngineProviderFlux installs Flux as the gitops engine — the mandatory
// default, installed by bootstrap before all packs.
const EngineProviderFlux EngineProvider = "flux"

// EngineSpec declares the gitops engine cube-idp bootstraps. Bootstrap (M7)
// installs it before all packs and hands steady-state ownership to it; an
// absent spec.engine is treated by bootstrap as the Flux default (the
// engine is mandatory), so this sub-struct only pins or overrides.
type EngineSpec struct {
	// Provider selects the engine backend. Defaults to "flux", the only
	// supported value.
	Provider EngineProvider `json:"provider,omitempty"`

	// Version pins the embedded Flux distribution; empty selects the
	// version vendored into the binary at build time.
	Version string `json:"version,omitempty"`

	// Source points the engine's sync at a location. Its concrete shape
	// (git vs OCI) is provisional pending the M7 demo-source decision —
	// do not depend on these fields yet.
	Source *EngineSource `json:"source,omitempty"`
}

// EngineSource is a provisional placeholder for the engine sync location.
// The git-vs-OCI shape is deferred to the M7 demo-source decision; only URL
// is defined so far and its semantics are not yet fixed.
type EngineSource struct {
	// URL is the sync location (a git URL or an OCI ref — provisional).
	URL string `json:"url,omitempty"`
}
