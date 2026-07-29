package cluster

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// InitOptions parameterizes Init. Namespace is a method-level option by
// design (no spec surface): today the CLI passes it empty; future flows
// set it programmatically (design §1 decision 6).
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
	target := defaultKubeconfigPath()
	existing, err := os.ReadFile(target)
	if err != nil && !os.IsNotExist(err) {
		return ErrKubeconfigFailed(fmt.Errorf("read kubeconfig %s: %w", target, err))
	}
	merged, err := Merge(existing, branded)
	if err != nil {
		return ErrKubeconfigFailed(fmt.Errorf("merge into %s: %w", target, err))
	}
	return writeKubeconfig(target, merged)
}

func writeKubeconfig(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return ErrKubeconfigFailed(fmt.Errorf("create kubeconfig dir for %s: %w", path, err))
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return ErrKubeconfigFailed(fmt.Errorf("write kubeconfig %s: %w", path, err))
	}
	return nil
}

// defaultKubeconfigPath mirrors kubectl's resolution: first KUBECONFIG
// list entry, else ~/.kube/config.
func defaultKubeconfigPath() string {
	if env := os.Getenv("KUBECONFIG"); env != "" {
		for _, p := range filepath.SplitList(env) {
			if p != "" {
				return p
			}
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".kube/config"
	}
	return filepath.Join(home, ".kube", "config")
}
