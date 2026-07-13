package kindp

import (
	"context"
	"fmt"
	"slices"
	"time"

	"k8s.io/client-go/tools/clientcmd"
	kindcluster "sigs.k8s.io/kind/pkg/cluster"

	"github.com/rafpe/cube-idp/internal/config"
	"github.com/rafpe/cube-idp/internal/diag"
	"github.com/rafpe/cube-idp/internal/kube"
)

// Kind implements cluster.Provider for local kind (kubernetes-in-docker)
// clusters (spec §4.1). It is a thin wrapper around sigs.k8s.io/kind: all
// config assembly happens in RenderConfig (merge.go), which stays pure and
// cluster-free.
type Kind struct {
	gw       config.GatewaySpec
	provider *kindcluster.Provider
}

// New returns a Kind provider bound to the given gateway spec. It
// autodetects the node backend (docker/podman/nerdctl, spec §4.1); if
// detection fails, the provider falls back to docker so that later calls
// (Exists/Diagnose) surface a clear CUBE-1203 rather than panicking here.
func New(gw config.GatewaySpec) *Kind {
	np, _ := kindcluster.DetectNodeProvider()
	var opts []kindcluster.ProviderOption
	if np != nil {
		opts = append(opts, np)
	}
	return &Kind{gw: gw, provider: kindcluster.NewProvider(opts...)}
}

// Ensure creates the named kind cluster if it doesn't already exist, then
// returns a connection to it.
func (k *Kind) Ensure(ctx context.Context, name string, spec config.ClusterSpec) (*kube.Conn, error) {
	exists, err := k.Exists(ctx, name)
	if err != nil {
		return nil, err
	}
	if !exists {
		cfg, err := RenderConfig(name, spec, k.gw)
		if err != nil {
			return nil, err
		}
		err = k.provider.Create(name,
			kindcluster.CreateWithRawConfig(cfg),
			kindcluster.CreateWithWaitForReady(120*time.Second),
		)
		if err != nil {
			return nil, diag.Wrap(err, diag.CodeKindCreateFailed, "kind cluster creation failed",
				"check that the container runtime is running and has free resources; `cube-idp doctor` (Phase 2) will preflight this")
		}
	}
	kc, err := k.provider.KubeConfig(name, false)
	if err != nil {
		return nil, diag.Wrap(err, diag.CodeKindKubeconfigGet, "cannot get kubeconfig from kind", "retry; if it persists, `cube-idp down` and `up` again")
	}
	restCfg, err := clientcmd.RESTConfigFromKubeConfig([]byte(kc))
	if err != nil {
		return nil, diag.Wrap(err, diag.CodeKindKubeconfigGet, "kind kubeconfig is invalid", "delete the cluster with `cube-idp down` and retry")
	}
	return &kube.Conn{Kubeconfig: []byte(kc), Context: "kind-" + name, REST: restCfg}, nil
}

// Exists reports whether a kind cluster with the given name exists.
func (k *Kind) Exists(ctx context.Context, name string) (bool, error) {
	names, err := k.provider.List()
	if err != nil {
		return false, diag.Wrap(err, diag.CodeKindCreateFailed, "cannot list kind clusters", "is the container runtime running?")
	}
	return slices.Contains(names, name), nil
}

// Delete tears down the named kind cluster.
func (k *Kind) Delete(ctx context.Context, name string) error {
	if err := k.provider.Delete(name, ""); err != nil {
		return diag.Wrap(err, diag.CodeKindDeleteFailed, fmt.Sprintf("failed to delete kind cluster %q", name), "retry, or remove the container manually")
	}
	return nil
}

// Kubeconfig returns the kubeconfig for the named kind cluster.
func (k *Kind) Kubeconfig(ctx context.Context, name string) ([]byte, error) {
	kc, err := k.provider.KubeConfig(name, false)
	if err != nil {
		return nil, diag.Wrap(err, diag.CodeKindKubeconfigGet, "cannot get kubeconfig from kind", "retry; if it persists, `cube-idp down` and `up` again")
	}
	return []byte(kc), nil
}

// Diagnose reports non-fatal findings about the kind/container runtime
// environment (Phase 2's `doctor` command surfaces these).
func (k *Kind) Diagnose(ctx context.Context, name string) []diag.Finding {
	if _, err := k.provider.List(); err != nil {
		return []diag.Finding{{Code: diag.CodeKindCreateFailed, Severity: diag.SeverityError,
			Message:     "container runtime unreachable: " + err.Error(),
			Remediation: "start Docker/Podman and retry"}}
	}
	return nil
}
