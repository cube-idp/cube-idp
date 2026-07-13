package registry

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// NodePort is zot's node-local port: kind nodes pull "registry.<host>/..."
// images through it via containerd certs.d (Phase 2, D6).
const NodePort = 30500

// GatewayRoute exposes zot at registry.<host> through the gateway so the
// developer's docker/oras on the HOST can push with TLS + the cube-idp CA.
// Applied by `up` after the gateway pack is delivered (the HTTPRoute CRD
// arrives with the gateway pack's Gateway API CRDs). The parentRef matches
// the phase-1 Gateway ("cube-idp" in ns "traefik", checkpoint 0.14); it
// crosses namespaces (route lives in cube-idp-system, next to zot), which
// the phase-1 Gateway allows via allowedRoutes: {namespaces: {from: All}}.
// Omitting sectionName means the route attaches to every listener the
// Gateway exposes, including websecure.
func GatewayRoute(host string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "gateway.networking.k8s.io/v1",
		"kind":       "HTTPRoute",
		"metadata":   map[string]any{"name": "cube-idp-registry", "namespace": "cube-idp-system"},
		"spec": map[string]any{
			"parentRefs": []any{map[string]any{"name": "cube-idp", "namespace": "traefik"}},
			"hostnames":  []any{"registry." + host},
			"rules": []any{map[string]any{
				"backendRefs": []any{map[string]any{"name": "zot", "port": int64(5000)}},
			}},
		},
	}}
}
