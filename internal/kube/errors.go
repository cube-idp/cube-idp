package kube

import (
	"fmt"

	"github.com/cube-idp/cube-idp/internal/cubeerr"
)

// The kube domain owns the CUBE-KUB-* code range. Codes are declared here
// and nowhere else; the cross-domain tag registry lives in
// docs/ARCHITECTURE.md. Constructors stay unexported: no other package
// raises KUB errors.
const (
	// CodeInvalidKubeconfig reports kubeconfig bytes that do not parse.
	CodeInvalidKubeconfig cubeerr.Code = "CUBE-KUB-001"
	// CodeContextNotFound reports a context name absent from the kubeconfig.
	CodeContextNotFound cubeerr.Code = "CUBE-KUB-002"
	// CodeConstructionFailed reports a failure to build clients from the
	// selected context.
	CodeConstructionFailed cubeerr.Code = "CUBE-KUB-003"
	// CodeUnreachable reports an API server that did not answer the
	// readiness probe.
	CodeUnreachable cubeerr.Code = "CUBE-KUB-004"
)

func newInvalidKubeconfigError(cause error) error {
	return cubeerr.Wrap(CodeInvalidKubeconfig,
		"invalid kubeconfig bytes",
		"regenerate the kubeconfig; `cube-idp create` writes a valid one", cause)
}

func newContextNotFoundError(context string) error {
	return cubeerr.Wrap(CodeContextNotFound,
		fmt.Sprintf("context %q not found in kubeconfig", context),
		"run `cube-idp create` to install the cube context, or pass an existing context name", nil)
}

func newConstructionFailedError(context string, cause error) error {
	return cubeerr.Wrap(CodeConstructionFailed,
		fmt.Sprintf("cannot build Kubernetes clients for context %q", context),
		"inspect the kubeconfig entry for this context; regenerate it with `cube-idp create` if damaged", cause)
}

func newUnreachableError(host string, cause error) error {
	return cubeerr.Wrap(CodeUnreachable,
		fmt.Sprintf("Kubernetes API server %s is unreachable", host),
		"check the cluster is running (`cube-idp status`) and the network path to it", cause)
}
