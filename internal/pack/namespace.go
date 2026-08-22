package pack

import (
	"strings"

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

// crdKind and crdGroup identify the CustomResourceDefinitions a pack bundles.
const (
	crdKind  = "CustomResourceDefinition"
	crdGroup = "apiextensions.k8s.io"
)

// The scopes a CustomResourceDefinition may declare.
const (
	scopeCluster    = "Cluster"
	scopeNamespaced = "Namespaced"
)

// resourceKey identifies a kind of custom resource the way its definition
// does: by API group and kind, never by kind alone. Two groups may define the
// same kind, and only the pair says which definition governs an object.
type resourceKey struct {
	group string
	kind  string
}

// scopeIndex records the scope every CustomResourceDefinition in a pack's own
// rendered output declares for the resources it defines.
//
// It exists because a pack is self-contained: a pack shipping a CRD ships the
// authoritative, offline answer to "is this custom resource cluster-scoped?",
// and reading it costs nothing and needs no cluster. Custom resources whose
// definition the pack does not bundle are still unknown here — they are the
// remaining sharp edge documented in docs/domains/pack.md.
type scopeIndex map[resourceKey]bool

// indexCRDScopes builds the index from the objects a pack rendered. A
// definition that does not spell out a scope this contract recognises is
// skipped rather than guessed at, which leaves its resources on the default.
func indexCRDScopes(objs []*unstructured.Unstructured) scopeIndex {
	scopes := scopeIndex{}
	for _, obj := range objs {
		if obj.GetKind() != crdKind || apiGroup(obj.GetAPIVersion()) != crdGroup {
			continue
		}
		group, _, _ := unstructured.NestedString(obj.Object, "spec", "group")
		kind, _, _ := unstructured.NestedString(obj.Object, "spec", "names", "kind")
		scope, _, _ := unstructured.NestedString(obj.Object, "spec", "scope")
		if kind == "" || scope != scopeCluster && scope != scopeNamespaced {
			continue
		}
		scopes[resourceKey{group: group, kind: kind}] = scope == scopeCluster
	}
	return scopes
}

// clusterScoped reports whether an object lives outside any namespace,
// deciding in three layers: the built-in kinds first, then the definition the
// pack bundles for this group and kind, and finally the default.
//
// The default is "namespaced", and it is deliberately last: it is a guess, and
// every layer above it is a fact.
func (x scopeIndex) clusterScoped(obj *unstructured.Unstructured) bool {
	if clusterScopedKinds[obj.GetKind()] {
		return true
	}
	if cluster, known := x[resourceKey{group: apiGroup(obj.GetAPIVersion()), kind: obj.GetKind()}]; known {
		return cluster
	}
	return false
}

// apiGroup is the group half of an apiVersion. Core kinds carry a bare
// version ("v1") and therefore the empty group, exactly as a CRD's spec.group
// would be empty for them.
func apiGroup(apiVersion string) string {
	group, _, found := strings.Cut(apiVersion, "/")
	if !found {
		return ""
	}
	return group
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
func applyNamespace(objs []*unstructured.Unstructured, ns string, scopes scopeIndex) error {
	if ns == "" {
		return nil
	}
	for _, obj := range objs {
		if scopes.clusterScoped(obj) {
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
