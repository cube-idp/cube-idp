package v1alpha1

// The well-known prerequisite unit names. api/ states the vocabulary; what
// each unit installs and what the list is for is the gateway domain's
// contract (docs/domains/gateway.md) — api/ never imports internal/gateway.
const (
	// PrerequisiteGatewayPlatform is the built-in unit carrying the
	// cube-owned platform vocabulary: the gateway namespace and the stable
	// gateway Service.
	PrerequisiteGatewayPlatform = "gateway-platform"
	// PrerequisiteGatewayAPICRDs is the cube-shipped Gateway API CRDs pack.
	PrerequisiteGatewayAPICRDs = "gateway-api-crds"
	// PrerequisiteCASecrets is the built-in unit carrying the CA and leaf
	// Secret material minted by the ca domain.
	PrerequisiteCASecrets = "ca-secrets"
	// PrerequisiteTraefikGateway is the cube-shipped gateway pack.
	PrerequisiteTraefikGateway = "traefik-gateway"
)

// PrerequisiteSpec is one ordered entry of the bootstrap prerequisite list.
type PrerequisiteSpec struct {
	// Name identifies the unit. The compiled defaults' names are
	// well-known: "gateway-platform", "gateway-api-crds", "ca-secrets",
	// "traefik-gateway".
	Name string `json:"name"`

	// Ref locates a pack for a pack-shaped unit, in the internal/ref
	// grammar (local tree/file and https today; oci/git at M12). Empty on
	// a well-known cube-shipped pack name selects the embedded copy;
	// forbidden on built-in units; required otherwise.
	Ref string `json:"ref,omitempty"`
}

// isBuiltInPrerequisite reports whether a name selects cube-owned content
// and behavior rather than resolvable pack content. There is nothing to
// point a ref at, so ref is forbidden on these.
func isBuiltInPrerequisite(name string) bool {
	return name == PrerequisiteGatewayPlatform || name == PrerequisiteCASecrets
}

// isWellKnownPrerequisite reports whether a name is one the cube ships
// content or behavior for. A name that is not well-known is an override
// and must carry its own ref.
func isWellKnownPrerequisite(name string) bool {
	return isBuiltInPrerequisite(name) ||
		name == PrerequisiteGatewayAPICRDs ||
		name == PrerequisiteTraefikGateway
}

// defaultPrerequisites returns the compiled default list — the four units
// in their gated order. Materializing it here is the M11-A0 boundary: the
// entries are data, and defaulting is where data lands; the gateway domain
// owns the model's meaning (docs/domains/gateway.md).
func defaultPrerequisites() []PrerequisiteSpec {
	return []PrerequisiteSpec{
		{Name: PrerequisiteGatewayPlatform},
		{Name: PrerequisiteGatewayAPICRDs},
		{Name: PrerequisiteCASecrets},
		{Name: PrerequisiteTraefikGateway},
	}
}
