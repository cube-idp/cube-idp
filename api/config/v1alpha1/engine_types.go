package v1alpha1

// EngineProvider identifies a tier-2 gitops engine backend — the
// coordinator that owns steady-state pack installation. It selects the
// engine only: the tier-1 Flux substrate is invariant platform and is
// never selectable.
type EngineProvider string

// EngineProviderFlux selects Flux as the tier-2 engine — the default and
// the only admitted value today, the degenerate case where the invariant
// substrate doubles as the engine.
const EngineProviderFlux EngineProvider = "flux"

// EngineSpec declares the cube's tier-2 gitops engine: user-selected at
// day 0 and immutable for the cube's lifetime — no handover, migration,
// or engine-switching semantics exist. An absent spec.engine means the
// flux default (an engine is mandatory), so this sub-struct only pins or
// overrides.
type EngineSpec struct {
	// Provider selects the tier-2 engine only — the invariant tier-1
	// substrate is never selectable. The choice is immutable per cube;
	// mechanical enforcement of a change against an existing cube lands
	// with a second driver. Defaults to "flux", the only admitted value
	// today ("argo" is additive at its own design gate).
	Provider EngineProvider `json:"provider,omitempty"`

	// Version, when set, asserts the selected engine's version — in
	// clean SemVer spelling, never v-prefixed. For flux that is the
	// substrate's pinned release (degenerate: the substrate doubles as
	// the engine), asserted at the edge (CUBE-ENG-005) before any
	// apply; empty selects the embedded version. It does not select or
	// fetch a different engine — the embedded content is authoritative.
	Version string `json:"version,omitempty"`

	// Source points the engine's sync at a location. When set, the
	// engine driver emits the sync wiring (for flux: the source +
	// Kustomization CR pair) and bootstrap applies it; when absent, the
	// engine installs without a sync.
	Source *EngineSource `json:"source,omitempty"`
}

// EngineSourceKind selects the source backend for the engine's sync.
type EngineSourceKind string

const (
	// EngineSourceGit syncs from a Git repository (Flux GitRepository).
	EngineSourceGit EngineSourceKind = "git"
	// EngineSourceOCI syncs from an OCI artifact (Flux OCIRepository).
	EngineSourceOCI EngineSourceKind = "oci"
)

// EngineSource points the gitops engine's sync at a location — shared
// sync-wiring vocabulary every driver consumes. Kind selects the source
// kind — git or oci, an explicit discriminator rather than URL sniffing —
// and each kind pairs its source CR with a Kustomization that applies
// Path on Interval. Public URLs only today: the credential hook
// (secretRef) is reserved for the trust design gate (#142).
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
