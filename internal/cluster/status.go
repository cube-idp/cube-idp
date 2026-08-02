package cluster

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"sigs.k8s.io/yaml"
)

// StatusOptions parameterizes Status, resolving the context name and
// kubeconfig target exactly like InitOptions so the report describes
// what the matching init installed.
type StatusOptions struct {
	Name        string // cluster name (the cube identity)
	ContextName string // "" → ContextName(Name)
	// KubeconfigPath, when set, is the file inspected. When empty, the
	// report reads $KUBECONFIG (first entry) or ~/.kube/config.
	KubeconfigPath string
}

// StatusReport is the read-only result of Status: whether the declared
// cluster exists and whether its cube-owned context is installed, plus
// the resolved names so callers can render them.
type StatusReport struct {
	ClusterExists    bool
	ContextInstalled bool
	ContextName      string
	KubeconfigPath   string
}

// Status reports on the cluster and its kubeconfig context without
// changing anything: seam Exists plus a parse of the target kubeconfig.
// A missing kubeconfig file means not installed — only failures to
// determine the answer (backend errors, unreadable or unparseable
// kubeconfig) are errors.
func Status(ctx context.Context, p Provisioner, opts StatusOptions) (StatusReport, error) {
	exists, err := p.Exists(ctx, opts.Name)
	if err != nil {
		return StatusReport{}, err // drivers return coded errors already
	}
	name := opts.ContextName
	if name == "" {
		name = ContextName(opts.Name)
	}
	path := opts.KubeconfigPath
	if path == "" {
		if path, err = defaultKubeconfigPath(); err != nil {
			return StatusReport{}, NewKubeconfigFailedError(err)
		}
	}
	installed, err := contextInstalled(path, name)
	if err != nil {
		return StatusReport{}, err
	}
	return StatusReport{
		ClusterExists:    exists,
		ContextInstalled: installed,
		ContextName:      name,
		KubeconfigPath:   path,
	}, nil
}

// contextInstalled reports whether the kubeconfig at path contains a
// context named name. A missing file is false, never an error.
func contextInstalled(path, name string) (bool, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, NewKubeconfigFailedError(fmt.Errorf("read kubeconfig %s: %w", path, err))
	}
	var kc kubeconfig
	if err := yaml.Unmarshal(raw, &kc); err != nil {
		return false, NewKubeconfigFailedError(fmt.Errorf("parse kubeconfig %s: %w", path, err))
	}
	for _, c := range kc.Contexts {
		if c.Name == name {
			return true, nil
		}
	}
	return false, nil
}
