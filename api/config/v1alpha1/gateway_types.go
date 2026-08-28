package v1alpha1

// DefaultBaseDomain is the single compile-time base domain every cube's
// default hostname derives from: spec.gateway.domain falls back to
// <metadata.name>.cube.test. It is RFC 2606-reserved — no OS or mDNS
// collision, unlike .local — and deliberately recompilable: an operator
// building their own binary rebases every cube's default domain by
// editing this one constant.
const DefaultBaseDomain = "cube.test"

// GatewaySpec configures the cube's trust-fabric gateway. It is optional:
// an absent spec.gateway means the gateway is installed with defaults —
// the gateway is fundamental fabric, not opt-in, and has no off-switch
// (the engine precedent).
type GatewaySpec struct {
	// Domain is the cube's base domain: the gateway serves, and the leaf
	// certificate covers, the wildcard *.<domain>. Empty defaults to
	// <metadata.name>.<DefaultBaseDomain>. It must be a valid DNS name
	// (lowercase RFC 1123 labels).
	Domain string `json:"domain,omitempty"`
}
