// traefik-gateway — the embedded thin-helm gateway pack (M11), deliberate M9
// dogfooding: it carries chart coordinates, never chart content, and the
// substrate's own helm-controller reconciles it in cluster. The chart is
// digest-pinned; the digest is what pins, the tag is for legibility.
//
// Every value below is a compile-time constant, by contract (the R1
// resolution, 2026-08-28): nothing cube-derived is baked here, so this pack
// can be handed to a different operator on a different cluster unchanged.
// Everything cube-derived crosses as the domain-emitted Gateway object
// beside this render, not as a chart value.
//
// gateway.enabled: false disables the chart's own Gateway and listeners —
// the cube authors its own. The chart's GatewayClass is still created under
// that setting (verified against 41.3.0), which is what gatewayClassName
// "traefik" on the emitted Gateway attaches to.
//
// service.type stays the chart default (LoadBalancer), deliberately unset
// here: reachability is the hostPort path (D5d — labeled node, hostPorts
// 80/443, nodeSelector), so the implementation Service is never dialed for
// ingress, and on kind it simply sits <pending> — harmless. Setting it to
// ClusterIP would be a value change with no requirement behind it.
name:      "traefik-gateway"
version:   "41.3.0"
type:      "helm"
category:  "gateway"
namespace: "gateway-system"

chart: {
	kind:    "oci"
	url:     "oci://ghcr.io/traefik/helm/traefik"
	version: "41.3.0"
	digest:  "sha256:dcae2d586d7fbda6a08150eaeeca4132e9dd042d8a4d16ada287e8c40f6ff17a"
}

#Values: {
	providers: kubernetesGateway: enabled: true
	gateway: enabled:                      false
	ports: {
		web: hostPort:       80
		websecure: hostPort: 443
	}
	nodeSelector: "ingress-ready": "true"
	fullnameOverride: "traefik-gateway"
}
