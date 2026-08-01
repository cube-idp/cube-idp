// Package kind implements the cluster.Provisioner driver seam with kind
// (Kubernetes-in-Docker), driven as a Go library. It is the ONLY package
// allowed to import sigs.k8s.io/kind, keeping the heavy SDK out of every
// other build path.
package kind

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	v1alpha4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
	kindcluster "sigs.k8s.io/kind/pkg/cluster"
	"sigs.k8s.io/yaml"

	"github.com/cube-idp/cube-idp/internal/cluster"
)

// Provider drives kind. The kind library takes no context.Context; the
// seam's ctx parameters are accepted and unused — a documented library
// limitation, not a design choice.
type Provider struct {
	kp *kindcluster.Provider
}

// New detects the container runtime (docker/podman/nerdctl) and returns
// a kind-backed Provisioner.
func New() (*Provider, error) {
	opt, err := kindcluster.DetectNodeProvider()
	if err != nil {
		return nil, fmt.Errorf("detect container runtime for kind: %w", err)
	}
	return &Provider{kp: kindcluster.NewProvider(opt)}, nil
}

// Ensure creates the cluster if absent. Idempotency is by name only —
// an existing cluster is never diffed against the spec, so config
// changes require delete + re-init.
func (p *Provider) Ensure(ctx context.Context, s cluster.Spec) error {
	exists, err := p.Exists(ctx, s.Name)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	cfg := &v1alpha4.Cluster{}
	if s.ForProvider != nil && len(s.ForProvider.Raw) > 0 {
		if err := yaml.UnmarshalStrict(s.ForProvider.Raw, cfg); err != nil {
			return cluster.ErrInvalidForProvider(fmt.Errorf("decode forProvider as kind.x-k8s.io/v1alpha4 Cluster: %w", err))
		}
	}
	cfg.Name = s.Name

	// Point kind's own kubeconfig export at a throwaway path so it never
	// touches the user's file; the domain owns kubeconfig installation.
	tmp, err := os.MkdirTemp("", "cube-idp-kind-*")
	if err != nil {
		return cluster.ErrProvisionFailed("create", s.Name, fmt.Errorf("temp kubeconfig dir: %w", err))
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	if err := p.kp.Create(s.Name,
		kindcluster.CreateWithV1Alpha4Config(cfg),
		kindcluster.CreateWithKubeconfigPath(filepath.Join(tmp, "kubeconfig")),
	); err != nil {
		return cluster.ErrProvisionFailed("create", s.Name, err)
	}
	return nil
}

func (p *Provider) Exists(_ context.Context, name string) (bool, error) {
	names, err := p.kp.List()
	if err != nil {
		return false, cluster.ErrProvisionFailed("list", name, err)
	}
	return slices.Contains(names, name), nil
}

func (p *Provider) Delete(_ context.Context, name string) error {
	// kind's Delete is a no-op for absent clusters, matching the seam.
	if err := p.kp.Delete(name, ""); err != nil {
		return cluster.ErrProvisionFailed("delete", name, err)
	}
	return nil
}

func (p *Provider) Kubeconfig(_ context.Context, name string) ([]byte, error) {
	kc, err := p.kp.KubeConfig(name, false)
	if err != nil {
		return nil, fmt.Errorf("kind kubeconfig for %s: %w", name, err)
	}
	return []byte(kc), nil
}
