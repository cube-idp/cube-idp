package cluster

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// DeleteOptions parameterizes Delete, mirroring InitOptions: the same
// context-name derivation and the same kubeconfig target resolution, so
// a delete cleans up exactly what the matching init installed.
type DeleteOptions struct {
	Name        string // cluster name (the cube identity)
	ContextName string // "" → ContextName(Name)
	// KubeconfigPath, when set, is the file cleaned up. When empty,
	// cleanup targets $KUBECONFIG (first entry) or ~/.kube/config.
	KubeconfigPath string
}

// Delete removes the cluster and its cube-owned kubeconfig context:
// seam Delete (absent cluster is a no-op) → Remove → atomic write, the
// reverse of Init. Files are never unlinked, only rewritten without the
// cube-owned entries; an untouched file is not rewritten at all. The
// bool reports whether the kubeconfig was modified.
func Delete(ctx context.Context, p Provisioner, opts DeleteOptions) (bool, error) {
	if err := p.Delete(ctx, opts.Name); err != nil {
		return false, err // drivers return coded errors already
	}
	name := opts.ContextName
	if name == "" {
		name = ContextName(opts.Name)
	}
	return removeFromKubeconfig(opts.KubeconfigPath, name)
}

// removeFromKubeconfig strips contextName from the kubeconfig at path
// ("" → default resolution). A missing file means nothing is installed —
// a clean no-op, never an error.
func removeFromKubeconfig(path, contextName string) (bool, error) {
	if path == "" {
		var err error
		if path, err = defaultKubeconfigPath(); err != nil {
			return false, NewKubeconfigFailedError(err)
		}
	}
	existing, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, NewKubeconfigFailedError(fmt.Errorf("read kubeconfig %s: %w", path, err))
	}
	cleaned, changed, err := Remove(existing, contextName)
	if err != nil {
		return false, NewKubeconfigFailedError(fmt.Errorf("remove context %s from %s: %w", contextName, path, err))
	}
	if !changed {
		return false, nil
	}
	return true, writeKubeconfig(path, cleaned)
}
