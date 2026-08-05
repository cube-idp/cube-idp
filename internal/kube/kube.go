// Package kube constructs Kubernetes client access from injected
// kubeconfig bytes. It is a shared leaf: kubeconfig bytes and the context
// name are injected by the CLI/orchestrator edge — this package never
// reads files, never derives the cube context name, and never imports
// another domain. It is also the only package that turns kubeconfig bytes
// into clients (client-go construction confinement, ARCHITECTURE §8).
package kube

import (
	"context"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
)

// Client bundles the minimal client set consumers need: REST config,
// discovery, a memory-cached RESTMapper, and a dynamic client. Construct
// it with New; the zero value is not usable.
type Client struct {
	restConfig *rest.Config
	discovery  discovery.DiscoveryInterface
	mapper     meta.RESTMapper
	dynamic    dynamic.Interface
}

// New builds client access from kubeconfig bytes, selecting contextName
// (empty = the kubeconfig's current-context). Pure construction: no
// network round-trip — errors are kubeconfig, context, or construction
// problems only.
func New(kubeconfig []byte, contextName string) (*Client, error) {
	apiConfig, err := clientcmd.Load(kubeconfig)
	if err != nil {
		return nil, newInvalidKubeconfigError(err)
	}
	if contextName == "" {
		contextName = apiConfig.CurrentContext
	}
	if _, ok := apiConfig.Contexts[contextName]; !ok {
		return nil, newContextNotFoundError(contextName)
	}

	restConfig, err := clientcmd.NewNonInteractiveClientConfig(
		*apiConfig, contextName, &clientcmd.ConfigOverrides{}, nil).ClientConfig()
	if err != nil {
		return nil, newConstructionFailedError(contextName, err)
	}
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(restConfig)
	if err != nil {
		return nil, newConstructionFailedError(contextName, err)
	}
	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, newConstructionFailedError(contextName, err)
	}

	return &Client{
		restConfig: restConfig,
		discovery:  discoveryClient,
		mapper: restmapper.NewDeferredDiscoveryRESTMapper(
			memory.NewMemCacheClient(discoveryClient)),
		dynamic: dynamicClient,
	}, nil
}

// RESTConfig returns the REST configuration for the selected context.
func (c *Client) RESTConfig() *rest.Config { return c.restConfig }

// Discovery returns the discovery client for the selected context.
func (c *Client) Discovery() discovery.DiscoveryInterface { return c.discovery }

// RESTMapper returns a memory-cached deferred RESTMapper over Discovery.
func (c *Client) RESTMapper() meta.RESTMapper { return c.mapper }

// Dynamic returns the dynamic client for the selected context.
func (c *Client) Dynamic() dynamic.Interface { return c.dynamic }

// Ping reports whether the API server behind the selected context is
// reachable, via a readiness endpoint round-trip — the only network call
// in this package.
func (c *Client) Ping(ctx context.Context) error {
	err := c.discovery.RESTClient().Get().AbsPath("/readyz").Do(ctx).Error()
	if err != nil {
		return newUnreachableError(c.restConfig.Host, err)
	}
	return nil
}
