package cluster

import (
	"errors"
	"fmt"
	"io/fs"

	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

// The cluster domain owns the CUBE-CLU-* code range. Codes are declared
// here and nowhere else; the cross-domain tag registry lives in
// docs/ARCHITECTURE.md. Constructors are exported when driver subpackages
// or the CLI edge raise them, and stay unexported otherwise.
const (
	CodeNoClusterConfigured  cubeerr.Code = "CUBE-CLU-001"
	CodeUnsupportedProvider  cubeerr.Code = "CUBE-CLU-002"
	CodeInvalidForProvider   cubeerr.Code = "CUBE-CLU-003"
	CodeProvisionFailed      cubeerr.Code = "CUBE-CLU-004"
	CodeKubeconfigFailed     cubeerr.Code = "CUBE-CLU-005"
	CodeKubeconfigReadFailed cubeerr.Code = "CUBE-CLU-006"
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

// actionCreate is the provisioning action whose failures can stem from a
// host-port collision, so it carries wider remediation than the others.
// Unexported deliberately: the drivers that pass this action still spell
// it as a literal, so an exported constant would have no consumer outside
// this file. Wiring the kind driver to it is a separate, mechanical change.
const actionCreate = "create"

// NewProvisionFailedError reports a provisioning action (create/list/delete)
// that failed against the backend.
func NewProvisionFailedError(action, name string, cause error) error {
	return cubeerr.Wrap(CodeProvisionFailed,
		fmt.Sprintf("%s cluster %q failed", action, name),
		provisionRemediation(action), cause)
}

// provisionRemediation picks the guidance for a failed provisioning action.
// A create can fail because the container runtime is absent OR because the
// host ports the cluster publishes are already bound — two default cubes
// collide there, and an explicit spec.cluster.forProvider is the documented
// escape (docs/domains/cluster.md). Naming only the runtime sends an
// operator whose Docker is healthy in the wrong direction. The port numbers
// are the driver contract's and are not duplicated in generic guidance:
// this helper receives an action, not resolved port mappings, and hardcoded
// defaults would misdirect anyone who supplied their own forProvider.
// list and delete bind no ports, so they keep the narrower guidance.
func provisionRemediation(action string) string {
	const runtime = "check that the container runtime (Docker/Podman) is running"
	if action != actionCreate {
		return runtime + "; see cause above"
	}
	return runtime + ", and that the host ports the cluster publishes are free — " +
		"a second default cube collides on them; set spec.cluster.forProvider to " +
		"choose different ports. See cause above"
}

// NewKubeconfigFailedError reports a failure generating, merging, writing,
// or cleaning up the cube-branded kubeconfig — exactly the write-side set
// docs/domains/cluster.md's error table names, and nothing wider. A failure
// on a read-only operation is CUBE-CLU-006, not this.
func NewKubeconfigFailedError(cause error) error {
	return cubeerr.Wrap(CodeKubeconfigFailed,
		"kubeconfig update failed",
		"see cause above; check permissions on the kubeconfig target, or pass --kubeconfig <path> to write elsewhere", cause)
}

// NewKubeconfigReadError reports a failure to read or parse a kubeconfig
// during a read-only operation. It is the read-side counterpart to
// NewKubeconfigFailedError: `status` and the bootstrap edge's pre-apply
// read change nothing, so "update failed" and "write elsewhere" are the
// wrong words for them (docs/domains/cluster.md, README's read-only
// statement for `status`). The remediation is cause-specific because the
// two ways a read fails want opposite advice: an absent file is a missing
// prerequisite, anything else is a problem with the file that is there.
func NewKubeconfigReadError(cause error) error {
	return cubeerr.Wrap(CodeKubeconfigReadFailed,
		"kubeconfig read failed",
		kubeconfigReadRemediation(cause), cause)
}

func kubeconfigReadRemediation(cause error) string {
	if errors.Is(cause, fs.ErrNotExist) {
		return "no kubeconfig at that location yet — run `cube-idp create` to provision the cluster " +
			"and install its context, or pass --kubeconfig <path> to read a different file"
	}
	return "check that the target is readable and is a valid kubeconfig, or pass --kubeconfig <path> " +
		"to read a different file; see cause above"
}

// newKubeconfigLocationError reports a read-only operation that could not
// even determine which kubeconfig to read. It shares CUBE-CLU-006 — the
// failure is the same class, an unsatisfiable read — but neither of
// NewKubeconfigReadError's remediations applies: there is no file to
// check the permissions of, and `create` would fail the same way.
// Unexported: only this domain resolves the default location.
func newKubeconfigLocationError(cause error) error {
	return cubeerr.Wrap(CodeKubeconfigReadFailed,
		"cannot determine which kubeconfig to read",
		"set KUBECONFIG, or pass --kubeconfig <path> to name the file to read", cause)
}
