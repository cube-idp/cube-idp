package cluster

import (
	"fmt"

	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

// The cluster domain owns the CUBE-CLU-* code range. Codes are declared
// here and nowhere else; the cross-domain tag registry lives in
// docs/ARCHITECTURE.md. Constructors are exported because driver
// subpackages and the CLI edge raise these errors.
const (
	CodeNoClusterConfigured cubeerr.Code = "CUBE-CLU-001"
	CodeUnsupportedProvider cubeerr.Code = "CUBE-CLU-002"
	CodeInvalidForProvider  cubeerr.Code = "CUBE-CLU-003"
	CodeProvisionFailed     cubeerr.Code = "CUBE-CLU-004"
	CodeKubeconfigFailed    cubeerr.Code = "CUBE-CLU-005"
)

// NewNoClusterConfiguredError reports a config without spec.cluster where an
// operation requires a managed cluster.
func NewNoClusterConfiguredError() error {
	return cubeerr.Wrap(CodeNoClusterConfigured,
		"no cluster configured",
		"add spec.cluster to the config to let cube-idp manage a cluster", nil)
}

// NewUnsupportedProviderError reports a spec.cluster.provider no registered
// driver implements.
func NewUnsupportedProviderError(provider string) error {
	return cubeerr.Wrap(CodeUnsupportedProvider,
		fmt.Sprintf("no driver for provider %q", provider),
		"use a supported spec.cluster.provider (kind)", nil)
}

// NewInvalidForProviderError reports a spec.cluster.forProvider payload the
// selected provider cannot decode.
func NewInvalidForProviderError(cause error) error {
	return cubeerr.Wrap(CodeInvalidForProvider,
		"invalid spec.cluster.forProvider payload",
		"fix the provider config fields listed above (kind: kind.x-k8s.io/v1alpha4 Cluster)", cause)
}

// NewProvisionFailedError reports a provisioning action (create/list/delete)
// that failed against the backend.
func NewProvisionFailedError(action, name string, cause error) error {
	return cubeerr.Wrap(CodeProvisionFailed,
		fmt.Sprintf("%s cluster %q failed", action, name),
		"check that the container runtime (Docker/Podman) is running; see cause above", cause)
}

// NewKubeconfigFailedError reports a failure generating, merging, writing,
// or cleaning up the cube-branded kubeconfig.
func NewKubeconfigFailedError(cause error) error {
	return cubeerr.Wrap(CodeKubeconfigFailed,
		"kubeconfig update failed",
		"see cause above; check permissions on the kubeconfig target, or pass --kubeconfig <path> to write elsewhere", cause)
}
