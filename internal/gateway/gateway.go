// Package gateway owns the bootstrap-phase trust fabric: the ingress
// gateway's embedded prerequisite packs, the cube-authored platform
// objects that give it a home and a stable name, the Gateway API
// Gateway carrying the cube's wildcard listener, the CoreDNS marker
// block that makes the cube's hostnames resolve in cluster, and the
// readiness predicates for its own declared CRs. It is pure in the
// substrate's mold: it emits objects, blocks, and judgments, and never
// performs I/O — applying anything and reading or writing the live
// CoreDNS ConfigMap stay at the CLI/orchestrator edge. Certificate
// custody is not this domain's (see docs/domains/ca.md); this domain
// only exports the Secret names the edge injects there. It parses and
// emits its own pack content without importing internal/pack; dogfood
// tests at the composition edge enforce the equivalence. Contract:
// docs/domains/gateway.md.
package gateway

import (
	v1alpha1 "github.com/cube-idp/cube-idp/api/config/v1alpha1"
)

// Namespace is the invariant gateway namespace: where the gateway
// implementation, the CA Secrets, and the emitted Gateway live. The edge
// injects it as a string; a test ties the fact to the Namespace object
// PlatformObjects emits.
const Namespace = "gateway-system"

// Name is the cube's one platform gateway identity, carried by two kinds
// that cannot collide: the stable ExternalName Service DNS resolves, and
// the emitted Gateway that M12 routes attach to by name.
const Name = "gateway"

// ImplementationID is the effective instance id of the compiled default
// gateway implementation — the traefik prerequisite pack's name, and so
// the name of every object its render carries.
//
// The effective id IS the well-known unit name, so it is single-sourced
// from api/config's prerequisite vocabulary rather than respelled here:
// the list entry a user writes, the embedded pack that entry resolves to,
// and the stable Service's backend reference are one string, and the
// domain that owns what the unit means does not get a second spelling of
// what it is called. It appears in no live Corefile: it reaches DNS only
// through the stable Service's spec.externalName.
const ImplementationID = v1alpha1.PrerequisiteTraefikGateway

// GatewayClassName is the GatewayClass the emitted Gateway attaches to:
// the class the compiled default implementation's chart creates. It is a
// compiled default, not config — swapping the implementation re-points it
// and the stable Service's backend together, at that gate.
const GatewayClassName = "traefik"

// CASecretName is the Secret the cube's certificate authority is kept in.
// It is a gateway platform fact the edge injects into internal/ca by
// value; the two domains never import each other.
const CASecretName = "cube-idp-ca"

// LeafSecretName is the Secret the wildcard leaf certificate is kept in.
// The emitted Gateway's certificateRefs names it, which is what ties this
// fact to content.
const LeafSecretName = "gateway-tls"

// clusterSuffix is the in-cluster Service DNS suffix, without a trailing dot.
const clusterSuffix = "svc.cluster.local"

// ServiceFQDN is the stable gateway Service's fully qualified,
// DNS-absolute name — the CoreDNS rewrite target. The trailing dot is
// load-bearing: the rewrite rule's replacement is an absolute name, and
// it is why this spelling is built separately from implementationFQDN
// rather than derived from it.
const ServiceFQDN = Name + "." + Namespace + "." + clusterSuffix + "."

// implementationFQDN is the stable Service's ExternalName backend: the
// compiled default implementation's Service, relative (no trailing dot) —
// the spelling Kubernetes expects in spec.externalName.
const implementationFQDN = ImplementationID + "." + Namespace + "." + clusterSuffix
