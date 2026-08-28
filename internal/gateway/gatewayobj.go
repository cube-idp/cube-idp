package gateway

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// GatewayObject returns the cube-authored Gateway API Gateway for a cube's
// base domain: one HTTPS listener terminating TLS for *.<domain> with the
// leaf Secret as its certificateRef. It is emitted beside the traefik
// pack's render — cube-authored, never part of a pack render — and so it
// carries its own metadata.namespace.
//
// Three properties are decided rather than incidental. There is one HTTPS
// listener and no plaintext pair: no cube-owned application endpoints
// exist until M12, so a plaintext listener would serve nothing. There is
// no allowedRoutes key at all — route attachment is deliberately unstated
// until M12 owns route wiring, so adding it must be a deliberate act. And
// the Secret shares gateway-system with the Gateway, which is why the
// same-namespace certificateRefs entry needs no ReferenceGrant.
func GatewayObject(domain string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "gateway.networking.k8s.io/v1",
		"kind":       "Gateway",
		"metadata":   map[string]any{"name": Name, "namespace": Namespace},
		"spec": map[string]any{
			"gatewayClassName": GatewayClassName,
			"listeners": []any{map[string]any{
				"name":     "websecure",
				"protocol": "HTTPS",
				// int64, not int: an unstructured object may hold only
				// JSON-native types, and apimachinery's deep copy panics
				// on an int.
				"port":     int64(443),
				"hostname": "*." + domain,
				"tls": map[string]any{
					"mode": "Terminate",
					"certificateRefs": []any{map[string]any{
						"kind": "Secret",
						"name": LeafSecretName,
					}},
				},
			}},
		},
	}}
}
