// authclient.go builds the one oras auth client every OCI pull and push
// path shares. It lives in internal/pack (not internal/oci) because both
// internal/oci and internal/bundle already import this package, while the
// reverse import would cycle.
package pack

import (
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/credentials"
	"oras.land/oras-go/v2/registry/remote/retry"
)

// RegistryClient returns an oras auth client backed by the ambient docker
// credential chain (docker login / credsStore helpers / GITHUB_TOKEN via
// docker/login-action in CI), falling back to anonymous for hosts with no
// stored credentials. Pull paths (pullOCI, ResolveRemote, bundle's
// pullImageTar) and push paths (PushPackDir) all build their client here so
// private registries — e.g. private GHCR pack namespaces — behave
// identically everywhere.
func RegistryClient() (*auth.Client, error) {
	credStore, err := credentials.NewStoreFromDocker(credentials.StoreOptions{})
	if err != nil {
		return nil, err
	}
	return &auth.Client{
		Client:     retry.DefaultClient,
		Cache:      auth.NewCache(),
		Credential: credentials.Credential(credStore),
	}, nil
}
