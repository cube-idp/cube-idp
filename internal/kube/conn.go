// Package kube provides Kubernetes connection primitives used across cube-idp.
package kube

import (
	"k8s.io/client-go/rest"
)

// Conn holds a fully-established connection to a Kubernetes cluster,
// suitable for passing to kubeconfig-reading and client-go operations.
type Conn struct {
	Kubeconfig []byte       // serialized kubeconfig suitable for writing to $KUBECONFIG
	Context    string       // active context name in the kubeconfig
	REST       *rest.Config // client-go REST config for direct client initialization
}
