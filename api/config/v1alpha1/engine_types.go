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

	// Version, when set, is asserted against this binary's embedded Flux
	// distribution: a value that differs from the embedded version is
	// rejected (CUBE-BST-008). Empty selects the embedded version. It does
	// not select or fetch a different Flux — the embedded asset is
	// authoritative in M7.
	Version string `json:"version,omitempty"`

	// Source points the engine's sync at a location. When set, bootstrap
	// wires the engine's Flux source + Kustomization CRs; when absent,
	// bootstrap installs the engine without a sync.
	Source *EngineSource `json:"source,omitempty"`
}

// EngineSourceKind selects the Flux source backend for the engine's sync.
type EngineSourceKind string

const (
	// EngineSourceGit syncs from a Git repository (Flux GitRepository).
	EngineSourceGit EngineSourceKind = "git"
	// EngineSourceOCI syncs from an OCI artifact (Flux OCIRepository).
	EngineSourceOCI EngineSourceKind = "oci"
)

// EngineSource points the gitops engine's sync at a location. Kind selects the
// Flux source kind — git or oci, an explicit discriminator rather than URL
// sniffing — and each kind pairs its source CR with a Kustomization that
// applies Path on Interval. Public URLs only in M7: credential Secrets return
// when a real consumer needs them.
type EngineSource struct {
	// Kind selects the source backend: "git" (default) or "oci".
	Kind EngineSourceKind `json:"kind,omitempty"`

	// URL is the source location: a Git URL for kind git, an oci:// reference
	// for kind oci.
	URL string `json:"url"`

	// Ref pins the revision: a Git branch for kind git (default "main"), an OCI
	// tag for kind oci (default "latest").
	Ref string `json:"ref,omitempty"`

	// Path is the directory the Kustomization applies (default "./").
	Path string `json:"path,omitempty"`

	// Interval is the reconcile interval for the source and Kustomization
	// (default "10m").
	Interval string `json:"interval,omitempty"`
}
