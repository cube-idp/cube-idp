package gateway

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// PlatformObjects returns the gateway-platform unit's two cube-authored
// objects, in apply order: the gateway Namespace, and the stable
// ExternalName Service that fronts whatever implementation serves. Both
// carry their own metadata.namespace — only pack-rendered output is
// deliberately namespace-less and stamped at the edge.
func PlatformObjects() []*unstructured.Unstructured {
	return []*unstructured.Unstructured{gatewayNamespace(), stableService()}
}

// gatewayNamespace builds the namespace every gateway object, the CA
// Secrets, and the emitted Gateway share.
func gatewayNamespace() *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "Namespace",
		"metadata":   map[string]any{"name": Namespace},
	}}
}

// stableService builds the cube's one predictable gateway name: an
// ExternalName alias to the compiled default implementation's Service.
//
// The mechanism is DNS-layer by design — the in-cluster redirect and every
// future route target are names, not endpoints — and it is what keeps an
// implementation swap a one-field change in this object rather than a
// live-Corefile edit. The backend spelling is relative (no trailing dot):
// that is what Kubernetes expects in spec.externalName, and it is
// deliberately not the DNS-absolute ServiceFQDN the CoreDNS block targets.
func stableService() *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "Service",
		"metadata":   map[string]any{"name": Name, "namespace": Namespace},
		"spec": map[string]any{
			"type":         "ExternalName",
			"externalName": implementationFQDN,
		},
	}}
}
