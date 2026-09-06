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
// bool reports whether the kubeconfig was modified — never on a failed
// write, which leaves the target exactly as it was.
//
// Once the seam Delete has returned SUCCESSFULLY, the cluster is gone
// whatever happens next, so — as in Init — the coded error is built here,
// at the one seam that knows it, and never inside the cleanup helpers. A
// seam error establishes nothing and propagates untouched above.
func Delete(ctx context.Context, p Provisioner, opts DeleteOptions) (bool, error) {
	if err := p.Delete(ctx, opts.Name); err != nil {
		return false, err // drivers return coded errors already
	}
	name := opts.ContextName
	if name == "" {
		name = ContextName(opts.Name)
	}
	changed, err := removeFromKubeconfig(opts.KubeconfigPath, name)
	if err != nil {
		return changed, newKubeconfigFailedAfterDeleteError(err)
	}
	return changed, nil
}

// removeFromKubeconfig strips contextName from the kubeconfig at path
// ("" → default resolution). A missing file means nothing is installed —
// a clean no-op, never an error. Errors are plain: Delete owns the coded
// wrapping, because only it knows the cluster has already gone.
func removeFromKubeconfig(path, contextName string) (bool, error) {
	if path == "" {
		var err error
		if path, err = defaultKubeconfigPath(); err != nil {
			return false, err
		}
	}
	existing, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read kubeconfig %s: %w", path, err)
	}
	cleaned, changed, err := Remove(existing, contextName)
	if err != nil {
		return false, fmt.Errorf("remove context %s from %s: %w", contextName, path, err)
	}
	if !changed {
		return false, nil
	}
	// The write is atomic, so a failure leaves the file untouched: false
	// is the truthful answer to "was the kubeconfig modified", and true
	// would report a context removal that did not happen.
	if err := writeKubeconfig(path, cleaned); err != nil {
		return false, err
	}
	return true, nil
}
