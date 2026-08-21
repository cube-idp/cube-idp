package pack

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// clusterScopedKinds are the built-in Kubernetes kinds that live outside any
// namespace. The set mirrors the well-known cluster-scoped list kustomize
// carries for exactly this purpose (sigs.k8s.io/kustomize, the
// clusterScopedKinds/openapi scope data its namespace transformer consults) —
// deciding scope from a live API server would need discovery, and rendering
// stays a pure function of its inputs.
//
// The sharp edge that follows is documented in docs/domains/pack.md: a
// cluster-scoped custom resource whose kind is not listed here is treated as
// namespaced and gets pack.namespace injected. Every core cluster-scoped kind
// is covered, and a pack author controls their own manifests, so the tradeoff
// is deliberate.
var clusterScopedKinds = map[string]bool{
	"APIService":                       true,
	"CSIDriver":                        true,
	"CSINode":                          true,
	"CertificateSigningRequest":        true,
	"ClusterRole":                      true,
	"ClusterRoleBinding":               true,
	"ComponentStatus":                  true,
	"CustomResourceDefinition":         true,
	"FlowSchema":                       true,
	"IngressClass":                     true,
	"MutatingWebhookConfiguration":     true,
	"Namespace":                        true,
	"Node":                             true,
	"PersistentVolume":                 true,
	"PriorityClass":                    true,
	"PriorityLevelConfiguration":       true,
	"RuntimeClass":                     true,
	"SelfSubjectAccessReview":          true,
	"SelfSubjectRulesReview":           true,
	"StorageClass":                     true,
	"SubjectAccessReview":              true,
	"TokenReview":                      true,
	"ValidatingAdmissionPolicy":        true,
	"ValidatingAdmissionPolicyBinding": true,
	"ValidatingWebhookConfiguration":   true,
	"VolumeAttachment":                 true,
}

// applyNamespace forces every namespaced object into ns, in place. It is a
// post-render transform over already-rendered objects so that every pack type
// gets identical behavior — the namespace semantics belong to the pack
// contract, not to any one render backend.
//
// Cluster-scoped objects are left untouched. A namespaced object with no
// namespace gets ns; one that already declares ns is already correct; one that
// declares a different namespace is a conflict, because silently overriding an
// author's explicit choice is the kind of quiet override this contract exists
// to remove.
func applyNamespace(objs []*unstructured.Unstructured, ns string) error {
	if ns == "" {
		return nil
	}
	for _, obj := range objs {
		if clusterScopedKinds[obj.GetKind()] {
			continue
		}
		switch got := obj.GetNamespace(); got {
		case "", ns:
			obj.SetNamespace(ns)
		default:
			return newNamespaceConflictError(obj.GetKind(), obj.GetName(), got, ns)
		}
	}
	return nil
}
