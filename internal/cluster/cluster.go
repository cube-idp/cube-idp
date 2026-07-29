// Package cluster is the cluster-provisioning domain: the Provisioner
// driver seam, its conformance suite, the kubeconfig machinery that brands
// provider-native kubeconfigs as cube-owned contexts, and the Init
// operation the CLI calls. Implementations live in subpackages (kind);
// driver selection happens at the CLI edge, never here.
package cluster

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
)

// Spec is the provider-neutral input to a Provisioner.
type Spec struct {
	Name        string                // cluster name (the cube identity)
	ForProvider *runtime.RawExtension // provider-specific config, opaque here
}

// Provisioner is the driver seam for cluster backends (design §4).
// Implementations must satisfy RunClusterConformance.
type Provisioner interface {
	// Ensure creates the cluster if absent; no-op if it exists.
	// Idempotent by name: it does not diff a live cluster against Spec.
	Ensure(ctx context.Context, s Spec) error
	Exists(ctx context.Context, name string) (bool, error)
	// Delete removes the cluster; deleting an absent cluster is a no-op.
	Delete(ctx context.Context, name string) error
	// Kubeconfig returns the raw admin kubeconfig with provider-native
	// entry names; rebranding to cube-owned names is the domain's job.
	Kubeconfig(ctx context.Context, name string) ([]byte, error)
}
