package cluster

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// InitOptions parameterizes Init. Namespace deliberately has no config
// counterpart: a cube spans many namespaces, so a single spec-level
// default would misdirect resources. Callers that know the target
// namespace pass it here; empty omits it from the generated context.
type InitOptions struct {
	Spec        Spec
	ContextName string // "" → ContextName(Spec.Name)
	Namespace   string // "" → omitted from the generated context
	// KubeconfigPath, when set, is written as a standalone file — no merge
	// into the default location. When empty, the rebranded config is
	// merged into $KUBECONFIG (first entry) or ~/.kube/config.
	KubeconfigPath string
}

// Init ensures the cluster exists and installs its cube-branded
// kubeconfig context: Ensure → Kubeconfig → Rebrand → merge-or-write.
func Init(ctx context.Context, p Provisioner, opts InitOptions) error {
	if err := p.Ensure(ctx, opts.Spec); err != nil {
		return err // drivers return coded errors already
	}
	raw, err := p.Kubeconfig(ctx, opts.Spec.Name)
	if err != nil {
		return ErrKubeconfigFailed(fmt.Errorf("fetch kubeconfig for %s: %w", opts.Spec.Name, err))
	}
	name := opts.ContextName
	if name == "" {
		name = ContextName(opts.Spec.Name)
	}
	branded, err := Rebrand(raw, name, opts.Namespace)
	if err != nil {
		return ErrKubeconfigFailed(fmt.Errorf("rebrand kubeconfig as %s: %w", name, err))
	}
	if opts.KubeconfigPath != "" {
		return writeKubeconfig(opts.KubeconfigPath, branded)
	}
	return mergeIntoDefault(branded)
}

func mergeIntoDefault(branded []byte) error {
	target, err := defaultKubeconfigPath()
	if err != nil {
		return ErrKubeconfigFailed(err)
	}
	existing, err := os.ReadFile(target)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return ErrKubeconfigFailed(fmt.Errorf("read kubeconfig %s: %w", target, err))
	}
	merged, err := Merge(existing, branded)
	if err != nil {
		return ErrKubeconfigFailed(fmt.Errorf("merge into %s: %w", target, err))
	}
	return writeKubeconfig(target, merged)
}

// writeKubeconfig writes atomically: temp file in the target directory,
// then rename — a crash mid-write can never truncate the user's file.
func writeKubeconfig(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ErrKubeconfigFailed(fmt.Errorf("create kubeconfig dir for %s: %w", path, err))
	}
	tmp, err := os.CreateTemp(dir, ".kubeconfig-*") // 0600 by default
	if err != nil {
		return ErrKubeconfigFailed(fmt.Errorf("temp file for %s: %w", path, err))
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // no-op once renamed
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return ErrKubeconfigFailed(fmt.Errorf("write kubeconfig %s: %w", path, err))
	}
	if err := tmp.Close(); err != nil {
		return ErrKubeconfigFailed(fmt.Errorf("write kubeconfig %s: %w", path, err))
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return ErrKubeconfigFailed(fmt.Errorf("write kubeconfig %s: %w", path, err))
	}
	return nil
}

// defaultKubeconfigPath mirrors kubectl's resolution: first KUBECONFIG
// list entry, else ~/.kube/config. A home-less environment is an error,
// never a silent CWD-relative fallback.
func defaultKubeconfigPath() (string, error) {
	if env := os.Getenv("KUBECONFIG"); env != "" {
		for _, p := range filepath.SplitList(env) {
			if p != "" {
				return p, nil
			}
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("determine default kubeconfig location: %w", err)
	}
	return filepath.Join(home, ".kube", "config"), nil
}
