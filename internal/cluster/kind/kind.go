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
	"sync"

	v1alpha4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
	kindcluster "sigs.k8s.io/kind/pkg/cluster"
	"sigs.k8s.io/yaml"

	"github.com/cube-idp/cube-idp/internal/cluster"
)

// Provider drives kind. The kind library takes no context.Context; the
// seam's ctx parameters are accepted and unused — a documented library
// limitation, not a design choice.
type Provider struct {
	kp func() (*kindcluster.Provider, error)
}

// Signature drift must fail the build, not silently drop the optional
// capability the CLI edge type-asserts for.
var (
	_ cluster.Provisioner   = (*Provider)(nil)
	_ cluster.SpecValidator = (*Provider)(nil)
)

// New returns a kind-backed Provisioner. Container-runtime detection
// (docker/podman/nerdctl) is deferred to the first provisioning call so
// that construction — and the pure ValidateSpec capability — never needs
// a runtime; the error return is kept for future construction-time
// failures. Detection runs once: its result — including a failure — is
// cached for the Provider's lifetime.
func New() (*Provider, error) {
	return &Provider{kp: sync.OnceValues(func() (*kindcluster.Provider, error) {
		opt, err := kindcluster.DetectNodeProvider()
		if err != nil {
			return nil, fmt.Errorf("detect container runtime for kind: %w", err)
		}
		return kindcluster.NewProvider(opt), nil
	})}, nil
}

// hasForProvider reports whether the spec carries an explicit
// spec.cluster.forProvider payload. Absent — nil, or non-nil with
// zero-length Raw — is the only shape that takes the driver's
// ingress-ready default; a present-but-empty `forProvider: {}` is an
// explicit payload and is the documented minimal opt-out.
func hasForProvider(s cluster.Spec) bool {
	return s.ForProvider != nil && len(s.ForProvider.Raw) > 0
}

// decodeForProvider strictly decodes spec.cluster.forProvider as a
// kind.x-k8s.io/v1alpha4 Cluster. An absent payload decodes to the zero
// Cluster, which is what makes ValidateSpec pure: nothing to decode
// still passes.
func decodeForProvider(s cluster.Spec) (*v1alpha4.Cluster, error) {
	cfg := &v1alpha4.Cluster{}
	if hasForProvider(s) {
		if err := yaml.UnmarshalStrict(s.ForProvider.Raw, cfg); err != nil {
			return nil, cluster.NewInvalidForProviderError(fmt.Errorf("decode forProvider as kind.x-k8s.io/v1alpha4 Cluster: %w", err))
		}
	}
	return cfg, nil
}

// defaultClusterConfig is kind's documented ingress-ready shape: one
// control-plane node labeled ingress-ready=true, publishing host
// 8080/8443 to the node's 80/443 so an in-cluster gateway pinned to that
// label is reachable from the host. Host ports sit above the privileged
// range, so URLs carry the port.
func defaultClusterConfig() *v1alpha4.Cluster {
	return &v1alpha4.Cluster{
		Nodes: []v1alpha4.Node{{
			Role:   v1alpha4.ControlPlaneRole,
			Labels: map[string]string{"ingress-ready": "true"},
			ExtraPortMappings: []v1alpha4.PortMapping{
				{ContainerPort: 80, HostPort: 8080, Protocol: v1alpha4.PortMappingProtocolTCP},
				{ContainerPort: 443, HostPort: 8443, Protocol: v1alpha4.PortMappingProtocolTCP},
			},
		}},
	}
}

// clusterConfig picks the create-time kind config for a spec: an
// explicit forProvider payload wins wholesale — never merged with the
// default — and no explicit payload takes the ingress-ready default.
func clusterConfig(s cluster.Spec) (*v1alpha4.Cluster, error) {
	if !hasForProvider(s) {
		return defaultClusterConfig(), nil
	}
	return decodeForProvider(s)
}

// ValidateSpec implements the optional cluster.SpecValidator capability:
// pure payload validation, no container runtime touched.
func (p *Provider) ValidateSpec(s cluster.Spec) error {
	_, err := decodeForProvider(s)
	return err
}

// Ensure creates the cluster if absent. Idempotency is by name only —
// an existing cluster is never diffed against the spec, so config
// changes require delete + re-init. The ingress-ready default therefore
// shapes create only; it never touches the exists-then-no-op path.
func (p *Provider) Ensure(ctx context.Context, s cluster.Spec) error {
	cfg, err := clusterConfig(s)
	if err != nil {
		return err
	}
	exists, err := p.Exists(ctx, s.Name)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	cfg.Name = s.Name

	kp, err := p.kp()
	if err != nil {
		return cluster.NewProvisionFailedError("create", s.Name, err)
	}
	// Point kind's own kubeconfig export at a throwaway path so it never
	// touches the user's file; the domain owns kubeconfig installation.
	tmp, err := os.MkdirTemp("", "cube-idp-kind-*")
	if err != nil {
		return cluster.NewProvisionFailedError("create", s.Name, fmt.Errorf("temp kubeconfig dir: %w", err))
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	if err := kp.Create(s.Name,
		kindcluster.CreateWithV1Alpha4Config(cfg),
		kindcluster.CreateWithKubeconfigPath(filepath.Join(tmp, "kubeconfig")),
	); err != nil {
		return cluster.NewProvisionFailedError("create", s.Name, err)
	}
	return nil
}

func (p *Provider) Exists(_ context.Context, name string) (bool, error) {
	kp, err := p.kp()
	if err != nil {
		return false, cluster.NewProvisionFailedError("list", name, err)
	}
	names, err := kp.List()
	if err != nil {
		return false, cluster.NewProvisionFailedError("list", name, err)
	}
	return slices.Contains(names, name), nil
}

func (p *Provider) Delete(_ context.Context, name string) error {
	kp, err := p.kp()
	if err != nil {
		return cluster.NewProvisionFailedError("delete", name, err)
	}
	// kind's Delete is a no-op for absent clusters, matching the seam.
	if err := kp.Delete(name, ""); err != nil {
		return cluster.NewProvisionFailedError("delete", name, err)
	}
	return nil
}

func (p *Provider) Kubeconfig(_ context.Context, name string) ([]byte, error) {
	kp, err := p.kp()
	if err != nil {
		return nil, fmt.Errorf("kind kubeconfig for %s: %w", name, err)
	}
	kc, err := kp.KubeConfig(name, false)
	if err != nil {
		return nil, fmt.Errorf("kind kubeconfig for %s: %w", name, err)
	}
	return []byte(kc), nil
}
