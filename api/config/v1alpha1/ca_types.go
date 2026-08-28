package v1alpha1

// CAProvider identifies who provides the cube's certificate authority.
type CAProvider string

// CAProviderCube selects the cube-owned, stdlib-minted CA — the default
// and the only admitted value in M11. "user", "cert-manager", and
// "kubernetes" (PodCertificateRequest, GA v1.37) are named future
// providers, each admitted at its own design gate.
const CAProviderCube CAProvider = "cube"

// CASpec configures who provides the cube's certificate authority. It is
// optional: an absent spec.ca means the "cube" provider — the CA is
// fabric, like the engine and the gateway, not opt-in.
//
// There is deliberately no opaque forProvider: no provider-specific knob
// exists, and an empty opaque payload is ceremony. The gate that admits a
// second provider migrates the shape if and as needed.
type CASpec struct {
	// Provider selects the CA provider. Defaults to "cube", the only
	// admitted value today. The choice is immutable per cube — a provider
	// change on a live cube implies trust rotation, which M11 freezes;
	// mechanical enforcement of a change against an existing cube lands
	// with the second provider's gate.
	Provider CAProvider `json:"provider,omitempty"`
}
