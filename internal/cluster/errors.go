package cluster

import (
	"fmt"

	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

// The cluster domain owns the CUBE-CLU-* code range (design §6); the
// human-readable registry row lives in the design docs.
const (
	CodeNoClusterConfigured cubeerr.Code = "CUBE-CLU-001"
	CodeUnsupportedProvider cubeerr.Code = "CUBE-CLU-002"
	CodeInvalidForProvider  cubeerr.Code = "CUBE-CLU-003"
	CodeProvisionFailed     cubeerr.Code = "CUBE-CLU-004"
	CodeKubeconfigFailed    cubeerr.Code = "CUBE-CLU-005"
)

func ErrNoClusterConfigured() error {
	return cubeerr.Wrap(CodeNoClusterConfigured,
		"no cluster configured",
		"add spec.cluster to the config to let cube-idp manage a cluster", nil)
}

func ErrUnsupportedProvider(provider string) error {
	return cubeerr.Wrap(CodeUnsupportedProvider,
		fmt.Sprintf("no driver for provider %q", provider),
		"use a supported spec.cluster.provider (kind)", nil)
}

func ErrInvalidForProvider(cause error) error {
	return cubeerr.Wrap(CodeInvalidForProvider,
		"invalid spec.cluster.forProvider payload",
		"fix the provider config fields listed above (kind: kind.x-k8s.io/v1alpha4 Cluster)", cause)
}

func ErrProvisionFailed(action, name string, cause error) error {
	return cubeerr.Wrap(CodeProvisionFailed,
		fmt.Sprintf("%s cluster %q failed", action, name),
		"check that the container runtime (Docker/Podman) is running; see cause above", cause)
}

func ErrKubeconfigFailed(cause error) error {
	return cubeerr.Wrap(CodeKubeconfigFailed,
		"kubeconfig generation failed",
		"see cause above; check file permissions on the kubeconfig target", cause)
}
