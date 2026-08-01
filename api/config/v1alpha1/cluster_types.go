package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime"
)

// ClusterProvider identifies a cluster backend implementation.
type ClusterProvider string

// ClusterProviderKind provisions clusters with kind (Kubernetes-in-Docker).
const ClusterProviderKind ClusterProvider = "kind"

// ClusterSpec declares the cluster cube-idp manages.
type ClusterSpec struct {
	// Provider selects the backend. Defaults to "kind".
	Provider ClusterProvider `json:"provider,omitempty"`

	// ForProvider carries provider-specific configuration, passed through
	// opaquely at load time and strictly decoded + validated by the
	// selected provider (for kind: a kind.x-k8s.io/v1alpha4 Cluster).
	ForProvider *runtime.RawExtension `json:"forProvider,omitempty"`
}
